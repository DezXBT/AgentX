// Package account manages multiple X/Twitter credential sets so a single agentx
// install can drive many accounts. Credentials are cookie-based (auth_token +
// ct0, plus an optional full cookie string for a richer browser fingerprint).
package account

import (
	"fmt"
	"strings"
	"time"
)

// Account holds one X session's credentials and metadata.
type Account struct {
	Name string `json:"name"`

	// AuthToken is the x.com `auth_token` cookie (the actual session secret).
	AuthToken string `json:"authToken"`
	// CT0 is the `ct0` cookie, also sent as the x-csrf-token header.
	CT0 string `json:"ct0"`
	// CookieString is the optional full Cookie header. When present it is sent
	// verbatim, preserving every cookie the browser had (best for longevity).
	CookieString string `json:"cookieString,omitempty"`

	// UserAgent pins the browser UA for this account. Keeping it stable across
	// requests is important for not tripping anti-automation heuristics.
	UserAgent string `json:"userAgent,omitempty"`
	// Proxy is an optional per-account proxy URL (http/https/socks5).
	Proxy string `json:"proxy,omitempty"`

	ScreenName string    `json:"screenName,omitempty"`
	AddedAt    time.Time `json:"addedAt"`
	LastUsed   time.Time `json:"lastUsed,omitempty"`
}

// Validate checks that the minimum required credentials are present.
func (a *Account) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("account name is required")
	}
	if strings.TrimSpace(a.AuthToken) == "" {
		return fmt.Errorf("account %q: auth_token is required", a.Name)
	}
	if strings.TrimSpace(a.CT0) == "" {
		return fmt.Errorf("account %q: ct0 is required", a.Name)
	}
	return nil
}

// Cookie returns the Cookie header value to send for this account. It uses the
// full cookie string when available, otherwise synthesises a minimal one.
func (a *Account) Cookie() string {
	if s := strings.TrimSpace(a.CookieString); s != "" {
		return s
	}
	return fmt.Sprintf("auth_token=%s; ct0=%s", a.AuthToken, a.CT0)
}
