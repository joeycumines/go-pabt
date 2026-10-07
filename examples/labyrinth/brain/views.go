//go:build example

package brain

import (
	"fmt"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// ViewProvider is a pabt.TypedStateViewProvider over the belief. It opens
// views labelled CoherenceRevisioned with the brain's belief revision, so a
// transition claim bracketing one action is authoritative.
type ViewProvider struct {
	state *State
	brain *Brain
}

// NewViewProvider builds a provider over the given state and brain.
func NewViewProvider(s *State, b *Brain) *ViewProvider {
	return &ViewProvider{state: s, brain: b}
}

// OpenStateView implements pabt.TypedStateViewProvider.
func (p *ViewProvider) OpenStateView(req pabt.StateViewRequest) (pabt.TypedStateView[Condition], error) {
	if p == nil || p.state == nil {
		return nil, fmt.Errorf("brain: nil state for view")
	}
	return &beliefView{state: p.state, brain: p.brain, req: req}, nil
}

// beliefView is one state view over the belief.
type beliefView struct {
	state *State
	brain *Brain
	req   pabt.StateViewRequest
}

func (v *beliefView) Variable(key any) (any, error) { return v.state.Variable(key) }

func (v *beliefView) Actions(failed Condition) ([]pabt.Action[Condition], error) {
	return v.state.Actions(failed)
}

func (v *beliefView) Revision() pabt.StateRevision {
	return pabt.StateRevision{
		PlanRevision:  v.req.PlanRevision,
		WorldRevision: fmt.Sprintf("%d", v.brain.BeliefRevision()),
	}
}

func (v *beliefView) Coherence() pabt.StateCoherence {
	return pabt.CoherenceRevisioned
}

func (v *beliefView) Close() error { return nil }

// Observer is a pabt.ExecutionObserver that reads the post-action belief
// values for the action's effects.
type Observer struct {
	state *State
}

func NewObserver(s *State) *Observer { return &Observer{state: s} }

// ObserveExecution implements pabt.ExecutionObserver.
func (o *Observer) ObserveExecution(req pabt.ActionObservationRequest) (pabt.ActionObservation, error) {
	var out pabt.ActionObservation
	for _, eff := range req.Effects {
		k, ok := eff.Key.(VarKey)
		if !ok {
			continue
		}
		val, err := o.state.Variable(k)
		obs := pabt.EffectObservation{EffectID: eff.ID, Known: err == nil}
		if err == nil {
			obs.Value = val
		}
		out.EffectValues = append(out.EffectValues, obs)
	}
	return out, nil
}

// Verifier is a pabt.TransitionVerifier that issues preserves/breaks verdicts
// by comparing each effect's declared value against the post-action belief.
type Verifier struct {
	state *State
}

func NewVerifier(s *State) *Verifier { return &Verifier{state: s} }

// VerifyTransitions implements pabt.TransitionVerifier.
func (v *Verifier) VerifyTransitions(req pabt.TransitionRequest) ([]pabt.RelationVerdict, error) {
	var out []pabt.RelationVerdict
	for _, eff := range req.Effects {
		k, ok := eff.Key.(VarKey)
		if !ok {
			continue
		}
		val, err := v.state.Variable(k)
		verdict := pabt.RelationVerdict{
			EffectID:        eff.ID,
			PostconditionID: req.PostconditionID,
			Method:          "belief-compare",
		}
		if err != nil {
			verdict.Result = pabt.RelationUnavailable
			out = append(out, verdict)
			continue
		}
		// Compare the post-action value against the effect's declared value.
		if valuesEqual(val, eff.Value) {
			verdict.Result = pabt.RelationPreserves
		} else {
			verdict.Result = pabt.RelationBreaks
		}
		out = append(out, verdict)
	}
	return out, nil
}

func valuesEqual(a, b any) bool {
	switch av := a.(type) {
	case bool:
		if bv, ok := b.(bool); ok {
			return av == bv
		}
	case int:
		if bv, ok := b.(int); ok {
			return av == bv
		}
	case maze.Vec2:
		if bv, ok := b.(maze.Vec2); ok {
			return av == bv
		}
	}
	return a == b
}
