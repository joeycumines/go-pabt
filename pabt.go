/*
   Copyright 2020 Joseph Cumines

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
	State interface {
		// Variable must lookup a variable based on it's uniquely-identifiable key and return it's current value, or
		// return an error to be propagated by the planning / bt implementation.
		Variable(key interface{}) (value interface{}, err error)
		// Actions must instance and return all viable actions to achieve a given failed constraint, or return an
		// error to be propagated by the planning / bt implementation.
		Actions(failed Condition) ([]Action, error)
	}

	// Action models a templated action to achieve a failed condition.
	Action interface {
		// Conditions may be used to indicate that at least one of the returned Conditions must match / pass prior to
		// executing this action, unless none are returned, indicating that there are no conditions for this action.
		Conditions() []Conditions
		// Effects must return Effects, comprised of 1-n Effect values, modeling what this action achieves.
		Effects() Effects
		// Node must return the actual logic / behavior for this action, as a behavior tree node.
		Node() bt.Node
	}

	// Variable models a unique variable within the State, identifiable by means of a comparable key.
	Variable interface {
		// Key returns the unique variable identifier which may be any comparable / mappable type.
		Key() interface{}
	}

	// Condition models a constraint on a uniquely identifiable variable within the State.
	//
	// Note that values of this type will be passed into State.Actions as-is, in order to facilitate handling of
	// condition and/or failure specific action templating behavior. Note that failure-specific behavior might require
	// stateful conditions or the like, along with a way to differentiate calls to Match with values from the actual
	// state (State.Variable) vs values from effects (Effect.Value).
	Condition interface {
		Variable
		// Match returns true if the given value (which should be for the same variable identified by Key) matches /
		// passes the constraint, or false if it fails.
		Match(value interface{}) bool
	}

	// Effect models the expected changed in value of a state variable for a given action.
	Effect interface {
		Variable
		// Value must return the new value expected for the variable identified by Key (NOTE doesn't guarantee it).
		Value() interface{}
	}

	// Conditions maps 1-n constraints against a distinct set of uniquely identifiable state variables.
	//
	// All Condition values must pass (Condition.Match) for this Conditions to pass. Each Condition must be for a
	// distinct variable, meaning no duplicate keys.
	Conditions []Condition

	// Effects maps 1-n changes for an Action against a distinct set of uniquely identifiable state variables.
	//
	// All of each Action's effects (on relevant state space variables) must be mapped, i.e. no side effects.
	// Each Effect must be for a distinct variable, meaning no duplicate keys.
	Effects []Effect

	// Plan models the planning implementation, see the New factory function for initialisation.
	Plan struct {
		config
		root *node
	}

	// Option models planner configuration options and is used by New / option implementations.
	Option func(c *config) error

	config struct {
		state State
		goal  []Conditions
	}

	// node is 1-1 with a bt node, with additional embedded metadata and links to handle the traversal behavior
	// necessary to implement the planning algorithm, such as conflict resolution.
	node struct {
		// these are set with relevant context for each node

		goal          *goal          // goal links to the root goal and will always be set (has the State etc)
		ppa           *ppa           // ppa links the root of the pre-post condition tree
		action        *action        // action links to the action tree with any conditions guarding the node itself
		preconditions *preconditions // preconditions links to the root formed for a Conditions from an action
		precondition  *precondition  // precondition links to a leaf Condition

		// node will be non-nil for leaf nodes
		node bt.Node
		// tick will be set for all group nodes
		tick bt.Tick

		// these node links form the actual tree

		parent, first, last, prev, next *node
	}

	// contextual models for node, note that the root fields link to the node instance for each subtree
	// the root link is primarily used to modify the tree during conflict resolution

	goal struct {
		root  *node
		state State
		or    []*preconditions
	}
	ppa struct {
		root    *node
		post    *node
		actions []*action
	}
	action struct {
		root    *node
		node    *node
		effects map[interface{}]Effect
		or      []*preconditions
	}
	preconditions struct {
		root *node
		and  map[interface{}]*precondition
	}
	precondition struct {
		root      *node
		condition Condition
		status    bt.Status
	}
)

// New constructs a new Plan, which provides a BT (Plan.Node) that will attempt to achieve the given goal, within the
// given state space / using the given State implementation. Note that goal is simply a slice of 1-n success
// Conditions, at least one of which must eventually match, in order for the planning BT to succeed. An error will be
// returned for any invalid configuration. Note that callers must not later modify the contents of goal.
func New(state State, goal []Conditions, opts ...Option) (*Plan, error) {
	if state == nil {
		return nil, fmt.Errorf(`pabt: nil state`)
	}
	p := Plan{config: config{
		state: state,
		goal:  goal,
	}}
	for _, opt := range opts {
		if err := opt(&p.config); err != nil {
			return nil, err
		}
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Node returns the Plan as a behavior tree node.
func (p *Plan) Node() bt.Node { return p.bt }
func (p *Plan) init() (err error) {
	p.root = &node{goal: &goal{state: p.state}}
	p.root.goal.root = p.root
	p.root.goal.or, err = p.root.generateOr(p.goal)
	if err != nil {
		p.root = nil
	}
	return
}
func (p *Plan) bt() (bt.Tick, []bt.Node) {
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
