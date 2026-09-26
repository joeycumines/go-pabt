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
	"fmt"
	"io"
	"os"
	"sync"
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

// readBundle decodes a whole bundle, so the format's own framing is under test
// rather than a round trip through the writer.
func readBundle(t *testing.T, data []byte) (BundleHeader, []bundleFrame, BundleTrailer, error) {
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
		frames = append(frames, bundleFrame{
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
func rebuildBundle(t *testing.T, header BundleHeader, frames []bundleFrame) []byte {
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
func exportedBundle(t *testing.T, records int) (BundleHeader, []bundleFrame, BundleTrailer, []byte) {
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

func TestATreeFrameThatDisagreesWithItsRecordIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 4)
	var trees []int
	for i, frame := range frames {
		if frame.Kind == BundleEntryTree {
			trees = append(trees, i)
		}
	}
	if len(trees) < 2 {
		t.Fatal("the fixture exported fewer than two tree frames")
	}
	// The second tree's content, re-sealed and relabelled as the first tree's. It is a
	// well-formed tree and its own digest describes it, and the record it claims to
	// belong to really committed a tree, so only the comparison between the two copies
	// can refuse it.
	var second StoredTree
	if err := CanonicalDecode(frames[trees[1]].Payload, &second); err != nil {
		t.Fatalf("decode the second tree: %v", err)
	}
	var first StoredTree
	if err := CanonicalDecode(frames[trees[0]].Payload, &first); err != nil {
		t.Fatalf("decode the first tree: %v", err)
	}
	second.EventUID = first.EventUID
	second.Sequence = first.Sequence
	second.Digest = ""
	digest, err := HashTree(second)
	if err != nil {
		t.Fatalf("re-seal: %v", err)
	}
	second.Digest = digest
	payload, err := CanonicalEncode(second)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	frames[trees[0]].Payload = payload
	activateRefused(t, rebuildBundle(t, header, frames), ErrBundleInvalid)
}

func TestADeltaThatDoesNotReconstructItsTreeIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 5)
	deltaAt := -1
	for i, frame := range frames {
		if frame.Kind == BundleEntryDelta {
			deltaAt = i
			break
		}
	}
	if deltaAt < 0 {
		t.Skip("the fixture exported no delta, so the delta path is not exercised here")
	}
	// A delta that names a real base and a real target, carries a digest that
	// describes it, and does not describe the change between them: one operation
	// writes a node the target never held. Every check that looks at the delta alone
	// passes, and only applying it can refuse it.
	var delta ExactTreeDelta
	if err := CanonicalDecode(frames[deltaAt].Payload, &delta); err != nil {
		t.Fatalf("decode the delta: %v", err)
	}
	altered := false
	for i := range delta.Operations {
		switch {
		case delta.Operations[i].Replace != nil:
			delta.Operations[i].Replace.Node.Name += "-rewritten"
			altered = true
		case delta.Operations[i].Insert != nil:
			for j := range delta.Operations[i].Insert.Nodes {
				delta.Operations[i].Insert.Nodes[j].Name += "-rewritten"
			}
			altered = true
		}
		if altered {
			break
		}
	}
	if !altered {
		t.Skip("the fixture's delta carries no node content, so this case does not apply")
	}
	delta.SHA256 = ""
	digest, err := HashDelta(delta)
	if err != nil {
		t.Fatalf("re-seal: %v", err)
	}
	delta.SHA256 = digest
	payload, err := CanonicalEncode(delta)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	frames[deltaAt].Payload = payload
	activateRefused(t, rebuildBundle(t, header, frames), ErrBundleInvalid)
}

func TestAnUnknownFrameKindIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 3)
	frames[len(frames)-1].Kind = BundleEntryKind("mystery")
	stageRefused(t, rebuildBundle(t, header, frames), ErrBundleInvalid)
}

func TestAStagedBundleReplacedBeforeActivationIsRefused(t *testing.T) {
	_, _, _, first := exportedBundle(t, 3)
	_, _, _, second := exportedBundle(t, 4)
	ctx := context.Background()

	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	ticket, err := target.StageImport(ctx, bytes.NewReader(first), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	// Another bundle takes the staged path. The ticket's proof still verifies,
	// because it binds the digest the file had, so only the re-digest at activation
	// can tell that the file is not the one that was verified.
	if err := os.WriteFile(ticket.StagedPath, second, 0o600); err != nil {
		t.Fatalf("replace the staged bundle: %v", err)
	}
	if _, err := target.ActivateImport(ctx, ticket); !errors.Is(err, ErrImportStale) {
		t.Errorf("activating a replaced bundle = %v, want a stale refusal", err)
	}
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

func TestAnActivationLeavesNoStagedBundleBehind(t *testing.T) {
	_, _, _, payload := exportedBundle(t, 3)
	ctx := context.Background()
	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	ticket, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if _, err := os.Stat(ticket.StagedPath); err != nil {
		t.Fatalf("the staged bundle is not on disk: %v", err)
	}
	if _, err := target.ActivateImport(ctx, ticket); err != nil {
		t.Fatalf("ActivateImport: %v", err)
	}
	if _, err := os.Stat(ticket.StagedPath); !os.IsNotExist(err) {
		t.Errorf("the staged bundle survived the activation that consumed it: %v", err)
	}
}

func TestAnActivationDoesNotReportTheStoreReady(t *testing.T) {
	_, _, _, payload := exportedBundle(t, 3)
	ctx := context.Background()
	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	// An import resolves no intent, so it must not report a store that has one as
	// ready. The state is set here because only opening a store with an open intent
	// sets it, and the single-writer lock refuses a second handle on the same file.
	if err := target.setRecovery(ctx, RecoveryPending); err != nil {
		t.Fatalf("setRecovery: %v", err)
	}
	ticket, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	if _, err := target.ActivateImport(ctx, ticket); err != nil {
		t.Fatalf("ActivateImport: %v", err)
	}
	installation, err := target.Installation(ctx)
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.Recovery != RecoveryPending {
		t.Errorf("the activation reports recovery %q, want it left at %q",
			installation.Recovery, RecoveryPending)
	}
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
	// The staged file carries the digest the export reported, because both are
	// taken over the same three sections.
	if ticket.BundleSHA256 != artifact.Receipt().BundlesSHA256 {
		t.Errorf("the staged bundle is %q, the export reported %q",
			ticket.BundleSHA256, artifact.Receipt().BundlesSHA256)
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
	// Every representation survives the rewrite, and every imported delta still
	// reconstructs the tree it is stored beside, under the identities this import
	// minted rather than the ones the source used.
	importedDeltas := 0
	for sequence := uint64(1); sequence <= 6; sequence++ {
		tree, _, delta, err := target.Representation(ctx, imported, sequence)
		if err != nil {
			t.Fatalf("Representation %d: %v", sequence, err)
		}
		if delta == nil {
			continue
		}
		importedDeltas++
		base, err := target.TreeAt(ctx, imported, delta.BaseSequence)
		if err != nil {
			t.Fatalf("the delta base at %d is not readable: %v", delta.BaseSequence, err)
		}
		rebuilt, err := ApplyExactDelta(base, *delta)
		if err != nil {
			t.Errorf("an imported delta at %d does not apply: %v", sequence, err)
			continue
		}
		if rebuilt.Digest != tree.Digest {
			t.Errorf("an imported delta at %d reconstructs to %q, the stored tree is %q",
				sequence, rebuilt.Digest, tree.Digest)
		}
	}
	if importedDeltas == 0 {
		t.Error("no imported delta was stored, so the rewritten delta path is not exercised")
	}
}

func TestAHeaderThatDoesNotDescribeItsRecordsIsRefused(t *testing.T) {
	header, frames, _, _ := exportedBundle(t, 4)
	for _, test := range []struct {
		name   string
		damage func(BundleHeader) BundleHeader
	}{
		{
			name: "a range wider than the records it carries",
			damage: func(h BundleHeader) BundleHeader {
				h.ThroughSequence++
				return h
			},
		},
		{
			name: "a range that does not start at the first record",
			damage: func(h BundleHeader) BundleHeader {
				h.FirstSequence = 2
				return h
			},
		},
		{
			name: "a run the records do not belong to",
			damage: func(h BundleHeader) BundleHeader {
				h.SourceRunID = "a-run-that-never-existed"
				return h
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			damaged := test.damage(header)
			if damaged.ThroughSequence == header.ThroughSequence &&
				damaged.FirstSequence == header.FirstSequence &&
				damaged.SourceRunID == header.SourceRunID {
				t.Fatal("the damage changed nothing, so this case proves nothing")
			}
			// The trailer is rebuilt over the damaged header, so it is internally
			// consistent and only the comparison with the frames can refuse this.
			stageRefused(t, rebuildBundle(t, damaged, frames), ErrBundleInvalid)
		})
	}
}

func TestTwoActivationsAtOnceDoNotShareAGeneration(t *testing.T) {
	ctx := context.Background()
	_, _, _, payload := exportedBundle(t, 40)
	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)

	const racers = 2
	tickets := make([]ImportTicket, racers)
	for i := range tickets {
		ticket, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
		if err != nil {
			t.Fatalf("StageImport %d: %v", i, err)
		}
		tickets[i] = ticket
	}
	type outcome struct {
		generation uint64
		err        error
	}
	results := make([]outcome, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range tickets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			activated, err := target.ActivateImport(ctx, tickets[i])
			results[i] = outcome{generation: activated.Generation, err: err}
		}(i)
	}
	close(start)
	wg.Wait()

	// The generation is the store's publication serial, so two activations that both
	// succeed must not claim the same one, and the store must have moved by two. This
	// fails if the generation is read outside the transaction that publishes it, which
	// is how it read before.
	generations := map[uint64]bool{}
	succeeded := 0
	for i, result := range results {
		if result.err != nil {
			if !errors.Is(result.err, ErrImportStale) {
				t.Errorf("activation %d = %v, want success or a stale refusal", i, result.err)
			}
			continue
		}
		succeeded++
		if generations[result.generation] {
			t.Errorf("two activations both reported generation %d", result.generation)
		}
		generations[result.generation] = true
	}
	installation, err := target.Installation(ctx)
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if want := tickets[0].Generation + uint64(succeeded); installation.Generation != want {
		t.Errorf("the store is at generation %d after %d activations, want %d",
			installation.Generation, succeeded, want)
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

// activatedImport exports a run, imports it into a fresh store, and returns that
// store and the local run key the import published.
func activatedImport(t *testing.T, records int) (*SQLiteStore, RunKey, RunKey) {
	t.Helper()
	ctx := context.Background()
	source, key := bundleFixture(t, records)
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
	ticket, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	activated, err := target.ActivateImport(ctx, ticket)
	if err != nil {
		t.Fatalf("ActivateImport: %v", err)
	}
	imported := RunKey{SessionID: activated.SessionID, PlanID: activated.PlanID, RunID: activated.RunID}
	return target, key, imported
}

func TestAnImportedRecordNamesTheLocalRunThroughout(t *testing.T) {
	ctx := context.Background()
	store, source, imported := activatedImport(t, 5)
	// A record names its run three times: as itself, in the scope its facts were
	// observed at, and in the tree it committed. The import rewrites the identity
	// once; if it rewrote fewer than all of them, a local record would keep pointing
	// at the store the bundle came from, inside bytes the chain covers and nothing
	// else reports on.
	for sequence := uint64(1); sequence <= 5; sequence++ {
		record, err := store.RecordAt(ctx, imported, sequence)
		if err != nil {
			t.Fatalf("RecordAt %d: %v", sequence, err)
		}
		if record.RunKey != imported {
			t.Errorf("record %d names run %v, want %v", sequence, record.RunKey, imported)
		}
		if record.Revision.RunKey != imported {
			t.Errorf("record %d was observed in run %v, want %v",
				sequence, record.Revision.RunKey, imported)
		}
		if record.Revision.Sequence != sequence {
			t.Errorf("record %d was observed at sequence %d", sequence, record.Revision.Sequence)
		}
		if record.Sequence != sequence {
			t.Errorf("record %d carries sequence %d", sequence, record.Sequence)
		}
		if record.Tree != nil {
			if record.Tree.RunKey != imported {
				t.Errorf("record %d carries a tree for run %v, want %v",
					sequence, record.Tree.RunKey, imported)
			}
			if record.Tree.EventUID != record.EventUID {
				t.Errorf("record %d carries a tree for event %q, want %q",
					sequence, record.Tree.EventUID, record.EventUID)
			}
		}
	}
	// The run the bundle came from is not in this store at all, so nothing a reader
	// can reach names it.
	if _, err := store.RecordAt(ctx, source, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("the source run is readable in the importing store: %v", err)
	}
}

func TestAnImportedRunReportsItsHead(t *testing.T) {
	ctx := context.Background()
	store, _, imported := activatedImport(t, 5)
	head, err := store.Head(ctx, imported)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.HeadSequence != 5 {
		t.Errorf("the imported head is at %d, want 5", head.HeadSequence)
	}
	// The head tree is what a reader bootstraps from, so an imported run that reported
	// none would be a history a client could not open.
	if head.HeadTreeSequence == 0 {
		t.Fatal("the imported run reports no head tree")
	}
	if head.HeadTreeSequence > head.HeadSequence {
		t.Errorf("the head tree is at %d, past the head record %d",
			head.HeadTreeSequence, head.HeadSequence)
	}
	tree, err := store.TreeAt(ctx, imported, head.HeadTreeSequence)
	if err != nil {
		t.Fatalf("the head tree is not readable: %v", err)
	}
	if tree.Digest != head.HeadTreeDigest {
		t.Errorf("the head digest is %q, the tree is %q", head.HeadTreeDigest, tree.Digest)
	}
	if tree.EventUID != head.HeadTreeEventUID {
		t.Errorf("the head event is %q, the tree is %q", head.HeadTreeEventUID, tree.EventUID)
	}
	record, err := store.RecordAt(ctx, imported, head.HeadSequence)
	if err != nil {
		t.Fatalf("the head record is not readable: %v", err)
	}
	if record.RecordSHA256 != head.HeadRecordSHA256 {
		t.Errorf("the head record digest is %q, the record is %q",
			head.HeadRecordSHA256, record.RecordSHA256)
	}
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
	// Staged against the same generation, before anything moved.
	second, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	// Activating advances the generation, so the second ticket was staged against a
	// generation that is no longer current, and publishing it would move the store
	// backwards.
	if _, err := target.ActivateImport(ctx, first); err != nil {
		t.Fatalf("ActivateImport: %v", err)
	}
	if _, err := target.ActivateImport(ctx, second); !errors.Is(err, ErrImportStale) {
		t.Errorf("a ticket from a superseded generation = %v, want a stale refusal", err)
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
