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
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ErrUnauthenticated reports a request without the capability the server issued.
var ErrUnauthenticated = errors.New(`pabtdebug: the request is not authenticated`)

// DurableServerConfig configures the durable server.
type DurableServerConfig struct {
	// Addr is the loopback address to listen on. An empty port asks the kernel for
	// one, which is what a test and an evidence run both want.
	Addr string

	// Capability is the token every request must present. An empty one is minted, and
	// then the caller has to read it from Capability, because it is never written
	// anywhere a browser, a log, or a shell history would find it.
	Capability string

	// ReadyFile, when set, is written with the address the server actually bound and
	// removed on close. It carries the address and nothing else: a file another
	// process can read is not a place for a token.
	ReadyFile string

	// HubLimits bound what the stream's hub may hold.
	HubLimits DurableHubLimits

	// KeepAlive is how long a silent stream waits before a comment is written.
	KeepAlive time.Duration
}

// durable is the state the routes need, held on the Server so one type owns one
// lifecycle and one mux.
type durable struct {
	store      *SQLiteStore
	capability string
	readyFile  string
	keepAlive  time.Duration
	limits     DurableHubLimits

	mu     sync.RWMutex
	hub    *DurableHub
	closed bool
}

// NewDurableServer builds a server that serves the durable history and nothing else.
//
// The listener is created here rather than by Start, so the address is known before
// anything is served and a caller never has to guess which port it got. The listener
// binds loopback only: this is a trusted local debugger, and the token is a second
// boundary rather than the first.
//
// No route reads a live plan. A live tree read while a tick runs races with the
// writes expansion performs, and the history is the whole point: everything a route
// serves is a record the store committed.
func NewDurableServer(store *SQLiteStore, cfg DurableServerConfig) (*Server, error) {
	if store == nil {
		return nil, fmt.Errorf(`pabtdebug: the durable server needs a store`)
	}
	if err := store.checkUsable(false); err != nil {
		return nil, err
	}
	if cfg.HubLimits.MaxQueuedEvents == 0 {
		cfg.HubLimits = DefaultDurableHubLimits()
	}
	if err := cfg.HubLimits.Validate(); err != nil {
		return nil, err
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 15 * time.Second
	}
	addr := cfg.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("pabtdebug: the durable server address %q is not host:port: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		// A hostname is refused rather than resolved: resolving one and checking the
		// answer would leave a window where the name points somewhere else.
		return nil, fmt.Errorf("pabtdebug: the durable server binds loopback only, %q is not a loopback address", host)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("pabtdebug: listen on %s: %w", addr, err)
	}
	capability := cfg.Capability
	if capability == "" {
		capability, err = mintCapability()
		if err != nil {
			_ = listener.Close()
			return nil, err
		}
	}
	d := &durable{
		store:      store,
		capability: capability,
		readyFile:  cfg.ReadyFile,
		keepAlive:  cfg.KeepAlive,
		limits:     cfg.HubLimits,
	}
	mux := http.NewServeMux()
	s := &Server{
		addr:     addr,
		server:   &http.Server{Handler: mux},
		listener: listener,
		durable:  d,
		guarded:  http.NewServeMux(),
	}
	// The routes live on their own mux with the capability check in front of it.
	// Registering them on one mux with a prefix pattern would not work: a more specific
	// pattern wins, so every named route would answer without ever reaching the check.
	d.server(s)
	mux.Handle("/debug/pabt/", s.requireCapability(s.guarded))
	// The browser client is served outside that subtree, and the reason is in
	// uiPrefix: it carries no data and no capability, and keeping it out is what
	// leaves "every route requires the capability" checkable as a property.
	d.clientUI(mux)
	if err := d.writeReady(listener.Addr().String()); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return s, nil
}

// mintCapability returns a token nothing else can guess.
func mintCapability() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("pabtdebug: mint a capability: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// Capability returns the token every request must present.
func (s *Server) Capability() string {
	if s.durable == nil {
		return ""
	}
	return s.durable.capability
}

// writeReady records the bound address, and only the address.
func (d *durable) writeReady(addr string) error {
	if d.readyFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(d.readyFile), 0o700); err != nil {
		return fmt.Errorf("pabtdebug: ready file directory: %w", err)
	}
	if err := os.WriteFile(d.readyFile, []byte(addr+"\n"), 0o600); err != nil {
		return fmt.Errorf("pabtdebug: write the ready file: %w", err)
	}
	return nil
}

// requireCapability refuses every request that does not present the token.
//
// The token is read from a header, so it never reaches a URL, an access log, a
// referrer, or a shell history. The comparison is constant time, because a token that
// can be guessed a byte at a time is not a token.
func (s *Server) requireCapability(next *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			s.handleOptions(w, r)
			return
		}
		if !s.authenticated(r) {
			setCorsHeaders(w)
			w.Header().Set("WWW-Authenticate", `Bearer realm="pabtdebug"`)
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		setCorsHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// requireCapabilityHandler wraps one handler in the check, for a route registered
// outside the durable subtree where the guard does not reach.
func (s *Server) requireCapabilityHandler(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			s.handleOptions(w, r)
			return
		}
		if !s.authenticated(r) {
			setCorsHeaders(w)
			w.Header().Set("WWW-Authenticate", `Bearer realm="pabtdebug"`)
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		setCorsHeaders(w)
		next(w, r)
	})
}

// authenticated reports whether the request presented the token.
func (s *Server) authenticated(r *http.Request) bool {
	presented := r.Header.Get(capabilityHeader)
	want := s.durable.capability
	return presented != "" && subtle.ConstantTimeCompare([]byte(presented), []byte(want)) == 1
}

// capabilityHeader is where the token is presented.
const capabilityHeader = "X-PABT-Capability"

// setCorsHeaders marks the response as readable by any origin. The debugger
// serves loopback only and every route except the shell is capability-gated, so
// the origin header is a browser formality, not a security boundary. Without
// it, a sim page on a different loopback port cannot read committed records
// for replay (V6).
func setCorsHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", capabilityHeader)
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
}

// handleOptions answers the CORS preflight the browser sends before a
// cross-origin request that carries the capability header. It is outside the
// auth check by construction: a preflight carries no custom headers, so it
// cannot present the capability, and answering it reveals nothing.
func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	setCorsHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

// server registers the route set.
func (d *durable) server(s *Server) {
	mux := s.guarded
	mux.HandleFunc("/debug/pabt/state", d.handleState)
	mux.HandleFunc("/debug/pabt/records", d.handleRecords)
	mux.HandleFunc("/debug/pabt/records/{sequence}", d.handleRecord)
	mux.HandleFunc("/debug/pabt/trees/{sequence}", d.handleTree)
	mux.HandleFunc("/debug/pabt/representations/{sequence}", d.handleRepresentation)
	mux.HandleFunc("/debug/pabt/stream", d.handleStream)
	mux.HandleFunc("/debug/pabt/export", d.handleExport)
	mux.HandleFunc("/debug/pabt/import", d.handleImport)
	mux.HandleFunc("/debug/pabt/cursor", d.handleCursor)
}

// ready reports what the store is serving, refusing a store that is not ready to be
// read as though it were.
//
// A store with an open intent is pending, and a pending store's chain is checked on
// open. Serving it anyway would show a history whose last record may not be the last
// thing that happened.
func (d *durable) ready(ctx context.Context) (Installation, error) {
	installation, err := d.store.Installation(ctx)
	if err != nil {
		return Installation{}, err
	}
	if installation.Recovery == RecoveryCorrupt {
		return installation, fmt.Errorf("%w: the store reports itself corrupt", ErrStoreCorrupt)
	}
	return installation, nil
}

// activeRun returns the run the store is serving.
func (d *durable) activeRun(ctx context.Context) (RunKey, error) {
	installation, err := d.ready(ctx)
	if err != nil {
		return RunKey{}, err
	}
	var key RunKey
	row := d.store.readers.QueryRowContext(ctx,
		"SELECT plan_id, run_id FROM runs WHERE session_id = ? ORDER BY rowid DESC LIMIT 1",
		installation.ActiveSessionID)
	switch err := row.Scan(&key.PlanID, &key.RunID); {
	case errors.Is(err, sql.ErrNoRows):
		return RunKey{}, fmt.Errorf("%w: the active session has no run", ErrNotFound)
	case err != nil:
		return RunKey{}, fmt.Errorf("pabtdebug: read the active run: %w", err)
	}
	key.SessionID = installation.ActiveSessionID
	if key.IsZero() {
		return RunKey{}, fmt.Errorf("%w: the active session names no run", ErrNotFound)
	}
	return key, nil
}

// StateResponse is what the store is serving.
type StateResponse struct {
	StoreID        StoreID       `json:"storeId"`
	Generation     uint64        `json:"generation"`
	Recovery       RecoveryState `json:"recovery"`
	ActiveSession  string        `json:"activeSessionId"`
	Run            RunKey        `json:"run"`
	RunState       string        `json:"runState"`
	Head           RunHead       `json:"head"`
	ImportLimit    ImportLimits  `json:"-"`
	HubOverflows   uint64        `json:"hubOverflows"`
	HubSubscribers int           `json:"hubSubscribers"`
}

func (d *durable) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	installation, err := d.ready(ctx)
	if err != nil {
		d.fail(w, err)
		return
	}
	key, err := d.activeRun(ctx)
	if err != nil {
		d.fail(w, err)
		return
	}
	head, err := d.store.Head(ctx, key)
	if err != nil {
		d.fail(w, err)
		return
	}
	state, err := d.store.RunState(ctx, key)
	if err != nil {
		d.fail(w, err)
		return
	}
	out := StateResponse{
		StoreID:       installation.StoreID,
		Generation:    installation.Generation,
		Recovery:      installation.Recovery,
		ActiveSession: installation.ActiveSessionID,
		Run:           key,
		RunState:      state,
		Head:          head,
	}
	if hub := d.hubOf(r.Context(), key, installation.Generation); hub != nil {
		out.HubOverflows = hub.Overflowed()
		out.HubSubscribers = hub.Clients()
	}
	d.writeJSON(w, http.StatusOK, out)
}

// handleRecords serves a bounded page of whole records, which is what a resuming
// client reads.
func (d *durable) handleRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	key, installation, err := d.resolve(r)
	if err != nil {
		d.fail(w, err)
		return
	}
	after, err := queryUint(r, "after", 0)
	if err != nil {
		d.fail(w, err)
		return
	}
	head, err := d.store.Head(ctx, key)
	if err != nil {
		d.fail(w, err)
		return
	}
	if after > head.HeadSequence {
		// A position the run has not reached is refused rather than answered with an
		// empty page whose cursor reads zero, which would send a paging client back to
		// the start of the run.
		d.fail(w, fmt.Errorf("%w: position %d is past the head %d",
			ErrHeadMismatch, after, head.HeadSequence))
		return
	}
	page, err := d.store.RecordsSince(ctx, key, after)
	if err != nil {
		d.fail(w, err)
		return
	}
	page.RunKey = key
	// The cursor a client leaves with names the position it asked from when the page is
	// empty, so an empty page at a live position does not rewind it to zero.
	position := page.Through
	if position == 0 {
		position = after
	}
	cursor := Cursor{
		StoreID:    installation.StoreID,
		RunKey:     key,
		Generation: installation.Generation,
		Sequence:   position,
	}
	// The keyframe reference is not set here. It is a convenience naming a tree the
	// client may hold, and it is only useful with the digest beside it: a sequence
	// on its own says "some tree, unverifiably", and a client that fetched it would
	// have no way to tell a keyframe from anything else at that position. A page
	// leaves the client a position, and /cursor mints a cursor that carries the
	// whole reference.
	d.writeJSON(w, http.StatusOK, RecordPageResponse{Page: page, Cursor: cursor})
}

// RecordPageResponse is a bounded page plus the position it leaves the client at.
type RecordPageResponse struct {
	Page   RecordPage `json:"page"`
	Cursor Cursor     `json:"cursor"`
}

func (d *durable) handleRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sequence, ok := d.sequence(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	key, _, err := d.resolve(r)
	if err != nil {
		d.fail(w, err)
		return
	}
	record, err := d.store.RecordAt(ctx, key, sequence)
	if err != nil {
		d.fail(w, err)
		return
	}
	d.writeJSON(w, http.StatusOK, record)
}

// handleTree serves one record's tree.
func (d *durable) handleTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sequence, ok := d.sequence(w, r)
	if !ok {
		return
	}
	key, _, err := d.resolve(r)
	if err != nil {
		d.fail(w, err)
		return
	}
	tree, err := d.store.TreeAt(r.Context(), key, sequence)
	if err != nil {
		d.fail(w, err)
		return
	}
	d.writeJSON(w, http.StatusOK, tree)
}

// RepresentationResponse is a record's tree and the exact change from its base.
//
// The delta is served beside the keyframe because a client applies one to the other:
// sending the delta alone would make the client fetch a tree whose sequence it has to
// guess, and sending the tree alone would make it page a run to find the change.
type RepresentationResponse struct {
	RunKey   RunKey          `json:"runKey"`
	Sequence uint64          `json:"sequence"`
	Tree     StoredTree      `json:"tree"`
	Delta    *ExactTreeDelta `json:"delta,omitempty"`
	Base     *BaseReference  `json:"base,omitempty"`
}

// BaseReference names a tree a client can fetch to apply the delta.
type BaseReference struct {
	Sequence uint64 `json:"sequence"`
	EventUID string `json:"eventUid"`
	Digest   string `json:"digest"`
}

func (d *durable) handleRepresentation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sequence, ok := d.sequence(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	key, _, err := d.resolve(r)
	if err != nil {
		d.fail(w, err)
		return
	}
	tree, _, delta, err := d.store.Representation(ctx, key, sequence)
	if err != nil {
		d.fail(w, err)
		return
	}
	// A delta is only usable against the base it names, so the base is named rather
	// than sent: it is a tree the client very likely already holds, and sending it
	// again would be the thing that makes a stream expensive.
	out := RepresentationResponse{RunKey: key, Sequence: sequence, Tree: tree, Delta: delta}
	if delta != nil {
		out.Base = &BaseReference{
			Sequence: delta.BaseSequence,
			EventUID: delta.BaseEventUID,
			Digest:   delta.BaseDigest,
		}
	}
	d.writeJSON(w, http.StatusOK, out)
}

// handleCursor mints a cursor for a position, so a client does not have to construct
// one and does not have to know what is in it.
func (d *durable) handleCursor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	key, installation, err := d.resolve(r)
	if err != nil {
		d.fail(w, err)
		return
	}
	after, err := queryUint(r, "after", 0)
	if err != nil {
		d.fail(w, err)
		return
	}
	head, err := d.store.Head(ctx, key)
	if err != nil {
		d.fail(w, err)
		return
	}
	if after > head.HeadSequence {
		d.fail(w, fmt.Errorf("%w: position %d is past the head %d",
			ErrHeadMismatch, after, head.HeadSequence))
		return
	}
	cursor := Cursor{
		StoreID:    installation.StoreID,
		RunKey:     key,
		Generation: installation.Generation,
		Sequence:   after,
	}
	if head.HeadTreeSequence != 0 && head.HeadTreeSequence <= after {
		cursor.KeyframeSequence = head.HeadTreeSequence
		cursor.KeyframeDigest = head.HeadTreeDigest
	}
	token, err := cursor.Encode()
	if err != nil {
		d.fail(w, err)
		return
	}
	d.writeJSON(w, http.StatusOK, CursorResponse{Cursor: cursor, Token: token})
}

// CursorResponse is a cursor and its opaque form.
type CursorResponse struct {
	Cursor Cursor `json:"cursor"`
	Token  string `json:"token"`
}

// resolve returns the run a request names, which is the store's active run.
func (d *durable) resolve(r *http.Request) (RunKey, Installation, error) {
	ctx := r.Context()
	installation, err := d.ready(ctx)
	if err != nil {
		return RunKey{}, installation, err
	}
	key, err := d.activeRun(ctx)
	if err != nil {
		return RunKey{}, installation, err
	}
	return key, installation, nil
}

// maxSequence is the largest sequence the store can address. A sequence above it
// cannot be bound by the row it names, and a driver asked for one answers with its own
// error, so it is refused here as the bad request it is.
const maxSequence = uint64(math.MaxInt64)

// sequence reads a sequence from the path, refusing anything that is not one.
func (d *durable) sequence(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	raw := r.PathValue("sequence")
	sequence, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || sequence == 0 || sequence > maxSequence {
		http.Error(w, "the sequence must be a positive decimal number", http.StatusBadRequest)
		return 0, false
	}
	return sequence, true
}

// ErrBadParameter reports a request parameter that is not the value it must be.
var ErrBadParameter = errors.New(`pabtdebug: malformed request parameter`)

// queryUint reads a bounded unsigned query parameter.
//
// Bounded means the value, not only the type: a sequence the store cannot address is
// refused here rather than handed to a driver that would report it as an internal
// error.
func queryUint(r *http.Request, name string, fallback uint64) (uint64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be a decimal number", ErrBadParameter, name)
	}
	if value > maxSequence {
		return 0, fmt.Errorf("%w: %s is above the largest sequence a store can address",
			ErrBadParameter, name)
	}
	return value, nil
}

// fail maps a store error onto a status a client can act on.
func (d *durable) fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrBadParameter):
		status = http.StatusBadRequest
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrHeadMismatch), errors.Is(err, ErrCursorStale):
		status = http.StatusConflict
	case errors.Is(err, ErrStoreCorrupt):
		status = http.StatusInternalServerError
	case errors.Is(err, ErrImportStale), errors.Is(err, ErrBundleInvalid),
		errors.Is(err, ErrImportTooLarge):
		status = http.StatusBadRequest
	}
	http.Error(w, err.Error(), status)
}

// writeJSON writes a canonical response, so a client's bytes are the store's bytes.
func (d *durable) writeJSON(w http.ResponseWriter, status int, value any) {
	encoded, err := CanonicalEncode(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte("\n"))
}

// hubOf returns the hub for the run and generation the store is serving now.
//
// It is revalidated on every call rather than cached blindly, because a hub is about
// one history: an import replaces the active session and moves the generation, and a
// hub kept across that would announce one run's records on another run's stream. The
// old hub is closed, which ends the connections attached to it, and the next request
// builds one for what the store serves now.
func (d *durable) hubOf(ctx context.Context, key RunKey, generation uint64) *DurableHub {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	if d.hub != nil && (d.hub.RunKey() != key || d.hub.Generation() != generation) {
		d.hub.Close()
		d.hub = nil
	}
	if d.hub != nil {
		return d.hub
	}
	hub, err := NewDurableHub(key, generation, d.limits)
	if err != nil {
		return nil
	}
	d.hub = hub
	return hub
}

// Announce publishes a committed record to the stream's current hub.
//
// It is the wiring an application uses, and it resolves the hub on every call rather
// than capturing one: after an import the hub is replaced, and a session that captured
// the old one would announce into a closed hub for the rest of the process.
func (s *Server) Announce(ctx context.Context, receipt CommitReceipt, record StoredRecord) error {
	if s.durable == nil {
		return fmt.Errorf(`pabtdebug: this server does not serve a durable history`)
	}
	key, err := s.durable.activeRun(ctx)
	if err != nil {
		return err
	}
	installation, err := s.durable.store.Installation(ctx)
	if err != nil {
		return err
	}
	if record.RunKey != key {
		// A session recording into a run the store no longer serves would otherwise put
		// that run's records on the current run's stream, under the current generation.
		return fmt.Errorf("%w: the record names run %s, the store serves %s",
			ErrCursorStale, record.RunKey.RunID, key.RunID)
	}
	hub := s.durable.hubOf(ctx, key, installation.Generation)
	if hub == nil {
		return fmt.Errorf(`pabtdebug: the stream has no hub`)
	}
	return hub.Publish(ctx, receipt, record)
}

// close ends the server's own state.
func (d *durable) close() {
	d.mu.Lock()
	hub := d.hub
	d.hub = nil
	d.closed = true
	d.mu.Unlock()
	if hub != nil {
		hub.Close()
	}
	if d.readyFile != "" {
		_ = os.Remove(d.readyFile)
	}
}
