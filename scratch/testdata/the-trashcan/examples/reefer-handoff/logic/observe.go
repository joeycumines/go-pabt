// Copyright 2021 Joseph Cumines
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build example
// +build example

package logic

import (
	"github.com/joeycumines/go-pabt"
)

// reeferObserver reports post-action values by reading the effect keys through
// the borrowed view the planner hands it, which is what makes the observed
// value a reading rather than a restatement of the declared one. The view is
// the tick's pinned frame, so every value comes from the same world the plan
// reasoned over.
type reeferObserver struct{}

func (reeferObserver) ObserveExecution(req pabt.ActionObservationRequest) (pabt.ActionObservation, error) {
	out := pabt.ActionObservation{}
	for _, e := range req.Effects {
		value, err := req.View.Variable(e.Key)
		if err != nil {
			out.EffectValues = append(out.EffectValues, pabt.EffectObservation{
				EffectID: e.ID, Err: &pabt.RecordedError{Type: "reefer-observe", Message: err.Error()},
			})
			continue
		}
		out.EffectValues = append(out.EffectValues, pabt.EffectObservation{
			EffectID: e.ID, Value: value, Known: true,
		})
	}
	return out, nil
}

// reeferVerifier concludes one relation per effect by comparing the observed
// post-action value with the value the action declared. A shared variable key
// is support, not causation: the verdict says what was seen, and only the
// planner's Authoritative flag — set from the coherence of the bracketing
// revisions, never by this verifier — may claim a relation. Anything else
// renders as model support, with the reason.
type reeferVerifier struct{}

func (reeferVerifier) VerifyTransitions(req pabt.TransitionRequest) ([]pabt.RelationVerdict, error) {
	out := make([]pabt.RelationVerdict, 0, len(req.Effects))
	for _, e := range req.Effects {
		result := pabt.RelationDoesNotDecide
		for _, seen := range req.Observation.EffectValues {
			if seen.EffectID == e.ID && seen.Known && seen.Value == e.Value {
				result = pabt.RelationPreserves
			}
		}
		out = append(out, pabt.RelationVerdict{
			EffectID: e.ID, PostconditionID: req.PostconditionID,
			Result: result, Method: "reefer_observed_value_equals_declared_value",
		})
	}
	return out, nil
}
