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

package pabtdebug

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// Server provides an HTTP API server for live introspection of PA-BT planning trees.
type Server struct {
	addr     string
	server   *http.Server
	mu       sync.RWMutex
	trackers map[string]*Tracker
	primary  *Tracker
	// tracker is retained for backward compatibility; it aliases primary.
	tracker *Tracker
}

// NewServer creates a new debug server for the given tracker at the specified address.
func NewServer(tracker *Tracker, addr string) *Server {
	if tracker != nil && tracker.ID() == "" {
		tracker.WithID("0")
	}
	m := make(map[string]*Tracker)
	if tracker != nil {
		m[tracker.ID()] = tracker
	}
	mux := http.NewServeMux()
	s := &Server{
		addr:     addr,
		server:   &http.Server{Addr: addr, Handler: mux},
		trackers: m,
		primary:  tracker,
		tracker:  tracker,
	}

	mux.HandleFunc("/debug/pabt/plans", s.handlePlans)
	mux.HandleFunc("/debug/pabt/plans/{id}", s.handlePlanDetail)
	mux.HandleFunc("/debug/pabt/plans/{id}/events", s.handlePlanEvents)
	mux.HandleFunc("/debug/pabt/plans/{id}/timeline", s.handlePlanTimeline)
	mux.HandleFunc("/debug/pabt/plans/{id}/timeline/{iter}", s.handlePlanTimelineIter)
	mux.HandleFunc("/debug/pabt/plans/{id}/search", s.handlePlanSearch)
	mux.HandleFunc("/debug/pabt/plans/{id}/dot", s.handlePlanDot)
	mux.HandleFunc("/debug/pabt/plans/{id}/diff", s.handlePlanDiff)
	mux.HandleFunc("/debug/pabt/plans/{id}/profile", s.handlePlanProfile)
	mux.HandleFunc("/debug/pabt/plans/{id}/breakpoints", s.handlePlanBreakpoints)
	mux.HandleFunc("/debug/pabt/plans/{id}/breakpoints/{path}", s.handlePlanBreakpointDelete)
	mux.HandleFunc("/debug/pabt/ui", s.handleUI)
	mux.HandleFunc("/debug/pabt/timeline", s.handleTimeline)
	mux.HandleFunc("/debug/pabt/timeline/{iter}", s.handleTimelineIter)
	mux.HandleFunc("/debug/pabt/search", s.handleSearch)
	mux.HandleFunc("/debug/pabt/dot", s.handleDot)
	mux.HandleFunc("/debug/pabt/diff", s.handleDiff)
	mux.HandleFunc("/debug/pabt/profile", s.handleProfile)
	mux.HandleFunc("/debug/pabt/breakpoints", s.handleBreakpoints)
	mux.HandleFunc("/debug/pabt/breakpoints/{path}", s.handleBreakpointDelete)

	return s
}

// RegisterTracker registers an additional tracker for multi-plan debugging.
// The tracker must have a non-empty ID (use Tracker.WithID or NewTrackerWithID).
// If a tracker with the same ID already exists, it is replaced.
func (s *Server) RegisterTracker(t *Tracker) {
	if t == nil {
		return
	}
	if t.ID() == "" {
		t.WithID("0")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trackers == nil {
		s.trackers = make(map[string]*Tracker)
	}
	s.trackers[t.ID()] = t
	if s.primary == nil {
		s.primary = t
		s.tracker = t
	}
}

// Tracker returns the tracker for the given plan ID, if registered.
func (s *Server) Tracker(id string) (*Tracker, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.trackers[id]
	return t, ok
}

// Trackers returns a snapshot of all registered trackers.
func (s *Server) Trackers() map[string]*Tracker {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*Tracker, len(s.trackers))
	for k, v := range s.trackers {
		out[k] = v
	}
	return out
}

func (s *Server) resolveTracker(r *http.Request) (*Tracker, bool) {
	if id := r.PathValue("id"); id != "" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		t, ok := s.trackers[id]
		return t, ok
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.primary != nil {
		return s.primary, true
	}
	if s.tracker != nil {
		return s.tracker, true
	}
	return nil, false
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

// Close gracefully shuts down the server, draining SSE connections first.
func (s *Server) Close() error {
	s.mu.RLock()
	seen := make(map[*Hub]struct{}, len(s.trackers)+2)
	for _, tr := range s.trackers {
		if tr != nil && tr.Hub() != nil {
			if _, ok := seen[tr.Hub()]; !ok {
				seen[tr.Hub()] = struct{}{}
				tr.Hub().Close()
			}
		}
	}
	for _, tr := range []*Tracker{s.tracker, s.primary} {
		if tr != nil && tr.Hub() != nil {
			if _, ok := seen[tr.Hub()]; !ok {
				seen[tr.Hub()] = struct{}{}
				tr.Hub().Close()
			}
		}
	}
	s.mu.RUnlock()
	return s.server.Close()
}

// handlePlans returns a JSON array of plan info for all registered plans.
func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s.mu.RLock()
	trackers := make([]*Tracker, 0, len(s.trackers))
	for _, t := range s.trackers {
		trackers = append(trackers, t)
	}
	s.mu.RUnlock()
	sort.Slice(trackers, func(i, j int) bool {
		return trackers[i].ID() < trackers[j].ID()
	})
	var result []map[string]any
	for _, t := range trackers {
		tree := t.BuildTree()
		events := t.Events()
		info := map[string]any{
			"id":           t.ID(),
			"eventCount":   len(events),
			"rootNodeType": "",
		}
		if tree != nil {
			info["rootNodeType"] = tree.NodeType
		}
		result = append(result, info)
	}
	// Backward compatibility: if no trackers registered yet, return empty array (not nil).
	if result == nil {
		result = []map[string]any{}
	}
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handlePlanDetail returns JSON of the tree structure for the requested plan.
func (s *Server) handlePlanDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing plan id", http.StatusBadRequest)
		return
	}
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", id), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	tree := t.BuildTree()
	if tree == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", id), http.StatusNotFound)
		return
	}
	if err := json.NewEncoder(w).Encode(tree); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePlanEvents(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	t.Hub().ServeHTTP(w, r)
}

func (s *Server) handlePlanTimeline(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(t.Timeline()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePlanTimelineIter(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	iterStr := r.PathValue("iter")
	if iterStr == "" {
		http.Error(w, "missing iteration", http.StatusBadRequest)
		return
	}
	iter, err := strconv.Atoi(iterStr)
	if err != nil {
		http.Error(w, "invalid iteration", http.StatusBadRequest)
		return
	}
	event, ok := t.EventAt(iter)
	if !ok {
		http.Error(w, fmt.Sprintf("iteration %d not found", iter), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(event); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePlanSearch(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]SearchResult{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(t.Search(q)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePlanDot(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	tree := t.BuildTree()
	w.Header().Set("Content-Type", "text/vnd.graphviz")
	w.Write([]byte(treeToDot(tree)))
}

func (s *Server) handlePlanDiff(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		http.Error(w, "missing from or to parameter", http.StatusBadRequest)
		return
	}
	from, err := strconv.Atoi(fromStr)
	if err != nil {
		http.Error(w, "invalid from parameter", http.StatusBadRequest)
		return
	}
	to, err := strconv.Atoi(toStr)
	if err != nil {
		http.Error(w, "invalid to parameter", http.StatusBadRequest)
		return
	}
	result, err := t.Diff(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePlanProfile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(t.Profile()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePlanBreakpoints(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(t.Breakpoints()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	case http.MethodPost:
		var bp Breakpoint
		if err := json.NewDecoder(r.Body).Decode(&bp); err != nil {
			http.Error(w, "invalid breakpoint JSON", http.StatusBadRequest)
			return
		}
		t.SetBreakpoint(bp)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(bp)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePlanBreakpointDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, fmt.Sprintf("plan %s not found", r.PathValue("id")), http.StatusNotFound)
		return
	}
	path := r.PathValue("path")
	if path == "" {
		http.Error(w, "missing breakpoint path", http.StatusBadRequest)
		return
	}
	t.RemoveBreakpoint(path)
	w.WriteHeader(http.StatusNoContent)
}

// handleUI serves the embedded web UI.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := uiHTML()
	if _, err := strings.NewReader(html).WriteTo(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleTimeline returns the timeline of all tick events for the primary tracker.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(t.Timeline()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleTimelineIter returns a single timeline entry by iteration number for the primary tracker.
func (s *Server) handleTimelineIter(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	iterStr := r.PathValue("iter")
	if iterStr == "" {
		http.Error(w, "missing iteration", http.StatusBadRequest)
		return
	}
	iter, err := strconv.Atoi(iterStr)
	if err != nil {
		http.Error(w, "invalid iteration", http.StatusBadRequest)
		return
	}

	event, ok := t.EventAt(iter)
	if !ok {
		http.Error(w, fmt.Sprintf("iteration %d not found", iter), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(event); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleSearch searches the current tree for nodes matching the query (primary tracker).
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]SearchResult{})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(t.Search(q)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleDot returns the current tree as GraphViz DOT format (primary tracker).
func (s *Server) handleDot(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	tree := t.BuildTree()
	w.Header().Set("Content-Type", "text/vnd.graphviz")
	w.Write([]byte(treeToDot(tree)))
}

// handleDiff returns the diff between two tree snapshots (primary tracker).
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		http.Error(w, "missing from or to parameter", http.StatusBadRequest)
		return
	}
	from, err := strconv.Atoi(fromStr)
	if err != nil {
		http.Error(w, "invalid from parameter", http.StatusBadRequest)
		return
	}
	to, err := strconv.Atoi(toStr)
	if err != nil {
		http.Error(w, "invalid to parameter", http.StatusBadRequest)
		return
	}

	result, err := t.Diff(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleProfile returns aggregated profiling data (primary tracker).
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(t.Profile()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleBreakpoints handles GET and POST for breakpoints (primary tracker).
func (s *Server) handleBreakpoints(w http.ResponseWriter, r *http.Request) {
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(t.Breakpoints()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	case http.MethodPost:
		var bp Breakpoint
		if err := json.NewDecoder(r.Body).Decode(&bp); err != nil {
			http.Error(w, "invalid breakpoint JSON", http.StatusBadRequest)
			return
		}
		t.SetBreakpoint(bp)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(bp)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBreakpointDelete deletes a breakpoint by node path (primary tracker).
func (s *Server) handleBreakpointDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	t, ok := s.resolveTracker(r)
	if !ok || t == nil {
		http.Error(w, "no plan registered", http.StatusNotFound)
		return
	}
	path := r.PathValue("path")
	if path == "" {
		http.Error(w, "missing breakpoint path", http.StatusBadRequest)
		return
	}
	t.RemoveBreakpoint(path)
	w.WriteHeader(http.StatusNoContent)
}

// treeToDot generates a GraphViz DOT representation of the tree.
func treeToDot(tree *TreeNode) string {
	if tree == nil {
		return "digraph pabt {\n}\n"
	}

	var sb strings.Builder
	sb.WriteString("digraph pabt {\n")
	sb.WriteString("  rankdir=TB;\n")
	sb.WriteString("  node [shape=box style=filled fontname=\"monospace\" fontcolor=\"#ffffff\"];\n")

	dotNode(tree, &sb)
	dotEdges(tree, &sb)

	sb.WriteString("}\n")
	return sb.String()
}

// dotNode writes the DOT node declarations for a tree.
func dotNode(tree *TreeNode, sb *strings.Builder) {
	if tree == nil {
		return
	}

	fillColor := dotFillColor(tree.NodeType)
	borderColor := dotBorderColor(tree.Status)

	label := tree.NodeType
	if tree.Name != "" && tree.Name != tree.NodeType {
		label = tree.NodeType + "\\n" + tree.Name
	}

	fmt.Fprintf(sb, "  %q [label=%q fillcolor=%q color=%q penwidth=2];\n",
		tree.ID, label, fillColor, borderColor)

	for i := range tree.Children {
		dotNode(&tree.Children[i], sb)
	}
}

// dotEdges writes the DOT edge declarations for a tree.
func dotEdges(tree *TreeNode, sb *strings.Builder) {
	if tree == nil {
		return
	}

	for i := range tree.Children {
		fmt.Fprintf(sb, "  %q -> %q;\n", tree.ID, tree.Children[i].ID)
		dotEdges(&tree.Children[i], sb)
	}
}

// dotFillColor returns the DOT fill color for a node type.
func dotFillColor(nodeType string) string {
	switch nodeType {
	case "GoalRoot", "GoalSelector":
		return "#2196f3"
	case "PPARoot", "PPAPost":
		return "#9c27b0"
	case "ActionSelector", "ActionRoot":
		return "#ff9800"
	case "ActionNode":
		return "#4caf50"
	case "PreconditionsRoot", "PreconditionLeaf":
		return "#00bcd4"
	default:
		return "#888888"
	}
}

// dotBorderColor returns the DOT border color for a node status.
func dotBorderColor(status *pabt.NodeStatus) string {
	if status == nil {
		return "#666666"
	}
	switch status.LastStatus() {
	case bt.Success:
		return "#4caf50"
	case bt.Failure:
		return "#f44336"
	case bt.Running:
		return "#ff9800"
	default:
		return "#666666"
	}
}
