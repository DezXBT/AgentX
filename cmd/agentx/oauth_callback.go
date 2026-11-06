package main

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// callbackJarHTTP performs the app-callback GET with a cookie jar so any
// Set-Cookie the app issues on login (its session) is captured and reported
// back to the caller. This is how sign-in-with-X finishes: the callback
// response carries the site's own session cookies.
type callbackResult struct {
	FinalURL  string            `json:"finalUrl"`
	Status    int               `json:"status"`
	SetCookie map[string]string `json:"sessionCookies"`
	Body      string            `json:"response"`
}

func runCallback(ctx context.Context, rawURL string, cookies []string) (*callbackResult, error) {
	if !strings.HasPrefix(rawURL, "http") {
		return nil, xerrors.New(xerrors.InvalidInput, "callback url must be absolute")
	}
	jar, jErr := cookiejar.New(nil)
	if jErr != nil {
		return nil, xerrors.Wrap(xerrors.NetworkError, jErr, "cookie jar")
	}
	// site-state cookies (e.g. fanszn_oauth={state,verifier}) must ride along
	// or the app rejects the callback with state_mismatch.
	baseURL, _ := url.Parse(rawURL)
	for _, kv := range cookies {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, xerrors.New(xerrors.InvalidInput, "bad --cookie %q (want name=value)", kv)
		}
		jar.SetCookies(baseURL, []*http.Cookie{{Name: k, Value: v}})
	}
	base, _ := url.Parse(rawURL)
	client := &http.Client{Timeout: 45 * time.Second, Jar: jar}
	req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if rErr != nil {
		return nil, xerrors.Wrap(xerrors.InvalidInput, rErr, "bad callback url")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	resp, dErr := client.Do(req)
	if dErr != nil {
		return nil, xerrors.Wrap(xerrors.NetworkError, dErr, "callback fetch failed")
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 2048)
	for {
		n, rErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if rErr != nil || len(buf) > 256*1024 {
			break
		}
	}
	out := &callbackResult{FinalURL: resp.Request.URL.String(), Status: resp.StatusCode, SetCookie: map[string]string{}, Body: string(buf)}
	// report cookies that now live in the jar for the app domain(s)
	for _, cURL := range []*url.URL{resp.Request.URL, base} {
		if cURL == nil {
			continue
		}
		for _, ck := range jar.Cookies(cURL) {
			if _, seen := out.SetCookie[ck.Name]; !seen {
				out.SetCookie[ck.Name] = ck.Value
			}
		}
	}
	return out, nil
}
