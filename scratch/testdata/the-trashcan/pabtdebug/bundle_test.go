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
	"encoding/binary"
	"errors"
	"fmt"
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
	_, frames, trailer, err := readBundle(t, data)
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
			// The frame's digest is of the bytes it carries, so a reader can check
			// a frame without knowing what kind of object it holds.
			if got := HashBundleContent(frame.Payload); got != frame.SHA256 {
				t.Errorf("record %d frame digest %q, the bytes it carries hash to %q",
					records, frame.SHA256, got)
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
	_, frames, _, err := readBundle(t, bundleBytes(t, artifact))
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

// parsedFrame is a frame decoded by the test's own parser, including the two header
// fields production has no use for. Reading the format independently of the writer is
// the point: a round trip through the writer cannot tell a framing change from a
// writer change.

type parsedFrame struct {
	Kind    BundleEntryKind
	Run     string
	SHA256  string
	Payload []byte
}

// readBundle decodes a whole bundle, so the format's own framing is under test
// rather than a round trip through the writer.

func readBundle(t *testing.T, data []byte) (BundleHeader, []parsedFrame, BundleTrailer, error) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte(bundleMagic)) {
		return BundleHeader{}, nil, BundleTrailer{}, errors.New("the bundle does not begin with its magic")
	}
	rest := data[len(bundleMagic):]
	if len(rest) < 4 {
		return BundleHeader{}, nil, BundleTrailer{}, errors.New("the bundle has no header length")
	}
	headerLen := int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3])
	rest = rest[4:]
	if headerLen > len(rest) {
		return BundleHeader{}, nil, BundleTrailer{}, errors.New("the header length runs past the file")
	}
	headerBytes := rest[:headerLen]
	rest = rest[headerLen:]
	var header BundleHeader
	if err := CanonicalDecode(headerBytes, &header); err != nil {
		return BundleHeader{}, nil, BundleTrailer{}, err
	}

	var frames []parsedFrame
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
			return BundleHeader{}, nil, BundleTrailer{}, errors.New("a frame length is truncated")
		}
		frameHeaderLen := int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3])
		rest = rest[4:]
		if frameHeaderLen > len(rest) {
			return BundleHeader{}, nil, BundleTrailer{}, errors.New("a frame header runs past the file")
		}
		var frameHeader struct {
			Kind   BundleEntryKind `json:"kind"`
			Run    string          `json:"run"`
			Length uint64          `json:"length"`
			SHA256 string          `json:"sha256"`
		}
		if err := CanonicalDecode(rest[:frameHeaderLen], &frameHeader); err != nil {
			return BundleHeader{}, nil, BundleTrailer{}, err
		}
		rest = rest[frameHeaderLen:]
		length := int(frameHeader.Length)
		if length > len(rest) {
			return BundleHeader{}, nil, BundleTrailer{}, errors.New("a frame payload runs past the file")
		}
		frames = append(frames, parsedFrame{
			Kind:    frameHeader.Kind,
			Run:     frameHeader.Run,
			SHA256:  frameHeader.SHA256,
			Payload: rest[:length],
		})
		rest = rest[length:]
	}
	if len(rest) < 4 {
		return BundleHeader{}, nil, BundleTrailer{}, errors.New("the bundle has no trailer length")
	}
	trailerLen := int(rest[0])<<24 | int(rest[1])<<16 | int(rest[2])<<8 | int(rest[3])
	rest = rest[4:]
	if trailerLen > len(rest) {
		return BundleHeader{}, nil, BundleTrailer{}, errors.New("the trailer length runs past the file")
	}
	var trailer BundleTrailer
	if err := CanonicalDecode(rest[:trailerLen], &trailer); err != nil {
		return BundleHeader{}, nil, BundleTrailer{}, err
	}
	return header, frames, trailer, nil
}

func TestAnAlteredFrameIsRefusedByStaging(t *testing.T) {
	// The earlier version of this test re-derived SHA-256 in its own body and
	// returned silently on any failure, so it passed with every verification
	// removed from the reader. This one goes through staging, which is the
	// production check, and fails when the check does not fire.
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
	if err := artifact.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Exchanging two record payloads leaves each frame carrying a record whose own
	// digest does not describe it, which is what an alteration in transit and a
	// buggy rewriter both look like.
	header, frames, _, err := readBundle(t, payload)
	if err != nil {
		t.Fatalf("read the bundle: %v", err)
	}
	var first, second = -1, -1
	for i, frame := range frames {
		if frame.Kind != BundleEntryRecord {
			continue
		}
		switch {
		case first < 0:
			first = i
		case second < 0:
			second = i
		}
	}
	if first < 0 || second < 0 {
		t.Fatal("the bundle does not hold two record frames to exchange")
	}
	frames[first].Payload, frames[second].Payload = frames[second].Payload, frames[first].Payload

	// The bundle is rebuilt with the same writer, so its trailer and every frame
	// digest are internally consistent and only the object's own digest and the
	// chain can catch this.
	rebuilt := rebuildBundle(t, header, frames)

	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	if _, err := target.StageImport(ctx, bytes.NewReader(rebuilt), DefaultImportLimits()); err == nil {
		t.Error("a bundle whose record payloads were exchanged was staged")
	} else if !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("an exchanged bundle = %v, want an invalid bundle", err)
	}
}

// rebuildBundle writes frames back out with the production writer, so a bundle a
// test damaged is still internally consistent and only a check about content can
// refuse it.

func rebuildBundle(t *testing.T, header BundleHeader, frames []parsedFrame) []byte {
	t.Helper()
	var out bytes.Buffer
	writer, err := newBundleWriter(&out, header)
	if err != nil {
		t.Fatalf("newBundleWriter: %v", err)
	}
	for _, frame := range frames {
		if err := writer.add(frame.Kind, frame.Run, frame.Payload); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if _, err := writer.close(header); err != nil {
		t.Fatalf("close: %v", err)
	}
	return out.Bytes()
}

// exportedBundle returns a run's bundle and the frames it holds, so a test can
// damage one and rebuild it.

func exportedBundle(t *testing.T, records int) (BundleHeader, []parsedFrame, BundleTrailer, []byte) {
	t.Helper()
	store, key := bundleFixture(t, records)
	artifact, err := store.Export(context.Background(), ExportRequest{Key: key})
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
	header, frames, trailer, err := readBundle(t, payload)
	if err != nil {
		t.Fatalf("read the bundle: %v", err)
	}
	return header, frames, trailer, payload
}

// stageRefused runs a bundle against a fresh store and requires the refusal the
// test is about, so a check that stops firing fails here rather than passing.

func stageRefused(t *testing.T, payload []byte, want error) {
	t.Helper()
	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	_, err := target.StageImport(context.Background(), bytes.NewReader(payload), DefaultImportLimits())
	if err == nil {
		t.Fatal("the bundle was staged")
	}
	if want != nil && !errors.Is(err, want) {
		t.Errorf("= %v, want %v", err, want)
	}
}

// activateRefused stages a bundle and requires the activation to refuse it, for the
// checks that only exist once the bundle's frames are being installed.

func activateRefused(t *testing.T, payload []byte, want error) {
	t.Helper()
	ctx := context.Background()
	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	ticket, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if _, err := target.ActivateImport(ctx, ticket); !errors.Is(err, want) {
		t.Errorf("activating = %v, want %v", err, want)
	}
	installation, err := target.Installation(ctx)
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.ActiveSessionID == ticket.LocalSessionID {
		t.Error("a refused activation published the imported session anyway")
	}
	if installation.Generation == ticket.Generation+1 {
		t.Error("a refused activation moved the store's generation")
	}
}

func TestAnInconsistentTrailerIsRefused(t *testing.T) {
	_, _, trailer, payload := exportedBundle(t, 4)
	// Each case leaves the frames and the header alone and breaks exactly one of the
	// trailer's claims, which is what an edited trailer looks like.
	for _, test := range []struct {
		name   string
		damage func([]byte) []byte
	}{
		{
			name: "a frame count that does not match",
			damage: func(b []byte) []byte {
				return replaceCount(b, "frameCount", trailer.FrameCount, trailer.FrameCount+1)
			},
		},
		{
			name: "a content digest that does not match",
			damage: func(b []byte) []byte {
				return flipTrailerDigest(b, "contentSha256")
			},
		},
		{
			name: "a header digest that does not match",
			damage: func(b []byte) []byte {
				return flipTrailerDigest(b, "headerSha256")
			},
		},
		{
			name: "a per-kind count that does not match",
			damage: func(b []byte) []byte {
				count := trailer.Counts[BundleEntryRecord]
				return replaceCount(b, string(BundleEntryRecord), count, count+1)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			damaged := test.damage(append([]byte(nil), payload...))
			if bytes.Equal(damaged, payload) {
				t.Fatal("the damage changed nothing, so this case proves nothing")
			}
			stageRefused(t, damaged, ErrBundleInvalid)
		})
	}
}

// flipTrailerDigest changes one hexadecimal character of a trailer digest, which
// leaves the trailer decodable and its length unchanged.

func flipTrailerDigest(bundle []byte, field string) []byte {
	at := bytes.Index(bundle, []byte(`"`+field+`":"`))
	if at < 0 {
		return bundle
	}
	digest := at + len(field) + 4
	out := append([]byte(nil), bundle...)
	if out[digest] == 'a' {
		out[digest] = 'b'
	} else {
		out[digest] = 'a'
	}
	return out
}

// replaceCount rewrites one number in the trailer, which leaves the trailer
// decodable and its framing intact.

func replaceCount(bundle []byte, name string, from, to uint64) []byte {
	return bytes.Replace(bundle,
		[]byte(fmt.Sprintf(`"%s":%d`, name, from)),
		[]byte(fmt.Sprintf(`"%s":%d`, name, to)), 1)
}

func TestBytesAfterTheTrailerAreRefused(t *testing.T) {
	_, _, _, payload := exportedBundle(t, 3)
	stageRefused(t, append(append([]byte(nil), payload...), 'x'), ErrBundleInvalid)
}

func TestAMissingTreeFrameIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 5)
	// Dropping a tree frame leaves every surviving digest consistent: the trailer is
	// rebuilt here, and nothing in the file records that a tree went missing. What
	// refuses it is that a record which committed a tree has no representation to
	// serve, which is a state the store's own writer cannot produce.
	kept := frames[:0:0]
	dropped := 0
	for _, frame := range frames {
		if frame.Kind == BundleEntryTree && dropped == 0 {
			dropped++
			continue
		}
		kept = append(kept, frame)
	}
	if dropped == 0 {
		t.Fatal("the fixture exported no tree frame to drop")
	}
	stageRefused(t, rebuildBundle(t, header, kept), ErrBundleInvalid)
}

func TestAnUnknownFrameKindIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 3)
	frames[len(frames)-1].Kind = BundleEntryKind("mystery")
	stageRefused(t, rebuildBundle(t, header, frames), ErrBundleInvalid)
}

func TestABoundThatCutsTheBundleShortIsRefused(t *testing.T) {
	_, _, _, payload := exportedBundle(t, 4)
	// A bound larger than any single section but smaller than the file is enforced by
	// the stream rather than by one section's length, so the walk runs out of bytes
	// mid-bundle and has to refuse rather than stage a prefix.
	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	limits := DefaultImportLimits()
	limits.MaxBytes = uint64(len(payload) / 2)
	if _, err := target.StageImport(context.Background(), bytes.NewReader(payload), limits); err == nil {
		t.Fatal("a bundle past its bound was staged")
	}
	// The frame count is bounded too, so a bundle of many small frames cannot cost
	// unbounded work whatever its size.
	limits = DefaultImportLimits()
	limits.MaxObjects = 2
	if _, err := target.StageImport(context.Background(), bytes.NewReader(payload), limits); !errors.Is(err, ErrBundleInvalid) {
		t.Errorf("a bundle past its frame bound = %v, want an invalid bundle", err)
	}
}

func TestAnUnusableLengthIsRefusedBeforeItIsRead(t *testing.T) {
	// A bundle that declares a section far larger than anything the store admits must
	// be refused from its own header, not by reading the section. Without the section
	// bounds this allocates a gigabyte to discover the stream ends.
	oversized := make([]byte, 0, 16)
	oversized = append(oversized, bundleMagic...)
	oversized = binary.BigEndian.AppendUint32(oversized, 1<<30)
	stageRefused(t, oversized, ErrBundleInvalid)
	// A frame header inside a bundle is bounded the same way.
	_, _, _, payload := exportedBundle(t, 3)
	bundle := append([]byte(nil), payload...)
	at := bytes.Index(bundle, []byte(bundleTrailerMagic))
	if at < 0 {
		t.Fatal("the exported bundle has no trailer")
	}
	binary.BigEndian.PutUint32(bundle[at-4:at], 1<<30)
	stageRefused(t, bundle, ErrBundleInvalid)
}

func TestABundleThatNamesAnotherFormatIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 3)
	for _, test := range []struct {
		name   string
		damage func(BundleHeader) BundleHeader
	}{
		{"another format", func(h BundleHeader) BundleHeader {
			h.Format = "somebody-elses.bundle"
			return h
		}},
		{"another version", func(h BundleHeader) BundleHeader {
			h.Version = BundleFormatVersion + 1
			return h
		}},
		// A kind the frames carry and the header does not declare is a header that
		// does not describe the bundle. The reverse is not a defect: the declared kinds
		// are the order the format writes, so a run with no deltas declares the delta
		// kind and carries none.
		{"entry kinds missing one it does", func(h BundleHeader) BundleHeader {
			h.EntryKinds = h.EntryKinds[1:]
			return h
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stageRefused(t, rebuildBundle(t, test.damage(header), frames), ErrBundleInvalid)
		})
	}
}

func TestABundleWithNoRecordsIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 3)
	kept := make([]parsedFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.Kind == BundleEntryRecord || frame.Kind == BundleEntryTree || frame.Kind == BundleEntryDelta {
			continue
		}
		kept = append(kept, frame)
	}
	damaged := header
	damaged.ThroughSequence = 0
	stageRefused(t, rebuildBundle(t, damaged, kept), ErrBundleInvalid)
}

func TestAReorderedDeltaIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 5)
	// Every delta moved behind the frames it describes. The trailer is rebuilt over
	// the new order, so every digest in the file is consistent, and the only thing
	// that can notice is that a delta now names a record the bundle has not carried.
	// The contract calls that a reorder, and it is refused rather than dropped.
	kept := make([]parsedFrame, 0, len(frames))
	var deltas []parsedFrame
	for _, frame := range frames {
		if frame.Kind == BundleEntryDelta {
			deltas = append(deltas, frame)
			continue
		}
		kept = append(kept, frame)
	}
	if len(deltas) == 0 {
		t.Skip("the fixture exported no delta, so there is nothing to reorder")
	}
	stageRefused(t, rebuildBundle(t, header, append(kept, deltas...)), ErrBundleInvalid)
}

func TestATreeBeforeItsRecordIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 4)
	// The first tree moved to the front. Staging refuses it, so a ticket is never
	// issued for a bundle the activation would have to refuse.
	var firstTree int = -1
	for i, frame := range frames {
		if frame.Kind == BundleEntryTree {
			firstTree = i
			break
		}
	}
	if firstTree < 0 {
		t.Skip("the fixture exported no tree")
	}
	moved := append([]parsedFrame{frames[firstTree]}, frames[:firstTree]...)
	moved = append(moved, frames[firstTree+1:]...)
	stageRefused(t, rebuildBundle(t, header, moved), ErrBundleInvalid)
}

func TestARecordWithAStaleDigestIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 4)
	// A record's payload changed and its payload digest was restated, leaving only the
	// record digest stale. Every other digest in the file is consistent, and the chain
	// still links, so this is the case that only the record's own digest refuses.
	for i, frame := range frames {
		if frame.Kind != BundleEntryRecord {
			continue
		}
		var record StoredRecord
		if err := CanonicalDecode(frame.Payload, &record); err != nil {
			t.Fatalf("decode a record: %v", err)
		}
		record.AttemptID = "rewritten"
		payloadDigest, err := HashPayload(record)
		if err != nil {
			t.Fatalf("payload digest: %v", err)
		}
		record.PayloadSHA256 = payloadDigest
		payload, err := CanonicalEncode(record)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		frames[i].Payload = payload
		break
	}
	stageRefused(t, rebuildBundle(t, header, frames), ErrBundleInvalid)
}

func mustOpen(t *testing.T, a *ExportArtifact) io.ReadCloser {
	t.Helper()
	reader, err := a.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return reader
}

// activatedImport exports a run, imports it into a fresh store, and returns that
// store and the local run key the import published.

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
