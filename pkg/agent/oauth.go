package agent

import (
	"context"

	"github.com/dezxbt/agentx/pkg/xclient"
)

// OAuthAuthorize inspects an OAuth2 authorization request on behalf of an
// account: returns the app metadata and requested scopes without approving.
func (a *Agent) OAuthAuthorize(ctx context.Context, accountName, authorizeURL string) (*xclient.OAuthConsent, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.OAuthAuthorize(ctx, authorizeURL)
}

// OAuthApprove auto-approves an OAuth2 consent request (Sign-in-with-X) using
// the account's session cookies — the programmatic equivalent of clicking
// "Authorize app" on the consent screen. With inspect=true nothing is
// approved; the caller only gets the app name and scope list.
func (a *Agent) OAuthApprove(ctx context.Context, accountName, authorizeURL string, inspect bool) (*xclient.OAuthApproval, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.OAuthApprove(ctx, authorizeURL, inspect)
}
