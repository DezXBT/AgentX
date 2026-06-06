// Package xclient is an authenticated client for x.com's internal GraphQL/REST
// API. It signs every request with a freshly generated x-client-transaction-id
// (see pkg/transaction) and a complete browser-like header set, which is what
// keeps cookie sessions alive instead of getting them flagged and expired.
package xclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dezxbt/agentx/pkg/account"
	"github.com/dezxbt/agentx/pkg/transaction"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// Client talks to x.com on behalf of a single account.
type Client struct {
	account    *account.Account
	httpClient *http.Client
	tx         *transaction.Provider
	userAgent  string
	locale     string
	features   map[string]bool
	queryIDs   map[string]string
	maxRetries int
	baseDelay  time.Duration
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (e.g. to plug in a utls transport
// for JA3 impersonation, or a proxy).
func WithHTTPClient(c *http.Client) Option { return func(cl *Client) { cl.httpClient = c } }

// WithTransactionProvider shares a pre-warmed transaction.Provider.
func WithTransactionProvider(p *transaction.Provider) Option {
	return func(cl *Client) { cl.tx = p }
}

// WithLocale sets the x-twitter-client-language header (default "en").
func WithLocale(l string) Option { return func(cl *Client) { cl.locale = l } }

// New builds a Client for an account.
func New(acc *account.Account, opts ...Option) (*Client, error) {
	if err := acc.Validate(); err != nil {
		return nil, xerrors.Wrap(xerrors.NotAuthenticated, err, "invalid account")
	}
	ua := acc.UserAgent
	if ua == "" {
		ua = transaction.DefaultUserAgent
	}

	cl := &Client{
		account:    acc,
		userAgent:  ua,
		locale:     "en",
		features:   defaultFeatures,
		queryIDs:   cloneStrMap(queryIDs),
		maxRetries: 3,
		baseDelay:  5 * time.Second,
	}
	// allow rotated/new GraphQL query ids to be fixed at runtime without
	// recompiling, via AGENTX_QID_<Operation> env vars.
	for op := range cl.queryIDs {
		if v := os.Getenv("AGENTX_QID_" + op); v != "" {
			cl.queryIDs[op] = v
		}
	}
	for _, o := range opts {
		o(cl)
	}

	if cl.httpClient == nil {
		cl.httpClient = buildHTTPClient(acc.Proxy)
	}
	if cl.tx == nil {
		cl.tx = transaction.NewProvider(
			transaction.WithHTTPClient(cl.httpClient),
			transaction.WithUserAgent(ua),
		)
	}
	return cl, nil
}

func buildHTTPClient(proxy string) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: 45 * time.Second, Transport: tr}
}

// SetQueryID overrides a single operation's query id (used when x.com rotates
// them and a fresh id is discovered).
func (c *Client) SetQueryID(op, id string) { c.queryIDs[op] = id }

// Account returns the account this client serves.
func (c *Client) Account() *account.Account { return c.account }

func (c *Client) queryID(op string) (string, error) {
	id, ok := c.queryIDs[op]
	if !ok || id == "" {
		return "", xerrors.New(xerrors.QueryIDError, "unknown query id for operation %q", op)
	}
	return id, nil
}

// graphqlGET performs a GraphQL GET, returning the raw `data` object.
func (c *Client) graphqlGET(ctx context.Context, op string, variables map[string]any, fieldToggles, extraFeatures map[string]any) (json.RawMessage, error) {
	qid, err := c.queryID(op)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/i/api/graphql/%s/%s", qid, op)

	q := url.Values{}
	q.Set("variables", mustJSON(variables))
	q.Set("features", mustJSON(c.mergedFeatures(extraFeatures)))
	if fieldToggles != nil {
		q.Set("fieldToggles", mustJSON(fieldToggles))
	}
	full := fmt.Sprintf("%s/%s/%s?%s", graphqlBase, qid, op, q.Encode())

	return c.do(ctx, http.MethodGet, full, path, nil, false)
}

// graphqlPOST performs a GraphQL POST, returning the raw `data` object. write
// marks state-changing operations (which get write-flavoured headers).
func (c *Client) graphqlPOST(ctx context.Context, op string, variables, extraFeatures map[string]any, write bool) (json.RawMessage, error) {
	qid, err := c.queryID(op)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/i/api/graphql/%s/%s", qid, op)
	full := fmt.Sprintf("%s/%s/%s", graphqlBase, qid, op)

	body := map[string]any{
		"variables": variables,
		"features":  c.mergedFeatures(extraFeatures),
		"queryId":   qid,
	}
	return c.do(ctx, http.MethodPost, full, path, body, write)
}

// mergedFeatures overlays per-operation feature flags on top of the defaults.
func (c *Client) mergedFeatures(extra map[string]any) map[string]bool {
	if len(extra) == 0 {
		return c.features
	}
	out := make(map[string]bool, len(c.features)+len(extra))
	for k, v := range c.features {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = asBoolAny(v)
	}
	return out
}

func asBoolAny(v any) bool {
	b, _ := v.(bool)
	return b
}

// do executes a request with retry/backoff and returns the GraphQL `data` field
// (or the raw body for REST endpoints).
func (c *Client) do(ctx context.Context, method, fullURL, path string, body any, write bool) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			sleep(ctx, c.backoff(attempt))
		}
		raw, retry, err := c.attempt(ctx, method, fullURL, path, body, write)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) attempt(ctx context.Context, method, fullURL, path string, body any, write bool) (data json.RawMessage, retry bool, err error) {
	var reader io.Reader
	if body != nil {
		b, mErr := json.Marshal(body)
		if mErr != nil {
			return nil, false, xerrors.Wrap(xerrors.InvalidInput, mErr, "marshal request body")
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, false, xerrors.Wrap(xerrors.InvalidInput, err, "build request")
	}
	if err := c.setHeaders(req, method, path, body != nil, write); err != nil {
		return nil, false, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, xerrors.Wrap(xerrors.NetworkError, err, "request failed")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	captureRateLimit(resp.Header)

	switch resp.StatusCode {
	case http.StatusOK:
		return parseGraphQLEnvelope(respBody)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, false, xerrors.New(xerrors.NotAuthenticated, "x.com returned %d (cookies expired or invalid)", resp.StatusCode)
	case http.StatusTooManyRequests:
		return nil, true, xerrors.New(xerrors.RateLimited, "x.com rate limited (429)")
	case http.StatusNotFound:
		return nil, false, xerrors.New(xerrors.NotFound, "x.com returned 404 for %s", path)
	default:
		return nil, resp.StatusCode >= 500, xerrors.New(xerrors.APIError, "x.com returned %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}
}

// setHeaders applies the full browser-like header set plus a fresh
// x-client-transaction-id signed for this method/path.
func (c *Client) setHeaders(req *http.Request, method, path string, hasBody, write bool) error {
	txid, err := c.tx.TransactionID(method, path)
	if err != nil {
		// transaction id is best-effort; reads often succeed without it, but
		// writes almost always need it, so fail writes loudly.
		if write {
			return xerrors.Wrap(xerrors.APIError, err, "generate x-client-transaction-id")
		}
		txid = ""
	}

	h := req.Header
	h.Set("Authorization", "Bearer "+bearerToken)
	h.Set("Cookie", c.account.Cookie())
	h.Set("X-Csrf-Token", c.account.CT0)
	h.Set("X-Twitter-Active-User", "yes")
	h.Set("X-Twitter-Auth-Type", "OAuth2Session")
	h.Set("X-Twitter-Client-Language", c.locale)
	h.Set("User-Agent", c.userAgent)
	h.Set("Origin", "https://x.com")
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", c.locale+",en;q=0.8")
	h.Set("Sec-Fetch-Dest", "empty")
	h.Set("Sec-Fetch-Mode", "cors")
	h.Set("Sec-Fetch-Site", "same-origin")
	if txid != "" {
		h.Set("X-Client-Transaction-Id", txid)
	}
	if hasBody {
		h.Set("Content-Type", "application/json")
	}
	if write {
		h.Set("Referer", "https://x.com/compose/post")
		h.Set("Priority", "u=1, i")
	} else {
		h.Set("Referer", "https://x.com/")
	}
	return nil
}

func (c *Client) backoff(attempt int) time.Duration {
	d := c.baseDelay * time.Duration(1<<uint(attempt-1))
	return d + time.Duration(rand.Intn(2000))*time.Millisecond
}

// --- rate limiting ---

// RateLimit is a snapshot of an endpoint's x-rate-limit-* headers.
type RateLimit struct {
	Limit     int   `json:"limit"`
	Remaining int   `json:"remaining"`
	ResetAt   int64 `json:"resetAt"` // unix seconds
}

// lastRateLimit is the most recently observed rate-limit snapshot for the whole
// process. The CLI runs one command per process, so this is a simple, lock-light
// way to surface rate-limit info without threading it through every call.
var lastRateLimit atomic.Value // stores RateLimit

// LastRateLimit returns the most recently observed rate-limit snapshot, or nil
// if no rate-limited response has been seen yet.
func LastRateLimit() *RateLimit {
	if v, ok := lastRateLimit.Load().(RateLimit); ok && v.Limit > 0 {
		return &v
	}
	return nil
}

func captureRateLimit(h http.Header) {
	lim := h.Get("x-rate-limit-limit")
	if lim == "" {
		return
	}
	rl := RateLimit{
		Limit:     atoiSafe(lim),
		Remaining: atoiSafe(h.Get("x-rate-limit-remaining")),
		ResetAt:   int64(atoiSafe(h.Get("x-rate-limit-reset"))),
	}
	lastRateLimit.Store(rl)
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// --- REST (v1.1) helpers ---

func (c *Client) restGet(ctx context.Context, path string, q url.Values) ([]byte, error) {
	full := restBase + path
	if len(q) > 0 {
		full += "?" + q.Encode()
	}
	return c.doRaw(ctx, http.MethodGet, full, path, nil, "", false)
}

func (c *Client) restPostForm(ctx context.Context, path string, form url.Values) ([]byte, error) {
	return c.doRaw(ctx, http.MethodPost, restBase+path, path, []byte(form.Encode()), "application/x-www-form-urlencoded", true)
}

func (c *Client) restPostJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, xerrors.Wrap(xerrors.InvalidInput, err, "marshal request")
	}
	return c.doRaw(ctx, http.MethodPost, restBase+path, path, b, "application/json", true)
}

// doRaw runs a non-GraphQL request with retry/backoff and returns the raw body.
func (c *Client) doRaw(ctx context.Context, method, fullURL, path string, body []byte, contentType string, write bool) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			sleep(ctx, c.backoff(attempt))
		}
		raw, retry, err := c.rawAttempt(ctx, method, fullURL, path, body, contentType, write)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) rawAttempt(ctx context.Context, method, fullURL, path string, body []byte, contentType string, write bool) ([]byte, bool, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, false, xerrors.Wrap(xerrors.InvalidInput, err, "build request")
	}
	if err := c.setHeaders(req, method, path, body != nil, write); err != nil {
		return nil, false, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, xerrors.Wrap(xerrors.NetworkError, err, "request failed")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	captureRateLimit(resp.Header)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		// 202 Accepted is used by media upload INIT/APPEND.
		return respBody, false, nil
	case http.StatusUnauthorized:
		return nil, false, xerrors.New(xerrors.NotAuthenticated, "x.com returned 401: %s", restErrorMessage(respBody))
	case http.StatusForbidden:
		// 403 on a REST call with valid cookies is a permission/policy error
		// (e.g. DMing a non-follower), not an authentication failure.
		return nil, false, xerrors.New(xerrors.APIError, "x.com forbidden: %s", restErrorMessage(respBody))
	case http.StatusTooManyRequests:
		return nil, true, xerrors.New(xerrors.RateLimited, "x.com rate limited (429)")
	case http.StatusNotFound:
		return nil, false, xerrors.New(xerrors.NotFound, "x.com returned 404 for %s", path)
	default:
		return nil, resp.StatusCode >= 500, xerrors.New(xerrors.APIError, "x.com returned %d: %s", resp.StatusCode, restErrorMessage(respBody))
	}
}

// DownloadMedia fetches the bytes of an auth-gated media URL (e.g. a DM
// attachment on ton.twitter.com / video.twimg.com) using the account's cookies,
// returning the body and its Content-Type.
func (c *Client) DownloadMedia(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", xerrors.Wrap(xerrors.InvalidInput, err, "bad media url")
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Cookie", c.account.Cookie())
	req.Header.Set("X-Csrf-Token", c.account.CT0)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Referer", "https://x.com/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", xerrors.Wrap(xerrors.NetworkError, err, "download failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	captureRateLimit(resp.Header)
	if resp.StatusCode != http.StatusOK {
		return nil, "", xerrors.New(xerrors.APIError, "media download returned %d", resp.StatusCode)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// restErrorMessage extracts the message from a v1.1 {"errors":[{...}]} body,
// falling back to a truncated raw body.
func restErrorMessage(body []byte) string {
	var e struct {
		Errors []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 {
		return e.Errors[0].Message
	}
	return truncate(string(body), 200)
}

// --- helpers ---

func cloneStrMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func stringsTrim(s string) string { return strings.TrimSpace(s) }
