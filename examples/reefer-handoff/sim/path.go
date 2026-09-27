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

package sim

import "sort"

// Lane is one undirected connection between two nodes. A tractor crosses a lane
// with one MoveTo, so a route is a list of lanes and the planner holds a route
// only as a chain of MoveTo commands in its own tree. Nothing here caches a
// route against a version: a lane that closes is refused at the moment of the
// crossing, and that refusal is what drives re-expansion.
type Lane struct {
	ID   LaneID
	From NodeID
	To   NodeID
}

// Lanes returns the seven lanes of the terminal. The two approach lanes are why
// closing one of them forces a real re-route rather than making the bay
// unreachable.
func Lanes() []Lane {
	return []Lane{
		{ID: LaneWest, From: NodeStaging, To: NodeWest},
		{ID: LaneEast, From: NodeStaging, To: NodeEast},
		{ID: LaneBayWest, From: NodeWest, To: NodeBay},
		{ID: LaneBayEast, From: NodeEast, To: NodeBay},
		{ID: LaneSlot1, From: NodeBay, To: NodeSlot1},
		{ID: LaneSlot2, From: NodeBay, To: NodeSlot2},
		{ID: LaneSlot3, From: NodeBay, To: NodeSlot3},
	}
}

// Topology is the lane graph and which lanes the wind controller has closed.
//
// A Topology is a value, and the two rules below keep it one. locked is built
// fresh on every lock and reopen instead of being appended to and sorted, because
// appending to a slice whose capacity is already large enough writes through the
// backing array a previously returned Topology is still holding. Lane order is
// table order, never sorted, so the search below visits lanes the way the
// manifest declares them.
type Topology struct {
	lanes []Lane

	// locked holds the closed lanes in lane-ID order.
	locked []LaneID

	// version increases on every lock or reopen. It is a world fact recorded for
	// the debugger, and it is the value a MoveTo must expect.
	version uint64
}

// NewTopology returns the terminal's lane graph with every lane open.
func NewTopology() Topology { return Topology{lanes: Lanes()} }

// Version returns the topology version.
func (t Topology) Version() uint64 { return t.version }

// Lanes returns the lane table. The returned slice is the topology's own, and
// callers must not modify it.
func (t Topology) Lanes() []Lane { return t.lanes }

// Open reports whether a lane is currently crossable. A lane that does not exist
// is not open.
func (t Topology) Open(l LaneID) bool {
	if _, ok := t.find(l); !ok {
		return false
	}
	for _, id := range t.locked {
		if id == l {
			return false
		}
	}
	return true
}

// LockedLanes returns the closed lanes in lane-ID order, for the debugger ledger
// and the operations monitor.
func (t Topology) LockedLanes() []LaneID {
	out := make([]LaneID, len(t.locked))
	copy(out, t.locked)
	return out
}

// Lock closes a lane and returns the topology with its version advanced. Locking
// a lane that is already closed, or a lane that does not exist, is not an error
// and does not advance the version, because nothing changed.
func (t Topology) Lock(l LaneID) Topology {
	if _, ok := t.find(l); !ok || !t.Open(l) {
		return t
	}
	// A fresh slice, sorted in place. Appending to t.locked and sorting would
	// permute indices a caller of an earlier Lock is still looking at.
	locked := make([]LaneID, 0, len(t.locked)+1)
	locked = append(locked, l)
	locked = append(locked, t.locked...)
	sort.Slice(locked, func(i, j int) bool { return locked[i] < locked[j] })
	t.locked = locked
	t.version++
	return t
}

// Reopen opens a lane and returns the topology with its version advanced.
func (t Topology) Reopen(l LaneID) Topology {
	if _, ok := t.find(l); !ok || t.Open(l) {
		return t
	}
	out := t.locked[:0:0]
	for _, id := range t.locked {
		if id != l {
			out = append(out, id)
		}
	}
	t.locked = out
	t.version++
	return t
}

func (t Topology) find(l LaneID) (Lane, bool) {
	for _, lane := range t.lanes {
		if lane.ID == l {
			return lane, true
		}
	}
	return Lane{}, false
}

// Endpoints returns the two nodes a lane joins.
func (t Topology) Endpoints(l LaneID) (NodeID, NodeID, bool) {
	lane, ok := t.find(l)
	if !ok {
		return "", "", false
	}
	return lane.From, lane.To, true
}

// LaneBetween returns the lane joining two nodes, and whether such a lane exists
// at all. A pair of nodes with no lane between them is a permanent refusal, not
// a lock.
func (t Topology) LaneBetween(a, b NodeID) (LaneID, bool) {
	for _, lane := range t.lanes {
		if (lane.From == a && lane.To == b) || (lane.From == b && lane.To == a) {
			return lane.ID, true
		}
	}
	return "", false
}

// Route returns the lanes to cross to get from one node to another, and whether
// such a route exists over currently open lanes. It is a breadth-first search
// over open lanes only, so a locked lane is absent from the graph instead of
// weighted, and the route returned is crossable as it stands.
//
// The search visits lanes in table order, which is what makes the route from the
// apron to the bay go by the western approach first. That is load-bearing: the
// wind lockout closes the western approach, and if the search preferred the
// eastern one the closure would change nothing a plan was doing. Table order is
// fixed, so the route is deterministic.
func (t Topology) Route(from, to NodeID) ([]LaneID, bool) {
	if from == to {
		return nil, true
	}
	prev := map[NodeID]LaneID{from: ""}
	queue := []NodeID{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, id := range t.lanes {
			if !t.Open(id.ID) {
				continue
			}
			next, adjacent := otherEnd(id, cur)
			if !adjacent || next == "" {
				continue
			}
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = id.ID
			if next == to {
				return t.unwind(prev, from, to), true
			}
			queue = append(queue, next)
		}
	}
	return nil, false
}

// otherEnd returns the node at the far end of a lane from cur, and whether cur
// touches that lane at all.
func otherEnd(lane Lane, cur NodeID) (NodeID, bool) {
	switch cur {
	case lane.From:
		return lane.To, true
	case lane.To:
		return lane.From, true
	}
	return "", false
}

// unwind walks the predecessor map back from the destination to rebuild the lane
// sequence, then returns it in travel order.
func (t Topology) unwind(prev map[NodeID]LaneID, from, to NodeID) []LaneID {
	var rev []LaneID
	for cur := to; cur != from; {
		l, ok := prev[cur]
		if !ok {
			return nil
		}
		rev = append(rev, l)
		a, b, ok := t.Endpoints(l)
		if !ok {
			return nil
		}
		if b == cur {
			cur = a
		} else {
			cur = b
		}
	}
	route := make([]LaneID, len(rev))
	for i, l := range rev {
		route[len(rev)-1-i] = l
	}
	return route
}
