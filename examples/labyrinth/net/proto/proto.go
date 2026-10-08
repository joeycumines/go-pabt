//go:build example

package proto

import (
	"encoding/json"
	"sync/atomic"
)

// Envelope is the top-level wire message. All messages are JSON envelopes.
type Envelope struct {
	Type string `json:"t"`

	// For type "snap": world sequence number and last-acked input seq.
	WorldSeq uint64 `json:"w,omitempty"`
	AckSeq   uint64 `json:"ack,omitempty"`

	// For type "input": client input sequence number.
	InputSeq uint64 `json:"seq,omitempty"`

	// For type "bt": operation and target.
	Op  string `json:"op,omitempty"`
	MID string `json:"mid,omitempty"`

	// Payload is the type-specific body.
	Payload json.RawMessage `json:"p,omitempty"`
}

// SnapshotPayload is the world state sent at 20 Hz.
type SnapshotPayload struct {
	Round            int             `json:"round"`
	Phase            int             `json:"phase"`
	Runner           RunnerState     `json:"runner"`
	Minotaurs        []MinotaurState `json:"minotaurs"`
	PowerUps         []PowerUpState  `json:"powerUps"`
	Intensity        int             `json:"intensity"`
	TerritoryControl [4]int          `json:"territoryControl"`
	Hazards          []HazardState   `json:"hazards"`
}

// RunnerState is the player's state in a snapshot.
type RunnerState struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Yaw      float64 `json:"yaw"`
	Pitch    float64 `json:"pitch"`
	Grounded bool    `json:"grounded"`
	HP       int     `json:"hp"`
	// Element is the runner's active element: 0=fire 1=ice 2=lightning 3=void.
	Element int `json:"element"`
}

// MinotaurState is one minotaur's state in a snapshot.
type MinotaurState struct {
	ID      string  `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Stunned bool    `json:"stunned"`
	Type    int     `json:"type"`
	// Element is the actor's innate element: 0=fire 1=ice 2=lightning 3=void.
	Element int `json:"element"`
	// HP is the actor's remaining health.
	HP int `json:"hp"`
	// Strategy is the actor's current faction strategy: 0=patrol 1=raid
	// 2=retreat.
	Strategy int `json:"strategy"`
}

// PowerUpState is one elemental pickup in a snapshot.
type PowerUpState struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Element int     `json:"element"`
	Active  bool    `json:"active"`
}

// HazardState is one crossfire rift in a snapshot.
type HazardState struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Element int     `json:"element"`
	TTL     int     `json:"ttl"`
}

// InputPayload is the client's input command.
type InputPayload struct {
	Forward float64 `json:"f"`
	Strafe  float64 `json:"s"`
	Jump    bool    `json:"j"`
	Yaw     float64 `json:"yaw"`
	Pitch   float64 `json:"pitch"`
	Sprint  bool    `json:"sprint"`
	Fire    bool    `json:"fire"`
}

// MazePayload is the authoritative maze sent once on connect. The client
// uses this grid verbatim for rendering and collision, so there is never a
// client/server maze desync.
type MazePayload struct {
	W      int     `json:"w"`
	H      int     `json:"h"`
	Grid   []int   `json:"grid"` // cell kind per cell, row-major
	StartX float64 `json:"startX"`
	StartY float64 `json:"startY"`
	GateX  float64 `json:"gateX"`
	GateY  float64 `json:"gateY"`
}

// BTPayload is a BT operation request.
type BTPayload struct {
	// For belief injection: the fake player position.
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Event types sent from server to client.
const (
	EventCaught  = "caught"
	EventFired   = "fired"
	EventStunned = "stunned"
	EventWin     = "win"
	EventLose    = "lose"
	EventReplan  = "replan"
	EventSpawn   = "spawn"
	EventBelief  = "belief"
)

// Event is a typed game event.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Encoder assigns sequence numbers to outgoing envelopes.
type Encoder struct {
	worldSeq atomic.Uint64
}

// NewEncoder creates a new Encoder.
func NewEncoder() *Encoder { return &Encoder{} }

// NextWorldSeq returns the next world sequence number.
func (e *Encoder) NextWorldSeq() uint64 {
	return e.worldSeq.Add(1)
}

// EncodeSnap encodes a snapshot envelope.
func (e *Encoder) EncodeSnap(snap SnapshotPayload, ackSeq uint64) ([]byte, error) {
	payload, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	env := Envelope{
		Type:     "snap",
		WorldSeq: e.NextWorldSeq(),
		AckSeq:   ackSeq,
		Payload:  payload,
	}
	return json.Marshal(env)
}

// EncodeEvent encodes an event envelope.
func EncodeEvent(evt Event) ([]byte, error) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return nil, err
	}
	env := Envelope{
		Type:    "event",
		Payload: payload,
	}
	return json.Marshal(env)
}

// EncodeSession encodes a session token envelope.
func EncodeSession(token string) ([]byte, error) {
	payload, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return nil, err
	}
	env := Envelope{
		Type:    "session",
		Payload: payload,
	}
	return json.Marshal(env)
}

// EncodeMaze encodes the authoritative maze envelope, sent once on connect.
func EncodeMaze(m MazePayload) ([]byte, error) {
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	env := Envelope{
		Type:    "maze",
		Payload: payload,
	}
	return json.Marshal(env)
}

// Decode parses a raw message into an Envelope.
func Decode(data []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// DecodeInput parses an input payload from an envelope.
func DecodeInput(env *Envelope) (*InputPayload, error) {
	var input InputPayload
	if err := json.Unmarshal(env.Payload, &input); err != nil {
		return nil, err
	}
	return &input, nil
}

// DecodeBT parses a BT payload from an envelope.
func DecodeBT(env *Envelope) (*BTPayload, error) {
	var bt BTPayload
	if err := json.Unmarshal(env.Payload, &bt); err != nil {
		return nil, err
	}
	return &bt, nil
}

// DebugPayload is a debugging command from the integrated debug tooling
// (wire type "debug"): an authoritative override of the runner's position.
type DebugPayload struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// DecodeDebug parses a debug payload from an envelope.
func DecodeDebug(env *Envelope) (*DebugPayload, error) {
	var d DebugPayload
	if err := json.Unmarshal(env.Payload, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// DecodeMaze parses a maze payload from an envelope.
func DecodeMaze(env *Envelope) (*MazePayload, error) {
	var m MazePayload
	if err := json.Unmarshal(env.Payload, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
