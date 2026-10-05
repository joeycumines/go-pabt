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

package main

import "sync"

// ControlKind identifies a host-level control event. These are distinct from
// the five domain commands (ClaimJob, ScanTag, PickUp, MoveTo, Place): they
// never enter Reduce's command set and never mutate the world directly. They
// govern the drive loop's pacing and admission.
type ControlKind int

const (
	ControlPause      ControlKind = iota // freeze new admission; already-queued work still settles
	ControlResume                        // unfreeze admission
	ControlStep                          // admit exactly one round then re-pause
	ControlSpeed                         // change the round pacing multiplier
	ControlReroute                       // force runners to refresh locked-lane avoidance
	ControlBreakpoint                    // pause when a condition is met (round N / worldSeq N)
	ControlSearch                        // TUI/browser query over committed records (client-side)
	ControlQuit                          // cancel the drive context
)

// ControlEvent is a typed instruction from the operator (TUI keyboard, future
// API) to the drive loop. Every transition increments the admission epoch so
// a debugger can correlate a command's admission to the gate state at that
// moment.
type ControlEvent struct {
	Kind       ControlKind
	Epoch      uint64
	SpeedValue float64 // only meaningful for ControlSpeed
	Query      string  // only meaningful for ControlSearch
}

// Controller mediates between the operator and the drive loop. It is safe for
// concurrent use: the TUI goroutine sends events while drive reads them.
//
// The controller owns pause/resume/step/speed state. It does NOT touch
// WorldLoop.admitting, which remains the lifecycle/shutdown gate owned by
// Start and Shutdown. This separation is load-bearing: conflating the two
// would make a paused loop indistinguishable from a terminating one, breaking
// reconciliation and the drain path.
type Controller struct {
	mu      sync.Mutex
	ch      chan ControlEvent
	epoch   uint64
	paused  bool
	stepOne bool
	speed   float64
	quitCh  chan struct{}
}

// NewController returns a ready controller with speed 1.0 and admission open.
func NewController() *Controller {
	return &Controller{
		ch:     make(chan ControlEvent, 16),
		speed:  1.0,
		quitCh: make(chan struct{}),
	}
}

// Send delivers a control event to the drive loop. It increments the epoch
// under the lock and stamps it onto the event before enqueueing. Non-blocking:
// if the channel is full the event is dropped rather than deadlocking the
// sender (a TUI keypress must never hang the screen).
func (c *Controller) Send(ev ControlEvent) {
	c.mu.Lock()
	c.epoch++
	ev.Epoch = c.epoch
	switch ev.Kind {
	case ControlPause:
		c.paused = true
		c.stepOne = false
	case ControlResume:
		c.paused = false
		c.stepOne = false
	case ControlStep:
		if c.paused {
			c.stepOne = true
		}
	case ControlSpeed:
		if ev.SpeedValue < 0.5 {
			ev.SpeedValue = 0.5
		} else if ev.SpeedValue > 5.0 {
			ev.SpeedValue = 5.0
		}
		c.speed = ev.SpeedValue
	case ControlQuit:
		select {
		case <-c.quitCh:
		default:
			close(c.quitCh)
		}
	}
	epoch := c.epoch
	c.mu.Unlock()

	ev.Epoch = epoch
	select {
	case c.ch <- ev:
	default:
	}
}

// Check drains all pending control events and returns the composite state the
// drive loop should act on. It consumes the step permit: after one call that
// returns stepOne=true, the next call returns false unless another Step was
// sent.
func (c *Controller) Check() (paused bool, stepOne bool, speed float64, quit bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Drain the channel under the lock so state transitions are applied in
	// order before the drive loop reads the composite result.
drain:
	for {
		select {
		case ev := <-c.ch:
			switch ev.Kind {
			case ControlPause:
				c.paused = true
				c.stepOne = false
			case ControlResume:
				c.paused = false
				c.stepOne = false
			case ControlStep:
				if c.paused {
					c.stepOne = true
				}
			case ControlSpeed:
				v := ev.SpeedValue
				if v < 0.5 {
					v = 0.5
				} else if v > 5.0 {
					v = 5.0
				}
				c.speed = v
			case ControlQuit:
				select {
				case <-c.quitCh:
				default:
					close(c.quitCh)
				}
			}
		default:
			break drain
		}
	}

	paused = c.paused
	stepOne = c.stepOne
	speed = c.speed

	// Consume the step permit: it fires exactly once.
	if c.stepOne {
		c.stepOne = false
	}

	select {
	case <-c.quitCh:
		quit = true
	default:
	}

	return
}

// Epoch returns the current admission epoch, incremented on every gate
// transition (pause, resume, step, speed change, quit).
func (c *Controller) Epoch() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.epoch
}

// Paused reports whether the gate is currently frozen.
func (c *Controller) Paused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

// Speed returns the current pacing multiplier.
func (c *Controller) Speed() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.speed
}

// QuitCh returns a channel that closes when ControlQuit is sent.
func (c *Controller) QuitCh() <-chan struct{} {
	return c.quitCh
}

// SendPause sends a pause event. It satisfies tui.Controller.
func (c *Controller) SendPause() { c.Send(ControlEvent{Kind: ControlPause}) }

// SendResume sends a resume event. It satisfies tui.Controller.
func (c *Controller) SendResume() { c.Send(ControlEvent{Kind: ControlResume}) }

// SendStep sends a step event. It satisfies tui.Controller.
func (c *Controller) SendStep() { c.Send(ControlEvent{Kind: ControlStep}) }

// SendSpeed sends a speed delta. It satisfies tui.Controller.
func (c *Controller) SendSpeed(delta float64) {
	c.mu.Lock()
	current := c.speed
	c.mu.Unlock()
	c.Send(ControlEvent{Kind: ControlSpeed, SpeedValue: current + delta})
}

// SendQuit sends a quit event. It satisfies tui.Controller.
func (c *Controller) SendQuit() { c.Send(ControlEvent{Kind: ControlQuit}) }
