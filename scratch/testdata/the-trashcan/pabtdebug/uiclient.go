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
	"embed"
	"net/http"
	"strings"
)

// clientFS carries the browser client.
//
//go:embed client
var clientFS embed.FS

// uiPrefix is where the client is served. It sits outside the guarded subtree on
// purpose, and that is the whole reason for the choice.
//
// Everything under routePrefix is behind the capability check, and it must stay
// wholly behind it: a route that answers there without the check is the failure
// the capability test exists to catch. The shell is not a data route. It is a
// document that contains no record, no digest, and no capability, and every
// request it makes carries the token in a header. Serving it from outside the
// subtree therefore discloses nothing while keeping "every route requires the
// capability" true as a property a test can check rather than an intention.
//
// The consequence is that the shell is reachable by anything that can reach the
// port, and the token is what keeps the history private. That is the boundary
// unchanged: the loopback listener is the first boundary and the token is the
// second, and the shell does not weaken the second.
const uiPrefix = "/debug/pabt/ui"

// clientAssets is the exact set of files the client is made of, with the media
// type each one is served as.
//
// The names are looked up in this map rather than joined onto the embedded
// directory, so a request cannot name a file that is not listed and the
// directory layout is not a route table. The embedded tree holds nothing else,
// so the map is the whole of what the server will hand to a browser.
var clientAssets = map[string]string{
	"index.html":       "text/html; charset=utf-8",
	"app.css":          "text/css; charset=utf-8",
	"js/app.mjs":       "text/javascript; charset=utf-8",
	"js/lossless.mjs":  "text/javascript; charset=utf-8",
	"js/api.mjs":       "text/javascript; charset=utf-8",
	"js/stream.mjs":    "text/javascript; charset=utf-8",
	"js/delta.mjs":     "text/javascript; charset=utf-8",
	"js/model.mjs":     "text/javascript; charset=utf-8",
	"js/render.mjs":    "text/javascript; charset=utf-8",
	"js/detail.mjs":    "text/javascript; charset=utf-8",
	"js/dom.mjs":       "text/javascript; charset=utf-8",
	"js/causality.mjs": "text/javascript; charset=utf-8",
}

// clientUI registers the browser client on the server's own mux.
//
// It is called only from NewDurableServer, which is the only constructor there
// is: the client reads committed history, so there is nothing for it to be
// served without a store behind it.
func (d *durable) clientUI(mux *http.ServeMux) {
	mux.HandleFunc(uiPrefix, d.handleClientShell)
	mux.HandleFunc(uiPrefix+"/", d.handleClientAsset)
}

// handleClientShell serves the document the client boots from.
func (d *durable) handleClientShell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// With and without the trailing slash are the same document. A relative asset
	// reference would resolve differently under each, so the document uses
	// absolute ones and this answers both spellings.
	if r.URL.Path != uiPrefix && r.URL.Path != uiPrefix+"/" {
		http.NotFound(w, r)
		return
	}
	d.writeClientAsset(w, r, "index.html")
}

// handleClientAsset serves one file of the client.
func (d *durable) handleClientAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, uiPrefix+"/")
	if name == "" || name == "index.html" {
		d.writeClientAsset(w, r, "index.html")
		return
	}
	if _, ok := clientAssets[name]; !ok {
		http.NotFound(w, r)
		return
	}
	d.writeClientAsset(w, r, name)
}

// writeClientAsset writes one embedded file.
func (d *durable) writeClientAsset(w http.ResponseWriter, r *http.Request, name string) {
	mediaType, ok := clientAssets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := clientFS.ReadFile("client/" + name)
	if err != nil {
		// The map names a file the embed does not carry, which is a build error
		// rather than a bad request.
		http.Error(w, "the client is not built correctly", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	// The client is versioned by the binary that serves it, so a cached document
	// is a client talking to a history whose shapes it does not know. It is also
	// what makes "the served client is the current one" checkable: a runner that
	// sees a 200 has seen these bytes and not a disk copy.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}
