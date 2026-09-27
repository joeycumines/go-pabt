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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// namespace is the whole HTTP surface of the debugger, and routePrefix is the
// subtree under it that the capability guard owns. The browser client's shell is
// the single exception and is registered as an exact path beside the guard, so
// "every route requires the capability" stays true as a property rather than as a
// convention.
const (
	namespace   = "/debug/pabt"
	routePrefix = "/debug/pabt/"
)

// Server serves a plan's recorded history over HTTP and to a browser.
//
// It owns exactly one thing: the lifecycle of an http.Server and the mux the
// routes live on. What those routes answer comes from the durable state, and no
// route on this server reads a live plan. A live tree read while a tick runs
// races with the writes expansion performs, and the history is the whole point —
// everything a route serves is a record the store committed.
type Server struct {
	addr     string
	server   *http.Server
	mu       sync.RWMutex
	listener net.Listener

	// guarded is the route set's own mux, mounted behind the capability check. It
	// is separate from the server's handler so that a named route cannot be
	// reached without passing the check.
	guarded *http.ServeMux

	// durable is the state the routes need. It is never nil on a server this
	// package builds, which is why Close and the capability check can rely on it.
	durable *durable

	// started guards the one start path.
	started bool
}

// Start serves on the listener the constructor bound, so the address a caller was
// told is the one being served. A second Start is refused rather than serving the
// same listener twice, which would make Close report a closed connection.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf(`pabtdebug: the server is already serving`)
	}
	s.started = true
	s.mu.Unlock()
	return s.server.Serve(s.listener)
}

// Addr returns the address the server is listening on, which is the one its
// constructor bound rather than the one it was asked for.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Close shuts the server down, releasing the port.
//
// http.Server only closes the listeners it was handed by Serve, so a server that
// was built and abandoned without ever being started would keep its port for the
// life of the process. Closing an already-closed listener is harmless.
func (s *Server) Close() error {
	s.durable.close()
	err := s.server.Close()
	_ = s.listener.Close()
	return err
}

// HandleFunc registers a handler on the guarded route set.
//
// Registering on the outer mux would be an unauthenticated route waiting to
// happen: a named pattern wins over a subtree guard, so the route would answer
// without ever reaching the check.
//
// The decision cannot be made on the pattern as written, because the mux does not
// read it as written. net/http percent-decodes both a registered literal segment
// and a request path before matching them, so "/%64ebug/pabt/custom" is a route in
// the namespace while containing no literal "/debug/pabt/" to find. A prefix test
// on the raw string would send it to the outer mux, where it is more specific
// than the guard and answers without the capability. So the pattern is decoded
// first and the test is on what the mux will match.
//
// A path that cannot be decoded is treated as inside the namespace. The mux
// rejects such a pattern at registration anyway, so the case never reaches a
// decision, and failing toward the guarded mux is the direction that cannot open
// a hole.
func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
	mux, ok := s.server.Handler.(*http.ServeMux)
	if !ok {
		return
	}
	// A host-qualified pattern is "example.com/path", so the path is taken from the
	// first slash. Testing the whole string would let a caller register the
	// namespace under another host and decide on a part of the pattern that is not
	// a path at all.
	path := pattern
	if slash := strings.IndexByte(path, '/'); slash > 0 {
		path = path[slash:]
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		decoded = path
	}
	switch {
	case strings.HasPrefix(decoded, routePrefix):
		if decoded == namespace+"/ui" || strings.HasPrefix(decoded, routePrefix+"ui/") {
			// The shell owns this prefix on the outer mux, so a handler registered
			// here could never be reached - with or without the token. A registration
			// that silently does nothing is worse than one that refuses.
			panic(fmt.Sprintf(
				"pabtdebug: %q is served by the client shell and cannot be registered here", pattern))
		}
		// Inside the subtree the guarded mux owns it, so the check cannot be
		// bypassed.
		s.guarded.HandleFunc(pattern, handler)
	case strings.HasPrefix(decoded, namespace):
		// The bare prefix and anything beside it are exact paths on the outer mux,
		// which the subtree guard does not cover. They are registered wrapped in the
		// check rather than left to a caller's judgement about which side of the
		// namespace a string is on.
		mux.Handle(pattern, s.requireCapabilityHandler(handler))
	default:
		mux.HandleFunc(pattern, handler)
	}
}
