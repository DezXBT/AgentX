// Package agent is the high-level, agent-friendly facade over agentx. It hides
// account resolution, per-account client caching, the shared transaction
// provider, and zero-auth fallbacks behind a small, stable method set. This is
// the layer most agents should use.
package agent

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dezxbt/agentx/pkg/account"
	"github.com/dezxbt/agentx/pkg/backends/fxtwitter"
	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/transaction"
	"github.com/dezxbt/agentx/pkg/xclient"
)

// Agent coordinates accounts, clients and fallbacks. It is safe for concurrent
// use.
type Agent struct {
	store *account.Store
	tx    *transaction.Provider
	fx    *fxtwitter.Client

	mu      sync.Mutex
	clients map[string]*xclient.Client
}

// New builds an Agent over an account store. The transaction provider (which
// derives the x-client-transaction-id keys from x.com) is shared across all
// accounts because those keys are account-independent.
func New(store *account.Store) *Agent {
	hc := &http.Client{Timeout: 30 * time.Second}
	return &Agent{
		store:   store,
		tx:      transaction.NewProvider(transaction.WithHTTPClient(hc)),
		fx:      fxtwitter.New(hc),
		clients: map[string]*xclient.Client{},
	}
}

// Store exposes the underlying account store for management commands.
func (a *Agent) Store() *account.Store { return a.store }

// client resolves an account (empty => default) and returns a cached client.
func (a *Agent) client(name string) (*xclient.Client, error) {
	acc, err := a.store.Resolve(name)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cl, ok := a.clients[acc.Name]; ok {
		return cl, nil
	}
	opts := []xclient.Option{xclient.WithTransactionProvider(a.tx)}
	if os.Getenv("AGENTX_UTLS") != "" {
		// opt-in Chrome-like TLS fingerprint for authenticated traffic
		opts = append(opts, xclient.WithUTLS())
	}
	cl, err := xclient.New(acc, opts...)
	if err != nil {
		return nil, err
	}
	a.clients[acc.Name] = cl
	return cl, nil
}

// hasAccounts reports whether any account is configured.
func (a *Agent) hasAccounts() bool { return a.store.Default() != "" }

// limitPage trims a page to at most count tweets when a positive count is
// requested. x.com ignores small count hints on timelines and returns a full
// page, so this enforces the caller's requested size. Note: the page cursor
// still points past the full fetched page, so paginating with a small count
// skips the untrimmed remainder.
func limitPage(p *models.Page, count int) *models.Page {
	if p != nil && count > 0 && len(p.Tweets) > count {
		p.Tweets = p.Tweets[:count]
	}
	return p
}

// Feed returns the home timeline for an account.
func (a *Agent) Feed(ctx context.Context, accountName string, latest bool, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.HomeTimeline(ctx, latest, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// Search runs an advanced search for an account.
func (a *Agent) Search(ctx context.Context, accountName, query, product string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.Search(ctx, query, product, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// User resolves a profile. Requires an account.
func (a *Agent) User(ctx context.Context, accountName, screenName string) (*models.UserProfile, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.UserByScreenName(ctx, screenName)
}

// UserPosts fetches a user's tweets. Requires an account.
func (a *Agent) UserPosts(ctx context.Context, accountName, screenName string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.UserPosts(ctx, screenName, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// Thread fetches a tweet plus its conversation. Requires an account.
func (a *Agent) Thread(ctx context.Context, accountName, tweetID string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.TweetDetail(ctx, tweetID)
}

// Bookmarks returns the authenticated user's bookmarks.
func (a *Agent) Bookmarks(ctx context.Context, accountName string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.Bookmarks(ctx, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// Likes returns the tweets a user has liked.
func (a *Agent) Likes(ctx context.Context, accountName, screenName string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.LikesByScreenName(ctx, screenName, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// ListTweets returns the latest tweets of a List.
func (a *Agent) ListTweets(ctx context.Context, accountName, listID string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.ListTweets(ctx, listID, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// CommunityTweets returns the timeline of posts in a Community.
func (a *Agent) CommunityTweets(ctx context.Context, accountName, communityID string, count int, cursor string, latest bool) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.CommunityTweets(ctx, communityID, count, cursor, latest)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// CommunityInfo returns metadata about a Community.
func (a *Agent) CommunityInfo(ctx context.Context, accountName, communityID string) (map[string]any, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.CommunityInfo(ctx, communityID)
}

// JoinCommunity / LeaveCommunity move the authenticated account in/out of a Community.
func (a *Agent) JoinCommunity(ctx context.Context, accountName, communityID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.JoinCommunity(ctx, communityID) })
}

func (a *Agent) LeaveCommunity(ctx context.Context, accountName, communityID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.LeaveCommunity(ctx, communityID) })
}

// CreateCommunity creates a new Community (permanent, not undoable here).
func (a *Agent) CreateCommunity(ctx context.Context, accountName, name string) (map[string]any, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.CreateCommunity(ctx, name)
}

// ListMembers returns the members of a List.
func (a *Agent) ListMembers(ctx context.Context, accountName, listID string, count int, cursor string) (*models.UserPage, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.ListMembers(ctx, listID, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitUserPage(p, count), nil
}

// CreateList creates a List owned by the authenticated account.
func (a *Agent) CreateList(ctx context.Context, accountName, name, description string, private bool) (map[string]any, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.CreateList(ctx, name, description, private)
}

// DeleteList deletes a List owned by the authenticated account.
func (a *Agent) DeleteList(ctx context.Context, accountName, listID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.DeleteList(ctx, listID) })
}

// SubscribeList / UnsubscribeList follow or unfollow a List owned by anyone.
func (a *Agent) SubscribeList(ctx context.Context, accountName, listID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.SubscribeList(ctx, listID) })
}

func (a *Agent) UnsubscribeList(ctx context.Context, accountName, listID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.UnsubscribeList(ctx, listID) })
}

// UserByID resolves a profile by numeric user id.
func (a *Agent) UserByID(ctx context.Context, accountName, userID string) (*models.UserProfile, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.UserByRestId(ctx, userID)
}

// UserMedia returns a user's media tab. The target may be a @handle or numeric id.
func (a *Agent) UserMedia(ctx context.Context, accountName, target string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	userID, err := a.resolveUserID(ctx, cl, target)
	if err != nil {
		return nil, err
	}
	p, err := cl.UserMedia(ctx, userID, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// Trends returns trending topics for a WOEID location (empty = worldwide).
func (a *Agent) Trends(ctx context.Context, accountName, woeid string) ([]xclient.Trend, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.Trends(ctx, woeid)
}

// PinTweet / UnpinTweet manage the account's pinned profile tweet.
func (a *Agent) PinTweet(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.PinTweet(ctx, tweetID) })
}

func (a *Agent) UnpinTweet(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.UnpinTweet(ctx, tweetID) })
}

// AddListMember / RemoveListMember edit a List's membership. The member may be a
// @handle or numeric id.
func (a *Agent) AddListMember(ctx context.Context, accountName, listID, member string) error {
	return a.listMember(ctx, accountName, listID, member, true)
}

func (a *Agent) RemoveListMember(ctx context.Context, accountName, listID, member string) error {
	return a.listMember(ctx, accountName, listID, member, false)
}

func (a *Agent) listMember(ctx context.Context, accountName, listID, member string, add bool) error {
	cl, err := a.client(accountName)
	if err != nil {
		return err
	}
	userID, err := a.resolveUserID(ctx, cl, member)
	if err != nil {
		return err
	}
	if add {
		return cl.AddListMember(ctx, listID, userID)
	}
	return cl.RemoveListMember(ctx, listID, userID)
}

// isNumericID reports whether s is a non-empty all-digit string (a user id).
func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// resolveUserID returns target unchanged if it is already a numeric id,
// otherwise resolves the @handle via UserByScreenName.
func (a *Agent) resolveUserID(ctx context.Context, cl *xclient.Client, target string) (string, error) {
	target = strings.TrimPrefix(target, "@")
	if isNumericID(target) {
		return target, nil
	}
	u, err := cl.UserByScreenName(ctx, target)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// BookmarkFolders lists the account's bookmark folders.
func (a *Agent) BookmarkFolders(ctx context.Context, accountName string) ([]xclient.BookmarkFolder, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.BookmarkFolders(ctx)
}

// BookmarkFolderTweets returns the tweets in a bookmark folder.
func (a *Agent) BookmarkFolderTweets(ctx context.Context, accountName, folderID string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.BookmarkFolderTweets(ctx, folderID, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// CreateBookmarkFolder creates a bookmark folder.
func (a *Agent) CreateBookmarkFolder(ctx context.Context, accountName, name string) (*xclient.BookmarkFolder, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.CreateBookmarkFolder(ctx, name)
}

// DeleteBookmarkFolder deletes a bookmark folder.
func (a *Agent) DeleteBookmarkFolder(ctx context.Context, accountName, folderID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.DeleteBookmarkFolder(ctx, folderID) })
}

// BookmarkToFolder bookmarks a tweet into a specific folder.
func (a *Agent) BookmarkToFolder(ctx context.Context, accountName, tweetID, folderID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.BookmarkToFolder(ctx, tweetID, folderID) })
}

// Retweeters / Favoriters return the accounts that retweeted or liked a tweet.
func (a *Agent) Retweeters(ctx context.Context, accountName, tweetID string, count int, cursor string) (*models.UserPage, error) {
	return a.engagers(ctx, accountName, tweetID, count, cursor, true)
}

func (a *Agent) Favoriters(ctx context.Context, accountName, tweetID string, count int, cursor string) (*models.UserPage, error) {
	return a.engagers(ctx, accountName, tweetID, count, cursor, false)
}

func (a *Agent) engagers(ctx context.Context, accountName, tweetID string, count int, cursor string, rt bool) (*models.UserPage, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	var p *models.UserPage
	if rt {
		p, err = cl.Retweeters(ctx, tweetID, count, cursor)
	} else {
		p, err = cl.Favoriters(ctx, tweetID, count, cursor)
	}
	if err != nil {
		return nil, err
	}
	return limitUserPage(p, count), nil
}

// ScheduleTweet queues a tweet for executeAt (unix seconds).
func (a *Agent) ScheduleTweet(ctx context.Context, accountName, text string, executeAt int64) (string, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return "", err
	}
	return cl.ScheduleTweet(ctx, text, executeAt, nil)
}

// ScheduledTweets lists pending scheduled tweets.
func (a *Agent) ScheduledTweets(ctx context.Context, accountName string) ([]xclient.ScheduledTweet, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.ScheduledTweets(ctx)
}

// DeleteScheduledTweet removes a pending scheduled tweet.
func (a *Agent) DeleteScheduledTweet(ctx context.Context, accountName, id string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.DeleteScheduledTweet(ctx, id) })
}

// Badges returns the account's unread counts.
func (a *Agent) Badges(ctx context.Context, accountName string) (*xclient.BadgeCount, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.Badges(ctx)
}

// Notifications returns the authenticated account's notifications timeline.
func (a *Agent) Notifications(ctx context.Context, accountName, tab string, count int, cursor string) (*xclient.NotificationPage, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.Notifications(ctx, tab, count, cursor)
	if err != nil {
		return nil, err
	}
	if count > 0 && len(p.Items) > count {
		p.Items = p.Items[:count]
	}
	return p, nil
}

// Followers / Following return a user's social graph.
func (a *Agent) Followers(ctx context.Context, accountName, screenName string, count int, cursor string) (*models.UserPage, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.FollowersByScreenName(ctx, screenName, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitUserPage(p, count), nil
}

func (a *Agent) Following(ctx context.Context, accountName, screenName string, count int, cursor string) (*models.UserPage, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.FollowingByScreenName(ctx, screenName, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitUserPage(p, count), nil
}

func limitUserPage(p *models.UserPage, count int) *models.UserPage {
	if p != nil && count > 0 && len(p.Users) > count {
		p.Users = p.Users[:count]
	}
	return p
}

// Tweet fetches a single tweet. With an account it uses the authenticated API;
// without one it transparently falls back to the zero-auth FxTwitter backend.
func (a *Agent) Tweet(ctx context.Context, accountName, tweetID string) (*models.Tweet, error) {
	if a.hasAccounts() || accountName != "" {
		cl, err := a.client(accountName)
		if err == nil {
			if page, derr := cl.TweetDetail(ctx, tweetID); derr == nil {
				for i := range page.Tweets {
					if page.Tweets[i].ID == tweetID {
						return &page.Tweets[i], nil
					}
				}
				if len(page.Tweets) > 0 {
					return &page.Tweets[0], nil
				}
			}
		}
	}
	return a.fx.GetTweet(ctx, tweetID)
}

// TweetAnalytics is a tweet's engagement metrics plus a derived engagement rate.
type TweetAnalytics struct {
	ID             string         `json:"id"`
	Text           string         `json:"text,omitempty"`
	Author         string         `json:"author,omitempty"`
	Metrics        models.Metrics `json:"metrics"`
	Engagements    int            `json:"engagements"`       // likes+RTs+replies+quotes+bookmarks
	EngagementRate float64        `json:"engagementRatePct"` // engagements / views, percent
}

// Analytics fetches a tweet and reports its metrics with a computed engagement
// rate. X does not expose its private analytics (link clicks, profile visits)
// through a public API op, so this surfaces the full set of available counts
// (views/impressions, likes, retweets, replies, quotes, bookmarks).
func (a *Agent) Analytics(ctx context.Context, accountName, tweetID string) (*TweetAnalytics, error) {
	t, err := a.Tweet(ctx, accountName, tweetID)
	if err != nil {
		return nil, err
	}
	m := t.Metrics
	eng := m.Likes + m.Retweets + m.Replies + m.Quotes + m.Bookmarks
	rate := 0.0
	if m.Views > 0 {
		rate = float64(eng) / float64(m.Views) * 100
	}
	return &TweetAnalytics{
		ID: t.ID, Text: t.Text, Author: t.Author.ScreenName,
		Metrics: m, Engagements: eng, EngagementRate: rate,
	}, nil
}

// Article fetches a single tweet by id and populates the X Article fields when
// the tweet is a long-form article. The article endpoint is authenticated, so
// an account is required (no zero-auth fallback). markdown renders the article
// body as markdown instead of plain text.
func (a *Agent) Article(ctx context.Context, accountName, tweetID string, markdown bool) (*models.Tweet, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.Article(ctx, tweetID, markdown)
}

// Mentions returns tweets mentioning the authenticated user. cursor is the v1.1
// max_id for older pages.
func (a *Agent) Mentions(ctx context.Context, accountName string, count int, cursor string) (*models.Page, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	p, err := cl.Mentions(ctx, count, cursor)
	if err != nil {
		return nil, err
	}
	return limitPage(p, count), nil
}

// SendDM sends a direct message to a @handle (resolved to a user id first).
func (a *Agent) SendDM(ctx context.Context, accountName, screenName, text string) error {
	cl, err := a.client(accountName)
	if err != nil {
		return err
	}
	u, err := cl.UserByScreenName(ctx, screenName)
	if err != nil {
		return err
	}
	return cl.SendDM(ctx, u.ID, text)
}

// SendDMMedia sends a media DM to a @handle.
func (a *Agent) SendDMMedia(ctx context.Context, accountName, screenName, text string, data []byte, mimeType string) error {
	cl, err := a.client(accountName)
	if err != nil {
		return err
	}
	u, err := cl.UserByScreenName(ctx, screenName)
	if err != nil {
		return err
	}
	return cl.SendDMMedia(ctx, u.ID, text, data, mimeType)
}

// SetAltText sets accessibility alt text on an uploaded media id.
func (a *Agent) SetAltText(ctx context.Context, accountName, mediaID, text string) error {
	cl, err := a.client(accountName)
	if err != nil {
		return err
	}
	return cl.SetAltText(ctx, mediaID, text)
}

// DirectMessages lists recent DMs for an account.
func (a *Agent) DirectMessages(ctx context.Context, accountName string, count int) ([]xclient.DirectMessage, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.DirectMessages(ctx, count)
}

// DownloadMedia fetches the bytes of an auth-gated media URL for an account.
func (a *Agent) DownloadMedia(ctx context.Context, accountName, url string) ([]byte, string, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, "", err
	}
	return cl.DownloadMedia(ctx, url)
}

// PostThread posts a sequence of tweets as a self-reply chain (the first is a
// root tweet, each subsequent one replies to the previous) and returns them.
func (a *Agent) PostThread(ctx context.Context, accountName string, chunks []string, firstMedia []string) ([]models.Tweet, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	var out []models.Tweet
	replyTo := ""
	for i, c := range chunks {
		opts := xclient.PostOptions{ReplyToID: replyTo}
		if i == 0 {
			opts.MediaIDs = firstMedia
		}
		t, err := cl.Post(ctx, c, opts)
		if err != nil {
			return out, err
		}
		out = append(out, *t)
		replyTo = t.ID
	}
	if len(out) > 0 {
		a.store.Touch(cl.Account().Name, out[0].Author.ScreenName)
	}
	return out, nil
}

// Check verifies an account's cookies are still valid by resolving its own
// profile, returning that profile on success.
func (a *Agent) Check(ctx context.Context, accountName string) (*models.UserProfile, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.Me(ctx)
}

// Me returns the authenticated user's own profile for an account.
func (a *Agent) Me(ctx context.Context, accountName string) (*models.UserProfile, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	u, err := cl.Me(ctx)
	if err == nil && u != nil {
		a.store.Touch(cl.Account().Name, u.ScreenName)
	}
	return u, err
}

// Follow / Unfollow / Mute / Unmute / Block / Unblock act on a @handle.
func (a *Agent) Follow(ctx context.Context, accountName, screenName string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Follow(ctx, screenName) })
}
func (a *Agent) Unfollow(ctx context.Context, accountName, screenName string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Unfollow(ctx, screenName) })
}
func (a *Agent) Mute(ctx context.Context, accountName, screenName string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Mute(ctx, screenName) })
}
func (a *Agent) Unmute(ctx context.Context, accountName, screenName string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Unmute(ctx, screenName) })
}
func (a *Agent) Block(ctx context.Context, accountName, screenName string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Block(ctx, screenName) })
}
func (a *Agent) Unblock(ctx context.Context, accountName, screenName string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Unblock(ctx, screenName) })
}

// Post creates a tweet/reply/quote for an account.
func (a *Agent) Post(ctx context.Context, accountName, text string, opts xclient.PostOptions) (*models.Tweet, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	t, err := cl.Post(ctx, text, opts)
	if err == nil {
		a.store.Touch(cl.Account().Name, t.Author.ScreenName)
	}
	return t, err
}

// EditTweet replaces the text of an existing tweet (Premium-gated).
func (a *Agent) EditTweet(ctx context.Context, accountName, tweetID, text string) (*models.Tweet, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.Post(ctx, text, xclient.PostOptions{EditTweetID: tweetID})
}

// CreateDraft saves a draft tweet and returns its id.
func (a *Agent) CreateDraft(ctx context.Context, accountName, text string) (string, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return "", err
	}
	return cl.CreateDraft(ctx, text, nil)
}

// Drafts lists the account's saved drafts.
func (a *Agent) Drafts(ctx context.Context, accountName string) ([]xclient.Draft, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.Drafts(ctx)
}

// DeleteDraft removes a draft tweet.
func (a *Agent) DeleteDraft(ctx context.Context, accountName, draftID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.DeleteDraft(ctx, draftID) })
}

// GrokAsk sends a prompt to Grok and returns the reply.
func (a *Agent) GrokAsk(ctx context.Context, accountName, prompt string) (string, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return "", err
	}
	return cl.GrokAsk(ctx, prompt)
}

// GrokImage asks Grok to generate an image and returns the attachment URLs.
func (a *Agent) GrokImage(ctx context.Context, accountName, prompt string) ([]string, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.GrokImage(ctx, prompt)
}

// VotePoll votes choice (1..N) on the poll attached to a tweet.
func (a *Agent) VotePoll(ctx context.Context, accountName, tweetID string, choice int) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.VotePoll(ctx, tweetID, choice) })
}

// UpdateProfile edits the account's profile (name, bio, location, url).
func (a *Agent) UpdateProfile(ctx context.Context, accountName string, f xclient.ProfileFields) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.UpdateProfile(ctx, f) })
}

// Settings returns the account's privacy/DM/discovery settings.
func (a *Agent) Settings(ctx context.Context, accountName string) (map[string]any, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.Settings(ctx)
}

// UpdateSettings changes account settings (only the provided keys).
func (a *Agent) UpdateSettings(ctx context.Context, accountName string, fields map[string]string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.UpdateSettings(ctx, fields) })
}

// UpdateAvatar / UpdateBanner set the profile picture / banner from image bytes.
func (a *Agent) UpdateAvatar(ctx context.Context, accountName string, image []byte) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.UpdateAvatar(ctx, image) })
}

func (a *Agent) UpdateBanner(ctx context.Context, accountName string, image []byte) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.UpdateBanner(ctx, image) })
}

// FollowTopic / UnfollowTopic manage followed Topics.
func (a *Agent) FollowTopic(ctx context.Context, accountName, topicID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.FollowTopic(ctx, topicID) })
}

func (a *Agent) UnfollowTopic(ctx context.Context, accountName, topicID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.UnfollowTopic(ctx, topicID) })
}

// TrendLocations lists places (WOEIDs) with trending data.
func (a *Agent) TrendLocations(ctx context.Context, accountName string) ([]xclient.TrendLocation, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.TrendLocations(ctx)
}

// PostPoll posts a tweet with an attached poll (2–4 choices, duration minutes).
func (a *Agent) PostPoll(ctx context.Context, accountName, text string, choices []string, durationMinutes int) (*models.Tweet, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return nil, err
	}
	return cl.PostPoll(ctx, text, choices, durationMinutes)
}

// UploadMedia uploads media bytes for an account and returns the media id to
// attach to a subsequent Post.
func (a *Agent) UploadMedia(ctx context.Context, accountName string, data []byte, mediaType string) (string, error) {
	cl, err := a.client(accountName)
	if err != nil {
		return "", err
	}
	return cl.UploadMedia(ctx, data, mediaType, "")
}

// Like favourites a tweet for an account.
func (a *Agent) Like(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Like(ctx, tweetID) })
}

// Unlike removes a favourite for an account.
func (a *Agent) Unlike(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Unlike(ctx, tweetID) })
}

// Retweet reposts for an account.
func (a *Agent) Retweet(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Retweet(ctx, tweetID) })
}

// Unretweet undoes a repost for an account.
func (a *Agent) Unretweet(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Unretweet(ctx, tweetID) })
}

// Delete removes a tweet for an account.
func (a *Agent) Delete(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Delete(ctx, tweetID) })
}

// Bookmark saves a tweet for an account.
func (a *Agent) Bookmark(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Bookmark(ctx, tweetID) })
}

// Unbookmark removes a saved tweet for an account.
func (a *Agent) Unbookmark(ctx context.Context, accountName, tweetID string) error {
	return a.act(ctx, accountName, func(c *xclient.Client) error { return c.Unbookmark(ctx, tweetID) })
}

func (a *Agent) act(ctx context.Context, accountName string, fn func(*xclient.Client) error) error {
	cl, err := a.client(accountName)
	if err != nil {
		return err
	}
	return fn(cl)
}
