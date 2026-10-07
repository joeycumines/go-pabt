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
	"fmt"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// The node shape, named locally so the tick below reads as behavior-tree code
// rather than as a type parameter.
type (
	btNode   = bt.Node
	btTick   = bt.Tick
	btStatus = bt.Status
)

const (
	btStatusRunning = bt.Running
	btStatusSuccess = bt.Success
	btStatusFailure = bt.Failure
)

// Actions returns the actions that can achieve a failed condition.
//
// The four templates are chosen by the kind of variable that failed, and the
// alternatives a plan chooses between live here rather than in the goal:
//
//   - Delivered(reefer) offers one Place per slot the frame says will take it.
//     A slot that is unpowered or under-rated is never offered, so aiming at one
//     is not a mistake a plan can make and then retry.
//   - Holding(tractor, reefer) offers the pickup, guarded by the tag being
//     confirmed.
//   - At(tractor) offers one MoveTo per adjacent node over an open lane, so the
//     route a plan walks is the route the topology currently allows.
//   - TagConfirmed(reefer) offers the scan. This is the RFID fault made
//     plannable: a pickup cannot happen without it, so the only action that
//     achieves the failed tag condition is the scan, and pabt admits an action
//     only when one of its effects satisfies the condition that failed. The scan
//     therefore enters a real tree rather than being special-cased.
func (s *state) Actions(failed pabt.Condition) ([]pabt.IAction, error) {
	key, ok := failed.Key().(sim.VarKey)
	if !ok {
		return nil, fmt.Errorf("logic: %s asked for actions for an unexpected key of type %T",
			s.runner.tug, failed.Key())
	}
	o, ok := s.pinned, s.hasPinned
	if !ok {
		return nil, fmt.Errorf("logic: %s asked for actions with no frame pinned", s.runner.tug)
	}

	// Every action this returns is one the failed condition would accept: pabt
	// admits an action only when one of its effects satisfies the condition, so a
	// template that did not would simply never enter a tree. Checking it here
	// turns a template bug into an error at construction rather than a silently
	// unreachable plan.
	admit := func(acts []pabt.IAction) []pabt.IAction {
		out := make([]pabt.IAction, 0, len(acts))
		for _, a := range acts {
			if achieves(a, key, failed) {
				out = append(out, a)
			}
		}
		return out
	}

	// A condition about position carries the destination it wants; the rest do not.
	want := sim.NodeID("")
	if c, ok := failed.(condition); ok {
		want = c.node
	}

	switch key.Kind {
	case sim.VarDelivered:
		return admit(s.placeActions(o)), nil
	case sim.VarHolding:
		return admit(s.pickUpAction(o)), nil
	case sim.VarAt:
		return admit(s.moveActions(o, want)), nil
	case sim.VarTagConfirmed:
		return admit(s.scanAction(o)), nil
	}
	return nil, fmt.Errorf("logic: %s has no action template for %s", s.runner.tug, key)
}

// achieves reports whether an action's effects would satisfy a failed condition.
func achieves(a pabt.IAction, key sim.VarKey, failed pabt.Condition) bool {
	for _, e := range a.Effects() {
		if e.Key() != any(key) {
			continue
		}
		if failed.Match(e.Value()) {
			return true
		}
	}
	return false
}

// placeActions returns one candidate placement per slot the frame will accept.
func (s *state) placeActions(o sim.Observation) []pabt.IAction {
	// One group, because the slice an action declares is a set of OR alternatives
	// and the conditions inside one group are an AND. Two groups would mean
	// "holding or at the bay", which is a guard a plan could walk straight past.
	guards := []pabt.IConditions{{
		holding(o.Tractor, o.Reefer),
		at(o.Tractor, sim.NodeBay),
	}}
	var acts []pabt.IAction
	for _, slot := range o.AcceptingSlots() {
		slot := slot
		acts = append(acts, &action{
			name:       fmt.Sprintf("Place(%s,%s)", o.Reefer, slot),
			conditions: guards,
			effects:    pabt.Effects{deliveredEffect(o.Reefer)},
			node:       s.nodeFor(sim.CmdPlace, slot, sim.NodeID(""), o, delivered(o.Reefer), guards),
		})
	}
	return acts
}

// pickUpAction returns the pickup, guarded on the tag being confirmed and on
// standing where the reefer is. The tag guard is what makes the RFID fault a
// planning problem: with the tag unread this action is never offered, and the
// plan grows the scan instead. The position guard is evaluated from the pinned
// frame at generation time; the node re-checks it live before submitting, so a
// plan never fires past a guard it has not passed.
func (s *state) pickUpAction(o sim.Observation) []pabt.IAction {
	// Stale or Unknown tag belief cannot authorize pickup. The scan action is
	// the only way to transition the belief to Known, so returning nil here
	// forces the planner to grow a ScanTag into the tree.
	if b, ok := o.BeliefOf(sim.TagKey(o.Reefer)); ok && b.Status != sim.BeliefKnown {
		return nil
	}
	guards := []pabt.IConditions{{
		tagConfirmed(o.Reefer),
		at(o.Tractor, o.ReeferAt()),
	}}
	return []pabt.IAction{&action{
		name:       fmt.Sprintf("PickUp(%s)", o.Reefer),
		conditions: guards,
		effects:    pabt.Effects{holdingEffect(o.Tractor, o.Reefer)},
		node:       s.nodeFor(sim.CmdPickUp, sim.SlotID(""), sim.NodeID(""), o, holding(o.Tractor, o.Reefer), guards),
	}}
}

// scanAction returns the scan. It is an ordinary action with an ordinary effect,
// and it is the only thing in the domain that can make an unreadable tag
// confirmed. The scan reads the tag where the reefer stands, so the tractor has
// to be beside it: the guard names the reefer's node from the same frame the
// candidates were enumerated from, and the node re-checks it live before
// submitting. Without the guard a tractor holding a later job would scan from
// wherever it stands and the world would refuse with NotBesideReefer on every
// tick while the tree reports Running.
func (s *state) scanAction(o sim.Observation) []pabt.IAction {
	guards := []pabt.IConditions{{
		at(o.Tractor, o.ReeferAt()),
	}}
	return []pabt.IAction{&action{
		name:       fmt.Sprintf("ScanTag(%s)", o.Reefer),
		conditions: guards,
		effects:    pabt.Effects{tagEffect(o.Reefer)},
		node:       s.nodeFor(sim.CmdScanTag, sim.SlotID(""), sim.NodeID(""), o, tagConfirmed(o.Reefer), guards),
	}}
}

// moveActions returns the one action that can achieve being at a node: travel
// there.
//
// Not one action per adjacent node. The planner admits an action only when one of
// its effects satisfies the condition that failed, and the condition names a
// destination. A single hop to the next waypoint does not put a tractor in the
// bay, so a per-hop action would never be admitted and the tree could not grow at
// all. Travel is therefore one action whose effect is the destination and whose
// node crosses the lanes one at a time over as many ticks as the route needs.
//
// The action carries no preconditions. A guard naming the origin would be
// evaluated against the tick's pinned frame, which predates the travel the node
// performs from a fresh world read, so after the first hop the guard would fail
// and the planner would expand a leg it has already walked. The node itself
// reports Failure when no route exists, which is what expands the blocked
// precondition.
func (s *state) moveActions(o sim.Observation, want sim.NodeID) []pabt.IAction {
	if o.At() == want {
		// Already standing there. The condition should not have failed, and
		// offering an action that would cross no lane would be inventing work.
		return nil
	}
	return []pabt.IAction{&action{
		name:       fmt.Sprintf("TravelTo(%s)", want),
		conditions: []pabt.IConditions{},
		effects:    pabt.Effects{atEffect(o.Tractor, want)},
		node:       s.travelNode(o, want),
	}}
}

// travelNode crosses the route to a destination one lane per tick.
//
// The route is recomputed from a fresh frame whenever it is needed, rather than
// held from the frame the action was generated in. That is what makes a wind
// lockout a real incident: the lane closes under a route already in progress, the
// crossing is refused by the world, and the next route is derived over the other
// approach instead of replaying a lane that is shut.
func (s *state) travelNode(o sim.Observation, target sim.NodeID) btNode {
	return func() (btTick, []btNode) {
		tick := func([]btNode) (btStatus, error) {
			r := s.runner
			// A settled travel receipt is parked on the shared travel pending by
			// the central drain at the top of the tick, not on the in-flight
			// record, so a refusal is seen here without this node draining it.
			if out := r.takeTravelOutcome(); out != nil {
				if out.Abandoned != nil {
					// Nothing was decided. The next attempt is a new command.
					return btStatusRunning, nil
				}
				if !out.Receipt.Applied() {
					// The crossing was refused. A locked lane or a missing
					// adjacency fails the tree so the planner re-expands around
					// it; an occupied bay or waypoint is a wait on whoever holds
					// it, so the node holds Running and the next tick derives a
					// fresh route. Refusing-and-waiting is what keeps a plan
					// parked at a taken bay from oscillating around it, and what
					// lets a lane lockout re-expand instead of spinning.
					switch out.Receipt.Reason {
					case sim.ReasonLaneLocked, sim.ReasonNotAdjacent:
						return btStatusFailure, nil
					default:
						return btStatusRunning, nil
					}
				}
				// Applied: fall through to the arrival check below, which
				// verifies against a fresh frame rather than trusting the
				// receipt.
			}

			// Another action's command is outstanding. A runner carries one, and the
			// world is mid-decision, so this is a wait and not a failure.
			if r.inflight != nil {
				return btStatusRunning, nil
			}

			// Arrived?
			if r.atNodeLocked(target) {
				r.resetTravelLocked()
				return btStatusSuccess, nil
			}

			// Take one hop from a route derived now rather than held, so a lane that
			// closed under this plan is simply not on it. Occupancy is read from
			// the same live world: a waypoint holding another tractor is not on
			// this route, so the hop goes around it instead of refusing against
			// it while the tree reports Running. A refusal on an occupied hop is
			// a wait on whoever holds it (see above); the avoiding route is what
			// keeps the wait short, not what ends it.
			route := r.routeAvoidingLocked(target)
			if len(route) == 0 {
				// No route over open lanes. There is nothing sound this node can do,
				// so the tree fails and the runner reports the plan blocked rather
				// than spinning here.
				r.resetTravelLocked()
				return btStatusFailure, nil
			}
			from, to, ok := r.hopLocked(route)
			if !ok || !r.atNodeLocked(from) {
				// The route does not begin where the tractor is standing.
				r.resetTravelLocked()
				return btStatusFailure, nil
			}

			hop := to
			r.submitTravelLocked(hop)
			return btStatusRunning, nil
		}
		return tick, nil
	}
}

// nodeFor builds the behavior-tree node that performs one command. The slot is
// the candidate slot for a Place and empty otherwise; it is what the command's
// slot-version provenance names, so a reader sees which world the runner was
// reasoning over for the slot it aimed at.
//
// The node holds Running until a fresh frame verifies the post-condition, and
// this is load-bearing rather than cautious. A goal's expanded subtree is a
// selector whose first child re-evaluates the goal's own condition and whose
// remaining children are the candidate actions. An action that returned Success
// as soon as its command was applied would therefore short-circuit the selector
// and report the goal met, whether or not the reefer was ever in a slot: a
// receipt is evidence that a command happened, not proof that the post-condition
// now holds.
//
// So there are three gates, in order. The command is submitted once. Its receipt
// landing is not success. A frame taken after that receipt shows the
// post-condition holding is.
func (s *state) nodeFor(kind sim.CommandKind, slot sim.SlotID, node sim.NodeID, o sim.Observation, post condition, guards []pabt.IConditions) btNode {
	pend := &pending{post: post}
	return func() (btTick, []btNode) {
		tick := func([]btNode) (btStatus, error) {
			r := s.runner
			if pend.outcome != nil {
				return r.awaitingLocked(pend)
			}
			// Another action's command is outstanding. A runner carries one, and the
			// world is mid-decision, so this is a wait and not a failure.
			if r.inflight != nil {
				return btStatusRunning, nil
			}

			// Nothing outstanding. Before submitting, show the guards hold in a frame
			// taken now. If they do not, this is a failed precondition and the
			// planner has to be the one to hear about it.
			if !r.guardsHoldLocked(guards) {
				return btStatusFailure, nil
			}

			// Submit, once, under a key for this attempt.
			r.submitPendingLocked(pend, func(expect sim.Expectation, key string) sim.Command {
				return sim.Command{
					ID: key, IdempotencyKey: key, Kind: kind, Tractor: o.Tractor,
					Reefer: o.Reefer, Slot: slot, Node: node,
					Expect: r.state.expectationLocked(slot),
				}
			})
			return btStatusRunning, nil
		}
		return tick, nil
	}
}
