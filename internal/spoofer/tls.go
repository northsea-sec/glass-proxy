// tls.go — Chrome TLS fingerprint spoofing via utls.
// Wraps net/http.Transport with a utls.UConn that mimics Chrome's ClientHello.
// This prevents TLS fingerprint-based traffic classification (JA3/JA4 matching)
// which would reveal that traffic originates from a Go binary, not a browser.
package spoofer

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	utls "github.com/refraction-networking/utls"
)

// ChromeTransport returns an http.RoundTripper that uses utls to impersonate
// Chrome's TLS fingerprint. The underlying connection uses Chrome 120's
// ClientHello (supports TLS 1.3, GREASE, ALPS, etc).
func ChromeTransport(base *http.Transport) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	return newUTLSTransport(base, utls.HelloChrome_Auto)
}

// FirefoxTransport returns an http.RoundTripper impersonating Firefox.
func FirefoxTransport(base *http.Transport) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	return newUTLSTransport(base, utls.HelloFirefox_Auto)
}

type utlsTransport struct {
	inner *http.Transport // configured once, reused for connection pooling
}

func newUTLSTransport(base *http.Transport, helloID utls.ClientHelloID) *utlsTransport {
	t := &utlsTransport{}
	inner := base.Clone()
	inner.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialUTLS(ctx, network, addr, helloID)
	}
	// Disable Go's default TLS — we handle it via utls
	inner.TLSClientConfig = &tls.Config{InsecureSkipVerify: false}
	// IMPORTANT: Do NOT enable ForceAttemptHTTP2. Go's http2.Transport sends
	// default HTTP/2 SETTINGS frames (HEADER_TABLE_SIZE=4096, WINDOW_SIZE=4MB,
	// MAX_CONCURRENT_STREAMS=100) which do NOT match Chrome's values (64KB, 6MB,
	// 1000). Cloudflare fingerprints this mismatch (Chrome ClientHello + Go H2
	// SETTINGS = bot). HTTP/1.1 is safe: Chrome falls back to H/1.1 naturally.
	// The Rust egress-proxy handles real H2 with correct browser SETTINGS.
	inner.ForceAttemptHTTP2 = false
	t.inner = inner
	return t
}

func (t *utlsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.inner.RoundTrip(req)
}

// DialUTLS dials a TLS connection using utls with Chrome's ClientHello fingerprint.
// Used by WebSocket handler to avoid raw crypto/tls (which leaks Go's JA3 hash).
// The timeout parameter controls the TCP + TLS handshake deadline.
func DialUTLS(network, addr, serverName string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return dialUTLS(ctx, network, addr, utls.HelloChrome_Auto)
}

func dialUTLS(ctx context.Context, network, addr string, helloID utls.ClientHelloID) (net.Conn, error) {
	// Extract hostname for SNI
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	// Dial raw TCP
	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	// Wrap with utls using the specified browser fingerprint.
	// NextProtos is set to HTTP/1.1 only — we intentionally skip h2 because
	// Go's http2.Transport sends non-Chrome HTTP/2 SETTINGS frames that
	// Cloudflare can fingerprint. Chrome legitimately negotiates http/1.1 when
	// servers support it, so this is not suspicious. The Rust egress-proxy
	// handles the real H2 upstream connection with correct browser SETTINGS.
	config := &utls.Config{
		ServerName:         host,
		InsecureSkipVerify: false,
		NextProtos:         []string{"http/1.1"},
	}
	uconn := utls.UClient(rawConn, config, helloID)

	// Perform TLS handshake with the spoofed ClientHello
	if err := uconn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, err
	}

	return uconn, nil
}

// UpgradeToUTLS performs a utls TLS handshake over an existing connection.
// Used by the WebSocket handler to do Chrome-fingerprinted TLS through an
// already-established egress-proxy CONNECT tunnel. The underlying conn is
// typically a raw TCP connection to the egress-proxy which has tunneled
// through to the target host.
func UpgradeToUTLS(conn net.Conn, serverName string, timeout time.Duration) (net.Conn, error) {
	config := &utls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: false,
		NextProtos:         []string{"http/1.1"},
	}
	uconn := utls.UClient(conn, config, utls.HelloChrome_Auto)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := uconn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return uconn, nil
}
