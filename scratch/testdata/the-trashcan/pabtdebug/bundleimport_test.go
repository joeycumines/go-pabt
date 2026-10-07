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
	"sync"
	"testing"
)

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

func TestACancelledImportStops(t *testing.T) {
	_, _, _, payload := exportedBundle(t, 6)
	ctx, cancel := context.WithCancel(context.Background())
	target := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, target)
	// The reader checks the context once per frame, so a bundle big enough to still be
	// arriving is refused rather than staged in full.
	cancel()
	if _, err := target.StageImport(ctx, bytes.NewReader(payload), DefaultImportLimits()); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled import = %v, want it stopped", err)
	}
	if entries, err := os.ReadDir(target.path + ".artifacts"); err == nil {
		for _, entry := range entries {
			t.Errorf("a cancelled import left %s behind", entry.Name())
		}
	}
}

func TestAnImportedRunRefusesNewRecords(t *testing.T) {
	ctx := context.Background()
	store, _, imported := activatedImport(t, 4)
	// An imported history is marked read-only, and the mark is enforced: appending
	// would leave the store holding something other than what was imported.
	head, err := store.Head(ctx, imported)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	ticket, err := store.Begin(ctx, BeginRequest{
		RunKey: imported, ExpectedPlanRevision: head.HeadPlanRevision,
	})
	if !errors.Is(err, ErrStoreReadOnly) {
		t.Fatalf("appending to an imported run = %v, want a read-only refusal", err)
	}
	_ = ticket
	after, err := store.Head(ctx, imported)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if after.HeadSequence != head.HeadSequence {
		t.Errorf("the refused append moved the head from %d to %d",
			head.HeadSequence, after.HeadSequence)
	}
}

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
