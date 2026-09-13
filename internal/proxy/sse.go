// sse.go — SSE stream handling for the reverse proxy.
// Buffers final message_stop/message_delta events for usage spoofing,
// passes through intermediate content_block_delta events without buffering (low latency).
package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"proxy.local/app/internal/spoofer"
)

// SSEProxy streams SSE events from upstream to client, applying spoofing
// to usage events while passing content deltas through immediately.
type SSEProxy struct {
	spoofer *spoofer.Spoofer
}

// NewSSEProxy creates an SSE proxy with optional spoofing.
func NewSSEProxy(sp *spoofer.Spoofer) *SSEProxy {
	return &SSEProxy{spoofer: sp}
}

// Stream reads SSE events from upstream and writes them to the client.
// Usage events (message_start, message_delta with usage) are buffered for spoofing.
// Content events (content_block_delta) are passed through immediately.
func (s *SSEProxy) Stream(w http.ResponseWriter, upstream io.Reader) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// No flusher — just copy
		_, err := io.Copy(w, upstream)
		return err
	}

	scanner := bufio.NewScanner(upstream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var dataBuf bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line = event dispatch
		if line == "" {
			if dataBuf.Len() > 0 {
				data := dataBuf.String()
				dataBuf.Reset()

				// Determine if this event needs spoofing
				modified := s.maybeSpoof(data, eventType)

				// Write event
				if eventType != "" {
					w.Write([]byte("event: " + eventType + "\n"))
				}
				w.Write([]byte("data: " + modified + "\n\n"))
				flusher.Flush()

				eventType = ""
			} else {
				// Empty event boundary, forward as-is
				w.Write([]byte("\n"))
				flusher.Flush()
			}
			continue
		}

		// Parse SSE fields
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(data, " ") {
				data = data[1:]
			}
			if dataBuf.Len() > 0 {
				dataBuf.WriteString("\n")
			}
			dataBuf.WriteString(data)
		} else if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, ":") {
			// Comment — pass through
			w.Write([]byte(line + "\n"))
			flusher.Flush()
		}
	}

	// Flush remaining
	if dataBuf.Len() > 0 {
		data := dataBuf.String()
		modified := s.maybeSpoof(data, eventType)
		if eventType != "" {
			w.Write([]byte("event: " + eventType + "\n"))
		}
		w.Write([]byte("data: " + modified + "\n\n"))
		flusher.Flush()
	}

	return scanner.Err()
}

// maybeSpoof applies usage spoofing to events that contain usage data.
// Content deltas pass through unmodified.
func (s *SSEProxy) maybeSpoof(data string, eventType string) string {
	if s.spoofer == nil {
		return data
	}

	// Only spoof events with usage data
	if !strings.Contains(data, "\"usage\"") {
		return data
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return data
	}

	if s.spoofer.SpoofSSEEvent(event) {
		modified, err := json.Marshal(event)
		if err != nil {
			log.Printf("[SSE] Failed to re-marshal spoofed event: %v", err)
			return data
		}
		return string(modified)
	}
	return data
}
