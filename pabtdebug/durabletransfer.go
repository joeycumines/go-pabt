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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// handleExport serves a run's committed history as a bundle.
//
// The bundle is prepared whole before a byte of it is served, so a client either
// receives a whole history or an error, and the artifact is released whether or not
// the transfer finished. The route is a stream of the format's own bytes rather than
// a re-encoding, so what a client imports is what the store holds.
func (d *durable) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	key, installation, err := d.resolve(r)
	if err != nil {
		d.fail(w, err)
		return
	}
	req := ExportRequest{Key: key}
	if raw := r.URL.Query().Get("through"); raw != "" {
		through, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, "through must be a decimal number", http.StatusBadRequest)
			return
		}
		req.Through = &through
	}
	artifact, err := d.store.Export(ctx, req)
	if err != nil {
		d.fail(w, err)
		return
	}
	// Released on every path out of here, so a transfer that fails halfway leaves no
	// copy of the history in the artifacts directory.
	defer func() { _ = artifact.Release() }()
	reader, err := artifact.Open()
	if err != nil {
		d.fail(w, err)
		return
	}
	defer func() { _ = reader.Close() }()
	w.Header().Set("Content-Type", bundleMediaType)
	w.Header().Set("Content-Disposition", "attachment; filename=\"pabt-run-"+key.RunID+".pabt\"")
	w.Header().Set("X-PABT-Bundle-SHA256", artifact.Receipt().BundlesSHA256)
	w.Header().Set("X-PABT-Generation", strconv.FormatUint(installation.Generation, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := copyTo(w, reader); err != nil {
		// The headers are already sent, so there is nothing to say to the client
		// except by ending the body early, which a truncated content length shows.
		return
	}
}

// ImportResponse is what an import of a bundle did.
type ImportResponse struct {
	Report     ImportReport       `json:"report"`
	Activation ActivationResponse `json:"activation"`
}

// handleImport stages a bundle and activates it in one request.
//
// Staging and activation are one call rather than two because the ticket between them
// is a proof this server issued to itself: a client has no reason to hold one, and a
// two-step route would let a client activate a bundle it never saw verified. The
// staged bytes are verified in full either way, and the response reports what was
// stored rather than what was sent.
func (d *durable) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	// The bound is applied to the body, so an oversized upload is refused by the
	// transport rather than by the reader after it has arrived.
	r.Body = http.MaxBytesReader(w, r.Body, int64(DefaultImportLimits().MaxBytes))
	ticket, err := d.store.StageImport(ctx, r.Body, DefaultImportLimits())
	if err != nil {
		d.fail(w, err)
		return
	}
	activated, err := d.store.ActivateImport(ctx, ticket)
	if err != nil {
		d.fail(w, err)
		return
	}
	// The store now serves a different session, so the stream's hub is dropped: a
	// client that was attached to the old one would otherwise be told about records
	// from a history it is no longer reading.
	d.invalidateHub()
	d.writeJSON(w, http.StatusOK, ImportResponse{Report: ticket.Report(), Activation: activated})
}

// copyTo writes a reader to a response, which is what the export route needs.
func copyTo(w http.ResponseWriter, reader io.Reader) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			written, writeErr := w.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, fmt.Errorf("pabtdebug: read an export: %w", err)
		}
	}
}
