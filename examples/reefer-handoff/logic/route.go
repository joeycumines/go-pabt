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
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// routeLocked returns the lanes from where the tractor is to a node, derived now
// rather than held, so a lane that closes under the plan is simply not on it.
func (r *Runner) routeLocked(target sim.NodeID) []sim.LaneID {
	w := r.loop.Snapshot()
	tug, ok := w.Tractor(r.tug)
	if !ok {
		return nil
	}
	route, ok := w.Route(tug.At, target)
	if !ok {
		return nil
	}
	return route
}

// routeAvoidingLocked returns the lanes to a node avoiding nodes that hold
// another tractor. The world's Route is over open lanes only and ignores
// occupancy, so a route through an occupied node would refuse on every tick
// while the tree reports Running and the planner never re-expands. Trying the
// other approach when the first hop is blocked lets a plan walk around a parked
// tractor instead of pushing against it. The bay is never detoured around for
// its own sake — a route whose destination is the bay and whose first hop is
// the bay waits on its occupant — but a taken waypoint on the way there is
// still walked around, because pushing against it refuses forever while its
// holder waits on the bay in turn.
func (r *Runner) routeAvoidingLocked(target sim.NodeID) []sim.LaneID {
	w := r.loop.Snapshot()
	tug, ok := w.Tractor(r.tug)
	if !ok {
		return nil
	}
	if route, ok := w.Route(tug.At, target); ok && r.hopFreeLocked(w, tug.At, route) {
		return route
	}
	// The first hop is taken. When it is the destination itself there is nothing
	// to walk around: the bay waits on its occupant, and a detour around it
	// oscillates, because the route back always leads through it. A taken
	// waypoint on the way there is walked around via the other approach.
	if route, ok := w.Route(tug.At, target); ok {
		if hop, ok := r.firstHopLocked(w, tug.At, route); ok && hop == target {
			return r.routeLocked(target)
		}
	}
	for _, via := range []sim.NodeID{sim.NodeWest, sim.NodeEast} {
		if via == tug.At {
			continue
		}
		if holder, taken := w.Occupant(via); taken && holder != r.tug {
			continue
		}
		first, ok := w.Route(tug.At, via)
		if !ok {
			continue
		}
		rest, ok := w.Route(via, target)
		if !ok {
			continue
		}
		return append(first, rest...)
	}
	return r.routeLocked(target)
}

// hopFreeLocked reports whether the first hop of a route lands on a node this
// tractor can stand on: the bay when it is free, or a waypoint holding nobody.
func (r *Runner) hopFreeLocked(w sim.World, from sim.NodeID, route []sim.LaneID) bool {
	if len(route) == 0 {
		return true
	}
	hop, ok := r.hopOfLocked(w, from, route)
	if !ok {
		return false
	}
	if holder, taken := w.Occupant(hop); taken && holder != r.tug {
		return false
	}
	return true
}

// firstHopLocked returns the node the first lane of a route leads to from where
// the tractor stands.
func (r *Runner) firstHopLocked(w sim.World, from sim.NodeID, route []sim.LaneID) (sim.NodeID, bool) {
	if len(route) == 0 {
		return "", false
	}
	return r.hopOfLocked(w, from, route)
}

// hopOfLocked resolves the far end of a route's first lane.
func (r *Runner) hopOfLocked(w sim.World, from sim.NodeID, route []sim.LaneID) (sim.NodeID, bool) {
	a, b, ok := w.Topology().Endpoints(route[0])
	if !ok {
		return "", false
	}
	if b == from {
		return a, true
	}
	return b, true
}

// hopLocked returns the first lane of a route and the node it leads to, in travel
// order.
func (r *Runner) hopLocked(route []sim.LaneID) (sim.NodeID, sim.NodeID, bool) {
	if len(route) == 0 {
		return "", "", false
	}
	w := r.loop.Snapshot()
	tug, ok := w.Tractor(r.tug)
	if !ok {
		return "", "", false
	}
	first, second, ok := w.Topology().Endpoints(route[0])
	if !ok {
		return "", "", false
	}
	// The lane is undirected, so the hop leads to whichever end the tractor is not
	// standing on.
	hop := second
	if second == tug.At {
		hop = first
	}
	return tug.At, hop, true
}

// atNodeLocked reports whether the tractor is standing at a node.
func (r *Runner) atNodeLocked(n sim.NodeID) bool {
	tug, ok := r.loop.Snapshot().Tractor(r.tug)
	return ok && tug.At == n
}
