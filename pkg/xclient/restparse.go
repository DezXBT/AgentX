package xclient

import (
	"encoding/json"
	"math/big"

	"github.com/dezxbt/agentx/pkg/models"
)

// parseV11Tweets decodes a v1.1 REST tweet array (e.g. mentions_timeline) into
// the shared Tweet model. The v1.1 schema is flatter than GraphQL: counts and
// text live directly on the object and the author is under "user".
func parseV11Tweets(body []byte) []models.Tweet {
	var arr []any
	if json.Unmarshal(body, &arr) != nil {
		return nil
	}
	out := make([]models.Tweet, 0, len(arr))
	for _, raw := range arr {
		if t := parseV11Tweet(asMap(raw)); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

func parseV11Tweet(m map[string]any) *models.Tweet {
	if m == nil {
		return nil
	}
	id := asString(m["id_str"])
	if id == "" {
		return nil
	}
	text := asString(m["full_text"])
	if text == "" {
		text = asString(m["text"])
	}
	t := &models.Tweet{
		ID:        id,
		Text:      text,
		CreatedAt: asString(m["created_at"]),
		Lang:      asString(m["lang"]),
		Metrics: models.Metrics{
			Likes:    asInt(m["favorite_count"]),
			Retweets: asInt(m["retweet_count"]),
			Replies:  asInt(m["reply_count"]),
		},
		Source: "rest_v1",
	}
	if u := asMap(m["user"]); u != nil {
		t.Author = models.Author{
			ID:              asString(u["id_str"]),
			Name:            asString(u["name"]),
			ScreenName:      asString(u["screen_name"]),
			ProfileImageURL: asString(u["profile_image_url_https"]),
			Verified:        asBool(u["verified"]),
		}
	}
	for _, ur := range asSlice(deepGet(m, "entities", "urls")) {
		if e := asString(asMap(ur)["expanded_url"]); e != "" {
			t.URLs = append(t.URLs, e)
		}
	}
	media := asSlice(deepGet(m, "extended_entities", "media"))
	if media == nil {
		media = asSlice(deepGet(m, "entities", "media"))
	}
	for _, raw := range media {
		if mm := asMap(raw); mm != nil {
			t.Media = append(t.Media, models.Media{Type: asString(mm["type"]), URL: asString(mm["media_url_https"])})
		}
	}
	return t
}

// decStr returns the decimal string of n-1, used to turn the oldest tweet id of
// a v1.1 page into the next max_id (which is inclusive, so we step back by one).
func decStr(s string) string {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return ""
	}
	return n.Sub(n, big.NewInt(1)).String()
}
