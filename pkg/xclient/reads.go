package xclient

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// Mentions fetches tweets mentioning the authenticated user via the v1.1
// mentions_timeline. Pagination is id-based: the returned NextCursor is the
// max_id for the next (older) page.
func (c *Client) Mentions(ctx context.Context, count int, maxID string) (*models.Page, error) {
	q := url.Values{
		"count":      {strconv.Itoa(clampCount(count))},
		"tweet_mode": {"extended"},
	}
	if maxID != "" {
		q.Set("max_id", maxID)
	}
	body, err := c.restGet(ctx, "/1.1/statuses/mentions_timeline.json", q)
	if err != nil {
		return nil, err
	}
	tweets := parseV11Tweets(body)
	next := ""
	if n := len(tweets); n > 0 {
		next = decStr(tweets[n-1].ID)
	}
	return &models.Page{Tweets: tweets, NextCursor: next}, nil
}

// userByScreenNameFeatures are required by the UserByScreenName operation in
// addition to the defaults.
var userByScreenNameFeatures = map[string]any{
	"hidden_profile_subscriptions_enabled":                         true,
	"subscriptions_verification_info_is_identity_verified_enabled": true,
	"subscriptions_verification_info_verified_since_enabled":       true,
	"highlights_tweets_tab_ui_enabled":                             true,
	"responsive_web_twitter_article_notes_tab_enabled":             true,
	"subscriptions_feature_can_gift_premium":                       true,
}

// Retweeters returns the accounts that retweeted a tweet.
func (c *Client) Retweeters(ctx context.Context, tweetID string, count int, cursor string) (*models.UserPage, error) {
	return c.engagers(ctx, "Retweeters", tweetID, count, cursor)
}

// Favoriters returns the accounts that liked a tweet.
func (c *Client) Favoriters(ctx context.Context, tweetID string, count int, cursor string) (*models.UserPage, error) {
	return c.engagers(ctx, "Favoriters", tweetID, count, cursor)
}

func (c *Client) engagers(ctx context.Context, op, tweetID string, count int, cursor string) (*models.UserPage, error) {
	vars := map[string]any{
		"tweetId":                tweetID,
		"count":                  clampCount(count),
		"includePromotedContent": false,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, op, vars, nil, nil)
	if err != nil {
		return nil, err
	}
	users, cur := extractUserTimeline(decode(data))
	return &models.UserPage{Users: users, NextCursor: cur}, nil
}

// Trend is a single trending topic.
type Trend struct {
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"`
	TweetVolume int    `json:"tweetVolume,omitempty"`
}

// Trends returns the trending topics for a WOEID location (1 = worldwide) via
// the v1.1 trends/place endpoint.
func (c *Client) Trends(ctx context.Context, woeid string) ([]Trend, error) {
	if woeid == "" {
		woeid = "1"
	}
	body, err := c.restGet(ctx, "/1.1/trends/place.json", url.Values{"id": {woeid}})
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Trends []struct {
			Name        string `json:"name"`
			URL         string `json:"url"`
			TweetVolume *int   `json:"tweet_volume"`
		} `json:"trends"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || len(raw) == 0 {
		return nil, xerrors.New(xerrors.APIError, "could not parse trends response")
	}
	out := make([]Trend, 0, len(raw[0].Trends))
	for _, t := range raw[0].Trends {
		tr := Trend{Name: t.Name, URL: t.URL}
		if t.TweetVolume != nil {
			tr.TweetVolume = *t.TweetVolume
		}
		out = append(out, tr)
	}
	return out, nil
}

// UserByRestId resolves a profile by its numeric user id (the counterpart to
// UserByScreenName).
func (c *Client) UserByRestId(ctx context.Context, userID string) (*models.UserProfile, error) {
	vars := map[string]any{
		"userId":                   userID,
		"withSafetyModeUserFields": true,
	}
	data, err := c.graphqlGET(ctx, "UserByRestId", vars, nil, userByScreenNameFeatures)
	if err != nil {
		return nil, err
	}
	u := parseUserResult(deepGet(decode(data), "user", "result"))
	if u == nil || u.ID == "" {
		return nil, xerrors.New(xerrors.NotFound, "user id %q not found", userID)
	}
	return u, nil
}

// UserMedia fetches the photos/videos tab of a user's profile by numeric id.
func (c *Client) UserMedia(ctx context.Context, userID string, count int, cursor string) (*models.Page, error) {
	vars := map[string]any{
		"userId":                 userID,
		"count":                  clampCount(count),
		"includePromotedContent": false,
		"withClientEventToken":   false,
		"withBirdwatchNotes":     false,
		"withVoice":              true,
		"withV2Timeline":         true,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "UserMedia", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// HomeTimeline fetches the authenticated home timeline. When latest is true it
// returns the chronological "Following" feed instead of "For You".
func (c *Client) HomeTimeline(ctx context.Context, latest bool, count int, cursor string) (*models.Page, error) {
	op := "HomeTimeline"
	if latest {
		op = "HomeLatestTimeline"
	}
	vars := map[string]any{
		"count":                  clampCount(count),
		"includePromotedContent": false,
		"latestControlAvailable": true,
		"withCommunity":          true,
		"requestContext":         "launch",
		"seenTweetIds":           []string{},
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, op, vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// UserByScreenName resolves a profile by @handle.
func (c *Client) UserByScreenName(ctx context.Context, screenName string) (*models.UserProfile, error) {
	vars := map[string]any{
		"screen_name":              screenName,
		"withSafetyModeUserFields": true,
	}
	data, err := c.graphqlGET(ctx, "UserByScreenName", vars, nil, userByScreenNameFeatures)
	if err != nil {
		return nil, err
	}
	result := deepGet(decode(data), "user", "result")
	u := parseUserResult(result)
	if u == nil || u.ID == "" {
		return nil, xerrors.New(xerrors.NotFound, "user %q not found", screenName)
	}
	return u, nil
}

// UserTweets fetches a user's posts by numeric user id.
func (c *Client) UserTweets(ctx context.Context, userID string, count int, cursor string) (*models.Page, error) {
	vars := map[string]any{
		"userId":                                 userID,
		"count":                                  clampCount(count),
		"includePromotedContent":                 false,
		"withQuickPromoteEligibilityTweetFields": true,
		"withVoice":                              true,
		"withV2Timeline":                         true,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "UserTweets", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// UserPosts is a convenience wrapper that resolves a @handle then fetches posts.
func (c *Client) UserPosts(ctx context.Context, screenName string, count int, cursor string) (*models.Page, error) {
	u, err := c.UserByScreenName(ctx, screenName)
	if err != nil {
		return nil, err
	}
	return c.UserTweets(ctx, u.ID, count, cursor)
}

// Search runs an advanced search. product is one of Top, Latest, People,
// Photos, Videos.
func (c *Client) Search(ctx context.Context, query, product string, count int, cursor string) (*models.Page, error) {
	if product == "" {
		product = "Top"
	}
	vars := map[string]any{
		"rawQuery":    query,
		"count":       clampCount(count),
		"querySource": "typed_query",
		"product":     product,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "SearchTimeline", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// TweetDetail fetches a tweet and its conversation thread.
func (c *Client) TweetDetail(ctx context.Context, tweetID string) (*models.Page, error) {
	vars := map[string]any{
		"focalTweetId":                           tweetID,
		"with_rux_injections":                    false,
		"includePromotedContent":                 true,
		"withCommunity":                          true,
		"withQuickPromoteEligibilityTweetFields": true,
		"withBirdwatchNotes":                     true,
		"withVoice":                              true,
		"withV2Timeline":                         true,
	}
	toggles := map[string]any{
		"withArticleRichContentState": true,
		"withArticlePlainText":        false,
	}
	data, err := c.graphqlGET(ctx, "TweetDetail", vars, toggles, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// articleFieldToggles request the full rich article content alongside the
// tweet in TweetResultByRestId responses.
var articleFieldToggles = map[string]any{
	"withArticleRichContentState": true,
	"withArticlePlainText":        false,
	"withGrokAnalyze":             false,
	"withDisallowedReplyControls": false,
}

// Article fetches a single tweet by id via TweetResultByRestId and populates
// the X Article fields when the tweet is a long-form article. markdown renders
// the article body as markdown instead of plain text. Non-article tweets are
// returned with empty article fields.
func (c *Client) Article(ctx context.Context, tweetID string, markdown bool) (*models.Tweet, error) {
	vars := map[string]any{
		"tweetId":                tweetID,
		"withCommunity":          false,
		"includePromotedContent": false,
		"withVoice":              false,
	}
	data, err := c.graphqlGET(ctx, "TweetResultByRestId", vars, articleFieldToggles, nil)
	if err != nil {
		return nil, err
	}
	result := deepGet(decode(data), "tweetResult", "result")
	t := parseTweetResult(result)
	if t == nil || t.ID == "" {
		return nil, xerrors.New(xerrors.NotFound, "tweet %q not found", tweetID)
	}
	// parseTweetResult fills the article body as plain text; re-render as
	// markdown on request, working from the same unwrapped tweet object.
	if markdown {
		fillArticle(t, unwrapTweet(result), true)
	}
	return t, nil
}

// bookmarksFeatures are required in addition to the defaults by the Bookmarks
// operation.
var bookmarksFeatures = map[string]any{
	"graphql_timeline_v2_bookmark_timeline": true,
}

// Bookmarks fetches the authenticated user's bookmarked tweets.
func (c *Client) Bookmarks(ctx context.Context, count int, cursor string) (*models.Page, error) {
	vars := map[string]any{
		"count":                  clampCount(count),
		"includePromotedContent": false,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "Bookmarks", vars, nil, bookmarksFeatures)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// Likes fetches the tweets a user has liked, by numeric user id.
func (c *Client) Likes(ctx context.Context, userID string, count int, cursor string) (*models.Page, error) {
	vars := map[string]any{
		"userId":                 userID,
		"count":                  clampCount(count),
		"includePromotedContent": false,
		"withClientEventToken":   false,
		"withVoice":              true,
		"withV2Timeline":         true,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "Likes", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// LikesByScreenName resolves a @handle then fetches that user's likes.
func (c *Client) LikesByScreenName(ctx context.Context, screenName string, count int, cursor string) (*models.Page, error) {
	u, err := c.UserByScreenName(ctx, screenName)
	if err != nil {
		return nil, err
	}
	return c.Likes(ctx, u.ID, count, cursor)
}

// ListTweets fetches the latest tweets of a List by its id.
func (c *Client) ListTweets(ctx context.Context, listID string, count int, cursor string) (*models.Page, error) {
	vars := map[string]any{
		"listId": listID,
		"count":  clampCount(count),
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "ListLatestTweetsTimeline", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// Followers fetches a user's followers by numeric user id.
func (c *Client) Followers(ctx context.Context, userID string, count int, cursor string) (*models.UserPage, error) {
	return c.userTimeline(ctx, "Followers", userID, count, cursor)
}

// Following fetches the accounts a user follows by numeric user id.
func (c *Client) Following(ctx context.Context, userID string, count int, cursor string) (*models.UserPage, error) {
	return c.userTimeline(ctx, "Following", userID, count, cursor)
}

func (c *Client) userTimeline(ctx context.Context, op, userID string, count int, cursor string) (*models.UserPage, error) {
	vars := map[string]any{
		"userId":                 userID,
		"count":                  clampCount(count),
		"includePromotedContent": false,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, op, vars, nil, nil)
	if err != nil {
		return nil, err
	}
	users, cur := extractUserTimeline(decode(data))
	return &models.UserPage{Users: users, NextCursor: cur}, nil
}

// FollowersByScreenName / FollowingByScreenName resolve a @handle first.
func (c *Client) FollowersByScreenName(ctx context.Context, screenName string, count int, cursor string) (*models.UserPage, error) {
	u, err := c.UserByScreenName(ctx, screenName)
	if err != nil {
		return nil, err
	}
	return c.Followers(ctx, u.ID, count, cursor)
}

func (c *Client) FollowingByScreenName(ctx context.Context, screenName string, count int, cursor string) (*models.UserPage, error) {
	u, err := c.UserByScreenName(ctx, screenName)
	if err != nil {
		return nil, err
	}
	return c.Following(ctx, u.ID, count, cursor)
}

// Me returns the authenticated user's own profile. It resolves the logged-in
// screen name via the v1.1 account/settings endpoint, then fetches the full
// profile through the GraphQL UserByScreenName path.
func (c *Client) Me(ctx context.Context) (*models.UserProfile, error) {
	body, err := c.restGet(ctx, "/1.1/account/settings.json", nil)
	if err != nil {
		return nil, err
	}
	var s struct {
		ScreenName string `json:"screen_name"`
	}
	if err := json.Unmarshal(body, &s); err != nil || s.ScreenName == "" {
		return nil, xerrors.New(xerrors.APIError, "could not determine current user")
	}
	return c.UserByScreenName(ctx, s.ScreenName)
}

func pageFrom(data json.RawMessage) *models.Page {
	tweets, cursor := extractTimeline(decode(data))
	return &models.Page{Tweets: tweets, NextCursor: cursor}
}

func clampCount(n int) int {
	if n <= 0 {
		return 40
	}
	if n > 100 {
		return 100
	}
	return n
}
