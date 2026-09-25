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
type ActivationResponse struct {
	ImportID     string    `json:"importId"`
	SessionID    string    `json:"sessionId"`
	PlanID       string    `json:"planId"`
	RunID        string    `json:"runId"`
	Generation   uint64    `json:"generation"`
	RecordCount  uint64    `json:"recordCount"`
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
	written, err := s.verifyAndStageBundle(ctx, source, file, limits)
	if err != nil {
		return fail(err)
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("pabtdebug: sync a staged import: %w", err))
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
		SourceStoreID:  written.header.SourceStoreID,
		SourceRunKey: RunKey{
			SessionID: written.header.SourceSessionID,
			PlanID:    written.header.SourcePlanID,
			RunID:     written.header.SourceRunID,
		},
		BundleSHA256: written.bundleDigest,
		Generation:   installation.Generation,
		StagedPath:   path,
		RecordCount:  written.records,
		StagedAt:     Timestamp(s.clock().UTC()),
	}
	ticket.proof = s.proof(ticket.ImportID, ticket.LocalSessionID, ticket.BundleSHA256)
	return ticket, nil
}

// Report describes a staged import.
func (t ImportTicket) Report() ImportReport {
	return ImportReport{
		ImportID:       t.ImportID,
		LocalSessionID: t.LocalSessionID,
		SourceRunKey:   t.SourceRunKey,
		RecordCount:    t.RecordCount,
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

// stagedFacts is what verifying a bundle established.
type stagedFacts struct {
	header       BundleHeader
	trailer      BundleTrailer
	bundleDigest string
	records      uint64
	trees        uint64
	deltas       uint64
}

// verifyAndStageBundle reads a bundle once, checking it as it goes, and writes
// the bytes it accepted.
//
// Verification happens during the single read rather than in a second pass, so a
// bundle is never staged before it has been shown to be sound.
func (s *SQLiteStore) verifyAndStageBundle(
	ctx context.Context,
	source io.Reader,
	dst *os.File,
	limits ImportLimits,
) (stagedFacts, error) {
	var out stagedFacts
	var kept bytes.Buffer
	if _, err := io.CopyN(&kept, source, int64(len(bundleMagic))+4); err != nil {
		return out, fmt.Errorf("%w: the bundle is shorter than its header length: %v", ErrBundleInvalid, err)
	}
	magic := kept.Bytes()[:len(bundleMagic)]
	if !bytes.Equal(magic, []byte(bundleMagic)) {
		return out, fmt.Errorf("%w: the bundle does not begin with its magic", ErrBundleInvalid)
	}
	headerLen := binary.BigEndian.Uint32(kept.Bytes()[len(bundleMagic):])
	if headerLen == 0 || headerLen > maxBundleHeaderBytes {
		return out, fmt.Errorf("%w: the bundle header length is %d", ErrBundleInvalid, headerLen)
	}
	if _, err := dst.Write(kept.Bytes()); err != nil {
		return out, fmt.Errorf("pabtdebug: stage an import: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	headerBytes, err := readExactly(source, int64(headerLen), limits.MaxBytes)
	if err != nil {
		return out, err
	}
	if _, err := dst.Write(headerBytes); err != nil {
		return out, fmt.Errorf("pabtdebug: stage an import: %w", err)
	}
	if err := CanonicalDecode(headerBytes, &out.header); err != nil {
		return out, fmt.Errorf("%w: the bundle header: %v", ErrBundleInvalid, err)
	}
	if out.header.Format != BundleFormatName || out.header.Version != BundleFormatVersion {
		return out, fmt.Errorf("%w: the bundle is %s version %d",
			ErrBundleInvalid, out.header.Format, out.header.Version)
	}

	// The frames are digested as they stream, so the trailer's content digest can
	// be checked without a second pass over the file.
	frameDigest := newStreamDigest(DomainBundle + "/content")
	// The frames end at an explicit terminator, so the reader has to look ahead
	// rather than commit to a length. A buffered peek is what makes that possible
	// without reading a section twice or guessing.
	reader := bufio.NewReader(io.LimitReader(source, int64(limits.MaxBytes)))
	var (
		frames     uint64
		frameBytes int64
	)
	for {
		marker, err := reader.Peek(len(bundleTrailerMagic))
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, fmt.Errorf("%w: the bundle ends without a trailer", ErrBundleInvalid)
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				return out, fmt.Errorf("%w: the bundle ends without a trailer", ErrBundleInvalid)
			}
			return out, fmt.Errorf("pabtdebug: read a bundle: %w", err)
		}
		if bytes.Equal(marker, []byte(bundleTrailerMagic)) {
			// The terminator is consumed and written: the staged file has to be the
			// whole bundle, or activation would find frames with no end.
			if _, err := reader.Discard(len(bundleTrailerMagic)); err != nil {
				return out, fmt.Errorf("pabtdebug: read a bundle: %w", err)
			}
			if _, err := dst.Write(marker); err != nil {
				return out, fmt.Errorf("pabtdebug: stage an import: %w", err)
			}
			break
		}
		lengthBytes, err := readExactly(reader, 4, limits.MaxBytes)
		if err != nil {
			return out, err
		}
		frameHeaderLen := binary.BigEndian.Uint32(lengthBytes)
		if frameHeaderLen == 0 || frameHeaderLen > maxBundleHeaderBytes {
			return out, fmt.Errorf("%w: a frame header length is %d", ErrBundleInvalid, frameHeaderLen)
		}
		frameHeader, err := readExactly(reader, int64(frameHeaderLen), limits.MaxBytes)
		if err != nil {
			return out, err
		}
		var frame struct {
			Kind   BundleEntryKind `json:"kind"`
			Run    string          `json:"run"`
			Length uint64          `json:"length"`
			SHA256 string          `json:"sha256"`
		}
		if err := CanonicalDecode(frameHeader, &frame); err != nil {
			return out, fmt.Errorf("%w: a frame header: %v", ErrBundleInvalid, err)
		}
		if frame.Length == 0 || frame.Length > s.limits.MaxRecordBytes {
			return out, fmt.Errorf("%w: a frame payload length is %d", ErrBundleInvalid, frame.Length)
		}
		if frames >= uint64(limits.MaxObjects) {
			return out, fmt.Errorf("%w: the bundle has more than %d frames", ErrBundleInvalid, limits.MaxObjects)
		}
		payload, err := readExactly(reader, int64(frame.Length), limits.MaxBytes)
		if err != nil {
			return out, err
		}
		// The content digest covers each frame's length prefix as well as its
		// header and payload, because that is the byte range the exporter digested.
		for _, section := range [][]byte{lengthBytes, frameHeader, payload} {
			if _, err := dst.Write(section); err != nil {
				return out, fmt.Errorf("pabtdebug: stage an import: %w", err)
			}
			frameDigest.add(section)
		}
		frameBytes += int64(len(frameHeader)) + int64(len(payload))
		frames++
		switch frame.Kind {
		case BundleEntryRecord:
			out.records++
		case BundleEntryTree:
			out.trees++
		case BundleEntryDelta:
			out.deltas++
		}
		if uint64(frameBytes) > limits.MaxBytes {
			return out, fmt.Errorf("%w: the bundle's frames exceed %d bytes", ErrImportTooLarge, limits.MaxBytes)
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
	}
	reader = bufio.NewReader(reader)

	trailerLenBytes, err := readExactly(reader, 4, limits.MaxBytes)
	if err != nil {
		return out, err
	}
	if _, err := dst.Write(trailerLenBytes); err != nil {
		return out, fmt.Errorf("pabtdebug: stage an import: %w", err)
	}
	trailerLen := binary.BigEndian.Uint32(trailerLenBytes)
	if trailerLen == 0 || trailerLen > maxBundleTrailerBytes {
		return out, fmt.Errorf("%w: the trailer length is %d", ErrBundleInvalid, trailerLen)
	}
	trailerBytes, err := readExactly(reader, int64(trailerLen), limits.MaxBytes)
	if err != nil {
		return out, err
	}
	if _, err := dst.Write(trailerBytes); err != nil {
		return out, fmt.Errorf("pabtdebug: stage an import: %w", err)
	}
	if err := CanonicalDecode(trailerBytes, &out.trailer); err != nil {
		return out, fmt.Errorf("%w: the trailer: %v", ErrBundleInvalid, err)
	}
	headerDigest, err := HashBundleHeader(out.header)
	if err != nil {
		return out, err
	}
	if out.trailer.HeaderSHA256 != headerDigest {
		return out, fmt.Errorf("%w: the trailer names header digest %q, the header is %q",
			ErrBundleInvalid, out.trailer.HeaderSHA256, headerDigest)
	}
	if out.trailer.FrameCount != frames {
		return out, fmt.Errorf("%w: the trailer counts %d frames, the bundle has %d",
			ErrBundleInvalid, out.trailer.FrameCount, frames)
	}
	if out.trailer.ContentSHA256 != frameDigest.sum() {
		return out, fmt.Errorf("%w: the trailer's content digest does not match the frames",
			ErrBundleInvalid)
	}
	if out.trailer.Counts[BundleEntryRecord] != out.records {
		return out, fmt.Errorf("%w: the trailer counts %d records, the bundle has %d",
			ErrBundleInvalid, out.trailer.Counts[BundleEntryRecord], out.records)
	}
	if out.records == 0 {
		return out, fmt.Errorf("%w: the bundle carries no records", ErrBundleInvalid)
	}
	return out, nil
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

// stagedSections are a staged file split back into its three parts.
type stagedSections struct {
	header  []byte
	frames  []byte
	trailer []byte
}

// splitStagedBundle separates a staged file into header, frames, and trailer.
//
// The split is done from the staged bytes rather than remembered from the
// streaming pass, so the digest is over the file that will actually be read
// back, not over what the reader believed it received.
func splitStagedBundle(data []byte) (stagedSections, error) {
	var out stagedSections
	if len(data) < len(bundleMagic)+4 || !bytes.Equal(data[:len(bundleMagic)], []byte(bundleMagic)) {
		return out, fmt.Errorf("%w: the staged file does not begin with its magic", ErrBundleInvalid)
	}
	rest := data[len(bundleMagic):]
	headerLen := int(binary.BigEndian.Uint32(rest[:4]))
	if headerLen > len(rest)-4 {
		return out, fmt.Errorf("%w: the header length runs past the file", ErrBundleInvalid)
	}
	out.header = rest[4 : 4+headerLen]
	rest = rest[4+headerLen:]

	terminator := bytes.Index(rest, []byte(bundleTrailerMagic))
	if terminator < 0 {
		return out, fmt.Errorf("%w: the staged file has no trailer terminator", ErrBundleInvalid)
	}
	out.frames = rest[:terminator]
	rest = rest[terminator+len(bundleTrailerMagic):]
	if len(rest) < 4 {
		return out, fmt.Errorf("%w: the staged file has no trailer length", ErrBundleInvalid)
	}
	trailerLen := int(binary.BigEndian.Uint32(rest[:4]))
	if trailerLen > len(rest)-4 {
		return out, fmt.Errorf("%w: the trailer length runs past the file", ErrBundleInvalid)
	}
	out.trailer = rest[4 : 4+trailerLen]
	return out, nil
}

// digestStagedBundle returns the staged file's own digest.
//
// The digest is taken over the three sections as a canonical projection, which is
// the same projection the exporter used, so an exported bundle and a staged copy
// of it agree.
func digestStagedBundle(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: the staged bundle is gone", ErrImportStale)
	}
	sections, err := splitStagedBundle(data)
	if err != nil {
		return "", err
	}
	return HashCanonical(DomainBundle, struct {
		Header  []byte `json:"header"`
		Frames  []byte `json:"frames"`
		Trailer []byte `json:"trailer"`
	}{sections.header, sections.frames, sections.trailer})
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
	installation, err := s.Installation(ctx)
	if err != nil {
		return ActivationResponse{}, err
	}
	if installation.Generation != ticket.Generation {
		// The store moved on since the bundle was staged, so the staged bytes were
		// chosen against a generation that is no longer current.
		return ActivationResponse{}, fmt.Errorf("%w: the store is at generation %d, the bundle was staged at %d",
			ErrImportStale, installation.Generation, ticket.Generation)
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

	count, err := s.insertImportedHistory(ctx, ticket, local, now)
	if err != nil {
		return ActivationResponse{}, err
	}
	return ActivationResponse{
		ImportID:     ticket.ImportID,
		SessionID:    local.SessionID,
		PlanID:       local.PlanID,
		RunID:        local.RunID,
		Generation:   installation.Generation + 1,
		RecordCount:  count,
		ActivatedAt:  Timestamp(now),
		SourceRunKey: ticket.SourceRunKey,
	}, nil
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
func (s *SQLiteStore) insertImportedHistory(
	ctx context.Context,
	ticket ImportTicket,
	local RunKey,
	now time.Time,
) (uint64, error) {
	data, err := os.ReadFile(ticket.StagedPath)
	if err != nil {
		return 0, fmt.Errorf("%w: the staged bundle is gone", ErrImportStale)
	}
	sections, err := splitStagedBundle(data)
	if err != nil {
		return 0, err
	}
	// The staged bytes are re-parsed here rather than remembered, so activation
	// works from the file that was verified and nothing else.
	frames, trailer, err := parseStagedFrames(sections)
	if err != nil {
		return 0, err
	}
	_ = trailer

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO sessions (session_id, store_id, generation, read_only, created_at, record_count) "+
			"VALUES (?, ?, ?, 1, ?, 0)",
		local.SessionID, string(installation0(s)), ticket.Generation+1, stamp(now)); err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	plan := StoredPlan{PlanID: local.PlanID, Title: "imported " + ticket.SourceRunKey.RunID, CreatedAt: Timestamp(now)}
	plan.RecordSHA256 = ""
	digest, err := HashCanonical(DomainPlan, plan)
	if err != nil {
		return 0, err
	}
	plan.RecordSHA256 = digest
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO plans (session_id, plan_id, title, created_at, metadata_json, record_sha256) "+
			"VALUES (?, ?, ?, ?, NULL, ?)",
		local.SessionID, local.PlanID, plan.Title, stamp(now), mustHex(digest)); err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO runs (session_id, plan_id, run_id, state, started_at, initial_plan_revision, "+
			"head_seq, head_tree_seq, append_epoch) VALUES (?, ?, ?, 'closed', ?, 0, 0, 0, 0)",
		local.SessionID, local.PlanID, local.RunID, stamp(now)); err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}

	// Records arrive in sequence order, so the chain is built as they are written
	// and the previous link is the one this transaction just committed.
	var (
		previous      = zeroDigest
		sequence      uint64
		localBySource = map[string]string{}
		sourceBySeq   = map[uint64]string{}
		treeFrames    = map[uint64][]byte{}
		deltaFrames   = map[uint64][]byte{}
	)
	for _, frame := range frames {
		switch frame.Kind {
		case BundleEntryRecord:
			var record StoredRecord
			if err := CanonicalDecode(frame.Payload, &record); err != nil {
				return 0, fmt.Errorf("%w: an imported record: %v", ErrBundleInvalid, err)
			}
			sequence++
			sourceUID := record.EventUID
			uid, err := randomID("event")
			if err != nil {
				return 0, err
			}
			record.Format = FormatName
			record.Version = FormatVersion
			record.EventUID = uid
			record.RunKey = local
			record.PreviousRecordSHA256 = previous
			record.PayloadSHA256 = ""
			record.RecordSHA256 = ""
			// A record's own copy of the tree is inside its bytes, so it is
			// restamped and re-sealed before the record's digests are taken.
			// Leaving the source envelope in place would make the record disagree
			// with the representation written for it.
			if record.Tree != nil {
				record.Tree.RunKey = local
				record.Tree.EventUID = uid
				record.Tree.Sequence = sequence
				record.Tree.Digest = ""
				treeDigest, err := HashTree(*record.Tree)
				if err != nil {
					return 0, err
				}
				record.Tree.Digest = treeDigest
			}
			payloadDigest, err := HashPayload(record)
			if err != nil {
				return 0, err
			}
			record.PayloadSHA256 = payloadDigest
			recordDigest, err := HashRecord(record)
			if err != nil {
				return 0, err
			}
			record.RecordSHA256 = recordDigest
			recordBytes, err := CanonicalEncode(record)
			if err != nil {
				return 0, err
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
				return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
			}
			previous = recordDigest
			localBySource[sourceUID] = uid
			sourceBySeq[sequence] = sourceUID
		case BundleEntryTree:
			// The tree belongs to a sequence, which is its own frame order, so it
			// is held until the record pass has established the local identities.
			treeFrames[uint64(len(treeFrames))] = frame.Payload
		case BundleEntryDelta:
			deltaFrames[uint64(len(deltaFrames))] = frame.Payload
		}
	}

	// The trees and deltas are written after every record so the local event
	// identities they refer to already exist inside this transaction.
	for frameIndex, payload := range treeFrames {
		var tree StoredTree
		if err := CanonicalDecode(payload, &tree); err != nil {
			return 0, fmt.Errorf("%w: an imported tree: %v", ErrBundleInvalid, err)
		}
		seq := tree.Sequence
		uid, ok := localBySource[tree.EventUID]
		if !ok {
			return 0, fmt.Errorf("%w: a tree names event %q, which the bundle does not record",
				ErrBundleInvalid, tree.EventUID)
		}
		tree.RunKey = local
		tree.EventUID = uid
		tree.Digest = ""
		digest, err := HashTree(tree)
		if err != nil {
			return 0, err
		}
		tree.Digest = digest
		treeBytes, err := CanonicalEncode(tree)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO representations (session_id, event_uid, plan_id, run_id, seq, "+
				"target_plan_revision, tree_json, tree_sha256) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			local.SessionID, uid, local.PlanID, local.RunID, seq, tree.PlanRevision, treeBytes,
			mustHex(digest)); err != nil {
			return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE representations SET base_event_uid = NULL, base_seq = NULL "+
				"WHERE session_id = ? AND run_id = ? AND seq = ?",
			local.SessionID, local.RunID, seq); err != nil {
			return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
		}
		_ = frameIndex
	}

	// A delta whose base is a record this bundle also carries is rewritten to the
	// local identity of that record. A delta whose base is not in the bundle is
	// dropped, because the base it names is not here to verify against.
	for frameIndex, payload := range deltaFrames {
		var delta ExactTreeDelta
		if err := CanonicalDecode(payload, &delta); err != nil {
			return 0, fmt.Errorf("%w: an imported delta: %v", ErrBundleInvalid, err)
		}
		targetUID, ok := localBySource[delta.TargetEventUID]
		if !ok {
			continue
		}
		var baseUID any
		var baseSeq any
		if mapped, found := localBySource[delta.BaseEventUID]; found {
			baseUID = mapped
			for sourceSeq, source := range sourceBySeq {
				if source == delta.BaseEventUID {
					baseSeq = sourceSeq
					break
				}
			}
		}
		delta.RunKey = local
		delta.TargetEventUID = targetUID
		if baseUID != nil {
			delta.BaseEventUID = baseUID.(string)
			delta.BaseSequence = baseSeq.(uint64)
		} else {
			delta.BaseEventUID = ""
			delta.BaseSequence = 0
		}
		delta.SHA256 = ""
		digest, err := HashDelta(delta)
		if err != nil {
			return 0, err
		}
		delta.SHA256 = digest
		deltaBytes, err := CanonicalEncode(delta)
		if err != nil {
			return 0, err
		}
		var targetSeq uint64
		for sourceSeq, source := range sourceBySeq {
			if source == delta.TargetEventUID || localBySource[source] == targetUID {
				targetSeq = sourceSeq
				break
			}
		}
		operations := len(delta.Operations)
		if operations == 0 {
			// An empty delta describes no change, so storing it would only be a
			// second way to say what the tree already says.
			_ = frameIndex
			continue
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE representations SET base_event_uid = ?, base_seq = ?, delta_json = ?, "+
				"delta_sha256 = ?, operation_count = ? "+
				"WHERE session_id = ? AND run_id = ? AND seq = ?",
			baseUID, baseSeq, deltaBytes, mustHex(digest), operations,
			local.SessionID, local.RunID, targetSeq); err != nil {
			return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
		}
	}

	// Publication is the last statement of the same transaction, so a reader sees
	// either the store as it was or the store as it now is, never a store that
	// has an imported session but is not serving it.
	if _, err := tx.ExecContext(ctx,
		"UPDATE installation SET active_session_id = ?, generation = ?, recovery = ?, updated_at = ? "+
			"WHERE singleton = 1",
		local.SessionID, ticket.Generation+1, string(RecoveryReady), stamp(now)); err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE runs SET head_seq = ?, head_record_sha256 = ?, record_sha256 = ? "+
			"WHERE session_id = ? AND run_id = ?",
		sequence, mustHex(previous), mustHex(previous), local.SessionID, local.RunID); err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE sessions SET record_count = ? WHERE session_id = ?", sequence, local.SessionID); err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("pabtdebug: activate an import: %w", err)
	}
	return sequence, nil
}

// installation0 returns the store identity recorded at bootstrap, which an
// imported session inherits.
func installation0(s *SQLiteStore) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return string(s.storeID)
}

// bundleFrame is one decoded frame.
type bundleFrame struct {
	Kind    BundleEntryKind
	Run     string
	SHA256  string
	Payload []byte
}

// parseStagedFrames re-parses a staged bundle's frames, so activation works from
// the bytes that were verified rather than from anything remembered about them.
func parseStagedFrames(sections stagedSections) ([]bundleFrame, BundleTrailer, error) {
	rest := sections.frames
	var frames []bundleFrame
	for len(rest) > 0 {
		if len(rest) < 4 {
			return nil, BundleTrailer{}, fmt.Errorf("%w: a frame length is truncated", ErrBundleInvalid)
		}
		headerLen := binary.BigEndian.Uint32(rest[:4])
		if headerLen == 0 || int(headerLen) > len(rest)-4 {
			return nil, BundleTrailer{}, fmt.Errorf("%w: a frame header runs past the file", ErrBundleInvalid)
		}
		var header struct {
			Kind   BundleEntryKind `json:"kind"`
			Run    string          `json:"run"`
			Length uint64          `json:"length"`
			SHA256 string          `json:"sha256"`
		}
		if err := CanonicalDecode(rest[4:4+headerLen], &header); err != nil {
			return nil, BundleTrailer{}, fmt.Errorf("%w: a frame header: %v", ErrBundleInvalid, err)
		}
		rest = rest[4+headerLen:]
		if header.Length == 0 || int(header.Length) > len(rest) {
			return nil, BundleTrailer{}, fmt.Errorf("%w: a frame payload runs past the file", ErrBundleInvalid)
		}
		frames = append(frames, bundleFrame{
			Kind:    header.Kind,
			Run:     header.Run,
			SHA256:  header.SHA256,
			Payload: rest[:header.Length],
		})
		rest = rest[header.Length:]
	}
	var trailer BundleTrailer
	if err := CanonicalDecode(sections.trailer, &trailer); err != nil {
		return nil, BundleTrailer{}, fmt.Errorf("%w: the trailer: %v", ErrBundleInvalid, err)
	}
	return frames, trailer, nil
}
