//go:build example

package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joeycumines/go-pabt/examples/labyrinth/game"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/net/proto"
	"github.com/joeycumines/go-pabt/examples/labyrinth/net/ws"
	"github.com/joeycumines/go-pabt/examples/labyrinth/netcode"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

var (
	sin = math.Sin
	cos = math.Cos
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

	// Build the maze, world, and game. NewGame registers the tick hook
	// (actor AI, stun timers, damage, win/lose, escalation) on the loop, so
	// all game logic runs on the single-writer goroutine.
	m := maze.Generate(*seed, 21, 21)
	g := game.NewGame(m, 8, *seed) // 8 initial actors: 2 per type

	// Start the authoritative loop.
	go g.Loop.Run(ctx)

	// Start the snapshot fan-out at 20Hz.
	encoder := proto.NewEncoder()
	go g.SnapshotFanOut(func(snap *world.Snapshot, ackSeq uint64) ([]byte, error) {
		return encoder.EncodeSnap(snapshotToProto(snap), ackSeq)
	})

	// HTTP server: static files + WebSocket upgrade.
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
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, g)
	})

	srv := &http.Server{Handler: mux}

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

// handleWebSocket upgrades the connection and manages the WebSocket session.
func handleWebSocket(w http.ResponseWriter, r *http.Request, g *game.Game) {
	// Hijack the HTTP connection to get the raw net.Conn for WebSocket.
	hj, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("ws: response writer does not support hijacking")
		return
	}
	rawConn, buf, err := hj.Hijack()
	if err != nil {
		log.Printf("ws: hijack failed: %v", err)
		return
	}

	// Perform the WebSocket upgrade using the already-parsed request.
	// The HTTP server has already consumed the request bytes, so we
	// validate the parsed request rather than re-reading from the wire.
	br := buf.Reader
	_, err = ws.UpgradeFromRequest(rawConn, br, r)
	if err != nil {
		log.Printf("ws: upgrade failed: %v", err)
		rawConn.Close()
		return
	}
	conn := ws.NewConn(rawConn, br, 32)
	defer conn.Close()

	// Create a player session.
	sess, token := g.AddSession()
	defer g.RemoveSession(sess.ID)

	// Send session token.
	sessionMsg, _ := proto.EncodeSession(token)
	conn.WriteMessage(ws.OpText, sessionMsg)

	// Send the authoritative maze. The client renders and collides against
	// this grid verbatim, so there is never a client/server maze desync.
	mazeMsg, err := proto.EncodeMaze(g.MazePayload())
	if err != nil {
		log.Printf("ws: encode maze: %v", err)
		rawConn.Close()
		return
	}
	conn.WriteMessage(ws.OpText, mazeMsg)

	// Subscribe to snapshots and events.
	snapCh := make(chan []byte, 32)
	eventCh := make(chan game.Event, 64)
	g.Subscribe(sess.ID, snapCh)
	g.SubscribeEvents(sess.ID, eventCh)

	// Writer goroutine: send snapshots and events.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case data, ok := <-snapCh:
				if !ok {
					return
				}
				if err := conn.WriteMessage(ws.OpText, data); err != nil {
					return
				}
			case evt, ok := <-eventCh:
				if !ok {
					return
				}
				data, err := proto.EncodeEvent(proto.Event{
					Type: evt.Type,
					Data: mustMarshal(evt.Data),
				})
				if err != nil {
					continue
				}
				conn.SendPriority(data) // events are priority (never drop)
			}
		}
	}()

	// Reader loop: receive inputs and fire events.
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		env, err := proto.Decode(msg)
		if err != nil {
			continue
		}

		switch env.Type {
		case "input":
			input, err := proto.DecodeInput(env)
			if err != nil {
				continue
			}
			cmd := netcode.InputCmd{
				Seq:     env.InputSeq,
				Forward: input.Forward,
				Strafe:  input.Strafe,
				Yaw:     input.Yaw,
				Sprint:  input.Sprint,
			}
			g.ApplyInput(sess.ID, cmd)

			if input.Fire {
				// Pulse fire: raycast from runner position in aim direction.
				snap := g.Loop.Snapshot()
				if snap != nil {
					// Aim 5 units ahead in the yaw direction.
					aimDist := 5.0
					aimPos := maze.Vec2{
						X: snap.Runner.Pos.X + aimDist*sin(input.Yaw),
						Y: snap.Runner.Pos.Y + aimDist*cos(input.Yaw),
					}
					g.FirePulse(sess.ID, snap.Round, aimPos)
				}
			}
		}
	}

	<-done
}

func snapshotToProto(snap *world.Snapshot) proto.SnapshotPayload {
	p := proto.SnapshotPayload{
		Round:     snap.Round,
		Phase:     int(snap.Phase),
		Intensity: snap.Intensity,
		Runner: proto.RunnerState{
			X:        snap.Runner.Pos.X,
			Y:        snap.Runner.Pos.Y,
			Yaw:      snap.Runner.Yaw,
			Pitch:    snap.Runner.Pitch,
			Grounded: snap.Runner.Grounded,
			HP:       snap.Runner.HP,
			Element:  snap.Runner.Element,
		},
	}
	for _, m := range snap.Minotaurs {
		p.Minotaurs = append(p.Minotaurs, proto.MinotaurState{
			ID:      m.ID,
			X:       m.Pos.X,
			Y:       m.Pos.Y,
			Stunned: m.Stunned,
			Type:    m.Type,
			Element: m.Element,
		})
	}
	for _, pu := range snap.PowerUps {
		p.PowerUps = append(p.PowerUps, proto.PowerUpState{
			X:       pu.Pos.X,
			Y:       pu.Pos.Y,
			Element: pu.Element,
			Active:  pu.Active,
		})
	}
	return p
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
