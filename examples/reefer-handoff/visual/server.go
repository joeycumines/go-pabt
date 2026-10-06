// Copyright 2026 Joseph Cumines
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

// Package visual serves the browser-based animated yard visualization.
// It reads world state via WorldLoop.Snapshot() and never mutates it —
// the simulation is a viewer, not a controller.
package visual

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// StateResponse is the JSON shape served at GET /sim/state.
type StateResponse struct {
	Round    int            `json:"round"`
	WorldSeq uint64         `json:"worldSeq"`
	Phase    sim.Phase      `json:"phase"`
	Cutoff   int            `json:"cutoff"`
	Tractors []TractorState `json:"tractors"`
	Reefers  []ReeferState  `json:"reefers"`
	Slots    []SlotState    `json:"slots"`
	Lanes    []LaneState    `json:"lanes"`
	Nodes    []NodeState    `json:"nodes"`
}

// TractorState is one tractor's position and assignment.
type TractorState struct {
	ID   sim.TractorID `json:"id"`
	At   sim.NodeID    `json:"at"`
	Job  sim.JobID     `json:"job"`
	Hold sim.ReeferID  `json:"hold"`
}

// ReeferState is one reefer's temperature, tag, and custody.
type ReeferState struct {
	ID          sim.ReeferID  `json:"id"`
	TempC       int           `json:"tempC"`
	TagReadable bool          `json:"tagReadable"`
	At          sim.NodeID    `json:"at"`
	Custodian   sim.TractorID `json:"custodian"`
}

// SlotState is one powered slot's rating, plug, and occupant.
type SlotState struct {
	ID        sim.SlotID   `json:"id"`
	Node      sim.NodeID   `json:"node"`
	RatedMaxC int          `json:"ratedMaxC"`
	Plugged   bool         `json:"plugged"`
	Occupied  sim.ReeferID `json:"occupied"`
}

// LaneState is one lane's endpoints and lock status.
type LaneState struct {
	ID     sim.LaneID `json:"id"`
	A      sim.NodeID `json:"a"`
	B      sim.NodeID `json:"b"`
	Locked bool       `json:"locked"`
}

// NodeState is one node's capacity and destination flag.
type NodeState struct {
	ID                sim.NodeID `json:"id"`
	Capacity          int        `json:"capacity"`
	ReeferDestination bool       `json:"reeferDestination"`
}

// Server serves the visual simulation's HTTP surface. It binds loopback in
// the constructor so the address is known before serving, following the same
// lifecycle as pabtdebug.Server.
type Server struct {
	listener net.Listener
	server   *http.Server
	loop     *sim.WorldLoop

	mu      sync.Mutex
	started bool
}

// NewServer creates the sim server bound to the given loopback address. An
// empty port asks the kernel for one. The world loop is read via Snapshot()
// on every request; no lock of its own is needed because Snapshot copies.
func NewServer(addr string, loop *sim.WorldLoop) (*Server, error) {
	if loop == nil {
		return nil, fmt.Errorf("visual: the sim server needs a world loop")
	}
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("visual: address %q is not host:port: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("visual: the sim server binds loopback only, %q is not a loopback address", host)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("visual: listen on %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	s := &Server{
		listener: listener,
		server:   &http.Server{Handler: mux},
		loop:     loop,
	}
	mux.HandleFunc("/sim/state", s.handleState)
	return s, nil
}

// Start serves on the listener the constructor bound.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("visual: the sim server is already serving")
	}
	s.started = true
	s.mu.Unlock()
	return s.server.Serve(s.listener)
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Close shuts the server down, releasing the port.
func (s *Server) Close() error {
	return s.server.Close()
}

// handleState serves GET /sim/state as a JSON snapshot of the world.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	world := s.loop.Snapshot()
	resp := buildStateResponse(world)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	if err := enc.Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func buildStateResponse(world sim.World) StateResponse {
	topo := world.Topology()
	locked := topo.LockedLanes()
	lockedSet := make(map[sim.LaneID]bool, len(locked))
	for _, id := range locked {
		lockedSet[id] = true
	}

	tractors := world.Tractors()
	tStates := make([]TractorState, len(tractors))
	for i, t := range tractors {
		tStates[i] = TractorState{ID: t.ID, At: t.At, Job: t.Job, Hold: t.Hold}
	}

	reefers := world.Reefers()
	rStates := make([]ReeferState, len(reefers))
	for i, rf := range reefers {
		rStates[i] = ReeferState{
			ID: rf.ID, TempC: rf.TempC, TagReadable: rf.TagReadable,
			At: rf.At, Custodian: rf.Custodian,
		}
	}

	slots := world.Slots()
	sStates := make([]SlotState, len(slots))
	for i, sl := range slots {
		sStates[i] = SlotState{
			ID: sl.ID, Node: sl.Node, RatedMaxC: sl.RatedMaxC,
			Plugged: sl.Plugged, Occupied: sl.Occupied,
		}
	}

	lanes := topo.Lanes()
	lStates := make([]LaneState, len(lanes))
	for i, ln := range lanes {
		lStates[i] = LaneState{ID: ln.ID, A: ln.From, B: ln.To, Locked: lockedSet[ln.ID]}
	}

	nodes := world.Nodes()
	nStates := make([]NodeState, len(nodes))
	for i, n := range nodes {
		nStates[i] = NodeState{ID: n.ID, Capacity: n.Capacity, ReeferDestination: n.ReeferDestination}
	}

	return StateResponse{
		Round:    world.Round(),
		WorldSeq: world.WorldSeq(),
		Phase:    world.Phase(),
		Cutoff:   sim.CutoffRound,
		Tractors: tStates,
		Reefers:  rStates,
		Slots:    sStates,
		Lanes:    lStates,
		Nodes:    nStates,
	}
}
