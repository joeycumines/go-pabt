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
	"os"
	"time"
)

var (
	// ErrBundleInvalid reports a bundle that is not a bundle, or is damaged.
	ErrBundleInvalid = errors.New(`pabtdebug: invalid bundle`)

	// ErrImportStale reports a ticket whose bundle is no longer staged, or whose
	// store has moved on since it was issued.
	ErrImportStale = errors.New(`pabtdebug: import is stale`)

	// ErrImportTooLarge reports a bundle over the caller's bound.
	ErrImportTooLarge = errors.New(`pabtdebug: bundle is too large`)
)

// ImportLimits bound what one import may cost.
type ImportLimits struct {
	// MaxBytes bounds the staged bundle.
	MaxBytes uint64

	// MaxObjects bounds the number of frames, so a bundle of tiny frames cannot
	// cost unbounded work.
	MaxObjects uint32
}

// DefaultImportLimits returns limits that suit an interactive local import.
func DefaultImportLimits() ImportLimits {
	return ImportLimits{MaxBytes: 1 << 30, MaxObjects: 5_000_000}
}

// Validate reports limits that cannot bound anything.
func (l ImportLimits) Validate() error {
	if l.MaxBytes == 0 || l.MaxObjects == 0 {
		return fmt.Errorf(`pabtdebug: incomplete import limits`)
	}
	return nil
}

// ImportTicket proves a staged bundle may be activated.
//
// The proof is unexported and binds the ticket to the bundle's digest, so an
// activated import is the one that was staged and the one that was verified.
type ImportTicket struct {
	ImportID       string    `json:"importId"`
	LocalSessionID string    `json:"localSessionId"`
	SourceStoreID  string    `json:"sourceStoreId"`
	SourceRunKey   RunKey    `json:"sourceRunKey"`
	BundleSHA256   string    `json:"bundleSha256"`
	Generation     uint64    `json:"generation"`
	StagedPath     string    `json:"-"`
	RecordCount    uint64    `json:"recordCount"`
	TreeCount      uint64    `json:"treeCount"`
	DeltaCount     uint64    `json:"deltaCount"`
	StagedAt       Timestamp `json:"stagedAt"`

	proof [32]byte
}

// ImportReport describes what an import staged.
type ImportReport struct {
	ImportID       string    `json:"importId"`
	LocalSessionID string    `json:"localSessionId"`
	SourceRunKey   RunKey    `json:"sourceRunKey"`
	RecordCount    uint64    `json:"recordCount"`
	TreeCount      uint64    `json:"treeCount"`
	DeltaCount     uint64    `json:"deltaCount"`
	BundleSHA256   string    `json:"bundleSha256"`
	StagedAt       Timestamp `json:"stagedAt"`
}

// ActivationResponse reports what an activation made active.
//
// TreeCount and DeltaCount are what the store actually holds afterwards, which can
// be fewer than the bundle carried: a delta whose base is not in the bundle cannot
// be verified against anything, so it is not stored.
type ActivationResponse struct {
	ImportID     string    `json:"importId"`
	SessionID    string    `json:"sessionId"`
	PlanID       string    `json:"planId"`
	RunID        string    `json:"runId"`
	Generation   uint64    `json:"generation"`
	RecordCount  uint64    `json:"recordCount"`
	TreeCount    uint64    `json:"treeCount"`
	DeltaCount   uint64    `json:"deltaCount"`
	ActivatedAt  Timestamp `json:"activatedAt"`
	SourceRunKey RunKey    `json:"sourceRunKey"`
}

// StageImport streams a bundle to disk and verifies it, without changing what the
// store is serving.
//
// It is a separate step from activation because a bundle can be large and
// verifying it can fail, and a caller must not lose the session it is currently
// recording into because a file it was handed was damaged.
func (s *SQLiteStore) StageImport(
	ctx context.Context,
	source io.Reader,
	limits ImportLimits,
) (ImportTicket, error) {
	if err := s.checkUsable(true); err != nil {
		return ImportTicket{}, err
	}
	if err := limits.Validate(); err != nil {
		return ImportTicket{}, err
	}
	path, err := s.tempPath("import")
	if err != nil {
		return ImportTicket{}, err
	}
	fail := func(err error) (ImportTicket, error) {
		_ = os.Remove(path)
		return ImportTicket{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return fail(fmt.Errorf("pabtdebug: stage an import: %w", err))
	}
	defer func() { _ = file.Close() }()

	// The bundle is written while it is read, so a large one is never held in
	// memory, and the bound is enforced on what actually arrived rather than on
	// what the caller claimed to send.
	facts, err := s.stageBundle(ctx, source, file, limits)
	if err != nil {
		return fail(err)
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("pabtdebug: sync a staged import: %w", err))
	}
	if err := file.Close(); err != nil {
		return fail(fmt.Errorf("pabtdebug: close a staged import: %w", err))
	}
	installation, err := s.Installation(ctx)
	if err != nil {
		return fail(err)
	}
	importID, err := randomID("import")
	if err != nil {
		return fail(err)
	}
	localSession, err := randomID("session")
	if err != nil {
		return fail(err)
	}
	ticket := ImportTicket{
		ImportID:       importID,
		LocalSessionID: localSession,
		SourceStoreID:  facts.header.SourceStoreID,
		SourceRunKey: RunKey{
			SessionID: facts.header.SourceSessionID,
			PlanID:    facts.header.SourcePlanID,
			RunID:     facts.header.SourceRunID,
		},
		// The digest is the one the reader computed over the bytes it just staged,
		// so the ticket names the file on disk rather than an intention.
		BundleSHA256: facts.digest,
		Generation:   installation.Generation,
		StagedPath:   path,
		RecordCount:  facts.records,
		TreeCount:    facts.trees,
		DeltaCount:   facts.deltas,
		StagedAt:     Timestamp(s.clock().UTC()),
	}
	ticket.proof = s.proof(ticket.ImportID, ticket.LocalSessionID, ticket.BundleSHA256)
	return ticket, nil
}

// stageBundle writes the bundle it accepted to dst and returns what accepting it
// established.
func (s *SQLiteStore) stageBundle(
	ctx context.Context,
	source io.Reader,
	dst *os.File,
	limits ImportLimits,
) (stagedFacts, error) {
	reader, err := newBundleReader(source, dst, limits, s.limits.MaxRecordBytes)
	if err != nil {
		return stagedFacts{}, err
	}
	for {
		if _, more, err := reader.next(ctx); err != nil {
			return stagedFacts{}, err
		} else if !more {
			break
		}
	}
	return reader.finish()
}

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
	source   *bufio.Reader
	dst      io.Writer
	limits   ImportLimits
	maxFrame uint64
	header   BundleHeader
	raw      []byte
	framed   *streamDigest
	frames   uint64
	counts   map[BundleEntryKind]uint64
	records  uint64
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
	maxFrame uint64,
) (*bundleReader, error) {
	reader := &bundleReader{
		dst:      dst,
		limits:   limits,
		maxFrame: maxFrame,
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
	if header.Length == 0 || header.Length > b.maxFrame {
		return out, false, fmt.Errorf("%w: a frame payload length is %d", ErrBundleInvalid, header.Length)
	}
	payload, err := readExactly(b.source, int64(header.Length), b.limits.MaxBytes)
	if err != nil {
		return out, false, err
	}
	if err := verifyFrame(header.Kind, header.SHA256, payload); err != nil {
		return out, false, err
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
		if b.records == 0 && record.RunKey != runKeyOf(b.header) {
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
	return bundleFrame{
		Kind:    header.Kind,
		Run:     header.Run,
		SHA256:  header.SHA256,
		Payload: payload,
	}, true, nil
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

func (s *SQLiteStore) verifyImportTicket(ticket ImportTicket) error {
	want := s.proof(ticket.ImportID, ticket.LocalSessionID, ticket.BundleSHA256)
	if ticket.proof != want {
		return fmt.Errorf("%w: the import ticket was not issued by this store", ErrImportStale)
	}
	return nil
}

// Report describes a staged import.
func (t ImportTicket) Report() ImportReport {
	return ImportReport{
		ImportID:       t.ImportID,
		LocalSessionID: t.LocalSessionID,
		SourceRunKey:   t.SourceRunKey,
		RecordCount:    t.RecordCount,
		TreeCount:      t.TreeCount,
		DeltaCount:     t.DeltaCount,
		BundleSHA256:   t.BundleSHA256,
		StagedAt:       t.StagedAt,
	}
}

// DiscardImport removes a staged bundle that was never activated.
func (s *SQLiteStore) DiscardImport(ticket ImportTicket) error {
	if err := s.checkUsable(true); err != nil {
		return err
	}
	if err := s.verifyImportTicket(ticket); err != nil {
		return err
	}
	if err := os.Remove(ticket.StagedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pabtdebug: discard an import: %w", err)
	}
	return nil
}

// ActivateImport publishes a staged bundle as this store's active session.
//
// The whole publication is one transaction: the new session, its plan and run,
// every rewritten record, and the installation's active session and generation.
// Either the store is serving the imported history or it is serving what it was
// serving before, and a reader can never catch it in between. That is why a store
// can be recording into itself while an import is activated.
func (s *SQLiteStore) ActivateImport(ctx context.Context, ticket ImportTicket) (ActivationResponse, error) {
	if err := s.checkUsable(true); err != nil {
		return ActivationResponse{}, err
	}
	if err := s.verifyImportTicket(ticket); err != nil {
		return ActivationResponse{}, err
	}
	planID, err := randomID("plan")
	if err != nil {
		return ActivationResponse{}, err
	}
	runID, err := randomID("run")
	if err != nil {
		return ActivationResponse{}, err
	}
	local := RunKey{
		SessionID: ticket.LocalSessionID,
		PlanID:    planID,
		RunID:     runID,
	}
	now := s.clock().UTC()
	published, err := s.insertImportedHistory(ctx, ticket, local, now)
	if err != nil {
		return ActivationResponse{}, err
	}
	return ActivationResponse{
		ImportID:     ticket.ImportID,
		SessionID:    local.SessionID,
		PlanID:       local.PlanID,
		RunID:        local.RunID,
		Generation:   published.generation,
		RecordCount:  published.records,
		TreeCount:    published.trees,
		DeltaCount:   published.deltas,
		ActivatedAt:  Timestamp(now),
		SourceRunKey: ticket.SourceRunKey,
	}, nil
}

// publishedHistory is what an activation wrote.
type publishedHistory struct {
	generation uint64
	records    uint64
	trees      uint64
	deltas     uint64
}

// insertImportedHistory writes the staged bundle's records under fresh local
// identities, in one transaction that also publishes them.
//
// Every local identity is rewritten: the session, the plan, the run, and each
// record's event identity. The source identities stay in the store as provenance,
// which is why the same bundle can be imported twice and produce two independent
// sessions rather than colliding on a key that only had room for one.
//
// Every digest is recomputed over the rewritten record, and the chain is rebuilt
// from the first record, because a rewritten record is a different record and its
// old digest no longer describes it.
//
// The bundle is streamed rather than read whole, in the frame order the format
// fixes: a tree or delta frame names a record the exporter wrote before it, so the
// identities a frame refers to already exist by the time it arrives. A frame that
// names a record the bundle has not carried is refused rather than deferred, which
// is the same rule the frame digest and the chain apply.
func (s *SQLiteStore) insertImportedHistory(
	ctx context.Context,
	ticket ImportTicket,
	local RunKey,
	now time.Time,
) (publishedHistory, error) {
	var out publishedHistory
	file, err := os.Open(ticket.StagedPath)
	if err != nil {
		return out, fmt.Errorf("%w: the staged bundle is gone", ErrImportStale)
	}
	defer func() { _ = file.Close() }()
	// The staged file is re-read under bounds derived from its own size rather than
	// from a default, so activation is bounded by the file it was handed whatever
	// limits it was staged under.
	info, err := file.Stat()
	if err != nil {
		return out, fmt.Errorf("%w: the staged bundle is gone", ErrImportStale)
	}
	size := uint64(info.Size())
	objects := uint64(^uint32(0))
	if size < objects {
		// Every frame is at least a length prefix and a header, so a file cannot
		// hold more frames than it has bytes.
		objects = size
	}
	reader, err := newBundleReader(file, nil, ImportLimits{MaxBytes: size, MaxObjects: uint32(objects)},
		s.limits.MaxRecordBytes)
	if err != nil {
		return out, err
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The generation is read inside the transaction that publishes it and the
	// publication is conditional on it, because two activations that both read the
	// generation first would otherwise publish the same one twice.
	var generation uint64
	row := tx.QueryRowContext(ctx, "SELECT generation FROM installation WHERE singleton = 1")
	if err := row.Scan(&generation); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	if generation != ticket.Generation {
		return out, fmt.Errorf("%w: the store is at generation %d, the bundle was staged at %d",
			ErrImportStale, generation, ticket.Generation)
	}
	out.generation = generation + 1

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO sessions (session_id, store_id, generation, read_only, created_at, record_count) "+
			"VALUES (?, ?, ?, 1, ?, 0)",
		local.SessionID, string(installation0(s)), out.generation, stamp(now)); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	plan := StoredPlan{PlanID: local.PlanID, Title: "imported " + ticket.SourceRunKey.RunID, CreatedAt: Timestamp(now)}
	plan.RecordSHA256 = ""
	planDigest, err := HashCanonical(DomainPlan, plan)
	if err != nil {
		return out, err
	}
	plan.RecordSHA256 = planDigest
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO plans (session_id, plan_id, title, created_at, metadata_json, record_sha256) "+
			"VALUES (?, ?, ?, ?, NULL, ?)",
		local.SessionID, local.PlanID, plan.Title, stamp(now), mustHex(planDigest)); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	// The run row is written before the records because they are bound to it, and
	// restated when the bundle's own run frame arrives, which is where the source
	// run's start and initial revision come from.
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO runs (session_id, plan_id, run_id, state, started_at, initial_plan_revision, "+
			"head_seq, head_tree_seq, append_epoch) VALUES (?, ?, ?, 'closed', ?, 0, 0, 0, 0)",
		local.SessionID, local.PlanID, local.RunID, stamp(now)); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}

	var (
		previous      = zeroDigest
		sequence      uint64
		localBySource = map[string]string{}
		// committed maps a local event identity to the tree its own record committed,
		// which is the copy the chain covers and therefore the one that decides.
		committed = map[string]string{}
		// trees holds each re-sealed tree, so a delta can be proved against the base
		// this import actually wrote.
		trees       = map[string]StoredTree{}
		headTree    StoredTree
		headTreeUID string
	)
	for {
		frame, more, err := reader.next(ctx)
		if err != nil {
			return out, err
		}
		if !more {
			break
		}
		switch frame.Kind {
		case BundleEntryRun:
			var source StoredRun
			if err := CanonicalDecode(frame.Payload, &source); err != nil {
				return out, fmt.Errorf("%w: a run frame: %v", ErrBundleInvalid, err)
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE runs SET started_at = ?, initial_plan_revision = ? "+
					"WHERE session_id = ? AND run_id = ?",
				stamp(source.StartedAt.Time()), source.InitialPlanRevision,
				local.SessionID, local.RunID); err != nil {
				return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
			}
		case BundleEntryRecord:
			var record StoredRecord
			if err := CanonicalDecode(frame.Payload, &record); err != nil {
				return out, fmt.Errorf("%w: a record frame: %v", ErrBundleInvalid, err)
			}
			sequence++
			sourceUID := record.EventUID
			uid, err := randomID("event")
			if err != nil {
				return out, err
			}
			record.Format = FormatName
			record.Version = FormatVersion
			record.EventUID = uid
			record.RunKey = local
			record.PreviousRecordSHA256 = previous
			record.PayloadSHA256 = ""
			record.RecordSHA256 = ""
			// The revision scope repeats the run and the sequence a fact was observed
			// at, so it is rewritten with the rest of the identity. Leaving the source
			// run in it would leave a local record permanently scoped to another
			// store's run, inside bytes the chain covers and nothing else reports on.
			record.Revision.RunKey = local
			record.Revision.Sequence = sequence
			// A record's own copy of the tree is inside its bytes, so it is restamped
			// and re-sealed before the record's digests are taken. Leaving the source
			// envelope in place would make the record disagree with the representation
			// written for it.
			if record.Tree != nil {
				record.Tree.RunKey = local
				record.Tree.EventUID = uid
				record.Tree.Sequence = sequence
				record.Tree.Digest = ""
				treeDigest, err := HashTree(*record.Tree)
				if err != nil {
					return out, err
				}
				record.Tree.Digest = treeDigest
				committed[uid] = treeDigest
			}
			payloadDigest, err := HashPayload(record)
			if err != nil {
				return out, err
			}
			record.PayloadSHA256 = payloadDigest
			recordDigest, err := HashRecord(record)
			if err != nil {
				return out, err
			}
			record.RecordSHA256 = recordDigest
			recordBytes, err := CanonicalEncode(record)
			if err != nil {
				return out, err
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO records (session_id, event_uid, plan_id, run_id, seq, attempt_id, kind, "+
					"payload_state, tree_eligible, execution_started, recorded_at, record_json, "+
					"payload_sha256, previous_record_sha256, record_sha256) "+
					"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				local.SessionID, uid, local.PlanID, local.RunID, sequence, record.AttemptID,
				string(record.Kind), string(record.PayloadState),
				boolToInt(record.Tree != nil), boolToInt(record.ExecutionStarted),
				stamp(now), recordBytes, mustHex(payloadDigest), mustHex(previous), mustHex(recordDigest),
			); err != nil {
				return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
			}
			previous = recordDigest
			localBySource[sourceUID] = uid
		case BundleEntryTree:
			var tree StoredTree
			if err := CanonicalDecode(frame.Payload, &tree); err != nil {
				return out, fmt.Errorf("%w: a tree frame: %v", ErrBundleInvalid, err)
			}
			uid, ok := localBySource[tree.EventUID]
			if !ok {
				return out, fmt.Errorf("%w: a tree names event %q, which the bundle has not carried",
					ErrBundleInvalid, tree.EventUID)
			}
			want, ok := committed[uid]
			if !ok {
				return out, fmt.Errorf("%w: a tree is committed for event %q, whose record holds none",
					ErrBundleInvalid, tree.EventUID)
			}
			tree.RunKey = local
			tree.EventUID = uid
			tree.Digest = ""
			digest, err := HashTree(tree)
			if err != nil {
				return out, err
			}
			// The record's own copy is re-sealed from the same bytes, so the two
			// digests are taken over equal content. They differ only when the frame
			// holds a different tree, which is a damaged bundle and not a history.
			if digest != want {
				return out, fmt.Errorf("%w: an imported tree re-seals to %q, its record committed %q",
					ErrBundleInvalid, digest, want)
			}
			tree.Digest = digest
			treeBytes, err := CanonicalEncode(tree)
			if err != nil {
				return out, err
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO representations (session_id, event_uid, plan_id, run_id, seq, "+
					"target_plan_revision, tree_json, tree_sha256) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
				local.SessionID, uid, local.PlanID, local.RunID, tree.Sequence, tree.PlanRevision,
				treeBytes, mustHex(digest)); err != nil {
				return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
			}
			trees[uid] = tree
			out.trees++
			// The run's head tree is the newest tree it holds, which is what the
			// store's own writer records and what a reader bootstraps from.
			if tree.Sequence >= headTree.Sequence {
				headTree = tree
				headTreeUID = uid
			}
		case BundleEntryDelta:
			var delta ExactTreeDelta
			if err := CanonicalDecode(frame.Payload, &delta); err != nil {
				return out, fmt.Errorf("%w: a delta frame: %v", ErrBundleInvalid, err)
			}
			// A delta is stored only when both of its ends are trees this import
			// wrote and it is not empty. A delta naming a base the bundle does not
			// carry has nothing to be verified against, and an empty one describes no
			// change the tree does not already say.
			targetUID, ok := localBySource[delta.TargetEventUID]
			if !ok {
				continue
			}
			targetTree, ok := trees[targetUID]
			if !ok {
				continue
			}
			baseUID, ok := localBySource[delta.BaseEventUID]
			if !ok {
				continue
			}
			baseTree, ok := trees[baseUID]
			if !ok || len(delta.Operations) == 0 {
				continue
			}
			// The trees were re-sealed, so every digest naming one is restated here.
			// A delta that still named the source digests would name trees that no
			// longer exist, and the imported run's own chain check would call it
			// corrupt.
			delta.RunKey = local
			delta.BaseEventUID = baseUID
			delta.BaseSequence = baseTree.Sequence
			delta.BaseDigest = baseTree.Digest
			delta.BasePlanRevision = baseTree.PlanRevision
			delta.TargetEventUID = targetUID
			delta.TargetSequence = targetTree.Sequence
			delta.TargetDigest = targetTree.Digest
			delta.SHA256 = ""
			digest, err := HashDelta(delta)
			if err != nil {
				return out, err
			}
			delta.SHA256 = digest
			// The delta is proved against the trees this import wrote before it is
			// stored beside them, so a representation is never a pair that disagrees
			// with itself. This is the store's own rule, applied to imported bytes.
			if _, err := ApplyExactDelta(baseTree, delta); err != nil {
				return out, fmt.Errorf("%w: an imported delta: %v", ErrBundleInvalid, err)
			}
			deltaBytes, err := CanonicalEncode(delta)
			if err != nil {
				return out, err
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE representations SET base_event_uid = ?, base_seq = ?, delta_json = ?, "+
					"delta_sha256 = ?, operation_count = ? "+
					"WHERE session_id = ? AND run_id = ? AND seq = ?",
				baseUID, baseTree.Sequence, deltaBytes, mustHex(digest), len(delta.Operations),
				local.SessionID, local.RunID, targetTree.Sequence); err != nil {
				return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
			}
			out.deltas++
		}
	}
	facts, err := reader.finish()
	if err != nil {
		return out, err
	}
	// The staged file is re-digested rather than trusted. A file replaced between
	// staging and activation would otherwise be published as though it were the one
	// that was verified.
	if facts.digest != ticket.BundleSHA256 {
		return out, fmt.Errorf("%w: the staged bundle is no longer the one that was verified",
			ErrImportStale)
	}
	out.records = sequence

	// The run's head is the last record and the newest tree, both recomputed here
	// rather than copied from the source, because the chain and the trees were
	// rewritten.
	if _, err := tx.ExecContext(ctx,
		"UPDATE runs SET head_seq = ?, head_tree_seq = ?, head_tree_event_uid = ?, "+
			"head_tree_digest = ?, head_record_sha256 = ?, head_plan_revision = ?, record_sha256 = ? "+
			"WHERE session_id = ? AND run_id = ?",
		sequence, headTree.Sequence, nullableUID(headTreeUID), nullableDigest(headTree.Digest),
		mustHex(previous), int64(headTree.PlanRevision), mustHex(previous),
		local.SessionID, local.RunID); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE sessions SET record_count = ? WHERE session_id = ?", sequence, local.SessionID); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	// Publication is the last statement of the same transaction, so a reader sees
	// either the store as it was or the store as it now is, never a store that has an
	// imported session and is not serving it. The generation is conditional on the one
	// this transaction read, which is what makes the publication atomic by statement
	// rather than by an argument about how many connections the writer has; the read
	// above is what refuses a superseded ticket, and that is the part a test pins. The
	// recovery state is left as it was found: an activation resolves no intent, so it
	// must not report a store that has one as ready.
	result, err := tx.ExecContext(ctx,
		"UPDATE installation SET active_session_id = ?, generation = ?, updated_at = ? "+
			"WHERE singleton = 1 AND generation = ?",
		local.SessionID, out.generation, stamp(now), generation)
	if err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	} else if affected != 1 {
		return out, fmt.Errorf("%w: the store moved to another generation during the activation",
			ErrImportStale)
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	// The staged copy has served its purpose. It is left on disk only if it cannot be
	// removed, which is a file to tidy rather than a bundle that could be activated.
	_ = os.Remove(ticket.StagedPath)
	return out, nil
}

// nullableUID renders an absent event identity as SQL NULL rather than an empty
// string, which is a different claim.
func nullableUID(uid string) any {
	if uid == "" {
		return nil
	}
	return uid
}

// nullableDigest renders an absent tree digest as SQL NULL, for the same reason.
func nullableDigest(digest string) any {
	if digest == "" {
		return nil
	}
	return digest
}

// installation0 returns the store identity recorded at bootstrap, which an
// imported session inherits.
func installation0(s *SQLiteStore) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return string(s.storeID)
}

// bundleFrame is one decoded frame. Its payload is only valid until the next call,
// because the reader does not hold a bundle it has already walked.
type bundleFrame struct {
	Kind    BundleEntryKind
	Run     string
	SHA256  string
	Payload []byte
}
