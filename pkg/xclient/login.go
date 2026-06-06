package xclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"time"

	"github.com/dezxbt/agentx/pkg/transaction"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// LoginResult carries the cookies obtained from a successful password login.
type LoginResult struct {
	AuthToken string
	CT0       string
}

// Login performs x.com's onboarding "login" flow with a username/handle and
// password, returning the resulting auth_token and ct0 cookies. It cannot solve
// interactive challenges (2FA codes, e-mail/phone verification) and returns a
// descriptive error when one is required. proxyURL (optional, http(s):// or
// socks5://) routes every request — useful because x.com blocks login from
// datacenter IPs, so a residential proxy is usually required.
func Login(ctx context.Context, username, password, proxyURL string) (*LoginResult, error) {
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{}
	if proxyURL != "" {
		pu, err := url.Parse(proxyURL)
		if err != nil {
			return nil, xerrors.Wrap(xerrors.InvalidInput, err, "parse proxy url")
		}
		tr.Proxy = http.ProxyURL(pu)
	}
	hc := &http.Client{Timeout: 45 * time.Second, Jar: jar, Transport: tr}

	guest, err := activateGuest(ctx, hc)
	if err != nil {
		return nil, err
	}

	flow := &loginFlow{hc: hc, jar: jar, guest: guest, tx: transaction.NewProvider(transaction.WithHTTPClient(hc))}
	resp, err := flow.start(ctx)
	if err != nil {
		return nil, err
	}

	// Drive the subtask state machine until login succeeds or we hit a subtask
	// we cannot answer automatically.
	for range [25]int{} {
		sub := firstSubtaskID(resp)
		if os.Getenv("AGENTX_LOGIN_DEBUG") != "" {
			fmt.Fprintln(os.Stderr, "LOGIN subtask:", sub)
		}
		switch sub {
		case "LoginSuccessSubtask", "":
			if at, ct0 := loginCookies(jar); at != "" {
				return &LoginResult{AuthToken: at, CT0: ct0}, nil
			}
			if sub == "" {
				return nil, xerrors.New(xerrors.NotAuthenticated, "login flow ended without auth cookies")
			}
		case "LoginAcid":
			return nil, xerrors.New(xerrors.NotAuthenticated, "login needs e-mail/phone verification (LoginAcid) — not automatable")
		case "LoginTwoFactorAuthChallenge":
			return nil, xerrors.New(xerrors.NotAuthenticated, "login needs a 2FA code (LoginTwoFactorAuthChallenge) — not automatable")
		case "DenyLoginSubtask":
			return nil, xerrors.New(xerrors.NotAuthenticated, "x.com denied this login attempt")
		}
		input := subtaskInput(sub, username, password)
		if input == nil {
			return nil, xerrors.New(xerrors.NotAuthenticated, "unsupported login subtask %q", sub)
		}
		resp, err = flow.submit(ctx, asString(resp["flow_token"]), input)
		if err != nil {
			return nil, err
		}
	}
	if at, ct0 := loginCookies(jar); at != "" {
		return &LoginResult{AuthToken: at, CT0: ct0}, nil
	}
	return nil, xerrors.New(xerrors.NotAuthenticated, "login did not complete")
}

// subtaskInput builds the subtask_inputs entry for the subtasks we can answer.
func subtaskInput(sub, username, password string) map[string]any {
	switch sub {
	case "LoginJsInstrumentationSubtask":
		return map[string]any{
			"subtask_id":         sub,
			"js_instrumentation": map[string]any{"response": "{}", "link": "next_link"},
		}
	case "LoginEnterUserIdentifierSSO":
		return map[string]any{
			"subtask_id": sub,
			"settings_list": map[string]any{
				"setting_responses": []any{map[string]any{
					"key":           "user_identifier",
					"response_data": map[string]any{"text_data": map[string]any{"result": username}},
				}},
				"link": "next_link",
			},
		}
	case "LoginEnterPassword":
		return map[string]any{
			"subtask_id":     sub,
			"enter_password": map[string]any{"password": password, "link": "next_link"},
		}
	case "AccountDuplicationCheck":
		return map[string]any{
			"subtask_id":              sub,
			"check_logged_in_account": map[string]any{"link": "AccountDuplicationCheck_false"},
		}
	}
	return nil
}

// loginFlow holds the state for a single onboarding flow.
type loginFlow struct {
	hc    *http.Client
	jar   *cookiejar.Jar
	guest string
	tx    *transaction.Provider
}

func (f *loginFlow) start(ctx context.Context) (map[string]any, error) {
	body := map[string]any{
		"input_flow_data": map[string]any{
			"flow_context": map[string]any{
				"debug_overrides": map[string]any{},
				"start_location":  map[string]any{"location": "splash_screen"},
			},
		},
		"subtask_versions": subtaskVersions,
	}
	return f.post(ctx, "https://api.x.com/1.1/onboarding/task.json?flow_name=login", body)
}

func (f *loginFlow) submit(ctx context.Context, flowToken string, input map[string]any) (map[string]any, error) {
	body := map[string]any{"flow_token": flowToken, "subtask_inputs": []any{input}}
	return f.post(ctx, "https://api.x.com/1.1/onboarding/task.json", body)
}

func (f *loginFlow) post(ctx context.Context, fullURL string, body map[string]any) (map[string]any, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(b))
	if err != nil {
		return nil, xerrors.Wrap(xerrors.InvalidInput, err, "build login request")
	}
	req.Header.Set("authorization", "Bearer "+bearerToken)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-guest-token", f.guest)
	req.Header.Set("x-twitter-active-user", "yes")
	req.Header.Set("x-twitter-client-language", "en")
	req.Header.Set("user-agent", transaction.DefaultUserAgent)
	req.Header.Set("origin", "https://x.com")
	req.Header.Set("referer", "https://x.com/")
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "en;q=0.9")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	// Sign with x-client-transaction-id (path only, no query) — x.com's
	// onboarding flags unsigned automated requests with a generic 399.
	if u, perr := url.Parse(fullURL); perr == nil {
		if txid, terr := f.tx.TransactionID(http.MethodPost, u.Path); terr == nil && txid != "" {
			req.Header.Set("x-client-transaction-id", txid)
		}
	}
	if ct0 := jarCookie(f.jar, "ct0"); ct0 != "" {
		req.Header.Set("x-csrf-token", ct0)
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, xerrors.Wrap(xerrors.NetworkError, err, "login request failed")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, xerrors.New(xerrors.NotAuthenticated, "x.com login returned %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil, xerrors.New(xerrors.APIError, "could not parse login response")
	}
	return out, nil
}

// activateGuest fetches a guest token for the unauthenticated flow.
func activateGuest(ctx context.Context, hc *http.Client) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.x.com/1.1/guest/activate.json", nil)
	req.Header.Set("authorization", "Bearer "+bearerToken)
	req.Header.Set("user-agent", transaction.DefaultUserAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return "", xerrors.Wrap(xerrors.NetworkError, err, "guest activate failed")
	}
	defer resp.Body.Close()
	var out struct {
		GuestToken string `json:"guest_token"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(raw, &out) != nil || out.GuestToken == "" {
		return "", xerrors.New(xerrors.APIError, "could not obtain guest token")
	}
	return out.GuestToken, nil
}

func firstSubtaskID(resp map[string]any) string {
	subs := asSlice(resp["subtasks"])
	if len(subs) == 0 {
		return ""
	}
	return asString(asMap(subs[0])["subtask_id"])
}

func loginCookies(jar *cookiejar.Jar) (authToken, ct0 string) {
	return jarCookie(jar, "auth_token"), jarCookie(jar, "ct0")
}

func jarCookie(jar *cookiejar.Jar, name string) string {
	for _, host := range []string{"https://x.com", "https://api.x.com", "https://twitter.com"} {
		u, _ := url.Parse(host)
		for _, c := range jar.Cookies(u) {
			if c.Name == name {
				return c.Value
			}
		}
	}
	return ""
}

// subtaskVersions is the version map x.com expects in the flow-start request.
var subtaskVersions = map[string]any{
	"action_list": 2, "alert_dialog": 1, "app_download_cta": 1, "check_logged_in_account": 1,
	"choice_selection": 3, "contacts_live_sync_permission_prompt": 0, "cta": 7,
	"email_verification": 2, "end_flow": 1, "enter_date": 1, "enter_email": 2, "enter_password": 5,
	"enter_phone": 2, "enter_recaptcha": 1, "enter_text": 5, "enter_username": 2,
	"generic_urt": 3, "in_app_notification": 1, "interest_picker": 3, "js_instrumentation": 1,
	"menu_dialog": 1, "notifications_permission_prompt": 2, "open_account": 2, "open_home_timeline": 1,
	"open_link": 1, "phone_verification": 4, "privacy_options": 1, "security_key": 3,
	"select_avatar": 4, "select_banner": 2, "settings_list": 7, "show_code": 1, "sign_up": 2,
	"sign_up_review": 4, "tweet_selection_urt": 1, "update_users": 1, "upload_media": 1,
	"user_recommendations_list": 4, "user_recommendations_urt": 1, "wait_spinner": 3, "web_modal": 1,
}
