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

import (
	"testing"
)

func TestControllerPauseBlocksDrive(t *testing.T) {
	c := NewController()
	c.Send(ControlEvent{Kind: ControlPause})
	paused, stepOne, _, quit := c.Check()
	if !paused {
		t.Fatal("expected paused after ControlPause")
	}
	if stepOne {
		t.Fatal("expected stepOne false after ControlPause")
	}
	if quit {
		t.Fatal("expected quit false after ControlPause")
	}
}

func TestControllerStepAllowsOneRound(t *testing.T) {
	c := NewController()
	c.Send(ControlEvent{Kind: ControlPause})
	c.Send(ControlEvent{Kind: ControlStep})

	// First check: step permit fires.
	paused, stepOne, _, _ := c.Check()
	if !paused {
		t.Fatal("expected paused to remain true after step")
	}
	if !stepOne {
		t.Fatal("expected stepOne true on first check after step")
	}

	// Second check: permit consumed, still paused.
	paused, stepOne, _, _ = c.Check()
	if !paused {
		t.Fatal("expected paused to remain true after step consumed")
	}
	if stepOne {
		t.Fatal("expected stepOne false after permit consumed")
	}
}

func TestControllerEpochIncrements(t *testing.T) {
	c := NewController()
	if c.Epoch() != 0 {
		t.Fatalf("initial epoch %d, want 0", c.Epoch())
	}
	c.Send(ControlEvent{Kind: ControlPause})
	c.Send(ControlEvent{Kind: ControlStep})
	c.Send(ControlEvent{Kind: ControlResume})
	if c.Epoch() != 3 {
		t.Fatalf("epoch %d after three events, want 3", c.Epoch())
	}
}

func TestControllerSpeedClamps(t *testing.T) {
	c := NewController()
	c.Send(ControlEvent{Kind: ControlSpeed, SpeedValue: -1.0})
	_, _, speed, _ := c.Check()
	if speed != 0.5 {
		t.Fatalf("speed %f after negative send, want 0.5", speed)
	}
	c.Send(ControlEvent{Kind: ControlSpeed, SpeedValue: 10.0})
	_, _, speed, _ = c.Check()
	if speed != 5.0 {
		t.Fatalf("speed %f after over-max send, want 5.0", speed)
	}
}

func TestControllerQuitClosesChannel(t *testing.T) {
	c := NewController()
	c.Send(ControlEvent{Kind: ControlQuit})
	_, _, _, quit := c.Check()
	if !quit {
		t.Fatal("expected quit true after ControlQuit")
	}
	select {
	case <-c.QuitCh():
	default:
		t.Fatal("expected QuitCh closed after ControlQuit")
	}
}

func TestControllerResumeUnpauses(t *testing.T) {
	c := NewController()
	c.Send(ControlEvent{Kind: ControlPause})
	c.Check() // consume pause
	c.Send(ControlEvent{Kind: ControlResume})
	paused, _, _, _ := c.Check()
	if paused {
		t.Fatal("expected paused false after ControlResume")
	}
}

func TestControllerStepWithoutPauseIsIgnored(t *testing.T) {
	c := NewController()
	c.Send(ControlEvent{Kind: ControlStep})
	_, stepOne, _, _ := c.Check()
	if stepOne {
		t.Fatal("step without prior pause should not set stepOne")
	}
}
