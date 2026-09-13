package proxy

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestIsCodexResponsesRequestDecodesZstdBody(t *testing.T) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer enc.Close()

	payload := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	compressed := enc.EncodeAll(payload, nil)

	req := httptest.NewRequest("POST", "/responses", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "zstd")

	p := &Proxy{}
	if !p.isCodexResponsesRequest(req) {
		t.Fatal("expected request to be classified as Codex")
	}

	if got := req.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want cleared", got)
	}

	rewound, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll rewound body: %v", err)
	}
	if !bytes.Equal(rewound, payload) {
		t.Fatalf("rewound body mismatch:\n got: %q\nwant: %q", rewound, payload)
	}
}
