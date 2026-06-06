package xclient

import (
	"context"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// Draft is an unsent draft tweet.
type Draft struct {
	ID   string `json:"id"`
	Text string `json:"text,omitempty"`
}

// CreateDraft saves a draft tweet and returns its id.
func (c *Client) CreateDraft(ctx context.Context, text string, mediaIDs []string) (string, error) {
	req := map[string]any{
		"auto_populate_reply_metadata": false,
		"status":                       text,
		"exclude_reply_user_ids":       []any{},
	}
	if len(mediaIDs) > 0 {
		ids := make([]any, len(mediaIDs))
		for i, id := range mediaIDs {
			ids[i] = id
		}
		req["media_ids"] = ids
	}
	data, err := c.graphqlPOST(ctx, "CreateDraftTweet", map[string]any{"post_tweet_request": req}, nil, true)
	if err != nil {
		return "", err
	}
	id := asString(findKey(decode(data), "draft_tweet_id"))
	if id == "" {
		id = asString(findKey(decode(data), "rest_id"))
	}
	if id == "" {
		return "", xerrors.New(xerrors.APIError, "draft not created: unexpected response")
	}
	return id, nil
}

// Drafts lists the account's saved draft tweets.
func (c *Client) Drafts(ctx context.Context) ([]Draft, error) {
	data, err := c.graphqlGET(ctx, "FetchDraftTweets", map[string]any{"ascending": true}, nil, nil)
	if err != nil {
		return nil, err
	}
	var out []Draft
	for _, raw := range asSlice(deepGet(decode(data), "viewer", "draft_list", "response_data")) {
		m := asMap(raw)
		if id := asString(m["rest_id"]); id != "" {
			out = append(out, Draft{ID: id, Text: asString(deepGet(m, "tweet_create_request", "status"))})
		}
	}
	return out, nil
}

// DeleteDraft removes a draft tweet by id.
func (c *Client) DeleteDraft(ctx context.Context, draftID string) error {
	return c.simpleWrite(ctx, "DeleteDraftTweet", map[string]any{"draft_tweet_id": draftID})
}
