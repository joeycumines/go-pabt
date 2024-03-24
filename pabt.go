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

import (
	"fmt"
	bt "github.com/joeycumines/go-behaviortree"
)

type (
	// State models the underlying control implementation.
	State[T Condition] interface {
		// Variable must look up a variable based on it's uniquely-identifiable key and return it's current value, or
		// return an error to be propagated by the planning / bt implementation.
		Variable(key any) (value any, err error)

		// Actions must instance and return all viable actions to achieve a given failed constraint, or return an
		// error to be propagated by the planning / bt implementation.
		Actions(failed T) ([]Action[T], error)
	}

	// IState is an alias for a [State] without a more-specific [Condition] type.
	IState = State[Condition]

	// Action models a templated action to achieve a failed condition.
	Action[T Condition] interface {
		// Conditions may be used to indicate that at least one of the returned [Conditions] must match / pass prior to
		// executing this action, unless none are returned, indicating that there are no conditions for this action.
		Conditions() []Conditions[T]

		// Effects must return [Effects], comprised of 1-n [Effect] values, modeling what this action achieves.
		Effects() Effects

		// Node must return the actual logic / behavior for this action, as a behavior tree node.
		Node() bt.Node
	}

	// IAction is an alias for an [Action] without a more-specific [Condition] type.
	IAction = Action[Condition]

	// Variable models a unique variable within the [State], identifiable by means of a comparable key.
	// The variable mechanism is how [Condition] and [Effect] values interact with the [State].
	Variable interface {
		// Key returns the unique variable identifier which may be any comparable / mappable type.
		Key() any
	}

	// Condition models a constraint on a uniquely identifiable variable within the [State].
	//
	// Note that values of this type will be passed into [State.Actions] as-is, in order to facilitate handling of
	// condition and/or failure specific action templating behavior. Failure-specific behavior MAY require stateful
	// conditions or similar, along with a way to differentiate calls to [Condition.Match] with values from the actual
	// state ([State.Variable]) vs values from effects ([Effect.Value]).
	Condition interface {
		Variable

		// Match returns true if the given value (which should be for the same variable identified by Key) matches /
		// passes the constraint, or false if it fails.
		Match(value any) bool
	}

	// Effect models the expected changed in value of a state variable for a given action.
	Effect interface {
		Variable

		// Value must return the new value expected for the variable identified by Key (NOTE doesn't guarantee it).
		Value() any
	}

	// Conditions maps 1-n constraints against a distinct set of uniquely identifiable state variables.
	//
	// All [Condition] values must pass ([Condition.Match]) for these [Conditions] to pass. Each [Condition] must be
	// for a distinct variable, meaning no duplicate keys are allowed.
	Conditions[T Condition] []T

	// IConditions is an alias for a [Conditions] without a more-specific [Condition] type.
	IConditions = Conditions[Condition]

	// Effects maps 1-n changes for an [Action] against a distinct set of uniquely-identifiable state variables.
	//
	// All of each [Action]'s effects (on relevant state space variables) must be mapped, i.e. no side effects.
	// Each [Effect] must be for a distinct variable, meaning no duplicate keys are allowed.
	Effects []Effect

	// Plan models the planning implementation, see the [New] factory function for initialisation.
	Plan[T Condition] struct {
		config[T]
		root *node[T]
	}

	// IPlan is an alias for a [Plan] without a more-specific [Condition] type.
	IPlan = Plan[Condition]

	// Option models a planner configuration option and is used by [New] / option implementations.
	Option[T Condition] interface {
		applyOption(c *config[T]) error
	}

	// IOption is an alias for an [Option] without a more-specific [Condition] type.
	IOption = Option[Condition]

	config[T Condition] struct {
		state State[T]
		goal  []Conditions[T]
	}

	// node is 1-1 with a bt node, with additional embedded metadata and links to handle the traversal behavior
	// necessary to implement the planning algorithm, such as conflict resolution.
	node[T Condition] struct {
		// these are set with relevant context for each node

		goal          *goal[T]          // goal links to the root goal and will always be set (has the State etc)
		ppa           *ppa[T]           // ppa links the root of the pre-post condition tree
		action        *action[T]        // action links to the action tree with any conditions guarding the node itself
		preconditions *preconditions[T] // preconditions links to the root formed for a Conditions from an action
		precondition  *precondition[T]  // precondition links to a leaf Condition

		// node will be non-nil for leaf nodes
		node bt.Node
		// tick will be set for all group nodes
		tick bt.Tick

		// these node links form the actual tree

		parent, first, last, prev, next *node[T]
	}

	// contextual models for node, note that the root fields link to the node instance for each subtree
	// the root link is primarily used to modify the tree during conflict resolution

	goal[T Condition] struct {
		root  *node[T]
		state State[T]
		or    []*preconditions[T]
	}
	ppa[T Condition] struct {
		root    *node[T]
		post    *node[T]
		actions []*action[T]
	}
	action[T Condition] struct {
		root    *node[T]
		node    *node[T]
		effects map[any]Effect
		or      []*preconditions[T]
	}
	preconditions[T Condition] struct {
		root *node[T]
		and  map[any]*precondition[T]
	}
	precondition[T Condition] struct {
		root      *node[T]
		condition T
		status    bt.Status
	}
)

// INew is an alias for the [New] factory function without a more-specific [Condition] type.
func INew(state IState, goal []IConditions, opts ...IOption) (*IPlan, error) {
	return New(state, goal, opts...)
}

// New constructs a new [Plan], which provides a BT ([Plan.Node]) that will attempt to achieve the given goal, within
// the given state space / using the given [State] implementation. Note that goal is simply a slice of 1-n success
// [Conditions], at least one of which must eventually match, in order for the planning BT to succeed. An error will be
// returned for any invalid configuration.
//
// WARNING: Mutating the goal after this call may result in undefined behavior.
func New[T Condition](
	state State[T],
	goal []Conditions[T],
	opts ...Option[T],
) (*Plan[T], error) {
	if state == nil {
		return nil, fmt.Errorf(`pabt: nil state`)
	}
	p := Plan[T]{config: config[T]{
		state: state,
		goal:  goal,
	}}
	for _, opt := range opts {
		if err := opt.applyOption(&p.config); err != nil {
			return nil, err
		}
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Node returns the [Plan] as a behavior tree node.
func (p *Plan[T]) Node() bt.Node { return p.bt }
func (p *Plan[T]) init() (err error) {
	p.root = &node[T]{goal: &goal[T]{state: p.state}}
	p.root.goal.root = p.root
	p.root.goal.or, err = p.root.generateOr(p.goal)
	if err != nil {
		p.root = nil
	}
	return
}
func (p *Plan[T]) bt() (bt.Tick, []bt.Node) {
	if p.root == nil {
		if err := p.init(); err != nil {
			return func(children []bt.Node) (bt.Status, error) { return bt.Failure, err }, nil
		}
	}
	var (
		node           = p.root
		tick, children = node.bt()()
	)
	return func(children []bt.Node) (status bt.Status, err error) {
		status, err = tick(children)
		if err != nil || status != bt.Failure {
			return
		}
		cf, ok := p.root.search()
		if !ok {
			// Relevant excerpt:
			//
			// If no such condition is found (Algorithm 7 Line 5) that means that an action returned Failure due to
			// an old refinement that is no longer valid. In that case, at the next loop of Algorithm 5 a new
			// refinement is found (Algorithm 5 Line 5), assuming that such a refinement always exists.
			// ..
			// Moreover, note that PA-BT refines the BT every time it returns Failure. This is to encompass the case
			// where an older refinement is no longer valid. Is such cases an action will return Failure. This Failure
			// is propagated up to the root. The function ExpandTree (Algorithm 5 Line 10) will return the very same
			// tree (the tree needs no extension as there is no failed condition of an action) which gets re-refined
			// in the next loop (Algorithm 5 Line 5). For example, if the robot planned to place the object in a
			// particular position on the desk but this position was no longer feasible (e.g. another object was
			// placed in that position by an external agent).
			p.root = nil
			return
		}
		err = cf.expand()
		if err != nil {
			return
		}
		cf.root.ppa.resolve()
		status = bt.Running
		return
	}, children
}
