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

import (
	"strconv"
	"testing"
)

// The reducer's contract is a set of properties, not a set of examples, so these
// tests drive the world through a driver that checks the properties after every
// single command rather than asserting a handful of end states.

type driver struct {
	t *testing.T
	w World
	// issued counts distinct idempotency keys handed out, so a test command and
	// its deliberate retry use the same key without the helper inventing one.
	issued int
}

func newDriver(t *testing.T) *driver {
	t.Helper()
	return &driver{t: t, w: NewWorld()}
}

// cmd builds a command with a fresh idempotency key, so a test that wants to
// retry must pass the same key explicitly.
func (d *driver) cmd(c Command) Command {
	if c.IdempotencyKey == "" {
		d.issued++
		c.IdempotencyKey = "k" + strconv.Itoa(d.issued)
	}
	if c.ID == "" {
		c.ID = c.IdempotencyKey
	}
	return c
}

func (d *driver) apply(c Command) Receipt {
	d.t.Helper()
	c = d.cmd(c)
	// StateKey, not Canonical: a refused command is still counted in the command
	// lane, so comparing the full canonical form would report a change that the
	// logical state did not make.
	before := d.w.StateKey()
	seqBefore := d.w.WorldSeq()
	var receipt Receipt
	var events []WorldEvent
	d.w, receipt, events = Reduce(d.w, c)
	d.t.Logf("%-9s %-8s %-7s seq=%d world=%d reason=%s events=%d",
		c.Kind, c.Tractor, receipt.Outcome, receipt.CommandSeq, receipt.WorldSeq, receipt.Reason, len(events))
	if receipt.Applied() {
		if d.w.WorldSeq() != seqBefore+1 {
			d.t.Fatalf("%s applied but worldSeq went %d -> %d, want exactly one step",
				c.Kind, seqBefore, d.w.WorldSeq())
		}
	} else {
		if d.w.StateKey() != before {
			d.t.Fatalf("%s refused as %s but the world changed:\n%s", c.Kind, receipt.Reason, d.w.StateKey())
		}
		if d.w.WorldSeq() != seqBefore {
			d.t.Fatalf("%s refused but worldSeq moved %d -> %d", c.Kind, seqBefore, d.w.WorldSeq())
		}
	}
	assertInvariants(d.t, d.w)

	// The placement rule is a property of the moment of placement, not of the
	// standing state: the power desk may legitimately take a plug out of a slot
	// after the fact. So it is checked here, against the slot the reefer just
	// entered.
	if c.Kind == CmdPlace && receipt.Applied() {
		s, okS := d.w.Slot(c.Slot)
		r, okR := d.w.Reefer(c.Reefer)
		if !okS || !okR {
			d.t.Fatalf("Place applied for a slot or reefer the world does not have")
		}
		if !s.Plugged {
			d.t.Fatalf("a reefer was placed into unpowered %s", s.ID)
		}
		if !s.RatingsAccept(r) {
			d.t.Fatalf("a %d reefer was placed into %s rated to %d", r.TempC, s.ID, s.RatedMaxC)
		}
	}
	return receipt
}

func (d *driver) applied(c Command) Receipt {
	d.t.Helper()
	r := d.apply(c)
	if !r.Applied() {
		d.t.Fatalf("%s was refused as %s, wanted it applied", c.Kind, r.Reason)
	}
	return r
}

// assertInvariants checks the structural properties the design record states.
// They are checked against the world's own fields rather than through a
// convenience accessor, so a getter that disagreed with the state could not hide
// a violation.
func assertInvariants(t *testing.T, w World) {
	t.Helper()

	held := map[ReeferID]TractorID{}
	standing := map[NodeID][]TractorID{}
	for _, tr := range w.tractors {
		standing[tr.At] = append(standing[tr.At], tr.ID)
		n, _ := w.Node(tr.At)
		if n.Capacity > 0 && len(standing[tr.At]) > n.Capacity {
			t.Fatalf("node %s holds %d tractors (%v) but its capacity is %d",
				tr.At, len(standing[tr.At]), standing[tr.At], n.Capacity)
		}

		if tr.Hold == "" {
			continue
		}
		if other, clash := held[tr.Hold]; clash {
			t.Fatalf("reefer %s is held by both %s and %s", tr.Hold, other, tr.ID)
		}
		held[tr.Hold] = tr.ID
	}

	for _, r := range w.reefers {
		if r.Custodian == "" {
			if _, heldSomewhere := held[r.ID]; heldSomewhere {
				t.Fatalf("reefer %s is in %s's hands but its record names no custodian", r.ID, held[r.ID])
			}
			continue
		}
		owner, ok := held[r.ID]
		if !ok {
			t.Fatalf("reefer %s names custodian %s, which does not hold it", r.ID, r.Custodian)
		}
		if owner != r.Custodian {
			t.Fatalf("reefer %s names custodian %s but %s holds it", r.ID, r.Custodian, owner)
		}
		if w.tractors[ownerIndex(t, w, owner)].At != r.At {
			t.Fatalf("reefer %s is held at %s but records itself at %s", r.ID, w.tractors[ownerIndex(t, w, owner)].At, r.At)
		}
	}

	placed := map[ReeferID]SlotID{}
	for _, s := range w.slots {
		if s.Occupied == "" {
			continue
		}
		if other, clash := placed[s.Occupied]; clash {
			t.Fatalf("reefer %s stands in both %s and %s", s.Occupied, other, s.ID)
		}
		placed[s.Occupied] = s.ID
		r, ok := w.Reefer(s.Occupied)
		if !ok {
			t.Fatalf("slot %s holds unknown reefer %s", s.ID, s.Occupied)
		}
		if r.At != s.Node {
			t.Fatalf("slot %s holds %s but the reefer records itself at %s", s.ID, s.Occupied, r.At)
		}
		// A powered slot never holds cargo it is not rated for. Unpowered is not
		// a violation: the power desk may take a plug out after a delivery.
		if s.Plugged && !s.RatingsAccept(r) {
			t.Fatalf("powered slot %s rated to %d holds a %d reefer", s.ID, s.RatedMaxC, r.TempC)
		}
	}
}

func ownerIndex(t *testing.T, w World, id TractorID) int {
	t.Helper()
	for i, tr := range w.tractors {
		if tr.ID == id {
			return i
		}
	}
	t.Fatalf("unknown tractor %s", id)
	return 0
}
