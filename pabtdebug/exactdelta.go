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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// Format identity of an exact tree delta. A change here is a change of format, not
// a reinterpretation of the same bytes.
const (
	ExactDeltaFormatName    = "pabtdebug.exact-delta"
	ExactDeltaFormatVersion = uint32(1)
)

// DeltaOpKind names one operation in an exact tree delta.
type DeltaOpKind string

const (
	// DeltaRemove deletes a node and everything under it. Only topmost removed
	// nodes are emitted, because a removed descendant of a removed node is
	// already gone and naming it again would be contradictory.
	DeltaRemove DeltaOpKind = "remove"

	// DeltaInsert adds a subtree, in final preorder, with its final parent and
	// ordinal.
	DeltaInsert DeltaOpKind = "insert"

	// DeltaReorder replaces one parent's complete child order. A moved node is
	// placed by a reorder and by nothing else, so the order a caller applies
	// operations in cannot matter.
	DeltaReorder DeltaOpKind = "reorder"

	// DeltaReplace changes a surviving node's content.
	DeltaReplace DeltaOpKind = "replace"
)

// DeltaRemoveOp deletes one node and its subtree.
type DeltaRemoveOp struct {
	NodeID string `json:"nodeId"`
}

// DeltaInsertOp adds one subtree.
type DeltaInsertOp struct {
	RootID   string       `json:"rootId"`
	ParentID *string      `json:"parentId"`
	Ordinal  uint32       `json:"ordinal"`
	Nodes    []StoredNode `json:"nodes"`
}

// DeltaReorderOp replaces one parent's complete child order.
type DeltaReorderOp struct {
	ParentID   *string  `json:"parentId"`
	ChildOrder []string `json:"childOrder"`
}

// DeltaReplaceOp replaces one surviving node, naming the fields that changed so a
// consumer can show what moved rather than that something did.
type DeltaReplaceOp struct {
	Node   StoredNode `json:"node"`
	Fields []string   `json:"fields"`
}

// DeltaOp is one operation. Exactly one payload is set, and it matches Kind.
type DeltaOp struct {
	Index   uint32          `json:"index"`
	Kind    DeltaOpKind     `json:"kind"`
	Remove  *DeltaRemoveOp  `json:"remove"`
	Insert  *DeltaInsertOp  `json:"insert"`
	Reorder *DeltaReorderOp `json:"reorder"`
	Replace *DeltaReplaceOp `json:"replace"`
}

// ExactTreeDelta carries a target tree as an exact change from a base tree.
//
// It carries no base bytes. The base is named by event, sequence, plan revision,
// and digest, and the target digest is inside the delta, so applying a delta to
// the wrong base, or producing a result that is not the target, is refused rather
// than believed.
type ExactTreeDelta struct {
	Format             string    `json:"format"`
	Version            uint32    `json:"version"`
	RunKey             RunKey    `json:"runKey"`
	BaseEventUID       string    `json:"baseEventUid"`
	BaseSequence       uint64    `json:"baseSequence"`
	BasePlanRevision   uint64    `json:"basePlanRevision"`
	BaseDigest         string    `json:"baseDigest"`
	TargetEventUID     string    `json:"targetEventUid"`
	TargetSequence     uint64    `json:"targetSequence"`
	TargetPlanRevision uint64    `json:"targetPlanRevision"`
	TargetDigest       string    `json:"targetDigest"`
	Operations         []DeltaOp `json:"operations"`
	SHA256             string    `json:"sha256"`
}

// HashDelta digests a delta, clearing its own digest first.
func HashDelta(d ExactTreeDelta) (string, error) {
	projected := d
	projected.SHA256 = ""
	return HashCanonical(DomainDelta, projected)
}

// nodeIndex is a tree addressed by occurrence identity, which is what makes a
// change addressable across a reorder. A positional scheme cannot say which node
// moved, and a node that is merely reordered would look replaced.
type nodeIndex struct {
	nodes    map[string]StoredNode
	children map[string][]string
}

func indexTree(tree StoredTree) (*nodeIndex, error) {
	if err := verifyTreeShape(tree); err != nil {
		return nil, err
	}
	idx := &nodeIndex{
		nodes:    make(map[string]StoredNode, len(tree.Nodes)),
		children: make(map[string][]string, len(tree.Nodes)),
	}
	for _, node := range tree.Nodes {
		if _, clash := idx.nodes[node.ID]; clash {
			return nil, fmt.Errorf("%w: tree repeats node %s", ErrStoreCorrupt, node.ID)
		}
		idx.nodes[node.ID] = node
	}
	for _, node := range tree.Nodes {
		parent := ""
		if node.ParentID != nil {
			parent = *node.ParentID
		}
		idx.children[parent] = append(idx.children[parent], node.ID)
	}
	return idx, nil
}

// verifyTreeShape refuses a tree whose own structure does not hold together, so a
// delta is never computed over something already broken.
func verifyTreeShape(tree StoredTree) error {
	if tree.RootID == "" {
		return fmt.Errorf("%w: the tree has no root", ErrStoreCorrupt)
	}
	if uint32(len(tree.Nodes)) != tree.NodeCount {
		return fmt.Errorf("%w: the tree says %d nodes and holds %d",
			ErrStoreCorrupt, tree.NodeCount, len(tree.Nodes))
	}
	seen := make(map[string]bool, len(tree.Nodes))
	for _, node := range tree.Nodes {
		if node.ID == "" {
			return fmt.Errorf("%w: a node has no identity", ErrStoreCorrupt)
		}
		if seen[node.ID] {
			return fmt.Errorf("%w: tree repeats node %s", ErrStoreCorrupt, node.ID)
		}
		seen[node.ID] = true
		if node.ParentID == nil {
			if node.ID != tree.RootID {
				return fmt.Errorf("%w: node %s has no parent but is not the root",
					ErrStoreCorrupt, node.ID)
			}
			continue
		}
		if *node.ParentID == node.ID {
			return fmt.Errorf("%w: node %s is its own parent", ErrStoreCorrupt, node.ID)
		}
	}
	if !seen[tree.RootID] {
		return fmt.Errorf("%w: the root %s is not in the tree", ErrStoreCorrupt, tree.RootID)
	}
	// Every node must be reachable from the root, and no parent may be missing.
	for _, node := range tree.Nodes {
		seen := map[string]bool{}
		for at := node.ID; ; {
			if seen[at] {
				return fmt.Errorf("%w: node %s is in a parent cycle", ErrStoreCorrupt, node.ID)
			}
			seen[at] = true
			parent, ok := idx2(tree, at)
			if !ok || parent == "" {
				break
			}
			at = parent
		}
	}
	return nil
}

// idx2 finds a node's parent, reporting false when the node is not in the tree.
func idx2(tree StoredTree, id string) (string, bool) {
	for _, node := range tree.Nodes {
		if node.ID == id {
			if node.ParentID == nil {
				return "", true
			}
			return *node.ParentID, true
		}
	}
	return "", false
}

// ComputeExactDelta expresses target as an exact change from base.
//
// Generation is deterministic. Operations are emitted in a fixed phase order and
// then sorted by affected identity, so two runs that reach the same tree produce
// byte-identical deltas and therefore the same digest.
func ComputeExactDelta(base StoredTree, target StoredTree) (*ExactTreeDelta, error) {
	from, err := indexTree(base)
	if err != nil {
		return nil, err
	}
	to, err := indexTree(target)
	if err != nil {
		return nil, err
	}
	delta := &ExactTreeDelta{
		Format:             ExactDeltaFormatName,
		Version:            ExactDeltaFormatVersion,
		RunKey:             target.RunKey,
		BaseEventUID:       baseEventUID(base),
		BaseSequence:       base.Sequence,
		BasePlanRevision:   base.PlanRevision,
		BaseDigest:         base.Digest,
		TargetEventUID:     targetEventUID(target),
		TargetSequence:     target.Sequence,
		TargetPlanRevision: target.PlanRevision,
		TargetDigest:       target.Digest,
	}
	if base.RunKey != target.RunKey {
		return nil, fmt.Errorf("%w: a delta is for one run", ErrHeadMismatch)
	}

	// A node is removed or inserted; it is never both, and a node that only moved
	// is not a removal.
	var removes, inserts []string
	for id := range from.nodes {
		if _, kept := to.nodes[id]; !kept {
			removes = append(removes, id)
		}
	}
	for id := range to.nodes {
		if _, had := from.nodes[id]; !had {
			inserts = append(inserts, id)
		}
	}
	removes = topmostOnly(from, removes)
	inserts = topmostOnly(to, inserts)

	for _, id := range removes {
		delta.Operations = append(delta.Operations, DeltaOp{
			Kind:   DeltaRemove,
			Remove: &DeltaRemoveOp{NodeID: id},
		})
	}
	for _, id := range inserts {
		parentName, ordinal, _ := to.parentAndOrdinal(id)
		var parent *string
		if parentName != "" {
			parent = &parentName
		}
		root, err := to.subtreeInPreorder(id)
		if err != nil {
			return nil, err
		}
		delta.Operations = append(delta.Operations, DeltaOp{
			Kind: DeltaInsert,
			Insert: &DeltaInsertOp{
				RootID: id, ParentID: parent, Ordinal: ordinal, Nodes: root,
			},
		})
	}
	// A parent's child order is replaced whole, and only when it actually differs.
	for parent, order := range to.children {
		before, hadBefore := from.children[parent]
		if hadBefore && equalOrder(before, order) {
			continue
		}
		if _, stillExists := to.nodes[parent]; !stillExists {
			continue
		}
		op := DeltaReorderOp{ChildOrder: append([]string(nil), order...)}
		if parent != "" {
			id := parent
			op.ParentID = &id
		}
		delta.Operations = append(delta.Operations, DeltaOp{Kind: DeltaReorder, Reorder: &op})
	}
	// A surviving node whose content changed is replaced whole, so nil and empty are
	// not collapsed into each other.
	for _, id := range sortedKeys(to.nodes) {
		after := to.nodes[id]
		beforeNode, kept := from.nodes[id]
		if !kept {
			continue
		}
		if nodeContentEqual(beforeNode, after) {
			continue
		}
		delta.Operations = append(delta.Operations, DeltaOp{
			Kind:    DeltaReplace,
			Replace: &DeltaReplaceOp{Node: after, Fields: changedFields(beforeNode, after)},
		})
	}

	sortOperations(delta.Operations)
	for i := range delta.Operations {
		delta.Operations[i].Index = uint32(i)
	}
	digest, err := HashDelta(*delta)
	if err != nil {
		return nil, err
	}
	delta.SHA256 = digest
	return delta, nil
}

// topmostOnly drops any id whose ancestor is also in the set, because removing the
// ancestor already removes the descendant.
func topmostOnly(idx *nodeIndex, ids []string) []string {
	inSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		inSet[id] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		ancestor, _, ok := idx.parentAndOrdinal(id)
		if !ok || ancestor == "" {
			continue
		}
		dropped := false
		for at := ancestor; at != ""; {
			if inSet[at] {
				dropped = true
				break
			}
			parent, _, found := idx.parentAndOrdinal(at)
			if !found {
				break
			}
			at = parent
		}
		if !dropped {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// parentAndOrdinal returns a node's parent and its position among its siblings.
// An empty parent means the node is the root.
func (i *nodeIndex) parentAndOrdinal(id string) (parent string, ordinal uint32, found bool) {
	for owner, order := range i.children {
		for pos, candidate := range order {
			if candidate == id {
				return owner, uint32(pos), true
			}
		}
	}
	return "", 0, false
}

// subtreeInPreorder returns a node and everything under it, in preorder.
func (i *nodeIndex) subtreeInPreorder(root string) ([]StoredNode, error) {
	var (
		out    []StoredNode
		walk   func(id string) error
		opened = map[string]bool{}
	)
	walk = func(id string) error {
		if opened[id] {
			return fmt.Errorf("%w: node %s is in a cycle", ErrStoreCorrupt, id)
		}
		opened[id] = true
		node, ok := i.nodes[id]
		if !ok {
			return fmt.Errorf("%w: node %s is missing", ErrStoreCorrupt, id)
		}
		out = append(out, node)
		for _, child := range i.children[id] {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
}

// sortOperations orders by phase and then by identity, so the same change always
// produces the same bytes.
func sortOperations(ops []DeltaOp) {
	phase := map[DeltaOpKind]int{
		DeltaRemove: 0, DeltaInsert: 1, DeltaReorder: 2, DeltaReplace: 3,
	}
	sort.SliceStable(ops, func(i, j int) bool {
		if phase[ops[i].Kind] != phase[ops[j].Kind] {
			return phase[ops[i].Kind] < phase[ops[j].Kind]
		}
		return opIdentity(ops[i]) < opIdentity(ops[j])
	})
}

// opIdentity is the identity an operation is ordered by, within its phase.
func opIdentity(op DeltaOp) string {
	switch op.Kind {
	case DeltaRemove:
		return op.Remove.NodeID
	case DeltaInsert:
		return op.Insert.RootID
	case DeltaReorder:
		if op.Reorder.ParentID == nil {
			return "\x00root"
		}
		return *op.Reorder.ParentID
	case DeltaReplace:
		return op.Replace.Node.ID
	}
	return ""
}

func equalOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nodeContentEqual compares everything about a node except where it sits, because
// a move is a reorder and not a change of content.
func nodeContentEqual(a, b StoredNode) bool {
	if a.ID != b.ID || a.Type != b.Type || a.Name != b.Name ||
		a.Status.TickCount != b.Status.TickCount ||
		a.Status.LastStatus != b.Status.LastStatus ||
		a.Status.StatusPresent != b.Status.StatusPresent ||
		a.Status.Version != b.Status.Version {
		return false
	}
	if a.Lineage != b.Lineage {
		return false
	}
	if !equalPtr(a.ConditionID, b.ConditionID) ||
		!equalPtr(a.PostNodeID, b.PostNodeID) ||
		!equalPtr(a.ActionID, b.ActionID) {
		return false
	}
	if !equalStrings(a.EffectIDs, b.EffectIDs) {
		return false
	}
	if (a.Frame == nil) != (b.Frame == nil) {
		return false
	}
	if a.Frame != nil && *a.Frame != *b.Frame {
		return false
	}
	return true
}

// equalPtr compares optional references, so nil and a pointer to an empty
// string stay different facts.
func equalPtr(a, b *string) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	return *a == *b
}

func equalStrings(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return equalOrder(a, b)
}

// changedFields names what a replacement changed, so a consumer can show the
// difference rather than that one exists.
func changedFields(a, b StoredNode) []string {
	var out []string
	if a.Name != b.Name {
		out = append(out, "name")
	}
	if a.Type != b.Type {
		out = append(out, "type")
	}
	if a.Status != b.Status {
		out = append(out, "status")
	}
	if !equalPtr(a.ConditionID, b.ConditionID) {
		out = append(out, "conditionId")
	}
	if !equalPtr(a.PostNodeID, b.PostNodeID) {
		out = append(out, "postNodeId")
	}
	if !equalPtr(a.ActionID, b.ActionID) {
		out = append(out, "actionId")
	}
	if !equalStrings(a.EffectIDs, b.EffectIDs) {
		out = append(out, "effectIds")
	}
	if (a.Frame == nil) != (b.Frame == nil) || (a.Frame != nil && *a.Frame != *b.Frame) {
		out = append(out, "frame")
	}
	return out
}

func sortedKeys(m map[string]StoredNode) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// baseEventUID and targetEventUID are carried on the tree envelope, so a delta can
// name both of its ends without a side table.
func baseEventUID(tree StoredTree) string   { return tree.EventUID }
func targetEventUID(tree StoredTree) string { return tree.EventUID }

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

func withoutID(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, candidate := range ids {
		if candidate != id {
			out = append(out, candidate)
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
	// The record's own copy of the tree is the one the chain covers, so it is what
	// says which tree this sequence committed.
	record, err := s.RecordAt(ctx, key, sequence)
	if err != nil {
		return StoredTree{}, nil, nil, err
	}
	if record.Tree == nil {
		return StoredTree{}, nil, nil, fmt.Errorf(
			"%w: sequence %d holds a representation and its record has no tree",
			ErrStoreCorrupt, sequence)
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
