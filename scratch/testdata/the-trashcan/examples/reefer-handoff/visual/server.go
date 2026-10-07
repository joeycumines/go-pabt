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
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

//go:embed client
var clientFS embed.FS

// StateResponse is the JSON shape served at GET /sim/state.
type StateResponse struct {
	Round      int            `json:"round"`
	WorldSeq   uint64         `json:"worldSeq"`
	CommandSeq uint64         `json:"commandSeq"`
	Phase      sim.Phase      `json:"phase"`
	Cutoff     int            `json:"cutoff"`
	Tractors   []TractorState `json:"tractors"`
	Reefers    []ReeferState  `json:"reefers"`
	Slots      []SlotState    `json:"slots"`
	Lanes      []LaneState    `json:"lanes"`
	Nodes      []NodeState    `json:"nodes"`
	Receipts   []ReceiptState `json:"receipts"`
	// Control is the host transport gate state, present only when a
	// controller is attached (V5). It lets the page show pause/speed truth
	// rather than assume its last POST landed.
	Control *ControlState `json:"control,omitempty"`
}

// ControlState reports the host transport gate the sim page drives.
type ControlState struct {
	Paused bool    `json:"paused"`
	Speed  float64 `json:"speed"`
}

// ControlSource is the host-level transport gate the sim page drives through
// POST /sim/control. The wired example attaches the main Controller; a
// fixture that attaches none serves 503 on the control route. The interface
// names the Controller's own methods, so attaching is a pass-through, never a
// translation layer.
type ControlSource interface {
	SendPause()
	SendResume()
	SendStep()
	SetSpeed(value float64)
	Paused() bool
	Speed() float64
}

// DebuggerLink pairs a tractor with the page where its committed plan trees
// can be inspected, for the causal link-through the detail panel offers.
type DebuggerLink struct {
	Tractor string `json:"tractor"`
	URL     string `json:"url"`
}

// ReceiptState is one command outcome from the world's receipt ledger. The
// ledger is world state, not durable history, so it is served without a
// capability gate (DD-40): the simulation reads what the world itself records,
// never a tractor's private causality records. The client diffs consecutive
// responses on CommandSeq and animates newly applied commands and refusals.
type ReceiptState struct {
	CommandSeq uint64          `json:"commandSeq"`
	Kind       sim.CommandKind `json:"kind"`
	Tractor    sim.TractorID   `json:"tractor"`
	Outcome    sim.Outcome     `json:"outcome"`
	Reason     sim.Reason      `json:"reason"`
	Holder     string          `json:"holder"`
}

// TractorState is one tractor's position and assignment. Generation is the
// plan generation the tractor's runner currently holds, filled from the
// attached generation source; it is zero when no source is attached or the
// runner holds no plan.
type TractorState struct {
	ID         sim.TractorID `json:"id"`
	At         sim.NodeID    `json:"at"`
	Job        sim.JobID     `json:"job"`
	Hold       sim.ReeferID  `json:"hold"`
	Generation uint64        `json:"generation"`
}

// ReeferState is one reefer's temperature, tag, and custody.
type ReeferState struct {
	ID          sim.ReeferID  `json:"id"`
	TempC       int           `json:"tempC"`
	TagReadable bool          `json:"tagReadable"`
	At          sim.NodeID    `json:"at"`
	Custodian   sim.TractorID `json:"custodian"`
}

// SlotState is one powered slot's rating, plug, occupant, and version.
type SlotState struct {
	ID        sim.SlotID   `json:"id"`
	Node      sim.NodeID   `json:"node"`
	RatedMaxC int          `json:"ratedMaxC"`
	Plugged   bool         `json:"plugged"`
	Occupied  sim.ReeferID `json:"occupied"`
	Version   uint64       `json:"version"`
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
//
// The control, generation, and debugger attach points are wired once, before
// Start, by the same main that builds the server; the handlers only read
// them, so they need no lock of their own.
type Server struct {
	listener net.Listener
	server   *http.Server
	loop     *sim.WorldLoop

	control     ControlSource
	generations func() map[string]uint64
	debuggers   []DebuggerLink

	mu      sync.Mutex
	started bool
}

// AttachController wires the host transport gate the page drives through
// POST /sim/control, and reports through /sim/state. Call before Start.
func (s *Server) AttachController(c ControlSource) { s.control = c }

// SetGenerationSource wires the function that reports each tractor's current
// plan generation, keyed by tractor ID. Call before Start.
func (s *Server) SetGenerationSource(fn func() map[string]uint64) { s.generations = fn }

// AddDebugger records one tractor's debugger page for the detail panel's
// causal link-through. Call before Start.
func (s *Server) AddDebugger(tractor, url string) {
	s.debuggers = append(s.debuggers, DebuggerLink{Tractor: tractor, URL: url})
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
	mux.HandleFunc("/sim/control", s.handleControl)
	mux.HandleFunc("/sim/debuggers", s.handleDebuggers)
	s.registerClient(mux)
	return s, nil
}

// clientAssets is the exact set of files the simulation client is made of,
// with the media type each is served as. Names are looked up in this map
// rather than joined onto the embedded directory, so a request cannot name a
// file that is not listed, and the .mjs module is guaranteed the JavaScript
// media type a browser requires in order to execute it.
var clientAssets = map[string]string{
	"index.html": "text/html; charset=utf-8",
	"sim.css":    "text/css; charset=utf-8",
	"js/sim.mjs": "text/javascript; charset=utf-8",
}

// registerClient serves the embedded simulation client at /sim/ui. No
// capability gate: the sim page shows world state only, never durable
// history (DD-40).
func (s *Server) registerClient(mux *http.ServeMux) {
	mux.HandleFunc("/sim/ui", s.handleClientShell)
	mux.HandleFunc("/sim/ui/", s.handleClientAsset)
}

// handleClientShell serves the document the client boots from. With and
// without the trailing slash are the same document; the asset references in
// it are absolute, so both spellings load the same page.
func (s *Server) handleClientShell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeClientAsset(w, r, "index.html")
}

// handleClientAsset serves one file of the client.
func (s *Server) handleClientAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/sim/ui/")
	if name == "" {
		name = "index.html"
	}
	s.writeClientAsset(w, r, name)
}

// writeClientAsset writes one embedded file with its declared media type.
func (s *Server) writeClientAsset(w http.ResponseWriter, r *http.Request, name string) {
	mediaType, ok := clientAssets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := clientFS.ReadFile("client/" + name)
	if err != nil {
		// The map names a file the embed does not carry: a build error, not a
		// bad request.
		http.Error(w, "the client is not built correctly", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
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
	if s.control != nil {
		resp.Control = &ControlState{Paused: s.control.Paused(), Speed: s.control.Speed()}
	}
	if s.generations != nil {
		gens := s.generations()
		for i := range resp.Tractors {
			resp.Tractors[i].Generation = gens[string(resp.Tractors[i].ID)]
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	if err := enc.Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// controlRequest is the JSON shape POST /sim/control accepts.
type controlRequest struct {
	Action string  `json:"action"`
	Value  float64 `json:"value"`
}

// handleControl drives the attached host transport gate. The page posts
// {action: pause|resume|step|speed, value}; the response carries the gate
// state after the event landed, so the UI reflects truth rather than intent.
// With no controller attached the route says so instead of pretending.
func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.control == nil {
		http.Error(w, "no controller attached", http.StatusServiceUnavailable)
		return
	}
	var req controlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "the body is not a control request", http.StatusBadRequest)
		return
	}
	switch req.Action {
	case "pause":
		s.control.SendPause()
	case "resume":
		s.control.SendResume()
	case "step":
		s.control.SendStep()
	case "speed":
		s.control.SetSpeed(req.Value)
	default:
		http.Error(w, fmt.Sprintf("unknown control action %q", req.Action), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(ControlState{Paused: s.control.Paused(), Speed: s.control.Speed()})
}

// handleDebuggers serves GET /sim/debuggers: the per-tractor debugger pages
// the detail panel links to. Unauthenticated, like the rest of the sim
// surface: it names addresses, and the debugger shell itself is the only
// unauthenticated page on those servers (DD-40).
func (s *Server) handleDebuggers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(struct {
		Debuggers []DebuggerLink `json:"debuggers"`
	}{Debuggers: s.debuggers})
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
			Plugged: sl.Plugged, Occupied: sl.Occupied, Version: sl.Version,
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

	receipts := world.Receipts()
	rcStates := make([]ReceiptState, len(receipts))
	for i, r := range receipts {
		rcStates[i] = ReceiptState{
			CommandSeq: r.CommandSeq,
			Kind:       r.Kind,
			Tractor:    r.Tractor,
			Outcome:    r.Outcome,
			Reason:     r.Reason,
			Holder:     r.Holder,
		}
	}

	return StateResponse{
		Round:      world.Round(),
		WorldSeq:   world.WorldSeq(),
		CommandSeq: world.CommandSeq(),
		Phase:      world.Phase(),
		Cutoff:     sim.CutoffRound,
		Tractors:   tStates,
		Reefers:    rStates,
		Slots:      sStates,
		Lanes:      lStates,
		Nodes:      nStates,
		Receipts:   rcStates,
	}
}
