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
	"fmt"

	"github.com/joeycumines/go-pabt"
)

// Turning one captured tick into the record's stored sections.
//
// The tick path in session.go owns the durable order: when a tick happens, what
// it produced, and what is committed when it fails. This file owns the
// translation of what the capture boundary reported into the record the store
// keeps: the tree, the conditions with their four evidence fields, the actions,
// the effects, and the verifier's relations. The two are separate jobs - one is
// about ordering and termination, the other about representation - and a change
// to either should not have to read the other.

// storedTree turns the capture boundary's nodes into the store's tree.
//
// The nodes are already in preorder with their parents, so the tree is the same
// sequence with each node's content digest sealed. A node's ordinal follows from
// its position, which is what makes a later delta able to express a move.
//
// Each node is also told what it is, because a tree whose nodes carry only
// shape cannot answer which preconditions govern a selected occurrence. The
// links are filled in one pass before any node is digested, so a node's content
// hash covers the links and a delta that changes one reports it.
func (s *Session) storedTree(snapshot pabt.TickSnapshot) (*StoredTree, error) {
	if len(snapshot.Nodes) == 0 {
		return nil, fmt.Errorf(`pabtdebug: the capture reported no nodes`)
	}
	tree := &StoredTree{
		// The run is named here so a delta can be computed against a base; the store
		// stamps the event and sequence when it commits. A tree that did not name
		// its run could not be compared with any base, so no delta would ever be
		// stored from this path.
		RunKey:       s.key,
		RootID:       string(snapshot.RootID),
		Nodes:        make([]StoredNode, 0, len(snapshot.Nodes)),
		NodeCount:    uint32(len(snapshot.Nodes)),
		PlanRevision: snapshot.PlanRevision,
	}
	// A condition is evaluated by two nodes: the leaf that first read it and the
	// post-condition node expansion created to re-evaluate it. Indexing both
	// before the walk is what lets a node say which of the two it is.
	conditionNode := make(map[string]string, len(snapshot.Conditions))
	postNode := make(map[string]string, len(snapshot.Conditions))
	for _, condition := range snapshot.Conditions {
		if condition.NodeID != "" {
			conditionNode[string(condition.NodeID)] = string(condition.ID)
		}
		if condition.PostNodeID != "" {
			postNode[string(condition.PostNodeID)] = string(condition.NodeID)
		}
	}
	actionNode := make(map[string]pabt.ActionOccurrence, len(snapshot.Actions))
	for _, action := range snapshot.Actions {
		if action.NodeID != "" {
			actionNode[string(action.NodeID)] = action
		}
	}
	for _, occurrence := range snapshot.Nodes {
		status, err := NodeStatusOf(occurrence.Status)
		if err != nil {
			return nil, err
		}
		node := StoredNode{
			ID:        string(occurrence.ID),
			Lineage:   string(occurrence.Lineage),
			Type:      occurrence.Type.String(),
			Name:      occurrence.Name,
			Status:    status,
			Ordinal:   occurrence.Ordinal,
			EffectIDs: []string{},
			Frame:     occurrence.Frame,
		}
		if occurrence.Parent != "" {
			parent := string(occurrence.Parent)
			node.ParentID = &parent
		}
		if id, ok := conditionNode[node.ID]; ok {
			condition := id
			node.ConditionID = &condition
		}
		if id, ok := postNode[node.ID]; ok {
			post := id
			node.PostNodeID = &post
		}
		if action, ok := actionNode[node.ID]; ok {
			id := string(action.ID)
			node.ActionID = &id
			for _, effectID := range action.EffectIDs {
				node.EffectIDs = append(node.EffectIDs, string(effectID))
			}
		}
		digest, err := HashNode(node)
		if err != nil {
			return nil, err
		}
		node.ContentHash = digest
		tree.Nodes = append(tree.Nodes, node)
	}
	digest, err := HashTree(*tree)
	if err != nil {
		return nil, err
	}
	tree.Digest = digest
	return tree, nil
}

// storedActions turns the capture boundary's actions into the store's own list.
//
// The action is the relation that connects an effect to the condition the
// action was there to make, so it is recorded as a section rather than left to
// be inferred from an effect and a node that happen to share a variable key.
func storedActions(snapshot pabt.TickSnapshot) []StoredAction {
	out := make([]StoredAction, 0, len(snapshot.Actions))
	for _, action := range snapshot.Actions {
		stored := StoredAction{
			ActionID: string(action.ID),
			Name:     action.Name,
			// The slices are made rather than aliased: a nil and an empty list are
			// different facts in a canonical record, and the snapshot's own nil must
			// not become this record's spelling.
			ConditionIDs: make([]string, 0, len(action.ConditionIDs)),
			EffectIDs:    make([]string, 0, len(action.EffectIDs)),
		}
		if action.NodeID != "" {
			node := string(action.NodeID)
			stored.NodeID = &node
		}
		if action.PostconditionID != "" {
			post := string(action.PostconditionID)
			stored.PostconditionID = &post
		}
		for _, id := range action.ConditionIDs {
			stored.ConditionIDs = append(stored.ConditionIDs, string(id))
		}
		for _, id := range action.EffectIDs {
			stored.EffectIDs = append(stored.EffectIDs, string(id))
		}
		out = append(out, stored)
	}
	return out
}

// storedEffects turns the capture boundary's effects into the store's own list,
// encoding each declared value so a redacted secret is redacted on this path
// too rather than only on the conditions that read it back.
func (s *Session) storedEffects(
	ctx context.Context,
	snapshot pabt.TickSnapshot,
) ([]StoredEffect, error) {
	out := make([]StoredEffect, 0, len(snapshot.Effects))
	for _, effect := range snapshot.Effects {
		stored := StoredEffect{
			EffectID: string(effect.ID),
			ActionID: string(effect.ActionID),
			Uncertainty: StoredUncertainty{
				State: UncertaintyNone,
			},
		}
		key, err := s.codecs.Encode(ctx, "effect."+stored.EffectID+".key", effect.Key)
		if err != nil {
			return nil, err
		}
		stored.Key = key
		switch {
		case effect.ValueKnown:
			encoded, err := s.codecs.Encode(ctx, "effect."+stored.EffectID+".value", effect.Value)
			if err != nil {
				return nil, err
			}
			if encoded.Visibility == ValueError {
				// A codec that fails returns the reason as a value, so the reason is
				// read here or it is lost, exactly as it is on a condition.
				stored.Value = encoded
				stored.Uncertainty = StoredUncertainty{
					State:   UncertaintyError,
					Code:    "codec_failed",
					Message: s.codecs.sanitizeText(encoded.Error),
				}
			} else {
				stored.Value = encoded
			}
		default:
			stored.Value = StoredValue{
				Visibility: ValueUnknown,
				Source:     "effect." + stored.EffectID + ".value",
			}
			stored.Uncertainty = StoredUncertainty{
				State:   UncertaintyUnavailable,
				Code:    "no_value",
				Message: "the action declared this effect without a value",
			}
		}
		out = append(out, stored)
	}
	return out, nil
}

// storedConditions turns the capture boundary's conditions into the store's
// four-field evidence, keeping expected, current, observed, and uncertainty apart.
//
// A value the application did not declare stays unknown. It is never replaced by
// one derived from the key, because a key names a variable and not a
// requirement, and a consumer shown a derived value would be reading a claim
// nothing supports.
func (s *Session) storedConditions(
	ctx context.Context,
	snapshot pabt.TickSnapshot,
) ([]StoredCondition, error) {
	observed, uncertainties := s.observedByCondition(ctx, snapshot)
	currentByCondition := currentValuesByCondition(ctx, s, snapshot)
	out := make([]StoredCondition, 0, len(snapshot.Conditions))
	for _, occurrence := range snapshot.Conditions {
		source := "condition." + string(occurrence.ID)
		condition := StoredCondition{
			ConditionID: string(occurrence.ID),
			Kind:        "precondition",
			Revision: RevisionScope{
				PlanRevision: snapshot.PlanRevision,
				State:        snapshot.StateRevision,
				Coherence:    snapshot.Coherence,
			},
		}
		// The key names the variable this condition reads. It goes through the codec
		// like every other piece of application data, because it is application text
		// that will be rendered in a browser: an unscrubbed string field would be the
		// one place a recorded secret could reach a response unmediated.
		key, err := s.codecs.Encode(ctx, source+".key", occurrence.Key)
		if err != nil {
			return nil, err
		}
		condition.Key = key
		if occurrence.NodeID != "" {
			node := string(occurrence.NodeID)
			condition.NodeID = &node
		}
		if occurrence.PostNodeID != "" {
			post := string(occurrence.PostNodeID)
			condition.PostNodeID = &post
		}
		if occurrence.ExpectedKnown {
			encoded, err := s.codecs.Encode(ctx, source+".expected", occurrence.Expected)
			if err != nil {
				return nil, err
			}
			condition.Expected = encoded
		} else {
			condition.Expected = StoredValue{
				Visibility: ValueUnknown,
				Source:     source + ".expected",
				Error:      string(UncertaintyUndeclared),
			}
			condition.Uncertainty = StoredUncertainty{
				State:   UncertaintyUndeclared,
				Code:    "no_declaration",
				Message: "the application declares no expected value for this condition",
			}
		}
		// The current value is the one the planner actually read and matched, taken
		// from the variable read recorded for this condition. A condition the
		// planner never evaluated has none, and claiming it is exact while carrying
		// nothing would tell a reader the value is available when it is not.
		current := StoredValue{
			Visibility: ValueUnknown,
			Source:     source + ".current",
		}
		if read, ok := currentByCondition[string(occurrence.ID)]; ok {
			current = read
		}
		condition.Current = current
		if occurrence.TruthKnown {
			text, err := StatusText(occurrence.Truth, true)
			if err != nil {
				return nil, err
			}
			condition.Truth = text
		}
		if seen, ok := observed[string(occurrence.ID)]; ok {
			condition.Observed = &seen
		}
		if reason, ok := uncertainties[string(occurrence.ID)]; ok {
			condition.Uncertainty = reason
		} else if condition.Uncertainty.State == "" {
			condition.Uncertainty = StoredUncertainty{State: UncertaintyNone}
		}
		out = append(out, condition)
	}
	return out, nil
}

// observedByCondition attaches the post-action values an observer reported to the
// condition each one explains.
//
// The bridge is through the action, not through a node. An observation names the
// effect it read, an effect belongs to an action, and an action's postcondition is
// the condition whose re-evaluation the action was there to make. Looking a
// post-condition *node* id up in a map keyed by *effect* ids finds nothing, because
// the two are minted from one counter but are different identities, and that
// mistake left every condition with no observed value at all.
func (s *Session) observedByCondition(
	ctx context.Context,
	snapshot pabt.TickSnapshot,
) (map[string]StoredValue, map[string]StoredUncertainty) {
	values := map[string]StoredValue{}
	unknown := map[string]StoredUncertainty{}

	// Two actions supporting one condition in a single tick both report a value,
	// and the record carries one. The last execution wins, which is deterministic
	// because the snapshot's order is the planner's, and is recorded here rather
	// than left to a map's iteration order.
	postOfAction := make(map[string]string, len(snapshot.Actions))
	for _, action := range snapshot.Actions {
		if action.PostconditionID != "" {
			postOfAction[string(action.ID)] = string(action.PostconditionID)
		}
	}
	for _, evidence := range snapshot.Executions {
		conditionID, ok := postOfAction[string(evidence.Execution.ActionID)]
		if !ok {
			continue
		}
		if evidence.Err != nil {
			// The observer or verifier failed as a whole rather than per value.
			// Reading only the per-value errors reported nothing at all for a
			// post-action read that had in fact failed.
			unknown[conditionID] = StoredUncertainty{
				State:   UncertaintyError,
				Code:    "observer_error",
				Message: s.codecs.sanitizeText(evidence.Err.Message),
			}
			continue
		}
		for _, observation := range evidence.Observation.EffectValues {
			source := "observed." + conditionID + "." + string(observation.EffectID)
			switch {
			case observation.Err != nil:
				unknown[conditionID] = StoredUncertainty{
					State:   UncertaintyError,
					Code:    "observer_error",
					Message: s.codecs.sanitizeText(observation.Err.Message),
				}
			case !observation.Known:
				if _, already := unknown[conditionID]; !already {
					unknown[conditionID] = StoredUncertainty{
						State:   UncertaintyUnavailable,
						Code:    "not_observed",
						Message: "the observer did not read this value",
					}
				}
			default:
				encoded, err := s.codecs.Encode(ctx, source, observation.Value)
				switch {
				case err != nil:
					unknown[conditionID] = StoredUncertainty{
						State:   UncertaintyError,
						Code:    "encoding_failed",
						Message: s.codecs.sanitizeText(err.Error()),
					}
				case encoded.Visibility == ValueError:
					// A codec that fails comes back as a value carrying the reason
					// rather than as an error, so the reason is read from the value
					// or it is lost.
					unknown[conditionID] = StoredUncertainty{
						State:   UncertaintyError,
						Code:    "codec_failed",
						Message: s.codecs.sanitizeText(encoded.Error),
					}
				default:
					values[conditionID] = encoded
				}
			}
		}
	}
	return values, unknown
}

// currentValuesByCondition collects the variable each condition was matched
// against.
//
// The capture boundary records the read that fed the matcher, so the value is the
// one the planner saw rather than one looked up again afterwards, which could
// have moved.
func currentValuesByCondition(
	ctx context.Context,
	s *Session,
	snapshot pabt.TickSnapshot,
) map[string]StoredValue {
	out := map[string]StoredValue{}
	for _, callback := range snapshot.Callbacks {
		if callback.Kind != pabt.CallbackVariableRead || callback.SubjectID == "" {
			continue
		}
		encoded, err := s.codecs.Encode(ctx, "current."+string(callback.SubjectID), callback.Result)
		if err != nil {
			continue
		}
		out[string(callback.SubjectID)] = encoded
	}
	return out
}

// storedRelations turns the capture boundary's verdicts into stored relations,
// recording whether the planner considered a claim authoritative.
//
// The authority decision is the planner's, made in the capture boundary from the
// coherence of the revisions bracketing the action. It is recorded rather than
// recomputed, because recomputing it here would be a second opinion about a fact
// the planner already had.
func (s *Session) storedRelations(
	_ context.Context,
	snapshot pabt.TickSnapshot,
) []StoredRelation {
	var out []StoredRelation
	for _, evidence := range snapshot.Executions {
		for _, verdict := range evidence.Relations {
			relation := StoredRelation{
				RelationID:      string(verdict.EffectID) + "->" + string(verdict.PostconditionID),
				EffectID:        string(verdict.EffectID),
				PostconditionID: string(verdict.PostconditionID),
				Result:          string(verdict.Result),
				Method:          verdict.Method,
				Authoritative:   verdict.Authoritative,
				Revision:        RevisionScope{Coherence: snapshot.Coherence},
			}
			// The authority is recorded as its own word, so a reader can tell a
			// claim the planner stood behind from one it did not.
			if verdict.Authoritative {
				relation.Authority = "transition_verifier"
			} else {
				relation.Authority = "reported_not_authoritative"
			}
			if verdict.Err != nil {
				relation.Error = &StoredError{
					Code:    ErrorCodeVerifier,
					Type:    verdict.Err.Type,
					Message: s.codecs.sanitizeText(errorText(verdict.Err)),
				}
			}
			out = append(out, relation)
		}
	}
	return out
}
