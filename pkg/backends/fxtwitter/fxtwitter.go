// Package fxtwitter is a zero-auth fallback backend for fetching single tweets
// via the public FxTwitter API (https://github.com/FxEmbed/FxEmbed). It needs
// no cookies, so agents can read a tweet even with no account configured.
package fxtwitter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

const apiBase = "https://api.fxtwitter.com"

// Client fetches tweets from FxTwitter.
type Client struct {
	httpClient *http.Client
	userAgent  string
}

// New builds a Client. A nil http client uses a sane default.
func New(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{httpClient: hc, userAgent: "Mozilla/5.0 (compatible; agentx/1.0)"}
}

type apiResponse struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Tweet   *apiTweet `json:"tweet"`
}

type apiTweet struct {
	URL       string    `json:"url"`
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Lang      string    `json:"lang"`
	CreatedAt string    `json:"created_at"`
	Likes     int       `json:"likes"`
	Retweets  int       `json:"retweets"`
	Replies   int       `json:"replies"`
	Views     int       `json:"views"`
	Bookmarks int       `json:"bookmarks"`
	Author    apiAuthor `json:"author"`
	Media     *apiMedia `json:"media"`
	Quote     *apiTweet `json:"quote"`
}

type apiAuthor struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ScreenName string `json:"screen_name"`
	AvatarURL  string `json:"avatar_url"`
}

type apiMedia struct {
	All []apiMediaItem `json:"all"`
}

type apiMediaItem struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// GetTweet fetches a single tweet by id.
func (c *Client) GetTweet(ctx context.Context, id string) (*models.Tweet, error) {
	url := fmt.Sprintf("%s/i/status/%s", apiBase, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, xerrors.Wrap(xerrors.InvalidInput, err, "build request")
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, xerrors.Wrap(xerrors.NetworkError, err, "fxtwitter request failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, xerrors.New(xerrors.NotFound, "tweet %s not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, xerrors.New(xerrors.APIError, "fxtwitter returned %d", resp.StatusCode)
	}

	var out apiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, xerrors.Wrap(xerrors.APIError, err, "decode fxtwitter response")
	}
	if out.Tweet == nil {
		return nil, xerrors.New(xerrors.NotFound, "tweet %s not available", id)
	}
	return convert(out.Tweet), nil
}

func convert(t *apiTweet) *models.Tweet {
	if t == nil {
		return nil
	}
	out := &models.Tweet{
		ID:        t.ID,
		Text:      t.Text,
		CreatedAt: t.CreatedAt,
		Lang:      t.Lang,
		Author: models.Author{
			ID:              t.Author.ID,
			Name:            t.Author.Name,
			ScreenName:      t.Author.ScreenName,
			ProfileImageURL: t.Author.AvatarURL,
		},
		Metrics: models.Metrics{
			Likes:     t.Likes,
			Retweets:  t.Retweets,
			Replies:   t.Replies,
			Views:     t.Views,
			Bookmarks: t.Bookmarks,
		},
		Source: "fxtwitter",
	}
	if t.Media != nil {
		for _, m := range t.Media.All {
			out.Media = append(out.Media, models.Media{
				Type: m.Type, URL: m.URL, Width: m.Width, Height: m.Height,
			})
		}
	}
	out.QuotedTweet = convert(t.Quote)
	return out
}
