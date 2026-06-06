package xclient

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// parseGraphQLEnvelope splits a 200 response into its `data` field, mapping
// GraphQL-level errors (including rate-limit code 88) to typed errors.
func parseGraphQLEnvelope(body []byte) (json.RawMessage, bool, error) {
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Code       int    `json:"code"`
			Extensions struct {
				Code int `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, false, xerrors.Wrap(xerrors.APIError, err, "decode response")
	}
	for _, e := range env.Errors {
		code := e.Code
		if code == 0 {
			code = e.Extensions.Code
		}
		switch code {
		case 88, 348, 349:
			return nil, true, xerrors.New(xerrors.RateLimited, "x.com: %s", e.Message)
		case 32, 64, 89:
			return nil, false, xerrors.New(xerrors.NotAuthenticated, "x.com: %s", e.Message)
		}
		// data may still be present alongside soft errors; only fail hard when
		// there's no usable data. x.com signals rejected writes (e.g. an invalid
		// quote attachment_url) with errors plus an empty {} data object, so an
		// empty object counts as no data and surfaces the real message.
		if isEmptyData(env.Data) {
			return nil, false, xerrors.New(xerrors.APIError, "x.com: %s", e.Message)
		}
	}
	return env.Data, false, nil
}

// isEmptyData reports whether a GraphQL data field carries nothing usable:
// absent, null, or an empty {} object.
func isEmptyData(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "{}"
}

// decode unmarshals raw data into a generic structure.
func decode(raw json.RawMessage) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

// parseTweetResult converts a tweet_results.result object into a Tweet.
func parseTweetResult(result any) *models.Tweet {
	m := unwrapTweet(result)
	if m == nil {
		return nil
	}
	legacy := asMap(m["legacy"])
	if legacy == nil {
		return nil
	}

	t := &models.Tweet{
		ID:        asString(m["rest_id"]),
		Text:      asString(legacy["full_text"]),
		CreatedAt: asString(legacy["created_at"]),
		Lang:      asString(legacy["lang"]),
		Metrics: models.Metrics{
			Likes:     asInt(legacy["favorite_count"]),
			Retweets:  asInt(legacy["retweet_count"]),
			Replies:   asInt(legacy["reply_count"]),
			Quotes:    asInt(legacy["quote_count"]),
			Bookmarks: asInt(legacy["bookmark_count"]),
			Views:     asInt(deepGet(m, "views", "count")),
		},
		Source: "graphql",
	}

	// long-form note tweets carry the full (untruncated) text
	if note := asString(deepGet(m, "note_tweet", "note_tweet_results", "result", "text")); note != "" {
		t.Text = note
	}

	t.Author = parseAuthor(deepGet(m, "core", "user_results", "result"))
	t.URLs = parseURLs(legacy)
	t.Media = parseMedia(legacy)

	if rt := asMap(deepGet(legacy, "retweeted_status_result", "result")); rt != nil {
		t.IsRetweet = true
		t.RetweetedBy = t.Author.ScreenName
	}
	if q := deepGet(m, "quoted_status_result", "result"); q != nil {
		t.QuotedTweet = parseTweetResult(q)
	}
	fillArticle(t, m, false)
	return t
}

// unwrapTweet returns the tweet object, peeling off the
// TweetWithVisibilityResults wrapper x.com uses for limited-visibility tweets.
func unwrapTweet(result any) map[string]any {
	m := asMap(result)
	if m == nil {
		return nil
	}
	if m["__typename"] == "TweetWithVisibilityResults" {
		if inner := asMap(m["tweet"]); inner != nil {
			return inner
		}
	}
	return m
}

// fillArticle populates the X Article fields when a tweet carries long-form
// article content. The body lives in a DraftJS content_state (blocks +
// entityMap); we render block-level structure (headers, lists, quotes, code)
// to markdown when requested, otherwise plain text. Inline styling
// (bold/italic/links via inlineStyleRanges/entityRanges) is not yet applied —
// see the TODO in CLAUDE.md.
func fillArticle(t *models.Tweet, m map[string]any, markdown bool) {
	res := asMap(deepGet(m, "article", "article_results", "result"))
	if res == nil {
		return
	}
	t.ArticleTitle = asString(res["title"])
	cs := articleContentState(res)
	blocks := asSlice(cs["blocks"])
	if len(blocks) == 0 {
		// fall back to whatever flat text the response offers
		if pt := asString(res["plain_text"]); pt != "" {
			t.ArticleText = pt
		} else {
			t.ArticleText = asString(res["preview_text"])
		}
		return
	}
	ents := indexEntities(asSlice(cs["entityMap"]))
	media := indexMedia(asSlice(res["media_entities"]))
	t.ArticleText = renderArticle(blocks, ents, media, markdown)
}

// articleContentState normalises the content_state field, which x.com returns
// either as a nested object or as a JSON-encoded string depending on the
// field toggles in play.
func articleContentState(res map[string]any) map[string]any {
	switch cs := res["content_state"].(type) {
	case string:
		var v any
		if json.Unmarshal([]byte(cs), &v) != nil {
			return nil
		}
		return asMap(v)
	default:
		return asMap(res["content_state"])
	}
}

// blockMarkdownPrefix maps DraftJS block types to their markdown line prefix.
// ordered-list-item is handled separately because it needs a running index.
var blockMarkdownPrefix = map[string]string{
	"header-one":          "# ",
	"header-two":          "## ",
	"header-three":        "### ",
	"header-four":         "#### ",
	"header-five":         "##### ",
	"header-six":          "###### ",
	"unordered-list-item": "- ",
	"blockquote":          "> ",
	"code-block":          "    ",
}

// renderArticle flattens DraftJS blocks into text. Plain mode newline-joins the
// block text; markdown mode adds structural prefixes (headers/lists/quotes),
// applies inline styling (bold/italic) and renders atomic entity blocks
// (dividers, embedded markdown tables/code, images/videos). Blocks are
// separated by a blank line in markdown, a single newline in plain.
func renderArticle(blocks []any, ents map[int]map[string]any, media map[string]map[string]any, markdown bool) string {
	var parts []string
	ordered := 0
	for _, raw := range blocks {
		b := asMap(raw)
		if b == nil {
			continue
		}
		typ := asString(b["type"])
		if typ == "ordered-list-item" {
			ordered++
		} else {
			ordered = 0
		}
		if typ == "atomic" {
			if s := renderAtomic(b, ents, media, markdown); s != "" {
				parts = append(parts, s)
			}
			continue
		}
		text := asString(b["text"])
		if markdown {
			text = applyInlineStyles(text, asSlice(b["inlineStyleRanges"]))
			prefix := blockMarkdownPrefix[typ]
			if typ == "ordered-list-item" {
				prefix = strconv.Itoa(ordered) + ". "
			}
			parts = append(parts, prefix+text)
		} else {
			parts = append(parts, text)
		}
	}
	if markdown {
		return strings.Join(parts, "\n\n")
	}
	return strings.Join(parts, "\n")
}

// indexEntities turns the entityMap list ([{key:"3",value:{...}}, ...]) into a
// lookup keyed by the integer entity key that block entityRanges reference.
func indexEntities(list []any) map[int]map[string]any {
	out := make(map[int]map[string]any, len(list))
	for _, raw := range list {
		e := asMap(raw)
		if e == nil {
			continue
		}
		if v := asMap(e["value"]); v != nil {
			if k, err := strconv.Atoi(asString(e["key"])); err == nil {
				out[k] = v
			}
		}
	}
	return out
}

// indexMedia keys the media_entities list by media_id for URL resolution.
func indexMedia(list []any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(list))
	for _, raw := range list {
		e := asMap(raw)
		if id := asString(e["media_id"]); id != "" {
			out[id] = e
		}
	}
	return out
}

// renderAtomic renders an atomic block by resolving its single entity:
// DIVIDER -> "---", MARKDOWN -> the embedded markdown verbatim (tables, code,
// diagrams), MEDIA -> image/video links. Returns "" when there is nothing to show.
func renderAtomic(b map[string]any, ents map[int]map[string]any, media map[string]map[string]any, markdown bool) string {
	ranges := asSlice(b["entityRanges"])
	if len(ranges) == 0 {
		return ""
	}
	ev := ents[asInt(asMap(ranges[0])["key"])]
	if ev == nil {
		return ""
	}
	data := asMap(ev["data"])
	switch asString(ev["type"]) {
	case "DIVIDER":
		if markdown {
			return "---"
		}
		return ""
	case "MARKDOWN":
		return asString(data["markdown"])
	case "MEDIA":
		return renderMedia(data, media, markdown)
	default:
		if md := asString(data["markdown"]); md != "" {
			return md
		}
		return asString(data["caption"])
	}
}

func renderMedia(data map[string]any, media map[string]map[string]any, markdown bool) string {
	caption := asString(data["caption"])
	var urls []string
	for _, it := range asSlice(data["mediaItems"]) {
		mi := media[asString(asMap(it)["mediaId"])]
		if u := mediaURL(asMap(mi["media_info"])); u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		if markdown && caption != "" {
			return "_" + caption + "_"
		}
		return caption
	}
	if markdown {
		var parts []string
		for _, u := range urls {
			parts = append(parts, "!["+caption+"]("+u+")")
		}
		return strings.Join(parts, "\n")
	}
	if caption != "" {
		return caption + ": " + strings.Join(urls, " ")
	}
	return strings.Join(urls, " ")
}

// mediaURL extracts a usable URL from media_info: the image URL for photos,
// the preview-image URL for videos/gifs.
func mediaURL(mi map[string]any) string {
	if u := asString(mi["original_img_url"]); u != "" {
		return u
	}
	return asString(deepGet(mi, "preview_image", "original_img_url"))
}

// styleMark maps a DraftJS inline style to its markdown delimiter.
func styleMark(style string) string {
	switch strings.ToLower(style) {
	case "bold":
		return "**"
	case "italic":
		return "*"
	case "code":
		return "`"
	default:
		return ""
	}
}

// applyInlineStyles wraps the styled spans of a block's text with markdown
// delimiters. DraftJS offsets/lengths are in UTF-16 code units, so the text is
// converted to UTF-16, sliced on style boundaries, and decoded back.
func applyInlineStyles(text string, ranges []any) string {
	if len(ranges) == 0 {
		return text
	}
	units := utf16.Encode([]rune(text))
	n := len(units)
	opens := make(map[int][]string)
	closes := make(map[int][]string)
	for _, raw := range ranges {
		r := asMap(raw)
		mark := styleMark(asString(r["style"]))
		if mark == "" {
			continue
		}
		off := asInt(r["offset"])
		end := off + asInt(r["length"])
		if off < 0 || off > n || end < off {
			continue
		}
		if end > n {
			end = n
		}
		opens[off] = append(opens[off], mark)
		// closings emit in reverse so nested spans unwind correctly
		closes[end] = append([]string{mark}, closes[end]...)
	}
	var sb strings.Builder
	cur := 0
	for i := 0; i <= n; i++ {
		c, o := closes[i], opens[i]
		if len(c) == 0 && len(o) == 0 {
			continue
		}
		if i > cur {
			sb.WriteString(string(utf16.Decode(units[cur:i])))
		}
		for _, m := range c {
			sb.WriteString(m)
		}
		for _, m := range o {
			sb.WriteString(m)
		}
		cur = i
	}
	if cur < n {
		sb.WriteString(string(utf16.Decode(units[cur:n])))
	}
	return sb.String()
}

func parseAuthor(result any) models.Author {
	m := asMap(result)
	if m == nil {
		return models.Author{}
	}
	legacy := asMap(m["legacy"])
	core := asMap(m["core"])
	a := models.Author{
		ID:       asString(m["rest_id"]),
		Verified: asBool(m["is_blue_verified"]),
	}
	// newer schema moved name/screen_name into core
	if core != nil {
		a.Name = asString(core["name"])
		a.ScreenName = asString(core["screen_name"])
	}
	if legacy != nil {
		if a.Name == "" {
			a.Name = asString(legacy["name"])
		}
		if a.ScreenName == "" {
			a.ScreenName = asString(legacy["screen_name"])
		}
		a.ProfileImageURL = asString(legacy["profile_image_url_https"])
	}
	return a
}

func parseURLs(legacy map[string]any) []string {
	var out []string
	for _, u := range asSlice(deepGet(legacy, "entities", "urls")) {
		if exp := asString(asMap(u)["expanded_url"]); exp != "" {
			out = append(out, exp)
		}
	}
	return out
}

func parseMedia(legacy map[string]any) []models.Media {
	media := asSlice(deepGet(legacy, "extended_entities", "media"))
	if media == nil {
		media = asSlice(deepGet(legacy, "entities", "media"))
	}
	var out []models.Media
	for _, raw := range media {
		mm := asMap(raw)
		if mm == nil {
			continue
		}
		item := models.Media{
			Type:   asString(mm["type"]),
			URL:    asString(mm["media_url_https"]),
			Width:  asInt(deepGet(mm, "original_info", "width")),
			Height: asInt(deepGet(mm, "original_info", "height")),
		}
		// pick the highest-bitrate video variant
		if variants := asSlice(deepGet(mm, "video_info", "variants")); len(variants) > 0 {
			best, bestRate := "", -1
			for _, v := range variants {
				vm := asMap(v)
				if asString(vm["content_type"]) != "video/mp4" {
					continue
				}
				if r := asInt(vm["bitrate"]); r > bestRate {
					bestRate, best = r, asString(vm["url"])
				}
			}
			if best != "" {
				item.URL = best
			}
		}
		out = append(out, item)
	}
	return out
}

// extractUserTimeline walks a timeline payload that carries users (followers /
// following) rather than tweets, collecting profiles and the bottom cursor.
func extractUserTimeline(data any) ([]models.UserProfile, string) {
	instructions := asSlice(findKey(data, "instructions"))
	var users []models.UserProfile
	var cursor string
	seen := map[string]bool{}

	for _, ins := range instructions {
		entries := asSlice(asMap(ins)["entries"])
		if e := asMap(ins)["entry"]; e != nil {
			entries = append(entries, e)
		}
		for _, e := range entries {
			em := asMap(e)
			entryID := asString(em["entryId"])
			content := asMap(em["content"])

			if strings.HasPrefix(entryID, "cursor-bottom") || asString(content["cursorType"]) == "Bottom" {
				if v := asString(content["value"]); v != "" {
					cursor = v
				}
				continue
			}
			r := deepGet(content, "itemContent", "user_results", "result")
			if r == nil {
				continue
			}
			u := parseUserResult(r)
			if u == nil || u.ID == "" || seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			users = append(users, *u)
		}
	}
	return users, cursor
}

func parseUserResult(result any) *models.UserProfile {
	m := asMap(result)
	if m == nil {
		return nil
	}
	legacy := asMap(m["legacy"])
	core := asMap(m["core"])
	u := &models.UserProfile{
		ID:       asString(m["rest_id"]),
		Verified: asBool(m["is_blue_verified"]),
	}
	if core != nil {
		u.Name = asString(core["name"])
		u.ScreenName = asString(core["screen_name"])
		u.CreatedAt = asString(core["created_at"])
	}
	if legacy != nil {
		if u.Name == "" {
			u.Name = asString(legacy["name"])
		}
		if u.ScreenName == "" {
			u.ScreenName = asString(legacy["screen_name"])
		}
		u.Bio = asString(legacy["description"])
		u.Location = asString(legacy["location"])
		u.FollowersCount = asInt(legacy["followers_count"])
		u.FollowingCount = asInt(legacy["friends_count"])
		u.TweetsCount = asInt(legacy["statuses_count"])
		u.ProfileImageURL = asString(legacy["profile_image_url_https"])
		if u.CreatedAt == "" {
			u.CreatedAt = asString(legacy["created_at"])
		}
		for _, ur := range asSlice(deepGet(legacy, "entities", "url", "urls")) {
			if exp := asString(asMap(ur)["expanded_url"]); exp != "" {
				u.URL = exp
				break
			}
		}
	}
	return u
}

// extractTimeline walks a timeline payload, collecting tweets and the bottom
// cursor. It works across operations by locating the "instructions" array.
func extractTimeline(data any) ([]models.Tweet, string) {
	instructions := asSlice(findKey(data, "instructions"))
	var tweets []models.Tweet
	var cursor string
	seen := map[string]bool{}

	add := func(t *models.Tweet) {
		if t == nil || t.ID == "" || seen[t.ID] {
			return
		}
		seen[t.ID] = true
		tweets = append(tweets, *t)
	}

	for _, ins := range instructions {
		entries := asSlice(asMap(ins)["entries"])
		// some instructions carry a single entry
		if e := asMap(ins)["entry"]; e != nil {
			entries = append(entries, e)
		}
		for _, e := range entries {
			em := asMap(e)
			entryID := asString(em["entryId"])
			content := asMap(em["content"])

			if strings.HasPrefix(entryID, "cursor-bottom") || asString(content["cursorType"]) == "Bottom" {
				if v := asString(content["value"]); v != "" {
					cursor = v
				}
				continue
			}
			// single tweet entry
			if r := deepGet(content, "itemContent", "tweet_results", "result"); r != nil {
				add(parseTweetResult(r))
				continue
			}
			// module (conversation / who-to-follow style) entries
			for _, it := range asSlice(content["items"]) {
				if r := deepGet(asMap(it), "item", "itemContent", "tweet_results", "result"); r != nil {
					add(parseTweetResult(r))
				}
			}
		}
	}
	return tweets, cursor
}
