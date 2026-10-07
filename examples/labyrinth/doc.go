//go:build example

// Command labyrinth is ARIADNE: a networked first-person maze run in which
// PA-BT planners visibly drive every hunter, and the browser page renders the
// hunters' live plan trees while the game is being played.
//
// Task T1 ships the server shell only: it binds loopback, serves the embedded
// client, prints the URL and maze seed before serving, and stops cleanly on
// SIGINT. The authoritative loop, brains, transport, and renderer land in the
// later blueprint tasks.
package main
