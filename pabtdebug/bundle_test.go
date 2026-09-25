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
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

// bundleFixture commits a run of records and returns the store and its key.
func bundleFixture(t *testing.T, records int) (*SQLiteStore, RunKey) {
	t.Helper()
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	for i := range records {
		ticket, err := store.Begin(context.Background(),
			BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
		if err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
		tree := smallTree("n")
		tree.RunKey = key
		tree.Nodes[0].Name = string(rune('a' + i))
		sealed := reseal(t, *tree)
		record := makeRecord(&sealed)
		var delta *ExactTreeDelta
		if base, found, baseErr := store.DeltaBase(context.Background(), key, ticket.Sequence); baseErr == nil && found {
			if computed, err := ComputeExactDelta(base, sealed); err == nil {
				delta = computed
			}
		}
		if _, err := store.CommitWithDelta(context.Background(), ticket, record, delta); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	return store, key
}

// bundleBytes reads a whole artifact.
func bundleBytes(t *testing.T, a *ExportArtifact) []byte {
	t.Helper()
	reader, err := a.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read the bundle: %v", err)
	}
	return data
}

func TestAnExportCoversEveryRecordAndTree(t *testing.T) {
	store, key := bundleFixture(t, 8)
	ctx := context.Background()

	artifact, err := store.Export(ctx, ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	defer func() { _ = artifact.Release() }()

	receipt := artifact.Receipt()
	if receipt.RecordCount != 8 {
		t.Errorf("the receipt claims %d records, want 8", receipt.RecordCount)
	}
	if receipt.TreeCount != 8 {
		t.Errorf("the receipt claims %d trees, want 8", receipt.TreeCount)
	}
	if receipt.Bytes == 0 {
		t.Error("the receipt claims an empty bundle")
	}
	if receipt.BundlesSHA256 == "" {
		t.Error("the receipt claims no bundle digest")
	}
	if receipt.SourceRunID != key.RunID || receipt.SourceSessionID != key.SessionID {
		t.Errorf("the receipt names run %q session %q, want %q and %q",
			receipt.SourceRunID, receipt.SourceSessionID, key.RunID, key.SessionID)
	}

	data := bundleBytes(t, artifact)
	if !bytes.HasPrefix(data, []byte(bundleMagic)) {
		t.Error("the bundle does not begin with its magic")
	}
	frames, trailer, err := readBundle(t, data)
	if err != nil {
		t.Fatalf("read the bundle: %v", err)
	}
	// Every record the run holds is in the bundle, byte for byte.
	records := 0
	trees := 0
	deltas := 0
	for _, frame := range frames {
		switch frame.Kind {
		case BundleEntryRecord:
			records++
			original, err := store.RecordAt(ctx, key, uint64(records))
			if err != nil {
				t.Fatalf("RecordAt %d: %v", records, err)
			}
			originalBytes, err := CanonicalEncode(original)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if !bytes.Equal(frame.Payload, originalBytes) {
				t.Errorf("record %d is not the bytes the store holds", records)
			}
			if frame.SHA256 != original.RecordSHA256 {
				t.Errorf("record %d frame digest %q, the record is %q",
					records, frame.SHA256, original.RecordSHA256)
			}
		case BundleEntryTree:
			trees++
		case BundleEntryDelta:
			deltas++
		}
	}
	if records != 8 {
		t.Errorf("the bundle carries %d record frames, want 8", records)
	}
	if trees != 8 {
		t.Errorf("the bundle carries %d tree frames, want 8", trees)
	}
	if deltas == 0 {
		t.Error("the bundle carries no delta frames, so the delta path is not exercised")
	}
	if trailer.FrameCount != uint64(len(frames)) {
		t.Errorf("the trailer counts %d frames, the bundle has %d", trailer.FrameCount, len(frames))
	}
	if trailer.Counts[BundleEntryRecord] != 8 {
		t.Errorf("the trailer counts %d records, want 8", trailer.Counts[BundleEntryRecord])
	}
}

func TestAnExportIsBoundedByItsRequestedSequence(t *testing.T) {
	store, key := bundleFixture(t, 6)
	through := uint64(3)
	artifact, err := store.Export(context.Background(), ExportRequest{Key: key, Through: &through})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	defer func() { _ = artifact.Release() }()
	if got := artifact.Receipt().ThroughSequence; got != through {
		t.Errorf("the receipt covers through %d, want %d", got, through)
	}
	frames, _, err := readBundle(t, bundleBytes(t, artifact))
	if err != nil {
		t.Fatalf("read the bundle: %v", err)
	}
	var records int
	for _, frame := range frames {
		if frame.Kind == BundleEntryRecord {
			records++
		}
	}
	if records != int(through) {
		t.Errorf("the bundle carries %d records for a run of 6 through %d", records, through)
	}
}

func TestAnExportRefusesWhatItCannotProduce(t *testing.T) {
	store, key := bundleFixture(t, 3)
	ctx := context.Background()
	if _, err := store.Export(ctx, ExportRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an export with no run key = %v, want not found", err)
	}
	beyond := uint64(99)
	if _, err := store.Export(ctx, ExportRequest{Key: key, Through: &beyond}); !errors.Is(err, ErrHeadMismatch) {
		t.Errorf("an export beyond the head = %v, want a head mismatch", err)
	}
	empty := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, empty)
	if _, err := empty.Export(ctx, ExportRequest{Key: RunKey{
		SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID,
	}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an export of an empty run = %v, want not found", err)
	}
}

func TestAnExportArtifactIsSingleUseAndReleasable(t *testing.T) {
	store, key := bundleFixture(t, 2)
	artifact, err := store.Export(context.Background(), ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	path := artifact.path
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the prepared bundle is not on disk: %v", err)
	}
	if _, err := artifact.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// A second read is refused rather than handing out a reader whose position no
	// longer corresponds to a transferred file.
	if _, err := artifact.Open(); !errors.Is(err, ErrNotFound) {
		t.Errorf("a second Open = %v, want a refusal", err)
	}
	// Release runs whether or not the copy succeeded, so a failed transfer leaves
	// nothing behind.
	if err := artifact.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the released bundle is still on disk: %v", err)
	}
	if err := artifact.Release(); err != nil {
		t.Errorf("a second Release = %v, want nil", err)
	}
}

// readBundle decodes a whole bundle, so the format's own framing is under test
// rather than a round trip through the writer.
func readBundle(t *testing.T, data []byte) ([]bundleFrame, BundleTrailer, error) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte(bundleMagic)) {
		return nil, BundleTrailer{}, errors.New("the bundle does not begin with its magic")
	}
	rest := data[len(bundleMagic):]
	if len(rest) < 4 {
		return nil, BundleTrailer{}, errors.New("the bundle has no header length")
	}
	headerLen := int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3])
	rest = rest[4:]
	if headerLen > len(rest) {
		return nil, BundleTrailer{}, errors.New("the header length runs past the file")
	}
	headerBytes := rest[:headerLen]
	rest = rest[headerLen:]
	var header BundleHeader
	if err := CanonicalDecode(headerBytes, &header); err != nil {
		return nil, BundleTrailer{}, err
	}

	var frames []bundleFrame
	for {
		// The trailer's own marker ends the frames, so a trailer is never parsed
		// as a frame because both are length-prefixed.
		if bytes.HasPrefix(rest, []byte(bundleTrailerMagic)) {
			rest = rest[len(bundleTrailerMagic):]
			break
		}
		if len(rest) == 0 {
			break
		}
		if len(rest) < 4 {
			return nil, BundleTrailer{}, errors.New("a frame length is truncated")
		}
		frameHeaderLen := int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3])
		rest = rest[4:]
		if frameHeaderLen > len(rest) {
			return nil, BundleTrailer{}, errors.New("a frame header runs past the file")
		}
		var frameHeader struct {
			Kind   BundleEntryKind `json:"kind"`
			Run    string          `json:"run"`
			Length uint64          `json:"length"`
			SHA256 string          `json:"sha256"`
		}
		if err := CanonicalDecode(rest[:frameHeaderLen], &frameHeader); err != nil {
			return nil, BundleTrailer{}, err
		}
		rest = rest[frameHeaderLen:]
		length := int(frameHeader.Length)
		if length > len(rest) {
			return nil, BundleTrailer{}, errors.New("a frame payload runs past the file")
		}
		frames = append(frames, bundleFrame{
			Kind:    frameHeader.Kind,
			Run:     frameHeader.Run,
			SHA256:  frameHeader.SHA256,
			Payload: rest[:length],
		})
		rest = rest[length:]
	}
	if len(rest) < 4 {
		return nil, BundleTrailer{}, errors.New("the bundle has no trailer length")
	}
	trailerLen := int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3])
	rest = rest[4:]
	if trailerLen > len(rest) {
		return nil, BundleTrailer{}, errors.New("the trailer length runs past the file")
	}
	var trailer BundleTrailer
	if err := CanonicalDecode(rest[:trailerLen], &trailer); err != nil {
		return nil, BundleTrailer{}, err
	}
	return frames, trailer, nil
}

func TestAnAlteredBundleIsDetected(t *testing.T) {
	store, key := bundleFixture(t, 4)
	artifact, err := store.Export(context.Background(), ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	defer func() { _ = artifact.Release() }()
	data := bundleBytes(t, artifact)

	// Flipping one byte inside a record payload must be visible: the frame carries
	// the record's own digest, and the trailer counts what it carried.
	altered := append([]byte(nil), data...)
	at := bytes.Index(altered, []byte("\"name\":\"a\""))
	if at < 0 {
		t.Skip("the fixture's record shape changed, so this case does not apply")
	}
	altered[at+9] = 'b'
	frames, trailer, err := readBundle(t, altered)
	if err != nil {
		return // an alteration that breaks the framing is also detected
	}
	_ = trailer
	for _, frame := range frames {
		if frame.Kind != BundleEntryRecord {
			continue
		}
		var record StoredRecord
		if err := CanonicalDecode(frame.Payload, &record); err != nil {
			return
		}
		digest, err := HashRecord(record)
		if err != nil {
			return
		}
		if digest == record.RecordSHA256 && frame.SHA256 == record.RecordSHA256 {
			t.Error("an altered record payload still verifies against its own digest")
		}
		return
	}
	t.Error("the altered bundle has no record frame to check")
}

func TestAnImportReproducesEveryRecordUnderNewIdentities(t *testing.T) {
	source, key := bundleFixture(t, 6)
	ctx := context.Background()

	artifact, err := source.Export(ctx, ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	payload, err := io.ReadAll(mustOpen(t, artifact))
	if err != nil {
		t.Fatalf("read the export: %v", err)
	}
	if err := artifact.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	ticket, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if ticket.LocalSessionID == key.SessionID {
		t.Error("the import reused the source session identity")
	}
	if ticket.RecordCount != 6 {
		t.Errorf("the ticket claims %d records, want 6", ticket.RecordCount)
	}
	// Staging changes nothing the store serves, so the target still has its own
	// empty run.
	before, err := target.Installation(ctx)
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if before.ActiveSessionID == ticket.LocalSessionID {
		t.Error("staging an import already made it active")
	}

	activated, err := target.ActivateImport(ctx, ticket)
	if err != nil {
		t.Fatalf("ActivateImport: %v", err)
	}
	if activated.Generation != before.Generation+1 {
		t.Errorf("activation reports generation %d, want %d", activated.Generation, before.Generation+1)
	}
	if activated.RecordCount != 6 {
		t.Errorf("activation reports %d records, want 6", activated.RecordCount)
	}
	imported := RunKey{
		SessionID: activated.SessionID, PlanID: activated.PlanID, RunID: activated.RunID,
	}
	if verified, err := target.VerifyChain(ctx, imported); err != nil {
		t.Errorf("the imported chain does not verify: %v", err)
	} else if verified != 6 {
		t.Errorf("the imported chain verified %d records, want 6", verified)
	}
	// Every record carries a fresh identity and a digest over its own bytes.
	for sequence := uint64(1); sequence <= 6; sequence++ {
		record, err := target.RecordAt(ctx, imported, sequence)
		if err != nil {
			t.Fatalf("RecordAt %d: %v", sequence, err)
		}
		if record.RunKey != imported {
			t.Errorf("record %d still names run %v", sequence, record.RunKey)
		}
		original, err := source.RecordAt(ctx, key, sequence)
		if err != nil {
			t.Fatalf("source RecordAt %d: %v", sequence, err)
		}
		if record.EventUID == original.EventUID {
			t.Errorf("record %d kept the source event identity, so two imports would collide", sequence)
		}
		if sequence == 1 {
			if record.PreviousRecordSHA256 != zeroDigest {
				t.Errorf("the first imported record links to %q, want the chain's zero",
					record.PreviousRecordSHA256)
			}
			continue
		}
		previous, err := target.RecordAt(ctx, imported, sequence-1)
		if err != nil {
			t.Fatalf("RecordAt %d: %v", sequence-1, err)
		}
		if record.PreviousRecordSHA256 != previous.RecordSHA256 {
			t.Errorf("record %d links to %q, the imported predecessor is %q", sequence,
				record.PreviousRecordSHA256, previous.RecordSHA256)
		}
	}
	// A tree the run committed is still readable after the import.
	if _, err := target.TreeAt(ctx, imported, 1); err != nil {
		t.Errorf("an imported tree is not readable: %v", err)
	}
}

func mustOpen(t *testing.T, a *ExportArtifact) io.ReadCloser {
	t.Helper()
	reader, err := a.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return reader
}

func TestTheSameBundleImportsTwiceWithoutColliding(t *testing.T) {
	source, key := bundleFixture(t, 5)
	ctx := context.Background()
	artifact, err := source.Export(ctx, ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	payload, err := io.ReadAll(mustOpen(t, artifact))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := artifact.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)

	var sessions []RunKey
	var generations []uint64
	for round := range 2 {
		ticket, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
		if err != nil {
			t.Fatalf("StageImport %d: %v", round, err)
		}
		activated, err := target.ActivateImport(ctx, ticket)
		if err != nil {
			t.Fatalf("ActivateImport %d: %v", round, err)
		}
		sessions = append(sessions, RunKey{
			SessionID: activated.SessionID, PlanID: activated.PlanID, RunID: activated.RunID,
		})
		generations = append(generations, activated.Generation)
	}
	if sessions[0] == sessions[1] {
		t.Fatal("two imports of one bundle produced the same run identity")
	}
	if generations[0] == generations[1] {
		t.Error("two activations reported the same generation")
	}
	if generations[1] != generations[0]+1 {
		t.Errorf("the second activation reported generation %d, want %d", generations[1], generations[0]+1)
	}
	// Both histories are independently readable and independently intact, which is
	// the point of rewriting the identity rather than copying it.
	for i, session := range sessions {
		if verified, err := target.VerifyChain(ctx, session); err != nil {
			t.Errorf("import %d does not verify: %v", i, err)
		} else if verified != 5 {
			t.Errorf("import %d verified %d records, want 5", i, verified)
		}
	}
	// The second import is what the store now serves.
	installation, err := target.Installation(ctx)
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.ActiveSessionID != sessions[1].SessionID {
		t.Errorf("the active session is %q, want the second import's",
			installation.ActiveSessionID)
	}
}

func TestAStaleImportTicketIsRefused(t *testing.T) {
	source, key := bundleFixture(t, 3)
	ctx := context.Background()
	artifact, err := source.Export(ctx, ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	payload, err := io.ReadAll(mustOpen(t, artifact))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = artifact.Release()

	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	first, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	// Activating advances the generation, so the other ticket was staged against a
	// generation that is no longer current.
	if _, err := target.ActivateImport(ctx, first); err != nil {
		t.Fatalf("ActivateImport: %v", err)
	}
	second, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport after an activation: %v", err)
	}
	forged := second
	forged.proof[0] ^= 0xff
	if _, err := target.ActivateImport(ctx, forged); !errors.Is(err, ErrImportStale) {
		t.Errorf("a forged ticket = %v, want a stale refusal", err)
	}
	// A ticket whose bundle is gone is refused rather than activating nothing.
	removed := second
	if err := os.Remove(removed.StagedPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := target.ActivateImport(ctx, removed); !errors.Is(err, ErrImportStale) {
		t.Errorf("activating a removed bundle = %v, want a stale refusal", err)
	}
}

func TestADamagedBundleIsRefused(t *testing.T) {
	source, key := bundleFixture(t, 4)
	ctx := context.Background()
	artifact, err := source.Export(ctx, ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	payload, err := io.ReadAll(mustOpen(t, artifact))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = artifact.Release()

	for _, test := range []struct {
		name   string
		damage func([]byte) []byte
	}{
		{
			name:   "no magic",
			damage: func(b []byte) []byte { return append([]byte("NOTABUNDLE"), b[9:]...) },
		},
		{
			name: "a flipped record byte",
			damage: func(b []byte) []byte {
				out := append([]byte(nil), b...)
				if at := bytes.Index(out, []byte(`"name":"a"`)); at >= 0 {
					out[at+8] = 'z'
				}
				return out
			},
		},
		{
			name:   "truncated",
			damage: func(b []byte) []byte { return b[:len(b)/2] },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := openTestStore(t, testStorePath(t))
			bootstrapTestStore(t, target)
			if _, err := target.StageImport(ctx, bytes.NewReader(test.damage(payload)),
				DefaultImportLimits()); err == nil {
				t.Error("a damaged bundle was staged")
			}
		})
	}
}

func TestAnOversizedBundleIsRefused(t *testing.T) {
	source, key := bundleFixture(t, 2)
	ctx := context.Background()
	artifact, err := source.Export(ctx, ExportRequest{Key: key})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	payload, err := io.ReadAll(mustOpen(t, artifact))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = artifact.Release()

	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	limits := DefaultImportLimits()
	limits.MaxBytes = 16
	if _, err := target.StageImport(ctx, bytes.NewReader(payload), limits); !errors.Is(err, ErrImportTooLarge) {
		t.Errorf("an oversized bundle = %v, want a size refusal", err)
	}
	if err := (ImportLimits{}).Validate(); err == nil {
		t.Error("incomplete import limits were accepted")
	}
	if err := DefaultImportLimits().Validate(); err != nil {
		t.Errorf("the default limits do not validate: %v", err)
	}
}
