//go:build example

package brain

import (
	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// BTFrame is the encoded, wire-ready description of one minotaur's BT tick.
// It is built from the TickSnapshot INSIDE the tick boundary, before the
// snapshot's aliased values can change. All fields are plain values (no
// pointers into planner state), so a BTFrame is safe to retain and send.
type BTFrame struct {
	PlanRevision uint64        `json:"planRevision"`
	Coherence    string        `json:"coherence"`
	Nodes        []BTNode      `json:"nodes"`
	Conditions   []BTCondition `json:"conditions"`
	Effects      []BTEffect    `json:"effects"`
	Actions      []BTAction    `json:"actions"`
	Executions   []BTExecution `json:"executions"`
	Path         string        `json:"path"`
	WorldView    BTWorldView   `json:"worldView"`
	// H3: Planning process visibility.
	FailedCondition string   `json:"failedCondition,omitempty"`
	Candidates      []string `json:"candidates,omitempty"`
	// H6: Closed-set sizes.
	ClosedSetExpanded   int `json:"closedSetExpanded,omitempty"`
	ClosedSetUnexpanded int `json:"closedSetUnexpanded,omitempty"`
	// I5: Candidates with cost and verdict for planner reasoning overlay.
	CandidatesWithCost []CandidateInfo `json:"candidatesWithCost,omitempty"`
}

// CandidateInfo is one candidate action with its cost and verdict.
type CandidateInfo struct {
	ActionName string `json:"name"`
	Cost       int    `json:"cost"`
	Verdict    string `json:"verdict"`
	Executing  bool   `json:"executing"` // true if this action is currently executing
}

// BTWorldView is the actor's elemental and detection state at frame time. It
// makes elemental-disadvantage planning directly observable in the debugging
// GUI: an actor at disadvantage will show atDisadvantage=true alongside its
// retreat/reinforce action selection.
type BTWorldView struct {
	PlayerElement  int     `json:"playerElement"`
	SelfElement    int     `json:"selfElement"`
	AtDisadvantage bool    `json:"atDisadvantage"`
	AtAdvantage    bool    `json:"atAdvantage"`
	PlayerDetected bool    `json:"playerDetected"`
	Stunned        bool    `json:"stunned"`
	DistanceBand   int     `json:"distanceBand"`
	LastKnownX     float64 `json:"lastKnownX"`
	LastKnownY     float64 `json:"lastKnownY"`
	HasLastKnown   bool    `json:"hasLastKnown"`
}

// BTNode is one node in the encoded tree.
type BTNode struct {
	ID            string `json:"id"`
	Parent        string `json:"parent"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	TickCount     int    `json:"tickCount"`
	LastStatus    int    `json:"lastStatus"`
	StatusPresent bool   `json:"statusPresent"`
}

// BTCondition is one encoded condition.
type BTCondition struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Expected      any    `json:"expected"`
	ExpectedKnown bool   `json:"expectedKnown"`
	Truth         int    `json:"truth"`
	TruthKnown    bool   `json:"truthKnown"`
}

// BTEffect is one encoded effect.
type BTEffect struct {
	ID         string `json:"id"`
	ActionID   string `json:"actionId"`
	Key        string `json:"key"`
	Value      any    `json:"value"`
	ValueKnown bool   `json:"valueKnown"`
}

// BTAction is one encoded action.
type BTAction struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	PostconditionID string   `json:"postconditionId"`
	EffectIDs       []string `json:"effectIds"`
	ConditionIDs    []string `json:"conditionIds"`
}

// BTExecution is one encoded action execution.
type BTExecution struct {
	ActionID      string          `json:"actionId"`
	Status        int             `json:"status"`
	StatusPresent bool            `json:"statusPresent"`
	Err           string          `json:"err,omitempty"`
	Observations  []BTObservation `json:"observations"`
	Relations     []BTRelation    `json:"relations"`
}

// BTObservation is one observed post-action effect value.
type BTObservation struct {
	EffectID string `json:"effectId"`
	Value    any    `json:"value"`
	Known    bool   `json:"known"`
}

// BTRelation is one transition verdict.
type BTRelation struct {
	EffectID        string `json:"effectId"`
	PostconditionID string `json:"postconditionId"`
	Result          string `json:"result"`
	Method          string `json:"method"`
	Authoritative   bool   `json:"authoritative"`
}

// EncodeFrame converts a TickSnapshot and the plan's current path into a
// BTFrame. It must be called before the tick returns, because the snapshot's
// values alias live planner state.
func EncodeFrame(snap pabt.TickSnapshot, path pabt.Path[Condition]) *BTFrame {
	f := &BTFrame{
		PlanRevision: snap.PlanRevision,
		Coherence:    string(snap.Coherence),
		Path:         path.String(),
	}

	for _, n := range snap.Nodes {
		f.Nodes = append(f.Nodes, BTNode{
			ID:            string(n.ID),
			Parent:        string(n.Parent),
			Type:          n.Type.String(),
			Name:          n.Name,
			TickCount:     n.Status.TickCount,
			LastStatus:    int(n.Status.LastStatus),
			StatusPresent: n.Status.StatusPresent,
		})
	}

	for _, c := range snap.Conditions {
		f.Conditions = append(f.Conditions, BTCondition{
			ID:            string(c.ID),
			Key:           keyString(c.Key),
			Expected:      c.Expected,
			ExpectedKnown: c.ExpectedKnown,
			Truth:         int(c.Truth),
			TruthKnown:    c.TruthKnown,
		})
	}

	for _, e := range snap.Effects {
		f.Effects = append(f.Effects, BTEffect{
			ID:         string(e.ID),
			ActionID:   string(e.ActionID),
			Key:        keyString(e.Key),
			Value:      e.Value,
			ValueKnown: e.ValueKnown,
		})
	}

	for _, a := range snap.Actions {
		act := BTAction{
			ID:              string(a.ID),
			Name:            a.Name,
			PostconditionID: string(a.PostconditionID),
		}
		for _, id := range a.EffectIDs {
			act.EffectIDs = append(act.EffectIDs, string(id))
		}
		for _, id := range a.ConditionIDs {
			act.ConditionIDs = append(act.ConditionIDs, string(id))
		}
		f.Actions = append(f.Actions, act)
	}

	for _, ex := range snap.Executions {
		exec := BTExecution{
			ActionID:      string(ex.Execution.ActionID),
			Status:        int(ex.Execution.Status),
			StatusPresent: ex.Execution.StatusPresent,
		}
		if ex.Execution.Err != nil {
			exec.Err = ex.Execution.Err.Message
		}
		for _, obs := range ex.Observation.EffectValues {
			exec.Observations = append(exec.Observations, BTObservation{
				EffectID: string(obs.EffectID),
				Value:    obs.Value,
				Known:    obs.Known,
			})
		}
		for _, rel := range ex.Relations {
			exec.Relations = append(exec.Relations, BTRelation{
				EffectID:        string(rel.EffectID),
				PostconditionID: string(rel.PostconditionID),
				Result:          string(rel.Result),
				Method:          rel.Method,
				Authoritative:   rel.Authoritative,
			})
		}
		f.Executions = append(f.Executions, exec)
	}

	// H3: Identify the most recent failed condition and candidates considered.
	// Heuristic: find a condition with Truth=Failure that has actions targeting it
	// as a postcondition. This indicates recent expansion.
	for _, c := range snap.Conditions {
		if c.Truth == bt.Failure && c.TruthKnown {
			var candidates []string
			for _, a := range snap.Actions {
				if a.PostconditionID == c.ID {
					candidates = append(candidates, a.Name)
				}
			}
			if len(candidates) > 0 {
				f.FailedCondition = keyString(c.Key)
				f.Candidates = candidates
				break
			}
		}
	}

	// H6: Closed-set sizes — count conditions with TruthKnown (expanded) vs
	// without (unexpanded).
	var expanded, unexpanded int
	for _, c := range snap.Conditions {
		if c.TruthKnown {
			expanded++
		} else {
			unexpanded++
		}
	}
	f.ClosedSetExpanded = expanded
	f.ClosedSetUnexpanded = unexpanded

	// I5: Populate CandidatesWithCost with cost and verdict per candidate.
	// Show all actions in the plan with their costs, highlighting the
	// currently executing action. This makes the planner's reasoning visible
	// even during successful execution, not just during replanning.
	
	// First, find the currently executing action (if any).
	var executingActionID pabt.OccurrenceID
	if len(snap.Executions) > 0 {
		lastExec := snap.Executions[len(snap.Executions)-1]
		executingActionID = lastExec.Execution.ActionID
	}
	
	// Show all actions with their costs.
	for _, a := range snap.Actions {
		info := CandidateInfo{
			ActionName: a.Name,
			Cost:       actionCost(a.Name),
			Verdict:    "unknown",
			Executing:  a.ID == executingActionID,
		}
		// Find a verdict from any execution of this action.
		for _, ex := range snap.Executions {
			if ex.Execution.ActionID == a.ID && len(ex.Relations) > 0 {
				// Use the last relation's result as the verdict.
				last := ex.Relations[len(ex.Relations)-1]
				info.Verdict = string(last.Result)
				break
			}
		}
		f.CandidatesWithCost = append(f.CandidatesWithCost, info)
	}

	return f
}

func actionCost(name string) int {
	switch name {
	case "Pursue":
		return 1
	case "Search":
		return 2
	case "Recover":
		return 1
	case "Retreat":
		return 3
	case "Reinforce":
		return 2
	default:
		return 99
	}
}

func keyString(key any) string {
	if k, ok := key.(VarKey); ok {
		return k.String()
	}
	return ""
}
