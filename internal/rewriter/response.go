// response.go — response body rewriting.
// JSON rewriting for API mode, HTML/JS/CSS sub_filter for web chat mode.
package rewriter

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
)

// RewriteResponse rewrites the response body, handling gzip and content type detection.
// Modifies the response in-place.
func RewriteResponse(resp *http.Response, filters []SubFilter) error {
	if len(filters) == 0 {
		return nil
	}

	ct := resp.Header.Get("Content-Type")
	if !isTextContent(ct) {
		return nil
	}

	// F07: Limit response body reads to prevent OOM from malicious upstream.
	const maxResponseBody int64 = 100 * 1024 * 1024 // 100MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	resp.Body.Close()
	if err != nil {
		return err
	}
	if int64(len(body)) > maxResponseBody {
		return fmt.Errorf("response body too large: %d bytes exceeds %d limit", len(body), maxResponseBody)
	}

	// Handle content encoding (gzip or brotli)
	encoding := strings.ToLower(resp.Header.Get("Content-Encoding"))
	switch encoding {
	case "gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err == nil {
			body, err = io.ReadAll(io.LimitReader(gr, maxResponseBody+1))
			gr.Close()
			if err != nil {
				return err
			}
			resp.Header.Del("Content-Encoding")
		}
	case "br":
		br := brotli.NewReader(bytes.NewReader(body))
		decoded, brErr := io.ReadAll(io.LimitReader(br, maxResponseBody+1))
		if brErr != nil {
			return fmt.Errorf("brotli decompression failed: %w", brErr)
		}
		body = decoded
		resp.Header.Del("Content-Encoding")
	}
	if int64(len(body)) > maxResponseBody {
		return fmt.Errorf("decompressed response body too large: %d bytes exceeds %d limit", len(body), maxResponseBody)
	}

	// Apply sub_filter replacements
	body = RewriteBody(body, ct, filters)

	// For HTML responses, also rewrite inline scripts/styles
	if strings.Contains(ct, "text/html") {
		body = rewriteHTML(body, filters)
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))

	return nil
}

// rewriteHTML does additional HTML-specific rewrites beyond simple sub_filter.
// Handles: inline <script> src attributes, <link> href, <meta> content, etc.
func rewriteHTML(body []byte, filters []SubFilter) []byte {
	// The basic SubFilter already handles most cases.
	// This is for edge cases where domain appears in HTML attributes
	// that aren't caught by simple string replacement.
	// For now, the sub_filter approach is sufficient — transparent proxy uses the same.
	return body
}
