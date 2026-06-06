package xclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// WithUTLS makes the client dial TLS through uTLS, sending a Chrome-like
// ClientHello (JA3) so authenticated traffic better matches real browser
// fingerprints — the same goal as the per-request x-client-transaction-id, one
// layer lower. ALPN advertises both h2 and http/1.1; whichever the server
// negotiates is used (so HTTP/2 is preserved over the Chrome fingerprint).
func WithUTLS() Option {
	return func(cl *Client) {
		cl.httpClient = newUTLSClient(cl.account.Proxy)
	}
}

func newUTLSClient(proxy string) *http.Client {
	return &http.Client{Timeout: 45 * time.Second, Transport: newUTLSRoundTripper(proxy)}
}

// utlsRoundTripper performs the uTLS handshake itself, then dispatches each
// request to an HTTP/2 or HTTP/1.1 transport depending on the ALPN protocol the
// host negotiated (probed once per host and cached).
type utlsRoundTripper struct {
	hello utls.ClientHelloID

	mu    sync.Mutex
	proto map[string]string // host:port -> negotiated ALPN

	h1 *http.Transport
	h2 *http2.Transport
}

func newUTLSRoundTripper(proxy string) *utlsRoundTripper {
	rt := &utlsRoundTripper{
		hello: utls.HelloChrome_133,
		proto: map[string]string{},
	}
	// HTTP/1.1 transport: ALPN pinned to http/1.1.
	rt.h1 = &http.Transport{
		MaxIdleConns:        20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return rt.dialUTLS(ctx, addr, []string{"http/1.1"})
		},
	}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			rt.h1.Proxy = http.ProxyURL(u)
		}
	}
	// HTTP/2 transport: ALPN advertises h2 (with http/1.1 fallback).
	rt.h2 = &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return rt.dialUTLS(ctx, addr, []string{"h2", "http/1.1"})
		},
	}
	return rt
}

func (t *utlsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	addr := canonicalAddr(req.URL)
	proto, err := t.protocolFor(req.Context(), addr)
	if err != nil {
		return nil, err
	}
	if proto == "h2" {
		return t.h2.RoundTrip(req)
	}
	return t.h1.RoundTrip(req)
}

// protocolFor returns the ALPN protocol for a host, probing once and caching it.
func (t *utlsRoundTripper) protocolFor(ctx context.Context, addr string) (string, error) {
	t.mu.Lock()
	if p, ok := t.proto[addr]; ok {
		t.mu.Unlock()
		return p, nil
	}
	t.mu.Unlock()

	conn, err := t.dialUTLS(ctx, addr, []string{"h2", "http/1.1"})
	if err != nil {
		return "", err
	}
	p := conn.(*utls.UConn).ConnectionState().NegotiatedProtocol
	_ = conn.Close()
	if p == "" {
		p = "http/1.1"
	}
	t.mu.Lock()
	t.proto[addr] = p
	t.mu.Unlock()
	return p, nil
}

// dialUTLS dials addr and performs a Chrome-fingerprinted uTLS handshake with
// the given ALPN protocols.
func (t *utlsRoundTripper) dialUTLS(ctx context.Context, addr string, alpn []string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	raw, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	spec, err := utls.UTLSIdToSpec(t.hello)
	if err != nil {
		raw.Close()
		return nil, err
	}
	for _, ext := range spec.Extensions {
		if a, ok := ext.(*utls.ALPNExtension); ok {
			a.AlpnProtocols = alpn
		}
	}
	uconn := utls.UClient(raw, &utls.Config{ServerName: host}, utls.HelloCustom)
	if err := uconn.ApplyPreset(&spec); err != nil {
		raw.Close()
		return nil, err
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return uconn, nil
}

func canonicalAddr(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	return net.JoinHostPort(u.Hostname(), "443")
}
