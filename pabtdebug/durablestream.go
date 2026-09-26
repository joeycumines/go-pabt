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
	"context"
	"fmt"
	"net/http"
	"time"
)

// handleStream serves committed records as server-sent events.
//
// The shape of a resume is what makes it correct, and the order is the whole design:
// subscribe first, then read the store up to the position the subscription reported,
// then stream the queue and skip anything the store already gave. Anything committed
// between the subscribe and the read is queued, and anything the read covered is
// filtered, so no record is missed and none arrives twice. A client that cannot keep
// up is told to resume from a cursor rather than served a slower stream, because the
// store can always give it the gap and the queue cannot.
func (d *durable) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported here", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	key, installation, err := d.resolve(r)
	if err != nil {
		d.fail(w, err)
		return
	}
	after := uint64(0)
	if token := r.URL.Query().Get("cursor"); token != "" {
		cursor, err := DecodeCursor(token)
		if err != nil {
			d.fail(w, err)
			return
		}
		if err := cursor.CheckRefuses(installation.StoreID, key, installation.Generation); err != nil {
			d.fail(w, err)
			return
		}
		after = cursor.Sequence
	}
	hub := d.hubOf(ctx)
	if hub == nil {
		d.fail(w, fmt.Errorf("%w: the stream has no hub", ErrNotFound))
		return
	}
	sub, err := hub.Subscribe()
	if err != nil {
		d.fail(w, err)
		return
	}
	defer sub.Client.release()
	// The gap is read up to the store's head, taken after the subscription rather than
	// before it. Anything committed between the two is queued and arrives on the live
	// half, and anything the read covered is filtered there, so the two halves meet
	// without a gap whichever side of the read a commit lands on.
	head, err := d.store.Head(ctx, key)
	if err != nil {
		d.fail(w, err)
		return
	}
	if after > head.HeadSequence {
		d.fail(w, fmt.Errorf("%w: the cursor is at %d and the run's head is %d",
			ErrHeadMismatch, after, head.HeadSequence))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	stream := &sseWriter{w: w, flusher: flusher}
	if err := stream.writeComment("connected"); err != nil {
		return
	}
	// The gap between the client's position and the subscription is the store's to
	// give, in bounded pages, and it is read before a single queued record is written.
	written, err := d.replay(ctx, stream, key, after, head.HeadSequence)
	if err != nil {
		_ = stream.writeEvent("error", map[string]string{"message": err.Error()})
		return
	}
	d.pump(ctx, stream, hub, sub, written)
}

// replay writes every committed record between two positions, in bounded pages.
func (d *durable) replay(
	ctx context.Context,
	stream *sseWriter,
	key RunKey,
	from uint64,
	to uint64,
) (uint64, error) {
	written := from
	for written < to {
		page, err := d.store.RecordsSince(ctx, key, written)
		if err != nil {
			return written, err
		}
		if len(page.Records) == 0 {
			break
		}
		for _, record := range page.Records {
			if record.Sequence > to {
				return written, nil
			}
			if err := stream.writeRecord(record); err != nil {
				return written, err
			}
			written = record.Sequence
		}
		if !page.HasMore {
			break
		}
	}
	return written, nil
}

// pump writes queued records until the client goes away or falls behind.
func (d *durable) pump(
	ctx context.Context,
	stream *sseWriter,
	hub *DurableHub,
	sub HubSubscription,
	written uint64,
) {
	keepAlive := d.keepAlive
	if keepAlive <= 0 {
		keepAlive = 15 * time.Second
	}
	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Client.BehindSignal():
			d.writeBehind(ctx, stream, hub, written)
			return
		case message, open := <-sub.Client.Messages():
			if !open {
				return
			}
			// The store already gave this one to the client, so the queue's copy is
			// dropped rather than written twice. Either way the hub is told the message
			// was read, because the bound is about what is waiting.
			sub.Client.Took(message)
			if message.Sequence <= written {
				continue
			}
			var record StoredRecord
			if err := CanonicalDecode(message.Bytes, &record); err != nil {
				_ = stream.writeEvent("error", map[string]string{"message": err.Error()})
				return
			}
			if err := stream.writeRecord(record); err != nil {
				return
			}
			written = record.Sequence
		case <-ticker.C:
			// A comment, not an event: it says the connection is alive without
			// advancing anything a client would have to apply.
			if err := stream.writeComment("keep-alive"); err != nil {
				return
			}
		}
		if sub.Client.Behind() {
			d.writeBehind(ctx, stream, hub, written)
			return
		}
	}
}

// writeBehind ends a stream whose client fell behind, with a position to resume from.
//
// The cursor is complete: a client that reconnects with it resumes at the exact next
// sequence, and one that does not reconnect has lost nothing it cannot fetch by
// position. Ending the connection is the point, because the alternative is a stream
// that arrives later and later while the client believes it is live.
func (d *durable) writeBehind(ctx context.Context, stream *sseWriter, hub *DurableHub, written uint64) {
	installation, err := d.store.Installation(ctx)
	if err != nil {
		_ = stream.writeEvent("error", map[string]string{"message": err.Error()})
		return
	}
	cursor := Cursor{
		StoreID:    installation.StoreID,
		RunKey:     hub.RunKey(),
		Generation: installation.Generation,
		Sequence:   written,
	}
	token, err := cursor.Encode()
	if err != nil {
		_ = stream.writeEvent("error", map[string]string{"message": err.Error()})
		return
	}
	_ = stream.writeEvent("behind", CursorResponse{Token: token})
}

// sseWriter writes server-sent events.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// writeComment writes a line that carries no event, which is how a stream says it is
// alive without the client having to interpret it.
func (s *sseWriter) writeComment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// writeEvent writes one named event whose data is canonical JSON.
func (s *sseWriter) writeEvent(name string, value any) error {
	encoded, err := CanonicalEncode(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, encoded); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// writeRecord writes one committed record, with its sequence as the event id so a
// client can see exactly where it is without parsing anything.
func (s *sseWriter) writeRecord(record StoredRecord) error {
	encoded, err := CanonicalEncode(record)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "id: %d\nevent: record\ndata: %s\n\n",
		record.Sequence, encoded); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
