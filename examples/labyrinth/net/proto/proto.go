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
	Round     int             `json:"round"`
	Phase     int             `json:"phase"`
	Runner    RunnerState     `json:"runner"`
	Minotaurs []MinotaurState `json:"minotaurs"`
	Intensity int             `json:"intensity"`
}

// RunnerState is the player's state in a snapshot.
type RunnerState struct {
	X, Y     float64 `json:"x,y"`
	Yaw      float64 `json:"yaw"`
	Pitch    float64 `json:"pitch"`
	Grounded bool    `json:"grounded"`
}

// MinotaurState is one minotaur's state in a snapshot.
type MinotaurState struct {
	ID      string  `json:"id"`
	X, Y    float64 `json:"x,y"`
	Stunned bool    `json:"stunned"`
}

// InputPayload is the client's input command.
type InputPayload struct {
	Forward float64 `json:"f"`
	Strafe  float64 `json:"s"`
	Jump    bool    `json:"j"`
	Yaw     float64 `json:"yaw"`
	Pitch   float64 `json:"pitch"`
	Fire    bool    `json:"fire"`
}

// BTPayload is a BT operation request.
type BTPayload struct {
	// For belief injection: the fake player position.
	X, Y float64 `json:"x,y"`
}

// Event types sent from server to client.
const (
	EventCaught   = "caught"
	EventFired    = "fired"
	EventStunned  = "stunned"
	EventWin      = "win"
	EventLose     = "lose"
	EventReplan   = "replan"
	EventSpawn    = "spawn"
	EventBelief   = "belief"
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
