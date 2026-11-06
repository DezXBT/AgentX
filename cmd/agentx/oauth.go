package main

import (
	"context"
	"os"
	"strings"

	"github.com/dezxbt/agentx/pkg/xclient"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// writeScopes are OAuth scopes that let an app ACT as the account (post,
// follow, DM, read DMs). Approving them hands over account control.
var writeScopes = map[string]string{
	"tweet.write":     "post/retweet/reply as the account",
	"users.write":     "follow/unfollow/mute/block as the account",
	"follows.write":   "follow/unfollow as the account",
	"dm.read":         "read the account's direct messages",
	"dm.write":        "send DMs as the account",
	"bookmark.write":  "modify bookmarks",
	"like.write":      "like/unlike as the account",
}

// persistentScopes keep access alive but cannot act — flagged separately
// from write scopes so read-only apps don't get a false account-control warn.
var persistentScopes = map[string]string{
	"offline.access": "stay connected until revoked (refresh tokens)",
}

// auditScopes splits the requested scopes into safe (read-only),
// account-control (write/DM), and persistent buckets.
func auditScopes(scopes []xclient.OAuthScopeInfo) (safe []string, dangerous map[string]string, persistent []string) {
	dangerous = map[string]string{}
	for _, s := range scopes {
		if reason, ok := writeScopes[s.Name]; ok {
			dangerous[s.Name] = reason
			continue
		}
		if _, ok := persistentScopes[s.Name]; ok {
			persistent = append(persistent, s.Name)
			continue
		}
		safe = append(safe, s.Name)
	}
	return safe, dangerous, persistent
}

// cmdOAuth handles `agentx oauth` — Sign-in-with-X via the account's session
// cookies, no browser needed:
//
//	agentx oauth inspect  <authorize_url>   show app name + scopes (no approval)
//	agentx oauth approve  <authorize_url>   auto-approve, print the callback URL
//	agentx oauth callback <callback_url>    GET the app callback (finishes sign-in)
//
// PERMISSION RULE: approve is a delegated account action. Before approving an
// app that requests ANY write scope, the operator (human) must explicitly
// confirm it — never approve account-control permissions on your own
// initiative. Read-only apps (users.read/tweet.read/follows.read) are safe to
// approve autonomously.
func cmdOAuth(ctx context.Context, args []string) {
	if len(args) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx oauth inspect|approve|callback <url>"))
	}
	sub, rest := args[0], args[1:]
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx oauth %s <authorize_url>", sub))
	}
	targetURL := rest[0]

	var (
		rest3     []string
		authToken string
		ct0       string
	)
	switch sub {
	case "callback":
		// GET the app callback WITH a cookie jar: any Set-Cookie the app
		// issues on login (its own session) is captured and returned, so the
		// agent finishes sign-in without a browser. The jar only ever holds
		// the third-party app's cookies — it never sees X credentials.
		siteCookies, _ := flagValues(rest, "--cookie")
		res2, cbErr := runCallback(ctx, targetURL, siteCookies)
		if cbErr != nil {
			fail(cbErr)
		}
		emit(res2, nil)
		return
	case "inspect", "approve":
		// fall through to the shared path below
	default:
		fail(xerrors.New(xerrors.InvalidInput, "unknown oauth subcommand %q (inspect|approve|callback)", sub))
	}

	// approve/inspect need the cookies of an account. --auth-token/--ct0
	// overrides are supported for one-shot use without a stored account.
	var rest2 []string
	authToken, rest2 = flagValue(rest, "--auth-token")
	ct0, rest3 = flagValue(rest2, "--ct0")
	if authToken == "" {
		authToken = os.Getenv("TWITTER_AUTH_TOKEN")
	}
	if ct0 == "" {
		ct0 = os.Getenv("TWITTER_CT0")
	}

	var (
		res any
		err error
	)
	if authToken != "" && ct0 != "" {
		cl, cErr := oneShotClient(authToken, ct0)
		if cErr != nil {
			fail(cErr)
		}
		if sub == "inspect" {
			res, err = cl.OAuthAuthorize(ctx, targetURL)
		} else {
			res, err = cl.OAuthApprove(ctx, targetURL, false)
		}
	} else {
		if sub == "inspect" {
			res, err = newAgent().OAuthAuthorize(ctx, mustAccount(rest3), targetURL)
		} else {
			res, err = newAgent().OAuthApprove(ctx, mustAccount(rest3), targetURL, false)
		}
	}
	if err != nil {
		fail(err)
	}

	if consent, ok := res.(*xclient.OAuthConsent); ok {
		// inspect: report the app + scope audit, explicitly not approved
		safe, dangerous, persistent := auditScopes(consent.Scopes)
		emit(map[string]any{
			"app":         consent.AppName,
			"description": consent.AppDesc,
			"safeScopes":  safe,
			"writeScopes": dangerous,
			"persistentScopes": persistent,
			"permissionWarning": permissionWarning(dangerous),
			"approved":    false,
		}, nil)
		return
	}

	// approve: attach the same audit to the result so the caller sees what it
	// just consented to (and agents can log it).
	if appr, ok := res.(*xclient.OAuthApproval); ok {
		if appr.Consent != nil {
			safe, dangerous, persistent := auditScopes(appr.Consent.Scopes)
			emit(map[string]any{
				"redirect_uri":      appr.RedirectURI,
				"app":               appr.AppName,
				"safeScopes":        safe,
				"writeScopes":       dangerous,
				"persistentScopes":  persistent,
				"permissionWarning": permissionWarning(dangerous),
				"scopes":            appr.Scopes,
			}, nil)
			return
		}
	}
	emit(res, nil)
}

// permissionWarning renders the account-control warning shown on every
// inspect/approve that includes write scopes.
func permissionWarning(dangerous map[string]string) string {
	if len(dangerous) == 0 {
		return "read-only scopes — safe to approve"
	}
	names := make([]string, 0, len(dangerous))
	for k := range dangerous {
		names = append(names, k)
	}
	return "ACCOUNT-CONTROL SCOPES REQUESTED (" + strings.Join(names, ", ") + "): this app can act as the account. " +
		"Never approve without the human operator's explicit confirmation — malicious apps bundle these to post, follow, or read DMs on the user's behalf."
}

// truncateStr caps a string for envelope output.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// fetchCallback performs a plain GET against the app's callback URL. The
// response body is surfaced for debugging; the app sets its own session
// cookies in the browser normally, so this only proves the code was accepted.
func fetchCallback(ctx context.Context, rawURL string) (string, string, error) {
	if !strings.HasPrefix(rawURL, "http") {
		return "", "", xerrors.New(xerrors.InvalidInput, "callback url must be absolute")
	}
	req, err := newPlainGet(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	resp, err := plainHTTP.Do(req)
	if err != nil {
		return "", "", xerrors.New(xerrors.NetworkError, "callback fetch failed: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 2048)
	for {
		n, rerr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if rerr != nil || len(buf) > 64*1024 {
			break
		}
	}
	return rawURL, string(buf), nil
}
