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

const HTTPExchangeVersion = 1

type HTTPExchange struct {
	Version         int               `json:"version"`
	CaptureID       string            `json:"capture_id"`
	Timestamp       string            `json:"timestamp"`
	RequestURI      string            `json:"request_uri,omitempty"`
	UpstreamURL     string            `json:"upstream_url,omitempty"`
	RequestMethod   string            `json:"request_method,omitempty"`
	RequestHeader   map[string]string `json:"request_header,omitempty"`
	ResponseStatus  int               `json:"response_status"`
	ResponseHeader  map[string]string `json:"response_header,omitempty"`
	RequestBody     json.RawMessage   `json:"request_body,omitempty"`
	ResponseBody    json.RawMessage   `json:"response_body,omitempty"`
	ResponseBase64  string            `json:"response_body_base64,omitempty"`
	ResponsePreview string            `json:"response_body_preview,omitempty"`
}

func (e *HTTPExchange) EnsureTimestamp() {
	if strings.TrimSpace(e.Timestamp) == "" {
		e.Timestamp = time.Now().Format(time.RFC3339Nano)
	}
}

func (e HTTPExchange) FileName() string {
	ts := sanitizeName(strings.ReplaceAll(strings.ReplaceAll(e.Timestamp, ":", "-"), "+", "_"))
	id := sanitizeName(e.CaptureID)
	if id == "" {
		id = "capture"
	}
	return fmt.Sprintf("%s_http_%s.json", ts, id)
}

func WriteHTTPExchange(dir string, e HTTPExchange) error {
	if dir == "" {
		return fmt.Errorf("empty http exchange dir")
	}
	e.EnsureTimestamp()
	if e.Version == 0 {
		e.Version = HTTPExchangeVersion
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	finalPath := filepath.Join(dir, e.FileName())
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}

func EncodeHTTPBodyPreview(body []byte, limit int) (jsonBody json.RawMessage, bodyBase64, bodyPreview string) {
	if len(body) == 0 {
		return nil, "", ""
	}
	if json.Valid(body) {
		jsonBody = append(json.RawMessage(nil), body...)
		return jsonBody, "", ""
	}
	if limit > 0 && len(body) > limit {
		body = body[:limit]
	}
	bodyBase64 = base64.StdEncoding.EncodeToString(body)
	bodyPreview = framePreviewText(body)
	return nil, bodyBase64, bodyPreview
}
