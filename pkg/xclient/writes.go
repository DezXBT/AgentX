package xclient

import (
	"context"
	"net/url"
	"unicode/utf8"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// PostOptions configures a tweet creation.
type PostOptions struct {
	// ReplyToID, when set, makes this a reply.
	ReplyToID string
	// QuoteTweetID, when set, makes this a quote tweet.
	QuoteTweetID string
	// MediaIDs are pre-uploaded media ids (see UploadMedia) to attach.
	MediaIDs []string
	// CardURI, when set (e.g. a poll card), attaches a card to the tweet.
	CardURI string
	// EditTweetID, when set, edits that existing tweet (Premium-gated).
	EditTweetID string
}

// Post creates a tweet, reply, or quote and returns the created tweet.
func (c *Client) Post(ctx context.Context, text string, opts PostOptions) (*models.Tweet, error) {
	if stringsTrim(text) == "" && opts.QuoteTweetID == "" && len(opts.MediaIDs) == 0 && opts.CardURI == "" {
		return nil, xerrors.New(xerrors.InvalidInput, "tweet has no text or media")
	}
	mediaEntities := make([]any, 0, len(opts.MediaIDs))
	for _, id := range opts.MediaIDs {
		mediaEntities = append(mediaEntities, map[string]any{"media_id": id, "tagged_users": []any{}})
	}
	vars := map[string]any{
		"tweet_text":   text,
		"dark_request": false,
		"media": map[string]any{
			"media_entities":     mediaEntities,
			"possibly_sensitive": false,
		},
		"semantic_annotation_ids": []any{},
	}
	if opts.ReplyToID != "" {
		vars["reply"] = map[string]any{
			"in_reply_to_tweet_id":   opts.ReplyToID,
			"exclude_reply_user_ids": []any{},
		}
	}
	if opts.QuoteTweetID != "" {
		// x.com rejects the /i/web/status/ permalink form for quote attachments
		// ("attachment_url parameter is invalid (44)"); the /i/status/ form is
		// accepted and needs no screen-name lookup.
		vars["attachment_url"] = "https://x.com/i/status/" + opts.QuoteTweetID
	}
	if opts.CardURI != "" {
		vars["card_uri"] = opts.CardURI
	}
	if opts.EditTweetID != "" {
		vars["edit_options"] = map[string]any{"previous_tweet_id": opts.EditTweetID}
	}

	// Text over the 280-char limit goes through the long-form ("note") tweet
	// mutation, which carries the full body and returns under a different key.
	// (Posting a note tweet requires a Premium account; x.com rejects it
	// otherwise and the error is surfaced.)
	op, resultKey := "CreateTweet", "create_tweet"
	if utf8.RuneCountInString(text) > 280 {
		op, resultKey = "CreateNoteTweet", "notetweet_create"
		vars["richtext_options"] = map[string]any{"richtext_tags": []any{}}
	}

	data, err := c.graphqlPOST(ctx, op, vars, nil, true)
	if err != nil {
		return nil, err
	}
	result := deepGet(decode(data), resultKey, "tweet_results", "result")
	t := parseTweetResult(result)
	if t == nil || t.ID == "" {
		return nil, xerrors.New(xerrors.APIError, "tweet not created: unexpected response from x.com")
	}
	return t, nil
}

// Delete removes one of the authenticated user's tweets.
func (c *Client) Delete(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "DeleteTweet", map[string]any{
		"tweet_id": tweetID, "dark_request": false,
	})
}

// Like favourites a tweet.
func (c *Client) Like(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "FavoriteTweet", map[string]any{"tweet_id": tweetID})
}

// Unlike removes a favourite.
func (c *Client) Unlike(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "UnfavoriteTweet", map[string]any{"tweet_id": tweetID})
}

// Retweet reposts a tweet.
func (c *Client) Retweet(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "CreateRetweet", map[string]any{
		"tweet_id": tweetID, "dark_request": false,
	})
}

// PinTweet pins one of the authenticated account's own tweets to its profile.
func (c *Client) PinTweet(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "PinTweet", map[string]any{"tweet_id": tweetID})
}

// UnpinTweet removes the pinned tweet from the authenticated account's profile.
func (c *Client) UnpinTweet(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "UnpinTweet", map[string]any{"tweet_id": tweetID})
}

// Unretweet undoes a repost.
func (c *Client) Unretweet(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "DeleteRetweet", map[string]any{
		"source_tweet_id": tweetID, "dark_request": false,
	})
}

// Bookmark saves a tweet.
func (c *Client) Bookmark(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "CreateBookmark", map[string]any{"tweet_id": tweetID})
}

// Unbookmark removes a saved tweet.
func (c *Client) Unbookmark(ctx context.Context, tweetID string) error {
	return c.simpleWrite(ctx, "DeleteBookmark", map[string]any{"tweet_id": tweetID})
}

func (c *Client) simpleWrite(ctx context.Context, op string, vars map[string]any) error {
	_, err := c.graphqlPOST(ctx, op, vars, nil, true)
	return err
}

// --- social graph (v1.1 REST) ---

// Follow / Unfollow / Mute / Unmute / Block / Unblock act on another user by
// @handle via the legacy v1.1 endpoints, which accept screen_name directly.
func (c *Client) Follow(ctx context.Context, screenName string) error {
	return c.friendship(ctx, "/1.1/friendships/create.json", screenName)
}
func (c *Client) Unfollow(ctx context.Context, screenName string) error {
	return c.friendship(ctx, "/1.1/friendships/destroy.json", screenName)
}
func (c *Client) Mute(ctx context.Context, screenName string) error {
	return c.friendship(ctx, "/1.1/mutes/users/create.json", screenName)
}
func (c *Client) Unmute(ctx context.Context, screenName string) error {
	return c.friendship(ctx, "/1.1/mutes/users/destroy.json", screenName)
}
func (c *Client) Block(ctx context.Context, screenName string) error {
	return c.friendship(ctx, "/1.1/blocks/create.json", screenName)
}
func (c *Client) Unblock(ctx context.Context, screenName string) error {
	return c.friendship(ctx, "/1.1/blocks/destroy.json", screenName)
}

func (c *Client) friendship(ctx context.Context, path, screenName string) error {
	if screenName == "" {
		return xerrors.New(xerrors.InvalidInput, "screen name is required")
	}
	_, err := c.restPostForm(ctx, path, url.Values{"screen_name": {screenName}})
	return err
}
