package requestbody

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

var ErrTooLarge = errors.New("request body too large")

// ReadAndNormalize reads, optionally decompresses, and rewinds the request body.
// The returned bytes are always the decoded body seen by downstream JSON parsing.
func ReadAndNormalize(r *http.Request, maxBytes int64) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrTooLarge
	}

	decoded, err := DecodeBytes(body, r.Header.Get("Content-Encoding"), maxBytes)
	if err != nil {
		return nil, err
	}

	r.Body = io.NopCloser(bytes.NewReader(decoded))
	r.ContentLength = int64(len(decoded))
	if len(decoded) > 0 {
		r.Header.Set("Content-Length", strconv.Itoa(len(decoded)))
	} else {
		r.Header.Del("Content-Length")
	}
	r.Header.Del("Content-Encoding")
	return decoded, nil
}

// DecodeBytes decodes request bytes according to Content-Encoding.
func DecodeBytes(body []byte, encoding string, maxBytes int64) ([]byte, error) {
	encoding = strings.TrimSpace(strings.ToLower(encoding))
	if strings.Contains(encoding, ",") {
		return nil, fmt.Errorf("unsupported content encoding chain: %s", encoding)
	}

	switch encoding {
	case "", "identity":
		return body, nil
	case "gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		return readLimited(gr, maxBytes)
	case "br":
		return readLimited(brotli.NewReader(bytes.NewReader(body)), maxBytes)
	case "zstd":
		dec, err := zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		decoded, err := dec.DecodeAll(body, nil)
		if err != nil {
			return nil, err
		}
		if int64(len(decoded)) > maxBytes {
			return nil, ErrTooLarge
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported content encoding: %s", encoding)
	}
}

func readLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrTooLarge
	}
	return body, nil
}
