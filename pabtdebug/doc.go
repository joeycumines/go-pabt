// Package pabtdebug records a PA-BT run and serves the recorded history.
//
// A captured plan (see pabt.NewCaptured) hands each tick to a Session, which
// orders intent, execution, bundle, and commit into a SQLite store. Every tick
// becomes a record with a gap-free chain of digests behind it, so the history is
// reconstructable and survives the process.
//
// NewDurableServer serves that history over HTTP, and with it a browser client
// that reads committed records and nothing else. No route on it reads a live
// plan: a live read while a tick runs races with the writes expansion performs,
// and a debugger that races the thing it is inspecting reports a tree that never
// existed.
//
// A run can be exported as a self-authenticating bundle and imported into
// another store as an independently rewritten session, so a history can be moved
// without either end being able to forge the other's.
package pabtdebug
