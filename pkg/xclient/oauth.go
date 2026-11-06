package xclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// OAuth2 consent endpoint (X web client's approval API). The browser consent
// page (/i/oauth2/authorize) is only HTML — the actual approve decision goes
// through this API with the session cookies. Rewriting the web path to the API
// path is required: the web path bot-checks and returns 403 to HTTP clients.
const oauth2AuthorizeURL = "https://api.x.com/2/oauth2/authorize"

var webAuthorizePath = regexp.MustCompile(`^https://(?:twitter\.com|x\.com)/i/oauth2/authorize`)

// OAuthConsent is the metadata x.com returns about the requesting app.
type OAuthConsent struct {
	AuthCode    string           `json:"auth_code"`
	AppName     string           `json:"app_name"`
	AppDesc     string           `json:"app_description"`
	AppURI      string           `json:"app_uri"`
	AppIcon     string           `json:"app_image_uri"`
	Scopes      []OAuthScopeInfo `json:"scopes"`
	RedirectURI string           `json:"-"`
}

// OAuthScopeInfo describes one requested scope.
type OAuthScopeInfo struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// OAuthApproval is the result of approving (or inspecting) an authorization.
type OAuthApproval struct {
	// RedirectURI is the third-party app's callback URL with the final OAuth
	// code attached — the app (or caller) must GET it to finish the sign-in.
	RedirectURI string           `json:"redirect_uri"`
	AppName     string           `json:"app"`
	Scopes      []string         `json:"scopes"`
	Consent     *OAuthConsent    `json:"consent,omitempty"`
}

// OAuthAuthorize starts an OAuth2 authorization for the account's session and
// returns the app metadata plus the consent code — WITHOUT approving yet.
// authorizeURL is the app's web authorize URL (twitter.com/x.com/i/oauth2/authorize?...)
// or an api.x.com/2/oauth2/authorize URL directly; query params are preserved.
func (c *Client) OAuthAuthorize(ctx context.Context, authorizeURL string) (*OAuthConsent, error) {
	ep := normalizeOAuthURL(authorizeURL)
	body, err := c.oauthCall(ctx, http.MethodGet, ep, "")
	if err != nil {
		return nil, err
	}
	var consent OAuthConsent
	if err := json.Unmarshal(body, &consent); err != nil {
		return nil, xerrors.Wrap(xerrors.APIError, err, "parse authorize response")
	}
	if consent.AuthCode == "" {
		return nil, xerrors.New(xerrors.APIError, "no auth_code in authorize response: %.200s", body)
	}
	return &consent, nil
}

// OAuthApprove auto-approves the consent (the equivalent of clicking
// "Authorize app") and returns the app callback URL with the final code.
// When inspect is true, nothing is approved — only the app/scopes metadata
// is returned so a caller can show what it would consent to.
func (c *Client) OAuthApprove(ctx context.Context, authorizeURL string, inspect bool) (*OAuthApproval, error) {
	consent, err := c.OAuthAuthorize(ctx, authorizeURL)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(consent.Scopes))
	for _, s := range consent.Scopes {
		names = append(names, s.Name)
	}
	out := &OAuthApproval{AppName: consent.AppName, Scopes: names, Consent: consent}
	if inspect {
		return out, nil
	}
	form := url.Values{"approval": {"true"}, "code": {consent.AuthCode}, "consent_flow": {"web_consent"}}
	body, err := c.oauthCall(ctx, http.MethodPost, oauth2AuthorizeURL, form.Encode())
	if err != nil {
		return nil, err
	}
	var resp struct {
		RedirectURI string `json:"redirect_uri"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, xerrors.Wrap(xerrors.APIError, err, "parse approve response")
	}
	if resp.RedirectURI == "" {
		return nil, xerrors.New(xerrors.APIError, "no redirect_uri in approve response: %.200s", body)
	}
	out.RedirectURI = resp.RedirectURI
	out.Consent = nil
	return out, nil
}

// normalizeOAuthURL rewrites the web consent URL to the JSON API endpoint,
// keeping the query string intact.
func normalizeOAuthURL(raw string) string {
	if webAuthorizePath.MatchString(raw) {
		if i := strings.Index(raw, "?"); i >= 0 {
			return oauth2AuthorizeURL + raw[i:]
		}
		return oauth2AuthorizeURL
	}
	return raw
}

// oauthCall performs one authorized call to the OAuth2 consent API. The
// endpoint returns plain JSON (not the GraphQL envelope), so it bypasses do().
func (c *Client) oauthCall(ctx context.Context, method, fullURL, formBody string) ([]byte, error) {
	var reader io.Reader
	if formBody != "" {
		reader = strings.NewReader(formBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, xerrors.Wrap(xerrors.InvalidInput, err, "build oauth request")
	}
	h := req.Header
	h.Set("Authorization", "Bearer "+bearerToken)
	h.Set("Cookie", c.account.Cookie())
	h.Set("X-Csrf-Token", c.account.CT0)
	h.Set("X-Twitter-Auth-Type", "OAuth2Session")
	h.Set("X-Twitter-Active-User", "yes")
	h.Set("X-Twitter-Client-Language", c.locale)
	h.Set("User-Agent", c.userAgent)
	h.Set("Origin", "https://x.com")
	h.Set("Referer", "https://x.com/")
	h.Set("Accept", "*/*")
	if formBody != "" {
		h.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, xerrors.Wrap(xerrors.NetworkError, err, "oauth request failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	captureRateLimit(resp.Header)

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusBadRequest:
		return nil, xerrors.New(xerrors.InvalidInput, "oauth endpoint rejected request (400): %.200s — usually a wrong/missing redirect_uri, client_id, or PKCE challenge in the authorize URL", body)
	case http.StatusUnauthorized, http.StatusForbidden:
		// 403 here is usually x.com's bot-check on a too-fast retry — the
		// caller sees a clean error and can back off.
		return nil, xerrors.New(xerrors.NotAuthenticated, "oauth endpoint returned %d (cookies expired, or rate/bot-limit — retry in a minute)", resp.StatusCode)
	case http.StatusTooManyRequests:
		return nil, xerrors.New(xerrors.RateLimited, "oauth endpoint rate limited (429)")
	default:
		return nil, xerrors.New(xerrors.APIError, "oauth endpoint returned %d: %.200s", resp.StatusCode, body)
	}
}

// String renders a compact human-readable summary for messages.
func (a *OAuthApproval) String() string {
	return fmt.Sprintf("%s (scopes: %s)", a.AppName, strings.Join(a.Scopes, ", "))
}
