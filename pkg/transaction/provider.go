package transaction

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultUserAgent matches a recent desktop Chrome, consistent with the headers
// xclient sends on API calls.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36"

// Provider fetches and caches the data needed to build a Generator (the x.com
// home page and its ondemand.s file), refreshing it after TTL. The verification
// and animation keys rotate infrequently, so a long TTL keeps requests cheap
// while still looking like a live browser session.
type Provider struct {
	httpClient *http.Client
	userAgent  string
	ttl        time.Duration

	mu        sync.Mutex
	gen       *Generator
	fetchedAt time.Time
}

// ProviderOption customises a Provider.
type ProviderOption func(*Provider)

// WithHTTPClient sets the HTTP client used to fetch x.com (share this with the
// API client so both present the same TLS fingerprint).
func WithHTTPClient(c *http.Client) ProviderOption {
	return func(p *Provider) { p.httpClient = c }
}

// WithUserAgent sets the User-Agent used when fetching x.com.
func WithUserAgent(ua string) ProviderOption {
	return func(p *Provider) { p.userAgent = ua }
}

// WithTTL sets how long fetched home/ondemand data is reused (default 3h).
func WithTTL(d time.Duration) ProviderOption {
	return func(p *Provider) { p.ttl = d }
}

// NewProvider creates a Provider with sensible defaults.
func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  DefaultUserAgent,
		ttl:        3 * time.Hour,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// TransactionID returns a fresh x-client-transaction-id for the method/path,
// refreshing the underlying keys if the cache has expired.
func (p *Provider) TransactionID(method, path string) (string, error) {
	g, err := p.generator()
	if err != nil {
		return "", err
	}
	return g.GenerateTransactionID(method, path), nil
}

func (p *Provider) generator() (*Generator, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gen != nil && time.Since(p.fetchedAt) < p.ttl {
		return p.gen, nil
	}
	if err := p.refreshLocked(); err != nil {
		if p.gen != nil {
			return p.gen, nil // fall back to stale keys rather than failing the request
		}
		return nil, err
	}
	return p.gen, nil
}

func (p *Provider) refreshLocked() error {
	homeHTML, err := p.fetch("https://x.com")
	if err != nil {
		return fmt.Errorf("transaction: fetch home page: %w", err)
	}
	ondemandURL, err := getOndemandFileURL(homeHTML)
	if err != nil {
		return err
	}
	ondemandText, err := p.fetch(ondemandURL)
	if err != nil {
		return fmt.Errorf("transaction: fetch ondemand file: %w", err)
	}
	gen, err := New(homeHTML, ondemandText)
	if err != nil {
		return err
	}
	p.gen = gen
	p.fetchedAt = time.Now()
	return nil
}

func (p *Provider) fetch(rawURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://x.com")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transaction: %s returned status %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
