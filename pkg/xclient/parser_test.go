package xclient

import (
	"strings"
	"testing"
)

const sampleTimeline = `{
  "home": { "home_timeline_urt": { "instructions": [
    { "type": "TimelineAddEntries", "entries": [
      { "entryId": "tweet-123", "content": { "entryType": "TimelineTimelineItem",
        "itemContent": { "itemType": "TimelineTweet", "tweet_results": { "result": {
          "__typename": "Tweet", "rest_id": "123",
          "core": { "user_results": { "result": {
            "rest_id": "9",
            "core": { "name": "Jack", "screen_name": "jack" },
            "is_blue_verified": true,
            "legacy": { "profile_image_url_https": "https://img/x.jpg" }
          } } },
          "legacy": {
            "full_text": "hello world",
            "favorite_count": 5, "retweet_count": 2, "reply_count": 1,
            "quote_count": 0, "bookmark_count": 3, "lang": "en",
            "created_at": "Wed Oct 10 20:19:24 +0000 2018",
            "entities": { "urls": [ { "expanded_url": "https://example.com" } ] }
          },
          "views": { "count": "1000" }
        } } } } },
      { "entryId": "cursor-bottom-99", "content": {
        "entryType": "TimelineTimelineCursor", "cursorType": "Bottom", "value": "NEXTCUR" } }
    ] }
  ] } }
}`

func TestExtractTimeline(t *testing.T) {
	page := pageFrom([]byte(sampleTimeline))
	if len(page.Tweets) != 1 {
		t.Fatalf("got %d tweets, want 1", len(page.Tweets))
	}
	tw := page.Tweets[0]
	if tw.ID != "123" {
		t.Errorf("id = %q, want 123", tw.ID)
	}
	if tw.Text != "hello world" {
		t.Errorf("text = %q", tw.Text)
	}
	if tw.Author.ScreenName != "jack" || tw.Author.Name != "Jack" {
		t.Errorf("author = %+v", tw.Author)
	}
	if !tw.Author.Verified {
		t.Error("expected verified author")
	}
	if tw.Metrics.Likes != 5 || tw.Metrics.Views != 1000 || tw.Metrics.Bookmarks != 3 {
		t.Errorf("metrics = %+v", tw.Metrics)
	}
	if len(tw.URLs) != 1 || tw.URLs[0] != "https://example.com" {
		t.Errorf("urls = %v", tw.URLs)
	}
	if page.NextCursor != "NEXTCUR" {
		t.Errorf("cursor = %q, want NEXTCUR", page.NextCursor)
	}
}

const sampleUserTimeline = `{ "data": { "user": { "result": { "timeline": { "timeline": { "instructions": [
  { "type": "TimelineAddEntries", "entries": [
    { "entryId": "user-9", "content": { "itemContent": { "user_results": { "result": {
      "rest_id": "9", "core": { "name": "Jack", "screen_name": "jack" }, "is_blue_verified": true,
      "legacy": { "followers_count": 100, "friends_count": 5 } } } } } },
    { "entryId": "cursor-bottom-1", "content": { "cursorType": "Bottom", "value": "UCUR" } }
  ] }
] } } } } } }`

func TestExtractUserTimeline(t *testing.T) {
	users, cursor := extractUserTimeline(decode([]byte(sampleUserTimeline)))
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1", len(users))
	}
	if users[0].ScreenName != "jack" || users[0].FollowersCount != 100 {
		t.Errorf("user = %+v", users[0])
	}
	if cursor != "UCUR" {
		t.Errorf("cursor = %q, want UCUR", cursor)
	}
}

func TestParseGraphQLErrorRateLimited(t *testing.T) {
	body := []byte(`{"errors":[{"message":"Rate limited","code":88}]}`)
	_, retry, err := parseGraphQLEnvelope(body)
	if err == nil || !retry {
		t.Fatalf("expected retryable rate-limit error, got retry=%v err=%v", retry, err)
	}
}

// x.com rejects bad writes (e.g. an invalid quote attachment_url) with an error
// alongside an empty {} data object; the real message must surface rather than
// being swallowed as "successful".
func TestParseGraphQLEmptyObjectDataSurfacesError(t *testing.T) {
	body := []byte(`{"errors":[{"message":"BadRequest: attachment_url parameter is invalid. (44)","code":44}],"data":{}}`)
	_, retry, err := parseGraphQLEnvelope(body)
	if err == nil {
		t.Fatal("expected error to surface from empty-object data")
	}
	if retry {
		t.Error("attachment error should not be retryable")
	}
	if got := err.Error(); !strings.Contains(got, "attachment_url") {
		t.Errorf("error message lost: %q", got)
	}
}

func TestIsEmptyData(t *testing.T) {
	cases := map[string]bool{"": true, "null": true, "{}": true, " {} ": true, `{"x":1}`: false, "[]": false}
	for in, want := range cases {
		if got := isEmptyData([]byte(in)); got != want {
			t.Errorf("isEmptyData(%q)=%v want %v", in, got, want)
		}
	}
}
