package xclient

import (
	"context"
	"strconv"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// ScheduledTweet is a tweet queued for future publication.
type ScheduledTweet struct {
	ID        string `json:"id"`
	ExecuteAt string `json:"executeAt,omitempty"` // unix seconds
	State     string `json:"state,omitempty"`     // e.g. "Scheduled"
	Text      string `json:"text,omitempty"`
}

// ScheduleTweet queues a tweet for publication at executeAt (unix seconds) and
// returns the scheduled tweet id. mediaIDs are optional attached media.
func (c *Client) ScheduleTweet(ctx context.Context, text string, executeAt int64, mediaIDs []string) (string, error) {
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
	vars := map[string]any{
		"post_tweet_request": req,
		"execute_at":         executeAt,
	}
	data, err := c.graphqlPOST(ctx, "CreateScheduledTweet", vars, nil, true)
	if err != nil {
		return "", err
	}
	id := asString(deepGet(decode(data), "tweet", "rest_id"))
	if id == "" {
		id = asString(findKey(decode(data), "rest_id"))
	}
	if id == "" {
		return "", xerrors.New(xerrors.APIError, "scheduled tweet not created: unexpected response")
	}
	return id, nil
}

// ScheduledTweets lists the account's pending scheduled tweets, soonest first.
func (c *Client) ScheduledTweets(ctx context.Context) ([]ScheduledTweet, error) {
	data, err := c.graphqlGET(ctx, "FetchScheduledTweets", map[string]any{"ascending": true}, nil, nil)
	if err != nil {
		return nil, err
	}
	var out []ScheduledTweet
	for _, raw := range asSlice(findKey(decode(data), "scheduled_tweet_list")) {
		m := asMap(raw)
		st := ScheduledTweet{
			ID:    asString(m["rest_id"]),
			State: asString(deepGet(m, "scheduling_info", "state")),
			Text:  asString(deepGet(m, "tweet_create_request", "status")),
		}
		// scheduling_info.execute_at is unix milliseconds; expose seconds.
		if ms, ok := deepGet(m, "scheduling_info", "execute_at").(float64); ok {
			st.ExecuteAt = strconv.FormatInt(int64(ms)/1000, 10)
		}
		if st.ID != "" {
			out = append(out, st)
		}
	}
	return out, nil
}

// DeleteScheduledTweet removes a pending scheduled tweet by id.
func (c *Client) DeleteScheduledTweet(ctx context.Context, scheduledTweetID string) error {
	return c.simpleWrite(ctx, "DeleteScheduledTweet", map[string]any{"scheduled_tweet_id": scheduledTweetID})
}
