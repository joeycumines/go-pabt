/*
   Copyright 2026 Joseph Cumines

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

package pabtdebug_test

import (
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

// Compile-time signature checks for the debug server example documented in
// README.md (Setup, under Debug Server). These assignments compile only if the
// real API matches the README, so any drift fails this build rather than a
// reader's program. See blueprint BUG-001.
var (
	_ func(*pabt.IPlan) *pabtdebug.Tracker               = pabtdebug.NewTracker
	_ func(*pabtdebug.Tracker, string) *pabtdebug.Server = pabtdebug.NewServer
	_ func(*pabtdebug.Tracker, bt.Status, error)         = (*pabtdebug.Tracker).Track
	_ func(*pabtdebug.Server) error                      = (*pabtdebug.Server).Start
	_ func(*pabtdebug.Server) error                      = (*pabtdebug.Server).Close
	_ func(*pabt.IPlan) bt.Node                          = (*pabt.IPlan).Node
)

// TestREADMEDebugServerExample exercises the constructor calls used by the
// README debug server example, confirming the documented setup path works with
// a fresh tracker. Ticking and listening are covered elsewhere.
func TestREADMEDebugServerExample(t *testing.T) {
	var plan *pabt.IPlan

	tracker := pabtdebug.NewTracker(plan)
	if tracker == nil {
		t.Fatal("pabtdebug.NewTracker returned nil")
	}

	server := pabtdebug.NewServer(tracker, "127.0.0.1:0")
	if server == nil {
		t.Fatal("pabtdebug.NewServer returned nil")
	}
}
