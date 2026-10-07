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
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Bundle format. One length-prefixed frame per object, a canonical header, and a
// trailer that binds itself to both.
//
// The design is deliberately one frame per object rather than a frame per chunk.
// Every object the store admits is already bounded by MaxRecordBytes or
// MaxTreeBytes, so sub-framing a single object would buy streaming of something
// that is bounded anyway, at the cost of a second length, a chunk index, and a
// second place for a digest to go missing.
//
// What the format proves is damage, not authorship. Nothing in it is signed, so a
// rewriter that recomputes every digest can substitute content. What it does prove
// is that a bundle was not truncated, that its frames were not reordered, and that
// a frame was not exchanged with another, because the record chain and the objects'
// own digests are claims a rewriter has to restate consistently across the whole
// file rather than one field at a time.
const (
	BundleFormatName    = "pabtdebug.bundle"
	BundleFormatVersion = uint32(1)

	// bundleMagic is the first eight bytes. A file that does not begin with it is
	// refused before anything is parsed.
	bundleMagic = "PABTDB1\n"

	// bundleTrailerMagic terminates the frames. Without it a trailer is
	// indistinguishable from a frame, because both are length-prefixed, and a
	// reader would parse the trailer's own members as a frame header.
	bundleTrailerMagic = "PABTEND1"

	// bundleMediaType is what a route serves and what an importer expects.
	bundleMediaType = "application/vnd.pabtdebug.bundle"

	maxBundleHeaderBytes  = 1 << 20
	maxBundleTrailerBytes = 1 << 20
)

// DomainBundle covers the whole file, so a bundle's digest changes if any byte
// inside it does.
const DomainBundle = "pabtdebug/bundle/v1"

// BundleEntryKind names what a frame carries.
type BundleEntryKind string

const (
	BundleEntryPlan    BundleEntryKind = "plan"
	BundleEntryRun     BundleEntryKind = "run"
	BundleEntryRecord  BundleEntryKind = "record"
	BundleEntryTree    BundleEntryKind = "tree"
	BundleEntryDelta   BundleEntryKind = "delta"
	BundleEntryCodec   BundleEntryKind = "codec"
	BundleEntryTrailer BundleEntryKind = "trailer"
)

// BundleEntryOrder fixes the order frames appear in, so the frames of two exports of
// the same history are byte-identical and a reader can rely on the sequence. The
// header is not part of that claim: it carries the export's time, so two exports of
// one history differ in their header and in the digest the header contributes to.
var BundleEntryOrder = []BundleEntryKind{
	BundleEntryPlan, BundleEntryRun,
	BundleEntryRecord, BundleEntryTree, BundleEntryDelta,
}

// BundleHeader identifies what a bundle is.
//
// Generation is the source store's generation at export. It travels as provenance:
// an import mints a session, a plan, and a run of its own, and publishes them at the
// local store's next generation, so the source generation is recorded rather than
// matched against. The sequence range, by contrast, is checked against the frames,
// because it is a claim about what the frames contain.
type BundleHeader struct {
	Format          string            `json:"format"`
	Version         uint32            `json:"version"`
	SourceStoreID   string            `json:"sourceStoreId"`
	SourceSessionID string            `json:"sourceSessionId"`
	SourcePlanID    string            `json:"sourcePlanId"`
	SourceRunID     string            `json:"sourceRunId"`
	Generation      uint64            `json:"generation"`
	FirstSequence   uint64            `json:"firstSequence"`
	ThroughSequence uint64            `json:"throughSequence"`
	EntryKinds      []BundleEntryKind `json:"entryKinds"`
	CreatedAt       Timestamp         `json:"createdAt"`
}

// BundleTrailer counts what the frames carried, so a truncated or reordered file
// is caught by the counts rather than by a reader noticing.
type BundleTrailer struct {
	HeaderSHA256  string                     `json:"headerSha256"`
	FrameCount    uint64                     `json:"frameCount"`
	Counts        map[BundleEntryKind]uint64 `json:"counts"`
	ContentSHA256 string                     `json:"contentSha256"`
}

// HashBundleHeader digests a header, which is what lets a trailer prove the
// header it was written beside is the one the frames were built for.
func HashBundleHeader(h BundleHeader) (string, error) {
	return HashCanonical(DomainBundle+"/header", h)
}

// HashBundleContent digests a byte range inside a bundle under the content
// domain, excluding the magic, the header length, and the trailer, so a trailer's
// own digest cannot be part of its input.
func HashBundleContent(frames []byte) string {
	return hashBytes(DomainBundle+"/content", frames)
}

// bundleDigestOf digests a whole bundle as the digests of its three sections.
//
// The sections are digested rather than carried, because the writer cannot hold a
// run's frames and a reader can recompute the same three digests while it streams.
// One projection for both is what makes an exported bundle and a staged copy of it
// agree without either side keeping the other's bytes.
func bundleDigestOf(header, frames, trailer string) (string, error) {
	return HashCanonical(DomainBundle, struct {
		Header  string `json:"header"`
		Frames  string `json:"frames"`
		Trailer string `json:"trailer"`
	}{header, frames, trailer})
}

// bundleWriter writes a bundle incrementally, so exporting a long history never
// holds it in memory.
type bundleWriter struct {
	w      io.Writer
	header []byte
	counts map[BundleEntryKind]uint64
	frames uint64
	// framed is digested as the frames go out, so the trailer's content digest can
	// be computed without holding the run. The bytes themselves are not buffered:
	// a run of any length has to stream, and at the default record and tree limits
	// buffering one would cost gigabytes for a short run.
	framed *streamDigest
}

// newBundleWriter writes the magic and the header, and leaves room for the
// trailer once the frames are known.
func newBundleWriter(w io.Writer, header BundleHeader) (*bundleWriter, error) {
	encoded, err := CanonicalEncode(header)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxBundleHeaderBytes {
		return nil, fmt.Errorf("%w: the bundle header is %d bytes", ErrValueUnencodable, len(encoded))
	}
	if _, err := io.WriteString(w, bundleMagic); err != nil {
		return nil, fmt.Errorf("pabtdebug: write a bundle: %w", err)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(encoded)))
	if _, err := w.Write(length[:]); err != nil {
		return nil, fmt.Errorf("pabtdebug: write a bundle: %w", err)
	}
	if _, err := w.Write(encoded); err != nil {
		return nil, fmt.Errorf("pabtdebug: write a bundle: %w", err)
	}
	return &bundleWriter{
		w:      w,
		header: encoded,
		counts: map[BundleEntryKind]uint64{},
		framed: newStreamDigest(DomainBundle + "/content"),
	}, nil
}

// add writes one frame.
//
// The frame's digest is of the bytes it carries, so a frame is self-checking
// without the reader having to know what kind of object it holds. The trailer's
// content digest is unkeyed and a rewriter recomputes it for free, so it cannot be
// the thing that makes a frame self-checking.
func (b *bundleWriter) add(kind BundleEntryKind, run string, payload []byte) error {
	digest := HashBundleContent(payload)
	header, err := CanonicalEncode(struct {
		Kind   BundleEntryKind `json:"kind"`
		Run    string          `json:"run"`
		Length uint64          `json:"length"`
		SHA256 string          `json:"sha256"`
	}{kind, run, uint64(len(payload)), digest})
	if err != nil {
		return err
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(header)))
	for _, section := range [][]byte{length[:], header, payload} {
		b.framed.add(section)
		if _, err := b.w.Write(section); err != nil {
			return err
		}
	}
	b.counts[kind]++
	b.frames++
	return nil
}

// close writes the trailer, which ends the frames the writer has already written,
// and returns the bundle's digest.
func (b *bundleWriter) close(header BundleHeader) (string, error) {
	headerDigest, err := HashBundleHeader(header)
	if err != nil {
		return "", err
	}
	contentDigest := b.framed.sum()
	trailer := BundleTrailer{
		HeaderSHA256:  headerDigest,
		FrameCount:    b.frames,
		Counts:        b.counts,
		ContentSHA256: contentDigest,
	}
	encoded, err := CanonicalEncode(trailer)
	if err != nil {
		return "", err
	}
	if len(encoded) > maxBundleTrailerBytes {
		return "", fmt.Errorf("%w: the bundle trailer is %d bytes", ErrValueUnencodable, len(encoded))
	}
	if _, err := io.WriteString(b.w, bundleTrailerMagic); err != nil {
		return "", fmt.Errorf("pabtdebug: write a bundle: %w", err)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(encoded)))
	if _, err := b.w.Write(length[:]); err != nil {
		return "", fmt.Errorf("pabtdebug: write a bundle: %w", err)
	}
	if _, err := b.w.Write(encoded); err != nil {
		return "", fmt.Errorf("pabtdebug: write a bundle: %w", err)
	}
	// The bundle's own digest covers the header, the frames, and the trailer, so any
	// change to any of them changes it. The framed bytes are digested as they are
	// written, so this is a digest of what went out rather than a second copy of it.
	return bundleDigestOf(HashBundleContent(b.header), b.framed.sum(), HashBundleContent(encoded))
}

// ExportRequest selects what an export covers.
type ExportRequest struct {
	// Key is the run to export. A zero key is refused rather than exporting
	// nothing, because an empty bundle that looks successful is the worst outcome.
	Key RunKey

	// Through, when set, exports up to and including that sequence.
	Through *uint64
}

// ExportArtifact is a prepared bundle on disk, owned by the caller until it is
// released or discarded.
//
// It is a file rather than a reader because an export of a long history is
// written once and may be copied much later, and holding it in memory would
// defeat the point of a durable store.
type ExportArtifact struct {
	path     string
	receipt  ExportReceipt
	taken    bool
	released bool
}

// ExportReceipt reports what an export covered.
type ExportReceipt struct {
	SourceStoreID   string `json:"sourceStoreId"`
	SourceSessionID string `json:"sourceSessionId"`
	SourceRunID     string `json:"sourceRunId"`
	Generation      uint64 `json:"generation"`
	FirstSequence   uint64 `json:"firstSequence"`
	ThroughSequence uint64 `json:"throughSequence"`
	RecordCount     uint64 `json:"recordCount"`
	TreeCount       uint64 `json:"treeCount"`
	DeltaCount      uint64 `json:"deltaCount"`
	Bytes           uint64 `json:"bytes"`
	BundlesSHA256   string `json:"bundleSha256"`
}

// Open returns the bundle's bytes.
//
// It is single-use. A second Open is refused rather than handing out a reader
// whose position no longer corresponds to a transferred file.
func (a *ExportArtifact) Open() (io.ReadCloser, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: no export artifact", ErrNotFound)
	}
	if a.released {
		return nil, fmt.Errorf("%w: the export artifact is already released", ErrNotFound)
	}
	if a.taken {
		return nil, fmt.Errorf("%w: the export artifact has already been read", ErrNotFound)
	}
	file, err := os.Open(a.path)
	if err != nil {
		return nil, fmt.Errorf("pabtdebug: open an export: %w", err)
	}
	a.taken = true
	return file, nil
}

// Receipt reports what the export covered.
func (a *ExportArtifact) Receipt() ExportReceipt { return a.receipt }

// Release removes the artifact and is idempotent. It runs whether or not the copy
// succeeded, so a failed transfer leaves nothing behind.
func (a *ExportArtifact) Release() error { return a.remove("release") }

// Discard removes an artifact that was never handed out.
func (a *ExportArtifact) Discard() error { return a.remove("discard") }

func (a *ExportArtifact) remove(what string) error {
	if a == nil || a.released {
		return nil
	}
	a.released = true
	if err := os.Remove(a.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pabtdebug: %s an export: %w", what, err)
	}
	return nil
}

// Export writes a run's committed history to a bundle on disk.
//
// The whole history is read in bounded pages and written frame by frame, so a
// long run is never held in memory. The bundle is prepared completely before it is
// returned, so a caller that receives an artifact has a whole file, not a prefix.
func (s *SQLiteStore) Export(ctx context.Context, req ExportRequest) (*ExportArtifact, error) {
	if err := s.checkUsable(false); err != nil {
		return nil, err
	}
	key := req.Key
	if key.IsZero() {
		return nil, fmt.Errorf("%w: an export needs a session, a plan, and a run", ErrNotFound)
	}
	head, err := s.Head(ctx, key)
	if err != nil {
		return nil, err
	}
	through := head.HeadSequence
	if req.Through != nil {
		through = *req.Through
		if through > head.HeadSequence {
			return nil, fmt.Errorf("%w: sequence %d is beyond the run's head %d",
				ErrHeadMismatch, through, head.HeadSequence)
		}
	}
	if through == 0 {
		return nil, fmt.Errorf("%w: the run has no records to export", ErrNotFound)
	}

	path, err := s.tempPath("export")
	if err != nil {
		return nil, err
	}
	// A partially written bundle is removed on every failure path, so a failed
	// export leaves nothing that could be mistaken for a whole one.
	fail := func(err error) (*ExportArtifact, error) {
		_ = os.Remove(path)
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return fail(fmt.Errorf("pabtdebug: create an export: %w", err))
	}
	defer func() { _ = file.Close() }()

	installation, err := s.Installation(ctx)
	if err != nil {
		return fail(err)
	}
	header := BundleHeader{
		Format:          BundleFormatName,
		Version:         BundleFormatVersion,
		SourceStoreID:   string(installation.StoreID),
		SourceSessionID: key.SessionID,
		SourcePlanID:    key.PlanID,
		SourceRunID:     key.RunID,
		Generation:      installation.Generation,
		FirstSequence:   1,
		ThroughSequence: through,
		EntryKinds:      BundleEntryOrder,
		CreatedAt:       Timestamp(s.clock().UTC()),
	}
	buffered := bufio.NewWriterSize(file, 1<<16)
	writer, err := newBundleWriter(buffered, header)
	if err != nil {
		return fail(err)
	}
	runLabel := key.SessionID + "/" + key.PlanID + "/" + key.RunID

	plan, err := s.readPlan(ctx, key)
	if err != nil {
		return fail(err)
	}
	planJSON, err := CanonicalEncode(plan)
	if err != nil {
		return fail(err)
	}
	planDigest, err := HashCanonical(DomainPlan, plan)
	if err != nil {
		return fail(err)
	}
	plan.RecordSHA256 = planDigest
	planJSON, err = CanonicalEncode(plan)
	if err != nil {
		return fail(err)
	}
	if err := writer.add(BundleEntryPlan, runLabel, planJSON); err != nil {
		return fail(err)
	}

	// The run frame describes the run as the store holds it, not a run invented for
	// the bundle: an importer that reports the source's start and state is reporting
	// something true, and one that reports the epoch is reporting nothing.
	run, err := s.readRun(ctx, key)
	if err != nil {
		return fail(err)
	}
	// The head is this bundle's, so a bounded export names the record it ends at
	// rather than the run's own head, which it may not carry. The head tree is left
	// empty for the same reason, and an importer derives its own head from the trees
	// it wrote.
	tail, err := s.RecordAt(ctx, key, through)
	if err != nil {
		return fail(err)
	}
	run.HeadSequence = through
	run.HeadRecordSHA256 = tail.RecordSHA256
	run.HeadTreeSequence = 0
	run.HeadTreeEventUID = ""
	run.HeadTreeDigest = ""
	runJSON, err := CanonicalEncode(run)
	if err != nil {
		return fail(err)
	}
	runDigest, err := HashRun(run)
	if err != nil {
		return fail(err)
	}
	run.RecordSHA256 = runDigest
	runJSON, err = CanonicalEncode(run)
	if err != nil {
		return fail(err)
	}
	if err := writer.add(BundleEntryRun, runLabel, runJSON); err != nil {
		return fail(err)
	}

	receipt := ExportReceipt{
		SourceStoreID:   header.SourceStoreID,
		SourceSessionID: key.SessionID,
		SourceRunID:     key.RunID,
		Generation:      installation.Generation,
		FirstSequence:   1,
		ThroughSequence: through,
	}
	for sequence := uint64(1); sequence <= through; sequence++ {
		record, err := s.RecordAt(ctx, key, sequence)
		if err != nil {
			return fail(err)
		}
		encoded, err := CanonicalEncode(record)
		if err != nil {
			return fail(err)
		}
		if err := writer.add(BundleEntryRecord, runLabel, encoded); err != nil {
			return fail(err)
		}
		receipt.RecordCount++

		tree, base, delta, err := s.Representation(ctx, key, sequence)
		switch {
		case errors.Is(err, ErrNotFound):
			// A record with no tree is normal, so this is not a failure. Any other
			// error is the store failing to read a representation it holds, and
			// continuing would produce a bundle that describes a history the store
			// cannot hand over, which is not a bundle an importer could accept.
			continue
		case err != nil:
			return fail(err)
		}
		treeJSON, err := CanonicalEncode(tree)
		if err != nil {
			return fail(err)
		}
		if err := writer.add(BundleEntryTree, runLabel, treeJSON); err != nil {
			return fail(err)
		}
		receipt.TreeCount++
		if delta != nil {
			deltaJSON, err := CanonicalEncode(*delta)
			if err != nil {
				return fail(err)
			}
			if err := writer.add(BundleEntryDelta, runLabel, deltaJSON); err != nil {
				return fail(err)
			}
			receipt.DeltaCount++
		}
		_ = base
	}

	digest, err := writer.close(header)
	if err != nil {
		return fail(err)
	}
	if err := buffered.Flush(); err != nil {
		return fail(fmt.Errorf("pabtdebug: flush an export: %w", err))
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("pabtdebug: sync an export: %w", err))
	}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	receipt.Bytes = uint64(info.Size())
	receipt.BundlesSHA256 = digest
	return &ExportArtifact{path: path, receipt: receipt}, nil
}

// readPlan returns the plan a run belongs to.
func (s *SQLiteStore) readPlan(ctx context.Context, key RunKey) (StoredPlan, error) {
	var (
		title    string
		metadata []byte
		created  string
	)
	row := s.readers.QueryRowContext(ctx,
		"SELECT title, metadata_json, created_at FROM plans WHERE session_id = ? AND plan_id = ?",
		key.SessionID, key.PlanID)
	switch err := row.Scan(&title, &metadata, &created); {
	case errors.Is(err, sql.ErrNoRows):
		return StoredPlan{}, fmt.Errorf("%w: plan %s", ErrNotFound, key.PlanID)
	case err != nil:
		return StoredPlan{}, fmt.Errorf("pabtdebug: read a plan: %w", err)
	}
	plan := StoredPlan{PlanID: key.PlanID, Title: title}
	if len(metadata) > 0 {
		if err := CanonicalDecode(metadata, &plan.Metadata); err != nil {
			return StoredPlan{}, fmt.Errorf("%w: plan metadata: %v", ErrStoreCorrupt, err)
		}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, created); err == nil {
		plan.CreatedAt = Timestamp(parsed.UTC())
	}
	return plan, nil
}

// readRun returns the run row as it is stored.
func (s *SQLiteStore) readRun(ctx context.Context, key RunKey) (StoredRun, error) {
	var (
		state      string
		started    string
		ended      sql.NullString
		initial    int
		headSeq    int
		headRecord []byte
	)
	row := s.readers.QueryRowContext(ctx,
		"SELECT state, started_at, ended_at, initial_plan_revision, head_seq, head_record_sha256 "+
			"FROM runs WHERE session_id = ? AND plan_id = ? AND run_id = ?",
		key.SessionID, key.PlanID, key.RunID)
	switch err := row.Scan(&state, &started, &ended, &initial, &headSeq, &headRecord); {
	case errors.Is(err, sql.ErrNoRows):
		return StoredRun{}, fmt.Errorf("%w: run %s", ErrNotFound, key.RunID)
	case err != nil:
		return StoredRun{}, fmt.Errorf("pabtdebug: read a run: %w", err)
	}
	run := StoredRun{
		RunKey:              key,
		State:               state,
		InitialPlanRevision: uint64(initial),
		HeadSequence:        uint64(headSeq),
	}
	if len(headRecord) > 0 {
		run.HeadRecordSHA256 = hex.EncodeToString(headRecord)
	}
	if parsed, err := time.Parse(time.RFC3339Nano, started); err == nil {
		run.StartedAt = Timestamp(parsed.UTC())
	}
	if ended.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, ended.String); err == nil {
			stamp := Timestamp(parsed.UTC())
			run.EndedAt = &stamp
		}
	}
	return run, nil
}

// tempPath returns a fresh owner-only path beside the store.
func (s *SQLiteStore) tempPath(kind string) (string, error) {
	dir := s.path + ".artifacts"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("pabtdebug: artifact directory: %w", err)
	}
	uid, err := randomID(kind)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, uid+".pabt"), nil
}
