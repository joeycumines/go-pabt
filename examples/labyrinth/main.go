//go:build example

package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/binary"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joeycumines/go-pabt/examples/labyrinth/loop"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

//go:embed client
var clientFS embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "loopback listen address for the game page")
	seed := flag.Uint64("seed", 0, "maze seed; 0 picks one at random")
	flag.Parse()

	if *seed == 0 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			log.Fatalf("labyrinth: seed: %v", err)
		}
		*seed = binary.LittleEndian.Uint64(b[:])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build the world and authoritative loop.
	m := maze.Generate(*seed, 21, 21)
	w := world.New(m)
	gameLoop := loop.New(w)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("labyrinth: listen %s: %v", *addr, err)
	}

	client, err := fs.Sub(clientFS, "client")
	if err != nil {
		log.Fatalf("labyrinth: client embed: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(client)))

	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("labyrinth: serve: %v", err)
		}
	}()

	// Start the authoritative loop.
	go gameLoop.Run(ctx)

	log.Printf("GAME  http://%s/  seed %d", ln.Addr(), *seed)

	<-ctx.Done()

	// Shutdown the HTTP server.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("labyrinth: shutdown: %v", err)
	}

	log.Printf("labyrinth: stopped")
}
