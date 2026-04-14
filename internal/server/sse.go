package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dshills/matter/pkg/matter"
)

// writeSSEHeaders sets the required headers for an SSE response.
func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx buffering
}

// writeSSEEvent writes a single SSE event to the response. It flushes
// after each write to ensure the client receives events immediately.
func writeSSEEvent(w http.ResponseWriter, event matter.ProgressEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal SSE event: %w", err)
	}

	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Event, data)
	if err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}
