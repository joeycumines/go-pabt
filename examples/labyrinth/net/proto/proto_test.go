//go:build example

package proto

import (
	"encoding/json"
	"testing"
)

func TestEncodeDecodeSnapshot(t *testing.T) {
	enc := NewEncoder()

	snap := SnapshotPayload{
		Round: 42,
		Phase: 0,
		Runner: RunnerState{
			X: 1.5, Y: 2.5,
			Yaw: 0.1, Pitch: -0.05,
			Grounded: true,
		},
		Minotaurs: []MinotaurState{
			{ID: "M-1", X: 3.0, Y: 4.0, Stunned: false},
			{ID: "M-2", X: 5.0, Y: 6.0, Stunned: true},
		},
		Intensity: 1,
	}

	data, err := enc.EncodeSnap(snap, 7)
	if err != nil {
		t.Fatalf("EncodeSnap: %v", err)
	}

	env, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if env.Type != "snap" {
		t.Fatalf("type = %q, want snap", env.Type)
	}
	if env.WorldSeq != 1 {
		t.Fatalf("worldSeq = %d, want 1", env.WorldSeq)
	}
	if env.AckSeq != 7 {
		t.Fatalf("ackSeq = %d, want 7", env.AckSeq)
	}

	var decoded SnapshotPayload
	if err := json.Unmarshal(env.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.Round != 42 {
		t.Errorf("round = %d, want 42", decoded.Round)
	}
	if decoded.Runner.X != 1.5 {
		t.Errorf("runner.X = %f, want 1.5", decoded.Runner.X)
	}
	if len(decoded.Minotaurs) != 2 {
		t.Fatalf("minotaurs = %d, want 2", len(decoded.Minotaurs))
	}
	if decoded.Minotaurs[1].Stunned != true {
		t.Error("minotaur 2 should be stunned")
	}

	// Second encode should increment worldSeq.
	data2, _ := enc.EncodeSnap(snap, 8)
	env2, _ := Decode(data2)
	if env2.WorldSeq != 2 {
		t.Fatalf("second worldSeq = %d, want 2", env2.WorldSeq)
	}
}

func TestEncodeDecodeEvent(t *testing.T) {
	evt := Event{Type: EventCaught}
	data, err := EncodeEvent(evt)
	if err != nil {
		t.Fatalf("EncodeEvent: %v", err)
	}

	env, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != "event" {
		t.Fatalf("type = %q, want event", env.Type)
	}

	var decoded Event
	if err := json.Unmarshal(env.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if decoded.Type != EventCaught {
		t.Fatalf("event type = %q, want %q", decoded.Type, EventCaught)
	}
}

func TestEncodeDecodeSession(t *testing.T) {
	token := "abc123-session-token"
	data, err := EncodeSession(token)
	if err != nil {
		t.Fatalf("EncodeSession: %v", err)
	}

	env, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != "session" {
		t.Fatalf("type = %q, want session", env.Type)
	}

	var decoded map[string]string
	if err := json.Unmarshal(env.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if decoded["token"] != token {
		t.Fatalf("token = %q, want %q", decoded["token"], token)
	}
}

func TestDecodeInput(t *testing.T) {
	input := InputPayload{
		Forward: 1.0,
		Strafe:  -0.5,
		Jump:    true,
		Yaw:     1.2,
		Pitch:   -0.3,
		Fire:    true,
	}
	payload, _ := json.Marshal(input)
	env := Envelope{
		Type:     "input",
		InputSeq: 5,
		Payload:  payload,
	}
	data, _ := json.Marshal(env)

	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Type != "input" {
		t.Fatalf("type = %q, want input", decoded.Type)
	}
	if decoded.InputSeq != 5 {
		t.Fatalf("inputSeq = %d, want 5", decoded.InputSeq)
	}

	parsed, err := DecodeInput(decoded)
	if err != nil {
		t.Fatalf("DecodeInput: %v", err)
	}
	if parsed.Forward != 1.0 {
		t.Errorf("forward = %f, want 1.0", parsed.Forward)
	}
	if !parsed.Fire {
		t.Error("fire should be true")
	}
}

func TestDecodeBT(t *testing.T) {
	btPayload := BTPayload{X: 5.5, Y: 3.3}
	payload, _ := json.Marshal(btPayload)
	env := Envelope{
		Type:    "bt",
		Op:      "inject",
		MID:     "M-1",
		Payload: payload,
	}
	data, _ := json.Marshal(env)

	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Type != "bt" {
		t.Fatalf("type = %q, want bt", decoded.Type)
	}
	if decoded.Op != "inject" {
		t.Fatalf("op = %q, want inject", decoded.Op)
	}
	if decoded.MID != "M-1" {
		t.Fatalf("mid = %q, want M-1", decoded.MID)
	}

	parsed, err := DecodeBT(decoded)
	if err != nil {
		t.Fatalf("DecodeBT: %v", err)
	}
	if parsed.X != 5.5 || parsed.Y != 3.3 {
		t.Errorf("position = (%f, %f), want (5.5, 3.3)", parsed.X, parsed.Y)
	}
}
