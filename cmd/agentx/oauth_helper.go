package main

import (
	"context"
	"net/http"
	"time"

	"github.com/dezxbt/agentx/pkg/account"
	"github.com/dezxbt/agentx/pkg/xclient"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// oneShotClient builds a throwaway xclient from inline cookies (for
// `oauth approve --auth-token T --ct0 C` without a stored account).
func oneShotClient(authToken, ct0 string) (*xclient.Client, error) {
	acc := &account.Account{Name: "__oneshot__", AuthToken: authToken, CT0: ct0}
	return xclient.New(acc)
}

// plainHTTP is used only for `oauth callback` — a GET to the *third-party*
// app's callback URL. It never carries X credentials.
var plainHTTP = &http.Client{Timeout: 45 * time.Second}

func newPlainGet(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, xerrors.Wrap(xerrors.InvalidInput, err, "bad callback url")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	return req, nil
}
