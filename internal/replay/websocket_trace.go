package replay

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const WebSocketTraceVersion = 1

type WebSocketFrame struct {
	Timestamp        string `json:"timestamp"`
	Direction        string `json:"direction"`
	MessageType      int    `json:"message_type"`
	SizeBytes        int    `json:"size_bytes"`
	PayloadBase64    string `json:"payload_base64,omitempty"`
	PayloadText      string `json:"payload_text,omitempty"`
	PayloadTruncated bool   `json:"payload_truncated,omitempty"`
}

type WebSocketTrace struct {
	Version       int               `json:"version"`
	CaptureID     string            `json:"capture_id"`
	Timestamp     string            `json:"timestamp"`
	RequestURI    string            `json:"request_uri,omitempty"`
	UpstreamURL   string            `json:"upstream_url,omitempty"`
	SelectedProto string            `json:"selected_proto,omitempty"`
	Header        map[string]string `json:"header,omitempty"`
	Frames        []WebSocketFrame  `json:"frames,omitempty"`
	BytesSeen     int               `json:"bytes_seen"`
	BytesCaptured int               `json:"bytes_captured"`
	Truncated     bool              `json:"truncated"`
	DroppedFrames int               `json:"dropped_frames"`
}

func (t *WebSocketTrace) EnsureTimestamp() {
	if strings.TrimSpace(t.Timestamp) == "" {
		t.Timestamp = time.Now().Format(time.RFC3339Nano)
	}
}

func (t WebSocketTrace) FileName() string {
	ts := sanitizeName(strings.ReplaceAll(strings.ReplaceAll(t.Timestamp, ":", "-"), "+", "_"))
	id := sanitizeName(t.CaptureID)
	if id == "" {
		id = "capture"
	}
	return fmt.Sprintf("%s_ws_%s.json", ts, id)
}

func WriteWebSocketTrace(dir string, t WebSocketTrace) error {
	if dir == "" {
		return fmt.Errorf("empty websocket trace dir")
	}
	t.EnsureTimestamp()
	if t.Version == 0 {
		t.Version = WebSocketTraceVersion
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	finalPath := filepath.Join(dir, t.FileName())
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}

func EncodeWebSocketFramePayload(payload []byte, limit int) (payloadBase64, payloadText string, truncated bool, size int) {
	size = len(payload)
	if limit <= 0 || len(payload) <= limit {
		payloadBase64 = base64.StdEncoding.EncodeToString(payload)
		payloadText = framePreviewText(payload)
		return payloadBase64, payloadText, false, size
	}
	payloadBase64 = base64.StdEncoding.EncodeToString(payload[:limit])
	payloadText = framePreviewText(payload[:limit])
	return payloadBase64, payloadText, true, size
}

func framePreviewText(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	text := string(payload)
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 32 {
			return ""
		}
	}
	return text
}
