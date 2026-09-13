// Package sse provides a Go-native SSE (Server-Sent Events) stream parser.
// Handles chunked events split across TCP reads, buffering partial lines,
// and extracting data/event/id fields per the SSE spec.
//
// Replaces the Python buffer logic from mitm_itt_addon.py entirely.
package sse

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// Event represents a single parsed SSE event.
type Event struct {
	Type      string    // "event:" field value (e.g. "message_start", "content_block_delta")
	Data      string    // "data:" field value (JSON string)
	ID        string    // "id:" field value
	Timestamp time.Time // when this event was received (for ITT calculation)
}

// ParsedData is the parsed JSON from an SSE data field (Anthropic format).
type ParsedData struct {
	Type  string                 `json:"type"`
	Index int                    `json:"index"`
	Delta map[string]interface{} `json:"delta"`
	Usage map[string]interface{} `json:"usage"`

	// message_start fields
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens        int `json:"input_tokens"`
			OutputTokens       int `json:"output_tokens"`
			CacheCreationInput int `json:"cache_creation_input_tokens"`
			CacheReadInput     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// TokenDelta extracts the text token from a content_block_delta event.
// Returns empty string if this event doesn't contain a text token.
func (e *Event) TokenDelta() string {
	if e.Type != "content_block_delta" && !strings.Contains(e.Data, "content_block_delta") {
		return ""
	}
	var parsed ParsedData
	if err := json.Unmarshal([]byte(e.Data), &parsed); err != nil {
		return ""
	}
	if parsed.Delta == nil {
		return ""
	}
	if text, ok := parsed.Delta["text"].(string); ok {
		return text
	}
	return ""
}

// ParseJSON parses the SSE data field as JSON into ParsedData.
func (e *Event) ParseJSON() (*ParsedData, error) {
	var parsed ParsedData
	err := json.Unmarshal([]byte(e.Data), &parsed)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// Reader reads SSE events from an io.Reader, handling partial lines
// and events split across TCP reads.
type Reader struct {
	scanner *bufio.Scanner
	buf     strings.Builder // accumulates data: lines for multi-line data
	event   string          // current event type
	id      string          // current event id
}

// NewReader creates an SSE reader from an io.Reader.
func NewReader(r io.Reader) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line
	return &Reader{
		scanner: scanner,
	}
}

// Next reads the next complete SSE event from the stream.
// Returns io.EOF when the stream ends.
func (r *Reader) Next() (*Event, error) {
	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Empty line = event boundary (dispatch)
		if line == "" {
			if r.buf.Len() > 0 {
				evt := &Event{
					Type:      r.event,
					Data:      r.buf.String(),
					ID:        r.id,
					Timestamp: time.Now(),
				}
				r.buf.Reset()
				r.event = ""
				r.id = ""
				return evt, nil
			}
			continue
		}

		// Parse field
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(data, " ") {
				data = data[1:] // strip single leading space per SSE spec
			}
			if r.buf.Len() > 0 {
				r.buf.WriteString("\n")
			}
			r.buf.WriteString(data)
		} else if strings.HasPrefix(line, "event:") {
			r.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "id:") {
			r.id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
		// ignore retry: and comments (:)
	}

	// Flush any remaining data
	if r.buf.Len() > 0 {
		evt := &Event{
			Type:      r.event,
			Data:      r.buf.String(),
			ID:        r.id,
			Timestamp: time.Now(),
		}
		r.buf.Reset()
		return evt, io.EOF
	}

	if err := r.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// ReadAll reads all events from the stream until EOF or error.
func (r *Reader) ReadAll() ([]*Event, error) {
	var events []*Event
	for {
		evt, err := r.Next()
		if evt != nil {
			events = append(events, evt)
		}
		if err != nil {
			if err == io.EOF {
				return events, nil
			}
			return events, err
		}
	}
}
