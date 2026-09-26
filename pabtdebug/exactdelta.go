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

// optionalID returns a pointer to a node's identity, or nil for the root, which is
// how a delta says "the whole tree" without a name.
func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	out := id
	return &out
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
	//
	// Both trees' parents are walked, not only the target's. A parent that loses
	// its last child has no entry in the target's child map, so walking the target
	// alone emitted no order for it and left the base's list naming a node the
	// delta had just removed. The applier then refused its own delta, and a tick
	// whose plan dropped an action's only child failed to commit as a storage
	// error. A delta that says "remove X" has to say what X's parent is left
	// holding, or it is not exact.
	for parent := range from.children {
		if _, inTarget := to.children[parent]; inTarget {
			continue
		}
		if _, stillExists := to.nodes[parent]; !stillExists {
			continue // The parent went with its subtree.
		}
		if len(from.children[parent]) == 0 {
			continue // It had no children either, so there is nothing to say.
		}
		delta.Operations = append(delta.Operations, DeltaOp{
			Kind: DeltaReorder,
			Reorder: &DeltaReorderOp{
				ParentID:   optionalID(parent),
				ChildOrder: []string{},
			},
		})
	}
	for parent, order := range to.children {
		before, hadBefore := from.children[parent]
		if hadBefore && equalOrder(before, order) {
			continue
		}
		if _, stillExists := to.nodes[parent]; !stillExists {
			continue
		}
		delta.Operations = append(delta.Operations, DeltaOp{
			Kind:    DeltaReorder,
			Reorder: &DeltaReorderOp{ParentID: optionalID(parent), ChildOrder: append([]string(nil), order...)},
		})
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
