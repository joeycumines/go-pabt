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
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	plan := newSessionFixture(t)
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	server, err := NewDurableServer(store, DurableServerConfig{
		ReadyFile: filepath.Join(t.TempDir(), "ready"),
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
	// The same session the server watches, so a record committed here is announced to
	// the stream rather than only reaching it through a replay.
	hub := f.server.durable.hubOf(ctx)
	if hub == nil {
		t.Fatalf("the server has no hub: %v", f.server.durable.activeRunErr(ctx))
	}
	session, err := OpenSession(ctx, f.store, f.plan.plan, SessionConfig{
		Identity: SessionIdentity{
			SessionID: f.key.SessionID, PlanID: f.key.PlanID, RunID: f.key.RunID,
		},
		Announce: hub.Publish,
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
	// The store serves the rest, which is the whole claim: the resume is a read, not a
	// thing the old process remembered.
	final, err := f.store.RecordsSince(context.Background(), f.key, restored.Sequence)
	if err != nil {
		t.Fatalf("RecordsSince: %v", err)
	}
	if len(final.Records) != 1 || final.Records[0].Sequence != through+1 {
		t.Fatalf("the resume returned %d records, want the one after %d",
			len(final.Records), through)
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

// readSSE reads one event, skipping comments.
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
			continue
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
