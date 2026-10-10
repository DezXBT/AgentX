package transaction

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// captureRT records the last request it saw so tests can assert on headers.
type captureRT struct{ req *http.Request }

func (c *captureRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.req = r
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("<html></html>")),
		Header:     make(http.Header),
	}, nil
}

func newTestProvider(rt *captureRT, cookie string) *Provider {
	return NewProvider(
		WithHTTPClient(&http.Client{Transport: rt}),
		WithCookie(cookie),
	)
}

func TestProviderCookieOptions(t *testing.T) {
	p := NewProvider(WithCookie("auth_token=a; ct0=b"))
	if p.cookie != "auth_token=a; ct0=b" {
		t.Fatalf("WithCookie not applied: %q", p.cookie)
	}
	p.SetCookie("auth_token=c")
	if p.cookie != "auth_token=c" {
		t.Fatalf("SetCookie not applied: %q", p.cookie)
	}
}

// x.com only serves the responsive-web chunk map (the `,<idx>:"ondemand.s"`
// entry getOndemandFileURL parses) to an authenticated session, so the home
// page fetch must carry the account cookie — and only that host may see it.
func TestProviderFetchCookieScope(t *testing.T) {
	rt := &captureRT{}
	p := newTestProvider(rt, "auth_token=a; ct0=b")

	if _, err := p.fetch("https://x.com/", p.cookie); err != nil {
		t.Fatalf("fetch x.com: %v", err)
	}
	if got := rt.req.Header.Get("Cookie"); got != "auth_token=a; ct0=b" {
		t.Fatalf("x.com home fetch must send the account cookie, got %q", got)
	}
	if got := rt.req.Header.Get("User-Agent"); got == "" {
		t.Fatal("home fetch lost its User-Agent")
	}

	// The ondemand.s file is a static asset on abs.twimg.com: no cookies.
	if _, err := p.fetch("https://abs.twimg.com/responsive-web/client-web/ondemand.s.deadbeef1a.js", ""); err != nil {
		t.Fatalf("fetch asset: %v", err)
	}
	if got := rt.req.Header.Get("Cookie"); got != "" {
		t.Fatalf("static asset host must not receive the account cookie, got %q", got)
	}

	// Belt and braces: even if a cookie is passed in, a non-x.com host is refused.
	if _, err := p.fetch("https://example.com/", "auth_token=a"); err != nil {
		t.Fatalf("fetch other host: %v", err)
	}
	if got := rt.req.Header.Get("Cookie"); got != "" {
		t.Fatalf("non-x.com host must not receive the account cookie, got %q", got)
	}
}
