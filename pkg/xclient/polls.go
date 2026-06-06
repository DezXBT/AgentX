package xclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// capsBase hosts the cards API used to build poll cards.
const capsBase = "https://caps.x.com"

// PostPoll creates a poll card (2–4 text choices, duration in minutes) and posts
// a tweet carrying it.
func (c *Client) PostPoll(ctx context.Context, text string, choices []string, durationMinutes int) (*models.Tweet, error) {
	if len(choices) < 2 || len(choices) > 4 {
		return nil, xerrors.New(xerrors.InvalidInput, "a poll needs 2 to 4 choices")
	}
	if durationMinutes <= 0 {
		durationMinutes = 1440 // 1 day, the x.com default
	}
	uri, err := c.createPollCard(ctx, choices, durationMinutes)
	if err != nil {
		return nil, err
	}
	return c.Post(ctx, text, PostOptions{CardURI: uri})
}

// VotePoll casts a vote (choice 1..N) on the poll attached to a tweet. It first
// reads the tweet to recover the poll card's uri and name, then submits the vote
// through the cards passthrough endpoint.
func (c *Client) VotePoll(ctx context.Context, tweetID string, choice int) error {
	vars := map[string]any{
		"tweetId":                tweetID,
		"withCommunity":          false,
		"includePromotedContent": false,
		"withVoice":              false,
	}
	data, err := c.graphqlGET(ctx, "TweetResultByRestId", vars, nil, nil)
	if err != nil {
		return err
	}
	tw := unwrapTweet(deepGet(decode(data), "tweetResult", "result"))
	card := asMap(deepGet(tw, "card", "legacy"))
	cardName := asString(card["name"])
	cardURI := asString(card["url"])
	if cardURI == "" {
		cardURI = asString(deepGet(tw, "card", "rest_id"))
	}
	if cardURI == "" || !strings.Contains(cardName, "poll") {
		return xerrors.New(xerrors.InvalidInput, "tweet %s has no poll to vote on", tweetID)
	}
	form := url.Values{
		"twitter:string:card_uri":           {cardURI},
		"twitter:long:original_tweet_id":    {tweetID},
		"twitter:string:response_card_name": {cardName},
		"twitter:string:cards_platform":     {"Web-12"},
		"twitter:string:selected_choice":    {strconv.Itoa(choice)},
	}
	const path = "/v2/capi/passthrough/1"
	_, err = c.doRaw(ctx, http.MethodPost, capsBase+path, path, []byte(form.Encode()), "application/x-www-form-urlencoded", true)
	return err
}

// createPollCard builds a poll card via the cards API and returns its card_uri.
func (c *Client) createPollCard(ctx context.Context, choices []string, durationMinutes int) (string, error) {
	cardData := map[string]any{
		"twitter:card":                  fmt.Sprintf("poll%dchoice_text_only", len(choices)),
		"twitter:api:api:endpoint":      "1",
		"twitter:long:duration_minutes": durationMinutes,
	}
	for i, ch := range choices {
		cardData[fmt.Sprintf("twitter:string:choice%d_label", i+1)] = ch
	}
	cd, err := json.Marshal(cardData)
	if err != nil {
		return "", xerrors.Wrap(xerrors.InvalidInput, err, "marshal poll card")
	}
	const path = "/v2/cards/create.json"
	form := url.Values{"card_data": {string(cd)}}
	body, err := c.doRaw(ctx, http.MethodPost, capsBase+path, path, []byte(form.Encode()), "application/x-www-form-urlencoded", true)
	if err != nil {
		return "", err
	}
	var resp struct {
		CardURI string `json:"card_uri"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.CardURI == "" {
		return "", xerrors.New(xerrors.APIError, "poll card not created: unexpected response")
	}
	return resp.CardURI, nil
}
