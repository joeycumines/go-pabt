/*
   Copyright 2026 Joseph Cumines

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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// Applying an exact delta, and refusing one that does not hold.
//
// Split from exactdelta.go, which generates a delta. Generation and application
// are two halves of one contract and they failed apart once already: the
// generator did not say what a parent was left holding after losing its last
// child, and this half then refused the delta the other half had produced.
// They are read together and changed together.

// ValidateExactDelta checks a delta against the base it claims.
//
// Every identity is checked, in both directions: the delta's own digest, the base
// it names, and the target it claims to produce. A delta that passes is one that
// ApplyExactDelta will not have to doubt.
func ValidateExactDelta(base StoredTree, delta ExactTreeDelta) error {
	if delta.Format != ExactDeltaFormatName || delta.Version != ExactDeltaFormatVersion {
		return fmt.Errorf("%w: the delta is %s version %d",
			ErrValueUnencodable, delta.Format, delta.Version)
	}
	digest, err := HashDelta(delta)
	if err != nil {
		return err
	}
	if digest != delta.SHA256 {
		return fmt.Errorf("%w: the delta digest is %q, recomputes to %q",
			ErrStoreCorrupt, delta.SHA256, digest)
	}
	if base.RunKey != delta.RunKey {
		return fmt.Errorf("%w: the delta is for run %s and the base is for %s",
			ErrHeadMismatch, delta.RunKey.RunID, base.RunKey.RunID)
	}
	baseDigest, err := HashTree(base)
	if err != nil {
		return err
	}
	if baseDigest != base.Digest {
		return fmt.Errorf("%w: the base tree digest is %q, recomputes to %q",
			ErrStoreCorrupt, base.Digest, baseDigest)
	}
	switch {
	case base.EventUID != delta.BaseEventUID:
		return fmt.Errorf("%w: the delta names base event %s, the base is %s",
			ErrHeadMismatch, delta.BaseEventUID, base.EventUID)
	case base.Sequence != delta.BaseSequence:
		return fmt.Errorf("%w: the delta names base sequence %d, the base is %d",
			ErrHeadMismatch, delta.BaseSequence, base.Sequence)
	case base.PlanRevision != delta.BasePlanRevision:
		return fmt.Errorf("%w: the delta names base plan revision %d, the base is %d",
			ErrHeadMismatch, delta.BasePlanRevision, base.PlanRevision)
	case base.Digest != delta.BaseDigest:
		return fmt.Errorf("%w: the delta names base digest %s, the base is %s",
			ErrHeadMismatch, delta.BaseDigest, base.Digest)
	case base.Sequence >= delta.TargetSequence:
		return fmt.Errorf("%w: the delta targets sequence %d, at or before its base %d",
			ErrHeadMismatch, delta.TargetSequence, base.Sequence)
	}
	for i, op := range delta.Operations {
		if op.Index != uint32(i) {
			return fmt.Errorf("%w: operation %d claims index %d", ErrStoreCorrupt, i, op.Index)
		}
		if err := validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

func validateOp(op DeltaOp) error {
	set := 0
	if op.Remove != nil {
		set++
	}
	if op.Insert != nil {
		set++
	}
	if op.Reorder != nil {
		set++
	}
	if op.Replace != nil {
		set++
	}
	if set != 1 {
		return fmt.Errorf("%w: operation %d carries %d payloads, want exactly one",
			ErrValueUnencodable, op.Index, set)
	}
	switch op.Kind {
	case DeltaRemove:
		if op.Remove == nil || op.Remove.NodeID == "" {
			return fmt.Errorf("%w: a remove operation names no node", ErrValueUnencodable)
		}
	case DeltaInsert:
		if op.Insert == nil || op.Insert.RootID == "" || len(op.Insert.Nodes) == 0 {
			return fmt.Errorf("%w: an insert operation carries no subtree", ErrValueUnencodable)
		}
		if op.Insert.Nodes[0].ID != op.Insert.RootID {
			return fmt.Errorf("%w: an insert's first node is not its root", ErrValueUnencodable)
		}
	case DeltaReorder:
		if op.Reorder == nil || op.Reorder.ChildOrder == nil {
			return fmt.Errorf("%w: a reorder operation carries no order", ErrValueUnencodable)
		}
	case DeltaReplace:
		if op.Replace == nil || op.Replace.Node.ID == "" {
			return fmt.Errorf("%w: a replace operation names no node", ErrValueUnencodable)
		}
	default:
		return fmt.Errorf("%w: unknown operation kind %q", ErrValueUnencodable, op.Kind)
	}
	return nil
}

// ApplyExactDelta produces the target tree from a base and a delta.
//
// The result is rebuilt and re-digested, and it must match the digest the delta
// claimed. A delta that reconstructs into something else is refused rather than
// returned, so a caller never receives a tree that is not the one recorded.
func ApplyExactDelta(base StoredTree, delta ExactTreeDelta) (StoredTree, error) {
	if err := ValidateExactDelta(base, delta); err != nil {
		return StoredTree{}, err
	}
	idx, err := indexTree(base)
	if err != nil {
		return StoredTree{}, err
	}
	removed := make(map[string]bool)
	inserted := make(map[string]bool)

	// Removal, then insertion, then order, then content: the same order the
	// generator emits, so a reader can follow a delta without consulting the
	// generator.
	for _, op := range delta.Operations {
		if op.Kind != DeltaRemove {
			continue
		}
		if _, err := idx.subtreeInPreorder(op.Remove.NodeID); err != nil {
			return StoredTree{}, fmt.Errorf("%w: removing %s: %v", ErrHeadMismatch, op.Remove.NodeID, err)
		}
		for _, node := range mustSubtree(idx, op.Remove.NodeID) {
			removed[node.ID] = true
			// The removed node is unlinked from its parent as well as deleted from
			// the node and child maps. A reader that rebuilt a tree by walking a
			// parent's child list would otherwise reach a node that is no longer
			// there, and refuse the tree it had just been told to build. The
			// generator now says this in a reorder, and doing it here as well means
			// the applier does not depend on the generator having said it.
			if node.ParentID != nil {
				idx.children[*node.ParentID] = withoutID(idx.children[*node.ParentID], node.ID)
			}
			delete(idx.nodes, node.ID)
			delete(idx.children, node.ID)
		}
	}
	for _, op := range delta.Operations {
		if op.Kind != DeltaInsert {
			continue
		}
		for _, node := range op.Insert.Nodes {
			if _, clash := idx.nodes[node.ID]; clash {
				return StoredTree{}, fmt.Errorf("%w: the delta inserts %s, which already exists",
					ErrHeadMismatch, node.ID)
			}
			idx.nodes[node.ID] = node
			inserted[node.ID] = true
		}
		if op.Insert.ParentID != nil {
			if _, ok := idx.nodes[*op.Insert.ParentID]; !ok {
				return StoredTree{}, fmt.Errorf("%w: the delta inserts under %s, which is not there",
					ErrHeadMismatch, *op.Insert.ParentID)
			}
		}
		idx.children[op.Insert.RootID] = childOrderOf(op.Insert.Nodes, op.Insert.RootID)
		if op.Insert.ParentID != nil {
			parent := *op.Insert.ParentID
			idx.children[parent] = withoutID(idx.children[parent], op.Insert.RootID)
		}
	}
	for _, op := range delta.Operations {
		if op.Kind != DeltaReorder {
			continue
		}
		parent := ""
		if op.Reorder.ParentID != nil {
			parent = *op.Reorder.ParentID
		}
		for _, id := range op.Reorder.ChildOrder {
			if _, ok := idx.nodes[id]; !ok {
				return StoredTree{}, fmt.Errorf("%w: the delta orders %s under %s, which is not there",
					ErrHeadMismatch, id, parent)
			}
		}
		idx.children[parent] = append([]string(nil), op.Reorder.ChildOrder...)
	}
	for _, op := range delta.Operations {
		if op.Kind != DeltaReplace {
			continue
		}
		if _, alive := idx.nodes[op.Replace.Node.ID]; !alive {
			return StoredTree{}, fmt.Errorf("%w: the delta replaces %s, which was removed or never added",
				ErrHeadMismatch, op.Replace.Node.ID)
		}
		idx.nodes[op.Replace.Node.ID] = op.Replace.Node
	}
	if len(removed) > 0 && len(inserted) > 0 {
		if clash := firstClash(removed, inserted); clash != "" {
			return StoredTree{}, fmt.Errorf("%w: %s is both removed and inserted", ErrValueUnencodable, clash)
		}
	}

	tree, err := rebuildTree(delta, idx)
	if err != nil {
		return StoredTree{}, err
	}
	produced, err := HashTree(tree)
	if err != nil {
		return StoredTree{}, err
	}
	if produced != delta.TargetDigest {
		return StoredTree{}, fmt.Errorf("%w: the delta reconstructs to %q, it claims %q",
			ErrStoreCorrupt, produced, delta.TargetDigest)
	}
	return tree, nil
}

// withoutID returns a slice with one identity removed, preserving the order of the
// rest. A copy is returned so the caller's slice is not edited underneath it.
func withoutID(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, candidate := range ids {
		if candidate == id {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func mustSubtree(idx *nodeIndex, root string) []StoredNode {
	out, err := idx.subtreeInPreorder(root)
	if err != nil {
		return nil
	}
	return out
}

func childOrderOf(nodes []StoredNode, root string) []string {
	var out []string
	for _, node := range nodes {
		if node.ParentID != nil && *node.ParentID == root {
			out = append(out, node.ID)
		}
	}
	return out
}

func firstClash(a, b map[string]bool) string {
	out := make([]string, 0)
	for id := range a {
		if b[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return ""
	}
	return out[0]
}

// rebuildTree turns an index back into a canonical tree, in preorder from the
// root, and re-seals every node's content digest and the tree's own.
func rebuildTree(delta ExactTreeDelta, idx *nodeIndex) (StoredTree, error) {
	tree := StoredTree{
		RunKey:       delta.RunKey,
		EventUID:     delta.TargetEventUID,
		Sequence:     delta.TargetSequence,
		PlanRevision: delta.TargetPlanRevision,
		Digest:       delta.TargetDigest,
	}
	rootID := ""
	for _, id := range idx.children[""] {
		rootID = id
		break
	}
	if rootID == "" {
		return StoredTree{}, fmt.Errorf("%w: the delta reconstructs to a tree with no root",
			ErrStoreCorrupt)
	}
	tree.RootID = rootID
	seen := make(map[string]bool, len(idx.nodes))
	// The parent travels with the walk, because a node's parent is where it came
	// from, not which node it is.
	var walk func(nodeID, parentID string, ordinal uint32) error
	walk = func(nodeID, parentID string, ordinal uint32) error {
		if seen[nodeID] {
			return fmt.Errorf("%w: node %s is in a cycle", ErrStoreCorrupt, nodeID)
		}
		seen[nodeID] = true
		node, ok := idx.nodes[nodeID]
		if !ok {
			return fmt.Errorf("%w: node %s is missing", ErrStoreCorrupt, nodeID)
		}
		if parentID == "" {
			node.ParentID = nil
		} else {
			node.ParentID = &parentID
		}
		// The ordinal follows from the child order rather than travelling on the
		// node, so a move is expressed once, in the order, and cannot disagree
		// with it.
		node.Ordinal = ordinal
		digest, err := HashNode(node)
		if err != nil {
			return err
		}
		node.ContentHash = digest
		tree.Nodes = append(tree.Nodes, node)
		for pos, child := range idx.children[nodeID] {
			if err := walk(child, nodeID, uint32(pos)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootID, "", 0); err != nil {
		return StoredTree{}, err
	}
	if len(seen) != len(idx.nodes) {
		return StoredTree{}, fmt.Errorf("%w: %d nodes are unreachable from the root",
			ErrStoreCorrupt, len(idx.nodes)-len(seen))
	}
	tree.NodeCount = uint32(len(tree.Nodes))
	sealed, err := HashTree(tree)
	if err != nil {
		return StoredTree{}, err
	}
	tree.Digest = sealed
	return tree, nil
}

// verifyDeltaReconstructs applies a delta to the base this store recorded and
// requires the tree this store stored.
//
// That is the check that matters. The caller supplied the operations; the store
// supplied the identity and the digest. A delta whose operations do not actually
// produce the committed tree is refused rather than stored beside it, so a
// representation is never a pair that disagrees with itself.
func (s *SQLiteStore) verifyDeltaReconstructs(
	ctx context.Context,
	key RunKey,
	delta ExactTreeDelta,
	targetDigest string,
) error {
	if err := s.checkUsable(false); err != nil {
		return err
	}
	baseRaw, err := s.readRepresentation(ctx, key, delta.BaseEventUID)
	if err != nil {
		return err
	}
	var base StoredTree
	if err := CanonicalDecode(baseRaw, &base); err != nil {
		return fmt.Errorf("%w: the delta base: %v", ErrStoreCorrupt, err)
	}
	rebuilt, err := ApplyExactDelta(base, delta)
	if err != nil {
		return err
	}
	if rebuilt.Digest != targetDigest {
		return fmt.Errorf("%w: the delta reconstructs to %q, the record holds %q",
			ErrStoreCorrupt, rebuilt.Digest, targetDigest)
	}
	return nil
}

// readRepresentation returns a run's stored tree bytes for one event.
func (s *SQLiteStore) readRepresentation(ctx context.Context, key RunKey, eventUID string) ([]byte, error) {
	var raw []byte
	row := s.readers.QueryRowContext(ctx,
		"SELECT tree_json FROM representations WHERE session_id = ? AND run_id = ? AND event_uid = ?",
		key.SessionID, key.RunID, eventUID)
	switch err := row.Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("%w: the delta names base event %s, which is not recorded",
			ErrHeadMismatch, eventUID)
	case err != nil:
		return nil, fmt.Errorf("pabtdebug: read the delta base: %w", err)
	}
	return raw, nil
}

// VerifyChain checks that a delta's base is genuinely recorded in the store, with
// the tree the delta names.
//
// A delta stored beside a base that no longer verifies would reconstruct into
// something the chain never committed, so the base is read and re-sealed here
// rather than trusted because a row exists.
func VerifyChain(ctx context.Context, readers *sql.DB, key RunKey, delta *ExactTreeDelta) error {
	if delta == nil {
		return nil
	}
	var raw []byte
	row := readers.QueryRowContext(ctx,
		"SELECT tree_json FROM representations WHERE session_id = ? AND run_id = ? AND event_uid = ?",
		key.SessionID, key.RunID, delta.BaseEventUID)
	switch err := row.Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: the delta names base event %s, which is not recorded",
			ErrHeadMismatch, delta.BaseEventUID)
	case err != nil:
		return fmt.Errorf("pabtdebug: read the delta base: %w", err)
	}
	var base StoredTree
	if err := CanonicalDecode(raw, &base); err != nil {
		return fmt.Errorf("%w: the delta base: %v", ErrStoreCorrupt, err)
	}
	return ValidateExactDelta(base, *delta)
}

// DeltaBase returns the representation a delta for targetSequence may be computed
// from, or found=false when the target must be stored as a keyframe.
//
// A base is eligible only when it is a complete tree and no committed record
// between it and the target is one a delta cannot be computed across: a
// failure, an interruption, an abort, or a record with no tree.
//
// An attempt that was released without a record leaves no row to exclude. That is
// deliberate: an aborted attempt recorded nothing, so a delta across it is still
// an exact change between the two trees that exist, and the chain is by digest
// rather than by position, so nothing is claimed that is not true. What the rule
// does not do is pretend a released attempt left a mark on the history.
func (s *SQLiteStore) DeltaBase(
	ctx context.Context,
	key RunKey,
	targetSequence uint64,
) (base StoredTree, found bool, err error) {
	if err := s.checkUsable(false); err != nil {
		return StoredTree{}, false, err
	}
	var raw []byte
	row := s.readers.QueryRowContext(ctx,
		`SELECT t.tree_json
		 FROM records AS r
		 JOIN representations AS t
		   ON t.session_id = r.session_id AND t.event_uid = r.event_uid
		 WHERE r.session_id = ? AND r.run_id = ? AND r.seq < ?
		   AND r.payload_state = 'complete'
		   AND r.tree_eligible = 1
		   AND NOT EXISTS (
		       SELECT 1 FROM records AS blocked
		        WHERE blocked.session_id = r.session_id
		          AND blocked.run_id = r.run_id
		          AND blocked.seq > r.seq
		          AND blocked.seq < ?
		          AND NOT (blocked.payload_state = 'complete' AND blocked.tree_eligible = 1)
		   )
		 ORDER BY r.seq DESC
		 LIMIT 1`,
		key.SessionID, key.RunID, targetSequence, targetSequence)
	switch err := row.Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return StoredTree{}, false, nil
	case err != nil:
		return StoredTree{}, false, fmt.Errorf("pabtdebug: read a delta base: %w", err)
	}
	if err := CanonicalDecode(raw, &base); err != nil {
		return StoredTree{}, false, fmt.Errorf("%w: a candidate base tree: %v", ErrStoreCorrupt, err)
	}
	digest, err := HashTree(base)
	if err != nil {
		return StoredTree{}, false, err
	}
	if digest != base.Digest {
		return StoredTree{}, false, fmt.Errorf("%w: a candidate base tree digest is %q, recomputes to %q",
			ErrStoreCorrupt, base.Digest, digest)
	}
	return base, true, nil
}

// Representation returns a record's stored tree and the base its delta names, so a
// caller can reconstruct a tree from a delta and check it against the keyframe the
// store also holds.
//
// What it returns has been verified the way TreeAt verifies, because a caller that is
// handed a representation is about to trust it: the tree against its own digest, the
// tree against the record the chain covers, and the delta against the base it names.
// A representation that disagrees with any of those is reported rather than served,
// so no caller has to remember to check.
func (s *SQLiteStore) Representation(
	ctx context.Context,
	key RunKey,
	sequence uint64,
) (tree StoredTree, base *string, delta *ExactTreeDelta, err error) {
	if err := s.checkUsable(false); err != nil {
		return StoredTree{}, nil, nil, err
	}
	var (
		raw      []byte
		baseUID  sql.NullString
		baseSeq  sql.NullInt64
		deltaRaw []byte
	)
	row := s.readers.QueryRowContext(ctx,
		"SELECT tree_json, base_event_uid, base_seq, delta_json FROM representations "+
			"WHERE session_id = ? AND run_id = ? AND seq = ?",
		key.SessionID, key.RunID, sequence)
	switch err := row.Scan(&raw, &baseUID, &baseSeq, &deltaRaw); {
	case errors.Is(err, sql.ErrNoRows):
		return StoredTree{}, nil, nil, fmt.Errorf("%w: no representation at sequence %d of run %s",
			ErrNotFound, sequence, key.RunID)
	case err != nil:
		return StoredTree{}, nil, nil, fmt.Errorf("pabtdebug: read a representation: %w", err)
	}
	if err := CanonicalDecode(raw, &tree); err != nil {
		return StoredTree{}, nil, nil, fmt.Errorf("%w: a representation tree: %v", ErrStoreCorrupt, err)
	}
	// From here the sequence has a representation, so a record without a tree is a
	// contradiction rather than an absence. The order matters: a record that committed
	// no tree has no representation row either, and was already answered as not found.
	record, err := s.RecordAt(ctx, key, sequence)
	if err != nil {
		return StoredTree{}, nil, nil, err
	}
	if record.Tree == nil {
		return StoredTree{}, nil, nil, fmt.Errorf(
			"%w: sequence %d holds a representation and its record has no tree",
			ErrStoreCorrupt, sequence)
	}
	if baseUID.Valid {
		uid := baseUID.String
		base = &uid
	}
	digest, err := HashTree(tree)
	if err != nil {
		return StoredTree{}, nil, nil, err
	}
	if digest != tree.Digest {
		return StoredTree{}, nil, nil, fmt.Errorf("%w: the tree at %d digest is %q, recomputes to %q",
			ErrStoreCorrupt, sequence, tree.Digest, digest)
	}
	committed, err := HashTree(*record.Tree)
	if err != nil {
		return StoredTree{}, nil, nil, err
	}
	if committed != digest {
		return StoredTree{}, nil, nil, fmt.Errorf(
			"%w: the tree at %d is %q, the committed record holds %q",
			ErrStoreCorrupt, sequence, digest, committed)
	}
	if len(deltaRaw) > 0 {
		var decoded ExactTreeDelta
		if err := CanonicalDecode(deltaRaw, &decoded); err != nil {
			return StoredTree{}, nil, nil, fmt.Errorf("%w: a stored delta: %v", ErrStoreCorrupt, err)
		}
		// The base the delta names is read and re-sealed rather than trusted, so a
		// caller is never told to apply a delta to a tree the store does not hold.
		if err := VerifyChain(ctx, s.readers, key, &decoded); err != nil {
			return StoredTree{}, nil, nil, err
		}
		delta = &decoded
	}
	return tree, base, delta, nil
}
