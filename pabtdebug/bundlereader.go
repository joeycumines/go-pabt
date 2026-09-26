/*
   Copyright 2021 Joseph Cumines

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pabtdebug

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"slices"
)

// stagedFacts is what reading a whole bundle established.
type stagedFacts struct {
	header  BundleHeader
	digest  string
	records uint64
	trees   uint64
	deltas  uint64
}

// bundleReader walks a bundle's frames from a reader.
//
// One implementation serves both halves of an import: staging streams a bundle in
// and writes what it accepted, and activation streams the staged file back out. A
// format with more than one kind of length-prefixed section needs a terminator and
// exactly one walk of it, because a second walk is a second chance to disagree with
// the first about where the frames end.
//
// Every frame is checked as it is read: the bytes against the digest its header
// names, the object against the digest inside it, and a record against the sequence
// it claims and the record before it. The accepted bytes are digested as they go,
// so a bundle is verified in the one pass that copies it and a rejected one leaves
// a file that is about to be removed rather than one that was kept.
type bundleReader struct {
	source *bufio.Reader
	dst    io.Writer
	limits ImportLimits
	// bounds is what the store admits for each kind of frame, so a bundle from a
	// generously configured store cannot put an object in a stricter one.
	bounds  map[BundleEntryKind]uint64
	header  BundleHeader
	raw     []byte
	framed  *streamDigest
	frames  uint64
	counts  map[BundleEntryKind]uint64
	records uint64
	// committed counts the records that committed a tree, which is how many tree
	// frames the bundle has to carry.
	committed uint64
	trees     uint64
	deltas    uint64
	// previous is the chain link the next record has to name.
	previous string
	finished bool
}

// newBundleReader reads the magic and the header and leaves the reader positioned
// at the first frame. Anything accepted is written to dst, which is nil when the
// bytes are already on disk.
func newBundleReader(
	source io.Reader,
	dst io.Writer,
	limits ImportLimits,
	bounds map[BundleEntryKind]uint64,
) (*bundleReader, error) {
	reader := &bundleReader{
		dst:      dst,
		limits:   limits,
		bounds:   bounds,
		framed:   newStreamDigest(DomainBundle + "/content"),
		counts:   map[BundleEntryKind]uint64{},
		previous: zeroDigest,
	}
	// The bound is on the stream rather than on a tally, so the staged file cannot
	// exceed it by however the caller chose to send the bytes.
	reader.source = bufio.NewReader(io.LimitReader(source, int64(limits.MaxBytes)))
	var kept bytes.Buffer
	if _, err := io.CopyN(&kept, reader.source, int64(len(bundleMagic))+4); err != nil {
		return nil, fmt.Errorf("%w: the bundle is shorter than its header length: %v", ErrBundleInvalid, err)
	}
	if !bytes.Equal(kept.Bytes()[:len(bundleMagic)], []byte(bundleMagic)) {
		return nil, fmt.Errorf("%w: the bundle does not begin with its magic", ErrBundleInvalid)
	}
	headerLen := binary.BigEndian.Uint32(kept.Bytes()[len(bundleMagic):])
	if headerLen == 0 || headerLen > maxBundleHeaderBytes {
		return nil, fmt.Errorf("%w: the bundle header length is %d", ErrBundleInvalid, headerLen)
	}
	if err := reader.stage(kept.Bytes()); err != nil {
		return nil, err
	}
	headerBytes, err := readExactly(reader.source, int64(headerLen), limits.MaxBytes)
	if err != nil {
		return nil, err
	}
	if err := reader.stage(headerBytes); err != nil {
		return nil, err
	}
	if err := CanonicalDecode(headerBytes, &reader.header); err != nil {
		return nil, fmt.Errorf("%w: the bundle header: %v", ErrBundleInvalid, err)
	}
	if reader.header.Format != BundleFormatName || reader.header.Version != BundleFormatVersion {
		return nil, fmt.Errorf("%w: the bundle is %s version %d",
			ErrBundleInvalid, reader.header.Format, reader.header.Version)
	}
	reader.raw = headerBytes
	return reader, nil
}

// next reads one frame and checks it, or reports that the frames have ended.
//
// more is false once the terminator has been consumed, which is the only way out
// of the walk short of an error.
func (b *bundleReader) next(ctx context.Context) (bundleFrame, bool, error) {
	var out bundleFrame
	// The frames end at an explicit terminator, so the reader has to look ahead
	// rather than commit to a length. A buffered peek is what makes that possible
	// without reading a section twice or guessing.
	marker, err := b.source.Peek(len(bundleTrailerMagic))
	switch {
	case err == nil && bytes.Equal(marker, []byte(bundleTrailerMagic)):
		// The terminator is consumed and written: whatever dst holds has to be the
		// whole bundle, or the walk that follows would find frames with no end.
		if _, err := b.source.Discard(len(bundleTrailerMagic)); err != nil {
			return out, false, fmt.Errorf("pabtdebug: read a bundle: %w", err)
		}
		return out, false, b.stage(marker)
	case errors.Is(err, io.EOF), errors.Is(err, bufio.ErrBufferFull):
		return out, false, fmt.Errorf("%w: the bundle ends without a trailer", ErrBundleInvalid)
	case err != nil:
		return out, false, fmt.Errorf("pabtdebug: read a bundle: %w", err)
	}
	if b.frames >= uint64(b.limits.MaxObjects) {
		return out, false, fmt.Errorf("%w: the bundle has more than %d frames",
			ErrBundleInvalid, b.limits.MaxObjects)
	}
	lengthBytes, err := readExactly(b.source, 4, b.limits.MaxBytes)
	if err != nil {
		return out, false, err
	}
	frameHeaderLen := binary.BigEndian.Uint32(lengthBytes)
	if frameHeaderLen == 0 || frameHeaderLen > maxBundleHeaderBytes {
		return out, false, fmt.Errorf("%w: a frame header length is %d", ErrBundleInvalid, frameHeaderLen)
	}
	frameHeader, err := readExactly(b.source, int64(frameHeaderLen), b.limits.MaxBytes)
	if err != nil {
		return out, false, err
	}
	var header struct {
		Kind   BundleEntryKind `json:"kind"`
		Run    string          `json:"run"`
		Length uint64          `json:"length"`
		SHA256 string          `json:"sha256"`
	}
	if err := CanonicalDecode(frameHeader, &header); err != nil {
		return out, false, fmt.Errorf("%w: a frame header: %v", ErrBundleInvalid, err)
	}
	// The payload is bound by what this store admits for that kind, not only by the
	// bundle's own size, so a representation a stricter store would refuse cannot
	// arrive by way of a bundle exported by a looser one.
	admitted, known := b.bounds[header.Kind]
	if !known {
		return out, false, fmt.Errorf("%w: a frame claims to be a %s, which this format does not carry",
			ErrBundleInvalid, header.Kind)
	}
	if header.Length == 0 || header.Length > admitted {
		return out, false, fmt.Errorf("%w: a %s frame payload length is %d, the store admits %d",
			ErrBundleInvalid, header.Kind, header.Length, admitted)
	}
	payload, err := readExactly(b.source, int64(header.Length), b.limits.MaxBytes)
	if err != nil {
		return out, false, err
	}
	if err := verifyFrame(header.Kind, header.SHA256, payload); err != nil {
		return out, false, err
	}
	// A tree belongs to a record the exporter wrote before it, so one that arrives
	// before any record has committed a tree is out of order. Catching it here means
	// a ticket is never issued for a bundle the activation would have to refuse.
	if header.Kind == BundleEntryTree && b.trees >= b.committed {
		return out, false, fmt.Errorf("%w: a tree arrives before the record that committed one",
			ErrBundleInvalid)
	}
	switch header.Kind {
	case BundleEntryRecord:
		var record StoredRecord
		if err := CanonicalDecode(payload, &record); err != nil {
			return out, false, fmt.Errorf("%w: a record frame: %v", ErrBundleInvalid, err)
		}
		if err := verifyRecord(record, b.records+1, b.previous); err != nil {
			return out, false, err
		}
		if record.RunKey != runKeyOf(b.header) {
			return out, false, fmt.Errorf("%w: a record names run %s, the header names %s",
				ErrBundleInvalid, record.RunKey.RunID, b.header.SourceRunID)
		}
		if record.Tree != nil {
			b.committed++
		}
		b.previous = record.RecordSHA256
		b.records++
	case BundleEntryTree:
		b.trees++
	case BundleEntryDelta:
		// A delta describes the record that came before it: the exporter writes a
		// record, its tree, and then the delta between them. A delta whose target is
		// anything else was moved, which the contract calls a reorder, and a moved
		// delta is refused here rather than dropped as one that cannot be applied.
		var delta ExactTreeDelta
		if err := CanonicalDecode(payload, &delta); err != nil {
			return out, false, fmt.Errorf("%w: a delta frame: %v", ErrBundleInvalid, err)
		}
		if delta.TargetSequence != b.records {
			return out, false, fmt.Errorf("%w: a delta describes sequence %d and arrived after %d records",
				ErrBundleInvalid, delta.TargetSequence, b.records)
		}
		if delta.BaseSequence >= delta.TargetSequence {
			return out, false, fmt.Errorf("%w: a delta has base sequence %d at or after its target %d",
				ErrBundleInvalid, delta.BaseSequence, delta.TargetSequence)
		}
		b.deltas++
	}
	for _, section := range [][]byte{lengthBytes, frameHeader, payload} {
		if err := b.stage(section); err != nil {
			return out, false, err
		}
		b.framed.add(section)
	}
	b.counts[header.Kind]++
	b.frames++
	if err := ctx.Err(); err != nil {
		return out, false, err
	}
	return bundleFrame{Kind: header.Kind, Payload: payload}, true, nil
}

// finish reads the trailer, checks it against everything the frames established,
// and returns the whole bundle's digest.
func (b *bundleReader) finish() (stagedFacts, error) {
	var out stagedFacts
	if b.finished {
		return out, fmt.Errorf("%w: the bundle was already read", ErrBundleInvalid)
	}
	b.finished = true
	trailerLenBytes, err := readExactly(b.source, 4, b.limits.MaxBytes)
	if err != nil {
		return out, err
	}
	if err := b.stage(trailerLenBytes); err != nil {
		return out, err
	}
	trailerLen := binary.BigEndian.Uint32(trailerLenBytes)
	if trailerLen == 0 || trailerLen > maxBundleTrailerBytes {
		return out, fmt.Errorf("%w: the trailer length is %d", ErrBundleInvalid, trailerLen)
	}
	trailerBytes, err := readExactly(b.source, int64(trailerLen), b.limits.MaxBytes)
	if err != nil {
		return out, err
	}
	if err := b.stage(trailerBytes); err != nil {
		return out, err
	}
	var trailer BundleTrailer
	if err := CanonicalDecode(trailerBytes, &trailer); err != nil {
		return out, fmt.Errorf("%w: the trailer: %v", ErrBundleInvalid, err)
	}
	headerDigest, err := HashBundleHeader(b.header)
	if err != nil {
		return out, err
	}
	if trailer.HeaderSHA256 != headerDigest {
		return out, fmt.Errorf("%w: the trailer names header digest %q, the header is %q",
			ErrBundleInvalid, trailer.HeaderSHA256, headerDigest)
	}
	if trailer.FrameCount != b.frames {
		return out, fmt.Errorf("%w: the trailer counts %d frames, the bundle has %d",
			ErrBundleInvalid, trailer.FrameCount, b.frames)
	}
	if trailer.ContentSHA256 != b.framed.sum() {
		return out, fmt.Errorf("%w: the trailer's content digest does not match the frames",
			ErrBundleInvalid)
	}
	// A frame kind the header does not declare is a header that does not describe this
	// bundle. A declared kind the frames do not carry is not: EntryKinds is the order
	// the format writes, not an inventory of one bundle, and a run with no deltas
	// declares the delta kind and carries none. The trailer's counts are the inventory,
	// and they are compared above.
	for kind, count := range b.counts {
		if !slices.Contains(b.header.EntryKinds, kind) {
			return out, fmt.Errorf("%w: the bundle carries %d %s frames the header does not declare",
				ErrBundleInvalid, count, kind)
		}
	}
	// Every count the trailer claims is compared, not only the records, so an edited
	// trailer cannot describe a bundle the frames do not support.
	for kind, claimed := range trailer.Counts {
		if b.counts[kind] != claimed {
			return out, fmt.Errorf("%w: the trailer counts %d %s frames, the bundle has %d",
				ErrBundleInvalid, claimed, kind, b.counts[kind])
		}
	}
	if b.records == 0 {
		return out, fmt.Errorf("%w: the bundle carries no records", ErrBundleInvalid)
	}
	// A record that committed a tree has to be accompanied by one. Deleting a tree
	// frame leaves every surviving digest consistent, because nothing in the file
	// records that it was there, and the pair is what a reader is served.
	if b.committed != b.trees {
		return out, fmt.Errorf("%w: the bundle carries %d trees for %d records that committed one",
			ErrBundleInvalid, b.trees, b.committed)
	}
	// The header's range is a claim about the frames, so it is checked against them.
	// Without this a bundle missing a tree or a delta frame describes the same
	// records and passes every other check.
	if b.header.FirstSequence != 1 || b.header.ThroughSequence != b.records {
		return out, fmt.Errorf("%w: the header covers sequences %d to %d, the bundle has %d records",
			ErrBundleInvalid, b.header.FirstSequence, b.header.ThroughSequence, b.records)
	}
	// Nothing may follow the trailer. A file with bytes after it is not the file that
	// was staged, and the digest is only over the three sections, so without this the
	// tail would be neither bounded nor compared.
	if _, err := b.source.Peek(1); !errors.Is(err, io.EOF) {
		return out, fmt.Errorf("%w: the bundle has bytes after its trailer", ErrBundleInvalid)
	}
	digest, err := bundleDigestOf(HashBundleContent(b.raw), b.framed.sum(), HashBundleContent(trailerBytes))
	if err != nil {
		return out, err
	}
	return stagedFacts{
		header:  b.header,
		digest:  digest,
		records: b.records,
		trees:   b.trees,
		deltas:  b.deltas,
	}, nil
}

// stage writes one section to dst.
func (b *bundleReader) stage(section []byte) error {
	if b.dst == nil {
		return nil
	}
	if _, err := b.dst.Write(section); err != nil {
		return fmt.Errorf("pabtdebug: stage an import: %w", err)
	}
	return nil
}

// runKeyOf is the run a header says it carries.
func runKeyOf(header BundleHeader) RunKey {
	return RunKey{
		SessionID: header.SourceSessionID,
		PlanID:    header.SourcePlanID,
		RunID:     header.SourceRunID,
	}
}

// readExactly reads n bytes, refusing a short read rather than accepting a
// truncated bundle as though it were whole.
func readExactly(r io.Reader, n int64, bound uint64) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("%w: a negative length", ErrBundleInvalid)
	}
	if uint64(n) > bound {
		return nil, fmt.Errorf("%w: a section of %d bytes exceeds the %d byte bound",
			ErrImportTooLarge, n, bound)
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(r, out); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("%w: the bundle is truncated: %v", ErrBundleInvalid, err)
		}
		return nil, fmt.Errorf("pabtdebug: read a bundle: %w", err)
	}
	return out, nil
}

// verifyFrame checks a frame against the digest its header names.
//
// The digest is of the payload's own bytes, so this needs no knowledge of what kind
// of object the payload holds. A rewriter recomputes it for free along with the
// trailer, so it is not on its own what makes a frame trustworthy; the object's own
// digest and the record chain are checked after it.
func verifyFrame(kind BundleEntryKind, digest string, payload []byte) error {
	if digest == "" {
		return fmt.Errorf("%w: a frame names no digest", ErrBundleInvalid)
	}
	if got := HashBundleContent(payload); got != digest {
		return fmt.Errorf("%w: a frame names digest %q and holds bytes that hash to %q",
			ErrBundleInvalid, digest, got)
	}
	switch kind {
	case BundleEntryPlan:
		var plan StoredPlan
		if err := CanonicalDecode(payload, &plan); err != nil {
			return fmt.Errorf("%w: a plan frame: %v", ErrBundleInvalid, err)
		}
		projected := plan
		projected.RecordSHA256 = ""
		digest, err := HashCanonical(DomainPlan, projected)
		if err != nil {
			return err
		}
		if digest != plan.RecordSHA256 {
			return fmt.Errorf("%w: a plan names digest %q and hashes to %q",
				ErrBundleInvalid, plan.RecordSHA256, digest)
		}
	case BundleEntryRun:
		var run StoredRun
		if err := CanonicalDecode(payload, &run); err != nil {
			return fmt.Errorf("%w: a run frame: %v", ErrBundleInvalid, err)
		}
		digest, err := HashRun(run)
		if err != nil {
			return err
		}
		if digest != run.RecordSHA256 {
			return fmt.Errorf("%w: a run names digest %q and hashes to %q",
				ErrBundleInvalid, run.RecordSHA256, digest)
		}
	case BundleEntryRecord, BundleEntryTree, BundleEntryDelta:
		// Checked by the caller, which already reads the record to see where it
		// belongs in the chain.
	case BundleEntryCodec, BundleEntryTrailer:
		return fmt.Errorf("%w: a frame claims to be a %s, which a bundle does not carry",
			ErrBundleInvalid, kind)
	default:
		return fmt.Errorf("%w: a frame claims to be a %s, which this format does not carry",
			ErrBundleInvalid, kind)
	}
	return nil
}

// verifyRecord checks one record of a bundle against the place it holds and the
// record before it.
//
// A record names its own sequence and links to its predecessor, so this is what
// refuses a bundle whose records were reordered, exchanged, or pulled from another
// history. Each payload's own digest survives such a bundle, because a record
// travels with the digest that describes it, and every other digest in the file is
// unkeyed and recomputed for free. The chain is the claim a rewriter has to restate
// consistently across every record after the one it changed.
func verifyRecord(record StoredRecord, sequence uint64, previous string) error {
	recordDigest, err := HashRecord(record)
	if err != nil {
		return err
	}
	if recordDigest != record.RecordSHA256 {
		return fmt.Errorf("%w: a record names digest %q and hashes to %q",
			ErrBundleInvalid, record.RecordSHA256, recordDigest)
	}
	payloadDigest, err := HashPayload(record)
	if err != nil {
		return err
	}
	if payloadDigest != record.PayloadSHA256 {
		return fmt.Errorf("%w: a record names payload digest %q and hashes to %q",
			ErrBundleInvalid, record.PayloadSHA256, payloadDigest)
	}
	if record.Sequence != sequence {
		return fmt.Errorf("%w: a record claims sequence %d and arrived %dth",
			ErrBundleInvalid, record.Sequence, sequence)
	}
	if record.PreviousRecordSHA256 != previous {
		return fmt.Errorf("%w: a record links to %q, the record before it is %q",
			ErrBundleInvalid, record.PreviousRecordSHA256, previous)
	}
	return nil
}

// bundleFrame is one decoded frame. Its payload is only valid until the next call,
// because the reader does not hold a bundle it has already walked.
type bundleFrame struct {
	Kind    BundleEntryKind
	Payload []byte
}

// streamDigest accumulates a digest over bytes that stream past, so a bundle is
// checked without being held.
type streamDigest struct{ h hash.Hash }

// newStreamDigest returns a digest seeded with its domain, so a stream and a
// one-shot hash of the same bytes agree. A domain is part of every digest this
// layer computes, and a streaming digest that omitted it would disagree with the
// one-shot form over identical bytes.
func newStreamDigest(domain string) *streamDigest {
	h := sha256.New()
	_, _ = h.Write([]byte(domain))
	_, _ = h.Write([]byte{0})
	return &streamDigest{h: h}
}

func (d *streamDigest) add(b []byte) { _, _ = d.h.Write(b) }

func (d *streamDigest) sum() string { return hex.EncodeToString(d.h.Sum(nil)) }
