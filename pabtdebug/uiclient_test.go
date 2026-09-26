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
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"testing"
)

// TestTheClientShellDisclosesNothing pins the one place the capability boundary
// is deliberately open, and states exactly what that costs: the document is
// reachable by anything that can reach the port, and it must therefore contain
// no capability, no record, and no digest.
func TestTheClientShellDisclosesNothing(t *testing.T) {
	f := newDurableFixture(t, 2)
	f.waitReady(t)

	resp, body := f.getUnauthenticated(t, uiPrefix)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the shell answered %d, want 200: %s", resp.StatusCode, body)
	}
	capability := f.server.Capability()
	if bytes.Contains(body, []byte(capability)) {
		t.Error("the shell contains the capability")
	}
	// The identities and digests the routes do serve must not be inlined either:
	// a shell that carried them would be a second copy of the history, and one
	// the store cannot revoke. The fixture's plan and run ids are the single
	// letters "p" and "r", which are not distinctive enough to assert on: a
	// document that does not contain the letter p is not a property.
	head, err := f.store.Head(context.Background(), f.key)
	if err != nil {
		t.Fatalf("read the head: %v", err)
	}
	for _, secret := range []string{
		f.key.SessionID,
		string(f.store.storeID),
		head.HeadTreeDigest,
		head.HeadRecordSHA256,
		head.HeadTreeEventUID,
	} {
		if secret == "" {
			continue
		}
		if bytes.Contains(body, []byte(secret)) {
			t.Errorf("the shell contains %q", secret)
		}
	}
	// The token the client sends is named in prose, which is not the same as
	// carrying it. The header name must be present or the page cannot work.
	if !bytes.Contains(body, []byte(capabilityHeader)) {
		t.Errorf("the shell never names the %s header, so the page cannot authenticate", capabilityHeader)
	}
}

// TestTheDurableSubtreeIsEntirelyBehindTheCapability is the reason the shell is
// served outside it.
//
// A path that matches no route still answers 401 rather than 404, which is only
// true when the guard is in front of the whole subtree rather than inside it. If
// that ever stops being so, the shell would be the least of the problems.
func TestTheDurableSubtreeIsEntirelyBehindTheCapability(t *testing.T) {
	f := newDurableFixture(t, 1)
	f.waitReady(t)
	for _, p := range []string{
		"/debug/pabt/v2/",
		"/debug/pabt/v2/nothing-is-registered-here",
		"/debug/pabt/v2/records/not-a-number",
	} {
		resp, _ := f.getUnauthenticated(t, p)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a capability = %d, want 401", p, resp.StatusCode)
		}
	}
	// A traversal is cleaned by the mux before the guard is consulted, so it
	// never reaches a handler at all. What matters is only that it serves
	// nothing: a 404 from path cleaning is as good as a 401 for this purpose, and
	// a 200 would not be.
	resp, body := f.getUnauthenticated(t, "/debug/pabt/v2/../../etc/passwd")
	if resp.StatusCode == http.StatusOK {
		t.Errorf("a traversal through the durable subtree served %d: %s", resp.StatusCode, body)
	}
	// The shell is the one documented exception, and it is outside the subtree.
	shell, _ := f.getUnauthenticated(t, uiPrefix)
	if shell.StatusCode != http.StatusOK {
		t.Errorf("the shell = %d, want 200: it is the one route outside the subtree", shell.StatusCode)
	}
}

// TestEveryClientAssetIsServed checks the map against the embed, in both
// directions, so a file can neither ship unserved nor be served unshipped.
func TestEveryClientAssetIsServed(t *testing.T) {
	f := newDurableFixture(t, 1)
	f.waitReady(t)
	for name, mediaType := range clientAssets {
		resp, body := f.getUnauthenticated(t, uiPrefix+"/"+name)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200", name, resp.StatusCode)
			continue
		}
		if got := resp.Header.Get("Content-Type"); got != mediaType {
			t.Errorf("%s content type = %q, want %q", name, got, mediaType)
		}
		// The shell must never be cached: a cached client is a client talking to
		// a history whose shapes it does not know, and a runner cannot tell a
		// disk copy from the served one.
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s cache control = %q, want no-store", name, got)
		}
		if len(body) == 0 {
			t.Errorf("%s served an empty body", name)
		}
	}
}

// TestAClientAssetThatIsNotListedIsNotServed refuses a name the map does not
// carry. The lookup is a map rather than a path join, so a traversal cannot be
// spelled, and this test says so rather than leaving it to inspection.
func TestAClientAssetThatIsNotListedIsNotServed(t *testing.T) {
	f := newDurableFixture(t, 1)
	f.waitReady(t)
	for _, name := range []string{
		"nope.js",
		"js/nope.mjs",
		"../client/js/api.mjs",
		"..%2fclient%2fjs%2fapi.mjs",
		"js/../../record.go",
		"index.html.bak",
	} {
		resp, _ := f.getUnauthenticated(t, uiPrefix+"/"+name)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", name, resp.StatusCode)
		}
	}
}

// TestTheClientEmbedAndItsRouteTableAgree walks the embedded tree. A file that
// is embedded but unroutable is dead weight in the binary, and a route naming a
// file that is not embedded is a 500 waiting for the first request.
func TestTheClientEmbedAndItsRouteTableAgree(t *testing.T) {
	err := fs.WalkDir(clientFS, "client", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.TrimPrefix(p, "client/")
		if _, ok := clientAssets[name]; !ok {
			t.Errorf("%s is embedded but no route serves it", name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the embedded client: %v", err)
	}
	for name := range clientAssets {
		if _, err := clientFS.ReadFile(path.Join("client", name)); err != nil {
			t.Errorf("%s is routable but not embedded: %v", name, err)
		}
	}
}

// TestTheShellRefusesAWrite keeps the client routes read-only, because a browser
// has no reason to POST to a document and one that accepted it would be a second
// write surface with no capability check on it.
func TestTheShellRefusesAWrite(t *testing.T) {
	f := newDurableFixture(t, 1)
	f.waitReady(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, err := http.NewRequest(method, "http://"+f.server.Addr()+uiPrefix, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp, err := f.client.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s the shell = %d, want 405", method, resp.StatusCode)
		}
	}
}

// getUnauthenticated issues a request with no capability, which is how a browser
// loads the shell and how a test proves the shell needs none.
func (f *durableFixture) getUnauthenticated(t *testing.T, p string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+f.server.Addr()+p, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("do %s: %v", p, err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	_ = resp.Body.Close()
	return resp, body
}
