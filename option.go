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

package pabt

import "fmt"

// stateViewsOption configures the state view provider for a plan.
type stateViewsOption[T Condition] struct {
	provider TypedStateViewProvider[T]
}

func (o *stateViewsOption[T]) applyOption(c *config[T]) error {
	if o == nil || o.provider == nil {
		return fmt.Errorf(`pabt: nil state view provider`)
	}
	c.views = o.provider
	return nil
}

// WithStateViews sets the state view provider a captured plan reads through.
//
// A configured provider is authoritative: a captured plan reads every
// condition value and action list through it and never falls back to the raw
// [State]. A plan built without this option uses [NewLiveStateViewProvider]
// and labels its reads [CoherenceLive].
func WithStateViews[T Condition](provider TypedStateViewProvider[T]) *stateViewsOption[T] {
	return &stateViewsOption[T]{provider: provider}
}

// executionObserverOption configures the post-action observer for a plan.
type executionObserverOption[T Condition] struct {
	observer ExecutionObserver
}

func (o *executionObserverOption[T]) applyOption(c *config[T]) error {
	if o == nil || o.observer == nil {
		return fmt.Errorf(`pabt: nil execution observer`)
	}
	c.observer = o.observer
	return nil
}

// WithExecutionObserver sets the observer that reports what each action
// changed. Without it, no observed value exists and consumers must show the
// value as unavailable rather than deriving one.
func WithExecutionObserver[T Condition](observer ExecutionObserver) *executionObserverOption[T] {
	return &executionObserverOption[T]{observer: observer}
}

// transitionVerifierOption configures the transition verifier for a plan.
type transitionVerifierOption[T Condition] struct {
	verifier TransitionVerifier
}

func (o *transitionVerifierOption[T]) applyOption(c *config[T]) error {
	if o == nil || o.verifier == nil {
		return fmt.Errorf(`pabt: nil transition verifier`)
	}
	c.verifier = o.verifier
	return nil
}

// WithTransitionVerifier sets the verifier that decides whether an effect
// achieved its post-condition. Without it, no causal relation exists and
// consumers must show support as non-causal.
func WithTransitionVerifier[T Condition](verifier TransitionVerifier) *transitionVerifierOption[T] {
	return &transitionVerifierOption[T]{verifier: verifier}
}

var (
	_ Option[Condition] = (*stateViewsOption[Condition])(nil)
	_ Option[Condition] = (*executionObserverOption[Condition])(nil)
	_ Option[Condition] = (*transitionVerifierOption[Condition])(nil)
)
