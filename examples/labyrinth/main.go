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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("labyrinth: serve: %v", err)
		}
	}()

	log.Printf("GAME  http://%s/  seed %d", ln.Addr(), *seed)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("labyrinth: shutdown: %v", err)
	}
	log.Printf("labyrinth: stopped")
}
