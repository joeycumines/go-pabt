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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// durableFixture is a running durable server over a store with a committed run, plus
// the identity a request needs to be authenticated at all.
type durableFixture struct {
	server *Server
	store  *SQLiteStore
	key    RunKey
	plan   *sessionFixture
	client *http.Client
	ticks  int
}

func newDurableFixture(t *testing.T, records int) *durableFixture {
	t.Helper()
	return newDurableFixtureWith(t, records, DurableHubLimits{})
}

// newDurableFixtureWith builds the fixture with hub limits of its own, which is the
// only way to exercise a bound: the hub is created when the server is first asked
// something, so a limit changed afterwards would never be the one in force.
func newDurableFixtureWith(t *testing.T, records int, hubLimits DurableHubLimits) *durableFixture {
	t.Helper()
	plan := newSessionFixture(t)
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	server, err := NewDurableServer(store, DurableServerConfig{
		ReadyFile: filepath.Join(t.TempDir(), "ready"),
		HubLimits: hubLimits,
	})
	if err != nil {
		t.Fatalf("NewDurableServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Start() }()
	f := &durableFixture{
		server: server,
		store:  store,
		key:    key,
		plan:   plan,
		client: &http.Client{Timeout: 10 * time.Second},
	}
	f.commit(t, records)
	return f
}

func (f *durableFixture) commit(t *testing.T, n int) {
	t.Helper()
	ctx := context.Background()
	// The same wiring an application uses, so a record committed here reaches the
	// stream the way it would in a real process rather than by reaching inside.
	session, err := OpenSession(ctx, f.store, f.plan.plan, SessionConfig{
		Identity: SessionIdentity{
			SessionID: f.key.SessionID, PlanID: f.key.PlanID, RunID: f.key.RunID,
		},
		Announce: f.server.Announce,
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	for f.ticks < n {
		result := session.Tick(ctx)
		if result.Err != nil {
			t.Fatalf("Tick %d: %v", f.ticks+1, result.Err)
		}
		if result.Kind == RecordTick && result.StatusPresent {
			f.ticks++
		}
	}
}

// get issues an authenticated request.
func (f *durableFixture) get(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+f.server.Addr()+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set(capabilityHeader, f.server.Capability())
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = resp.Body.Close()
	return resp, body
}

// waitReady blocks until the server is answering, so a test does not race the
// goroutine that started it.
func (f *durableFixture) waitReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, "http://"+f.server.Addr()+"/debug/pabt/v2/state", nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set(capabilityHeader, f.server.Capability())
		resp, err := f.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the durable server never became ready")
}

func TestEveryRequestCarriesTheCapabilityAndNothingElseDoes(t *testing.T) {
	f := newDurableFixture(t, 2)
	f.waitReady(t)
	capability := f.server.Capability()
	if capability == "" {
		t.Fatal("the server issued no capability")
	}
	for _, path := range []string{
		"/debug/pabt/v2/state",
		"/debug/pabt/v2/stream",
		"/debug/pabt/v2/records",
		"/debug/pabt/v2/records/1",
		"/debug/pabt/v2/trees/1",
		"/debug/pabt/v2/representations/1",
		"/debug/pabt/v2/cursor?after=0",
		"/debug/pabt/v2/export",
	} {
		req, err := http.NewRequest(http.MethodGet, "http://"+f.server.Addr()+path, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp, err := f.client.Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a capability = %d, want 401", path, resp.StatusCode)
		}
		if bytes.Contains(body, []byte(capability)) {
			t.Errorf("%s echoed the capability in its refusal", path)
		}
		// A token in the URL is a token in a log, so a query parameter is not one.
		inURL := path
		if strings.Contains(path, "?") {
			inURL += "&capability=" + capability
		} else {
			inURL += "?capability=" + capability
		}
		req, err = http.NewRequest(http.MethodGet,
			"http://"+f.server.Addr()+inURL, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp, err = f.client.Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with the token in the URL = %d, want 401", inURL, resp.StatusCode)
		}
	}
	// The import route is a write, so it is checked with a body and by a method that
	// would reach the store rather than the method check.
	post, err := http.NewRequest(http.MethodPost,
		"http://"+f.server.Addr()+"/debug/pabt/v2/import", bytes.NewReader([]byte("not a bundle")))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	postResp, err := f.client.Do(post)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	postBody, _ := io.ReadAll(postResp.Body)
	_ = postResp.Body.Close()
	if postResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the import route without a capability = %d, want 401", postResp.StatusCode)
	}
	if bytes.Contains(postBody, []byte(capability)) {
		t.Error("the import route echoed the capability in its refusal")
	}
	// The ready file names the address and nothing else, because a file any process
	// can read is not a place for a token.
	ready, err := os.ReadFile(f.server.durable.readyFile)
	if err != nil {
		t.Fatalf("read the ready file: %v", err)
	}
	if bytes.Contains(ready, []byte(capability)) {
		t.Error("the ready file contains the capability")
	}
	if !strings.Contains(string(ready), f.server.Addr()) {
		t.Errorf("the ready file says %q, want the bound address %q", ready, f.server.Addr())
	}
	// With the token, the same route answers.
	resp, body := f.get(t, "/debug/pabt/v2/state")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state = %d: %s", resp.StatusCode, body)
	}
	var state StateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("decode the state: %v", err)
	}
	if state.Run != f.key {
		t.Errorf("the state names run %v, want %v", state.Run, f.key)
	}
	if state.Head.HeadSequence == 0 {
		t.Error("the state reports no head, so a client could not bootstrap")
	}
	if state.Generation == 0 || state.Recovery == "" {
		t.Errorf("the state reports generation %d recovery %q", state.Generation, state.Recovery)
	}
}

func TestTheServerBindsLoopbackOnly(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, store)
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.5:0", "example.com:0"} {
		if _, err := NewDurableServer(store, DurableServerConfig{Addr: addr}); err == nil {
			_ = store.Close()
			t.Errorf("the server bound %q, want a refusal", addr)
		}
	}
	// A loopback port of zero is asked for and answered with, so a caller never has to
	// guess which port it got.
	server, err := NewDurableServer(store, DurableServerConfig{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewDurableServer: %v", err)
	}
	defer func() { _ = server.Close() }()
	if !strings.HasPrefix(server.Addr(), "127.0.0.1:") {
		t.Errorf("the server bound %q, want a loopback address", server.Addr())
	}
	if server.Addr() == "127.0.0.1:0" {
		t.Error("the server reports the address it was asked for rather than the one it bound")
	}
}

func TestAResumeIsExactAcrossARestart(t *testing.T) {
	f := newDurableFixture(t, 3)
	f.waitReady(t)
	// A client reads two records and takes a cursor for where it is.
	resp, body := f.get(t, "/debug/pabt/v2/records?after=0")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("records = %d: %s", resp.StatusCode, body)
	}
	var page RecordPageResponse
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Page.Records) != 3 {
		t.Fatalf("the page carried %d records, want 3", len(page.Page.Records))
	}
	through := page.Page.Records[1].Sequence
	token, err := Cursor{
		StoreID: f.store.StoreIdentity(), RunKey: f.key, Generation: page.Cursor.Generation,
		Sequence: through,
	}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The server goes away entirely, and a new one is built over the same store. The
	// resume has to work anyway, because the position and the history are both durable
	// and the transport held neither.
	if err := f.server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := NewDurableServer(f.store, DurableServerConfig{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() { _ = restarted.Close() }()
	go func() { _ = restarted.Start() }()
	f.server = restarted

	installation, err := f.store.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if page.Cursor.Generation != installation.Generation {
		t.Fatalf("the cursor carries generation %d, the store is at %d",
			page.Cursor.Generation, installation.Generation)
	}
	restored, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if err := restored.CheckRefuses(installation.StoreID, f.key, installation.Generation); err != nil {
		t.Fatalf("a cursor from this store was refused after a restart: %v", err)
	}
	// The new server serves the rest over its own stream, which is the whole claim: the
	// resume is a read of the store, not a thing the old process remembered.
	f.waitReady(t)
	streaming := &http.Client{}
	resumeReq, err := http.NewRequest(http.MethodGet,
		"http://"+f.server.Addr()+"/debug/pabt/v2/stream?cursor="+token, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resumeReq.Header.Set(capabilityHeader, f.server.Capability())
	resumeResp, err := streaming.Do(resumeReq)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resumeResp.Body.Close() }()
	if resumeResp.StatusCode != http.StatusOK {
		t.Fatalf("the restarted server refused the old server's cursor: %d", resumeResp.StatusCode)
	}
	resumed := bufio.NewReader(resumeResp.Body)
	if _, err := readSSE(resumed); err != nil {
		t.Fatalf("read the greeting: %v", err)
	}
	event, err := readSSE(resumed)
	if err != nil {
		t.Fatalf("read the resumed record: %v", err)
	}
	var record StoredRecord
	if err := CanonicalDecode([]byte(event.data), &record); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if record.Sequence != through+1 {
		t.Errorf("the restarted server resumed at %d, want %d", record.Sequence, through+1)
	}
	if record.EventUID == "" || record.RecordSHA256 == "" {
		t.Error("the resumed record carries no identity or digest")
	}
}

func TestACursorFromAnotherHistoryIsRefused(t *testing.T) {
	f := newDurableFixture(t, 2)
	f.waitReady(t)
	installation, err := f.store.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	other := RunKey{SessionID: "another-session", PlanID: f.key.PlanID, RunID: "another-run"}
	for _, test := range []struct {
		name   string
		cursor Cursor
	}{
		{"another store", Cursor{
			StoreID: "store-elsewhere", RunKey: f.key,
			Generation: installation.Generation,
		}},
		{"another run", Cursor{
			StoreID: installation.StoreID, RunKey: other,
			Generation: installation.Generation,
		}},
		{"another generation", Cursor{
			StoreID: installation.StoreID, RunKey: f.key,
			Generation: installation.Generation + 1,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			token, err := test.cursor.Encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			resp, body := f.get(t, "/debug/pabt/v2/stream?cursor="+token)
			if resp.StatusCode == http.StatusOK {
				// The stream is a long response, so a refusal has to arrive as a status
				// rather than as an event inside a 200.
				t.Fatalf("a cursor from %s was served: %s", test.name, firstLine(body))
			}
			if resp.StatusCode != http.StatusConflict {
				t.Errorf("a cursor from %s = %d, want 409", test.name, resp.StatusCode)
			}
			if !strings.Contains(string(body), "another history") {
				t.Errorf("the refusal says %q, want it to name the stale cursor", firstLine(body))
			}
		})
	}
	// A token that is not one of ours is refused rather than interpreted.
	resp, _ := f.get(t, "/debug/pabt/v2/stream?cursor=not-a-cursor")
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("a token that is not a cursor = %d, want 409", resp.StatusCode)
	}
}

func TestTheStreamResumesAtTheExactNextSequence(t *testing.T) {
	f := newDurableFixture(t, 3)
	f.waitReady(t)
	// The client reads the store up to sequence 2, then opens a stream from there. The
	// stream has to start at 3, not at 1 and not at 4.
	token, err := Cursor{
		StoreID: f.store.StoreIdentity(), RunKey: f.key, Generation: f.generation(t),
		Sequence: 2,
	}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet,
		"http://"+f.server.Addr()+"/debug/pabt/v2/stream?cursor="+token, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set(capabilityHeader, f.server.Capability())
	// A stream is a response that does not end, so the client that reads it cannot
	// carry a request timeout; the test closes the body when it has what it needs.
	streaming := &http.Client{}
	resp, err := streaming.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream = %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	if greeting, err := readSSE(reader); err != nil {
		t.Fatalf("read the greeting: %v", err)
	} else if greeting.name != "comment" {
		t.Fatalf("the stream opened with %q, want a comment", greeting.name)
	}
	first, err := readSSE(reader)
	if err != nil {
		t.Fatalf("read the first event: %v", err)
	}
	if first.name != "record" {
		t.Fatalf("the first event is %q, want a record", first.name)
	}
	var record StoredRecord
	if err := CanonicalDecode([]byte(first.data), &record); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if record.Sequence != 3 {
		t.Errorf("the stream resumed at %d, want 3", record.Sequence)
	}
	// A record committed while the stream is open arrives on it, with no duplicate of
	// the one the store already gave.
	f.commit(t, 4)
	second, err := readSSE(reader)
	if err != nil {
		t.Fatalf("read the second event: %v", err)
	}
	if err := CanonicalDecode([]byte(second.data), &record); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if record.Sequence != 4 {
		t.Errorf("the live half delivered %d, want 4", record.Sequence)
	}
}

func (f *durableFixture) generation(t *testing.T) uint64 {
	t.Helper()
	installation, err := f.store.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	return installation.Generation
}

// sseEvent is one decoded server-sent event.
type sseEvent struct {
	id   string
	name string
	data string
}

// readSSE reads one event. A comment is returned as a comment, because a stream opens
// with one and a test that skipped it would wait for an event that had already gone.
func readSSE(reader *bufio.Reader) (sseEvent, error) {
	var out sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return out, err
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, ":"):
			return sseEvent{name: "comment", data: strings.TrimSpace(line[1:])}, nil
		case strings.HasPrefix(line, "id: "):
			out.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			out.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			out.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if out.name != "" {
				return out, nil
			}
		}
	}
}

func firstLine(body []byte) string {
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		return string(body[:i])
	}
	return string(body)
}

func TestAnImportPublishesAndARefusedOneDoesNot(t *testing.T) {
	f := newDurableFixture(t, 3)
	f.waitReady(t)
	before := f.generation(t)
	beforeSession := f.key.SessionID

	// A bundle the fixture already holds, exported over HTTP and posted back.
	resp, bundle := f.get(t, "/debug/pabt/v2/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export = %d: %s", resp.StatusCode, firstLine(bundle))
	}
	if got := resp.Header.Get("Content-Type"); got != bundleMediaType {
		t.Errorf("the export is %q, want %q", got, bundleMediaType)
	}
	if resp.Header.Get("X-PABT-Bundle-SHA256") == "" {
		t.Error("the export reported no bundle digest")
	}
	imported := postBundle(t, f, bundle)
	if imported.Activation.Generation != before+1 {
		t.Errorf("the import published generation %d, want %d",
			imported.Activation.Generation, before+1)
	}
	if imported.Activation.SessionID == beforeSession {
		t.Error("the import published the session it came from")
	}
	if imported.Activation.RecordCount == 0 {
		t.Error("the import stored no records")
	}
	// The served state is the new history, not the old one with a new label on it.
	resp, body := f.get(t, "/debug/pabt/v2/state")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state = %d: %s", resp.StatusCode, body)
	}
	var state StateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.Generation != before+1 {
		t.Errorf("the server serves generation %d, want %d", state.Generation, before+1)
	}
	if state.ActiveSession != imported.Activation.SessionID {
		t.Errorf("the server serves session %q, want the imported %q",
			state.ActiveSession, imported.Activation.SessionID)
	}
	if state.Head.HeadSequence != imported.Activation.RecordCount {
		t.Errorf("the imported head is at %d, want %d",
			state.Head.HeadSequence, imported.Activation.RecordCount)
	}

	// A bundle that is not one is refused, and the store keeps serving what it was
	// serving at the generation it was serving it at.
	damaged := append([]byte(nil), bundle...)
	damaged[len(damaged)/2] ^= 0xff
	code, body := postBundleRaw(t, f, damaged)
	if code == http.StatusOK {
		t.Fatalf("a damaged bundle was imported: %s", firstLine(body))
	}
	after := f.generation(t)
	if after != before+1 {
		t.Errorf("a refused import moved the generation to %d, want %d", after, before+1)
	}
	installation, err := f.store.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.ActiveSessionID != imported.Activation.SessionID {
		t.Errorf("a refused import changed the served session to %q", installation.ActiveSessionID)
	}
}

func postBundle(t *testing.T, f *durableFixture, bundle []byte) ImportResponse {
	t.Helper()
	code, body := postBundleRaw(t, f, bundle)
	if code != http.StatusOK {
		t.Fatalf("import = %d: %s", code, firstLine(body))
	}
	var out ImportResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func postBundleRaw(t *testing.T, f *durableFixture, bundle []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		"http://"+f.server.Addr()+"/debug/pabt/v2/import", bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set(capabilityHeader, f.server.Capability())
	req.Header.Set("Content-Type", bundleMediaType)
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, body
}

func TestAKeyframeIsNamedRatherThanResentAndIsRetrievable(t *testing.T) {
	f := newDurableFixture(t, 4)
	f.waitReady(t)
	keyframes := 0
	// What holds for every sequence: a keyframe is the whole tree and names nothing,
	// and a delta names the base it applies to, retrievable by sequence, and applies.
	for sequence := uint64(1); sequence <= 4; sequence++ {
		resp, body := f.get(t, fmt.Sprintf("/debug/pabt/v2/representations/%d", sequence))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("representation %d = %d: %s", sequence, resp.StatusCode, body)
		}
		var one RepresentationResponse
		if err := json.Unmarshal(body, &one); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if one.Tree.EventUID == "" || one.Tree.Digest == "" {
			t.Fatalf("the representation at %d carries no tree", sequence)
		}
		if one.Delta == nil {
			keyframes++
			if one.Base != nil {
				t.Errorf("the keyframe at %d names a base at %d", sequence, one.Base.Sequence)
			}
			continue
		}
		if one.Base == nil {
			t.Fatalf("the delta at %d names no base", sequence)
		}
		if one.Base.Digest != one.Delta.BaseDigest {
			t.Errorf("the base reference digest %q is not the delta's %q",
				one.Base.Digest, one.Delta.BaseDigest)
		}
		resp, body = f.get(t, fmt.Sprintf("/debug/pabt/v2/trees/%d", one.Base.Sequence))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the base tree = %d: %s", resp.StatusCode, body)
		}
		var base StoredTree
		if err := CanonicalDecode(bytes.TrimSpace(body), &base); err != nil {
			t.Fatalf("decode the base: %v", err)
		}
		if base.Digest != one.Base.Digest {
			t.Errorf("the fetched base is %q, the reference named %q", base.Digest, one.Base.Digest)
		}
		// The delta, applied to the tree the route served, reproduces the tree the
		// same route served. That is the whole point of serving the two together: a
		// client can check it rather than trust it.
		applied, err := ApplyExactDelta(base, *one.Delta)
		if err != nil {
			t.Fatalf("the delta at %d does not apply to the base the route served: %v", sequence, err)
		}
		if applied.Digest != one.Tree.Digest {
			t.Errorf("the delta at %d reconstructs to %q, the tree is %q",
				sequence, applied.Digest, one.Tree.Digest)
		}
	}
	if keyframes == 0 {
		t.Error("no sequence was a keyframe, so the resume path is untested")
	}
}

func TestNoRouteReachesALivePlan(t *testing.T) {
	f := newDurableFixture(t, 2)
	f.waitReady(t)
	// The durable server has no tracker at all, so the v1 routes, which do read a
	// live plan, have nothing to read. That is the boundary stated rather than assumed.
	if len(f.server.Trackers()) != 0 {
		t.Errorf("the durable server holds %d trackers", len(f.server.Trackers()))
	}
	if _, found := f.server.Tracker("0"); found {
		t.Error("the durable server resolved a tracker")
	}
	for _, path := range []string{"/debug/pabt/plans", "/debug/pabt/timeline", "/debug/pabt/dot"} {
		req, err := http.NewRequest(http.MethodGet, "http://"+f.server.Addr()+path, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set(capabilityHeader, f.server.Capability())
		resp, err := f.client.Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404: a durable server has no live plan", path, resp.StatusCode)
		}
		if bytes.Contains(body, []byte("nodeType")) {
			t.Errorf("%s returned tree content from a durable server", path)
		}
	}
}

func TestAStreamEndsWithAResumablePositionWhenAClientFallsBehind(t *testing.T) {
	// A byte bound no record fits inside, so the first commit puts the client behind
	// however fast it reads. That is the deterministic form of the case: a client whose
	// queue cannot hold what is committed is told where to resume, and its connection
	// ends, rather than being served a stream that arrives later and later.
	f := newDurableFixtureWith(t, 0, DurableHubLimits{
		MaxQueuedEvents: 8, MaxQueuedBytes: 1, KeepAlive: 15 * time.Second,
	})
	f.waitReady(t)
	streaming := &http.Client{}
	req, err := http.NewRequest(http.MethodGet,
		"http://"+f.server.Addr()+"/debug/pabt/v2/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set(capabilityHeader, f.server.Capability())
	resp, err := streaming.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	reader := bufio.NewReader(resp.Body)
	if _, err := readSSE(reader); err != nil {
		t.Fatalf("read the greeting: %v", err)
	}
	f.commit(t, 2)
	event, err := readSSE(reader)
	if err != nil {
		t.Fatalf("read the event: %v", err)
	}
	if event.name != "behind" {
		t.Fatalf("the stream ended with %q, want it to say the client fell behind", event.name)
	}
	var resume CursorResponse
	if err := json.Unmarshal([]byte(event.data), &resume); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cursor, err := DecodeCursor(resume.Token)
	if err != nil {
		t.Fatalf("the resume cursor does not decode: %v", err)
	}
	if err := cursor.CheckRefuses(f.store.StoreIdentity(), f.key, f.generation(t)); err != nil {
		t.Errorf("the resume cursor is not usable against this history: %v", err)
	}
	// And it is usable: the store serves everything after the position it names, so
	// nothing was lost by ending the connection.
	page, err := f.store.RecordsSince(context.Background(), f.key, cursor.Sequence)
	if err != nil {
		t.Fatalf("RecordsSince: %v", err)
	}
	if len(page.Records) != 2 {
		t.Errorf("the resume returned %d records, want the 2 after %d",
			len(page.Records), cursor.Sequence)
	}
	installation, err := f.store.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if f.server.durable.hubOf(context.Background(), f.key, installation.Generation).Overflowed() != 1 {
		t.Error("the hub did not count the disconnects, so an operator would not learn the bound is too small")
	}
}

func TestAnOutOfBandImportStopsTheStreamAnnouncingTheOldRun(t *testing.T) {
	f := newDurableFixture(t, 3)
	f.waitReady(t)
	// A bundle is imported by calling the store directly, the way an application that
	// never went through the route would. The server hears nothing, so the only thing
	// that can stop its stream from announcing the old run is revalidating the hub on
	// use rather than caching one forever.
	resp, bundle := f.get(t, "/debug/pabt/v2/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export = %d: %s", resp.StatusCode, firstLine(bundle))
	}
	ticket, err := f.store.StageImport(context.Background(), bytes.NewReader(bundle), DefaultImportLimits())
	if err != nil {
		t.Fatalf("StageImport: %v", err)
	}
	activated, err := f.store.ActivateImport(context.Background(), ticket)
	if err != nil {
		t.Fatalf("ActivateImport: %v", err)
	}
	imported := RunKey{
		SessionID: activated.SessionID, PlanID: activated.PlanID, RunID: activated.RunID,
	}
	streaming := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, "http://"+f.server.Addr()+"/debug/pabt/v2/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set(capabilityHeader, f.server.Capability())
	streamResp, err := streaming.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = streamResp.Body.Close() }()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream = %d", streamResp.StatusCode)
	}
	reader := bufio.NewReader(streamResp.Body)
	if _, err := readSSE(reader); err != nil {
		t.Fatalf("read the greeting: %v", err)
	}
	// The stream replayed the new run's records, and a record from the old run is
	// never delivered on it, however many the old session commits.
	f.commit(t, 6)
	seen := []string{}
	for i := 0; i < 3; i++ {
		event, err := readSSE(reader)
		if err != nil {
			break
		}
		if event.name != "record" {
			continue
		}
		var record StoredRecord
		if err := CanonicalDecode([]byte(event.data), &record); err != nil {
			t.Fatalf("decode: %v", err)
		}
		seen = append(seen, record.RunKey.RunID)
	}
	for _, runID := range seen {
		if runID == f.key.RunID {
			t.Fatalf("the stream on the imported run delivered a record from %q", runID)
		}
	}
	// And the hub it is reading is the one for the run the store serves.
	hub := f.server.durable.hubOf(context.Background(), imported, activated.Generation)
	if hub == nil {
		t.Fatal("the server has no hub for the imported run")
	}
	if hub.RunKey() != imported {
		t.Errorf("the hub names run %v, want %v", hub.RunKey(), imported)
	}
	if hub.Generation() != activated.Generation {
		t.Errorf("the hub names generation %d, want %d", hub.Generation(), activated.Generation)
	}
}

func TestARepresentationThatDisagreesWithTheChainIsRefused(t *testing.T) {
	f := newDurableFixture(t, 3)
	f.waitReady(t)
	// The representation is rewritten with a renamed node and its own digest
	// recomputed, so the row is internally consistent and only the record the chain
	// covers says otherwise. A route that served it would hand a client a tree the
	// history never committed, which is the one thing a representation is not.
	original, err := f.store.TreeAt(context.Background(), f.key, 1)
	if err != nil {
		t.Fatalf("TreeAt: %v", err)
	}
	altered := original
	altered.Nodes = append([]StoredNode(nil), original.Nodes...)
	altered.Nodes[0].Name = "PlantedRoot"
	altered.Digest = ""
	digest, err := HashTree(altered)
	if err != nil {
		t.Fatalf("re-seal: %v", err)
	}
	altered.Digest = digest
	encoded, err := CanonicalEncode(altered)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := f.store.writer.ExecContext(context.Background(),
		"UPDATE representations SET tree_json = ?, tree_sha256 = ? "+
			"WHERE session_id = ? AND run_id = ? AND seq = 1",
		encoded, mustHex(digest), f.key.SessionID, f.key.RunID); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	resp, body := f.get(t, "/debug/pabt/v2/representations/1")
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a representation that disagrees with the record was served: %s", firstLine(body))
	}
	if !strings.Contains(string(body), "corrupt") {
		t.Errorf("the refusal says %q, want it to name the corruption", firstLine(body))
	}
	// The tree route refuses it too, and says the same thing: one rule, not two.
	resp, body = f.get(t, "/debug/pabt/v2/trees/1")
	if resp.StatusCode == http.StatusOK {
		t.Errorf("the tampered tree was served: %s", firstLine(body))
	}
}

func TestTheServerReleasesItsPortWhetherOrNotItWasStarted(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, store)
	server, err := NewDurableServer(store, DurableServerConfig{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewDurableServer: %v", err)
	}
	addr := server.Addr()
	// Closed without ever having been started, which is what a construct-then-abandon
	// path does, and what would hold the port for the life of the process.
	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the port is still held after Close: %v", err)
	}
	_ = listener.Close()
	// Two servers over one store both bind, and both release.
	first, err := NewDurableServer(store, DurableServerConfig{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewDurableServer: %v", err)
	}
	second, err := NewDurableServer(store, DurableServerConfig{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewDurableServer: %v", err)
	}
	if first.Addr() == second.Addr() {
		t.Error("two servers bound the same address")
	}
	for _, s := range []*Server{first, second} {
		addr := s.Addr()
		go func() { _ = s.Start() }()
		f := &durableFixture{server: s, client: &http.Client{Timeout: 5 * time.Second}}
		f.waitReady(t)
		// One start path: a second Start is refused rather than serving the same
		// listener twice, which would make Close report a closed connection.
		if err := s.Start(); err == nil {
			t.Error("a second Start was accepted")
		}
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			t.Errorf("the port is still held after Close: %v", err)
			continue
		}
		_ = listener.Close()
	}
}

func TestABadParameterOrAnUnreachableSequenceIsABadRequest(t *testing.T) {
	f := newDurableFixture(t, 2)
	f.waitReady(t)
	huge := strconv.FormatUint(math.MaxUint64, 10)
	for _, path := range []string{
		"/debug/pabt/v2/records/after=" + "abc",
		"/debug/pabt/v2/records?after=abc",
		"/debug/pabt/v2/records?after=" + huge,
		"/debug/pabt/v2/records/" + huge,
		"/debug/pabt/v2/trees/" + huge,
		"/debug/pabt/v2/representations/" + huge,
		"/debug/pabt/v2/records/0",
		"/debug/pabt/v2/records/-1",
	} {
		resp, body := f.get(t, path)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", path, resp.StatusCode, firstLine(body))
		}
		if bytes.Contains(body, []byte("sql:")) {
			t.Errorf("%s leaked a driver error: %s", path, firstLine(body))
		}
	}
	// A position the run has not reached is a conflict, not an empty page whose cursor
	// reads zero and would send a paging client back to the start.
	resp, body := f.get(t, "/debug/pabt/v2/records?after=9999")
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("a position past the head = %d, want 409: %s", resp.StatusCode, firstLine(body))
	}
	// A write method on a read route is refused, so every route answers the same way.
	for _, path := range []string{"/records/1", "/trees/1", "/representations/1"} {
		req, err := http.NewRequest(http.MethodPost, "http://"+f.server.Addr()+"/debug/pabt/v2"+path, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set(capabilityHeader, f.server.Capability())
		postResp, err := f.client.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		_ = postResp.Body.Close()
		if postResp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, postResp.StatusCode)
		}
	}
	// An untrusted cursor field is not reflected back into the refusal.
	hostile := Cursor{StoreID: `PABTPROBE"><script>`, RunKey: f.key, Generation: 1}
	token, err := hostile.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, body = f.get(t, "/debug/pabt/v2/stream?cursor="+token)
	if bytes.Contains(body, []byte("script")) {
		t.Errorf("the refusal quotes the cursor it was sent: %s", firstLine(body))
	}
}

func TestACustomRouteUnderTheDurableSetIsStillBehindTheCapability(t *testing.T) {
	f := newDurableFixture(t, 1)
	f.waitReady(t)
	// Registering a handler is an in-process mistake waiting to happen: on the outer
	// mux a named pattern wins over the subtree guard, so the route would answer
	// without the capability. The registration goes on the guarded set instead.
	f.server.HandleFunc("/debug/pabt/v2/custom", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"secret":"planted"}`))
	})
	req, err := http.NewRequest(http.MethodGet,
		"http://"+f.server.Addr()+"/debug/pabt/v2/custom", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a custom route answered without the capability: %d %s", resp.StatusCode, body)
	}
}
