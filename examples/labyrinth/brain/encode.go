//go:build example

package brain

import (
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
	// Heuristic: find a condition with Truth=false that has actions targeting it
	// as a postcondition. This indicates recent expansion.
	for _, c := range snap.Conditions {
		if !c.Truth && c.TruthKnown {
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

	return f
}

func keyString(key any) string {
	if k, ok := key.(VarKey); ok {
		return k.String()
	}
	return ""
}
