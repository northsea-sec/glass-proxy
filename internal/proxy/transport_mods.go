package proxy

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"

	"proxy.local/app/internal/config"
)

// applyTransportMods modifies an outbound HTTP request based on active
// transport evasion settings. Called before forwarding to any upstream.
func applyTransportMods(req *http.Request, cfg config.Config) {
	if cfg.TransportMethodOverride != "" {
		req.Header.Set("X-HTTP-Method-Override", cfg.TransportMethodOverride)
	}

	if cfg.TransportContentType != "" {
		req.Header.Set("Content-Type", cfg.TransportContentType)
	}

	if cfg.TransportPadBytes > 0 && req.Body != nil {
		origBody, err := io.ReadAll(req.Body)
		if err == nil {
			req.Body.Close()
			pad := make([]byte, cfg.TransportPadBytes)
			rand.Read(pad)
			padded := append(origBody, pad...)
			req.Body = io.NopCloser(bytes.NewReader(padded))
			req.ContentLength = int64(len(padded))
		}
	}

	if cfg.TransportChunked {
		req.TransferEncoding = []string{"chunked"}
		req.ContentLength = -1
	}
}
