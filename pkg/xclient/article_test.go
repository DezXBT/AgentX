package xclient

import (
	"testing"

	"github.com/dezxbt/agentx/pkg/models"
)

// richArticleResult mirrors the real article schema: an inline Bold range, plus
// atomic blocks whose entityRanges reference an entityMap (list of
// {key,value}) carrying DIVIDER, MARKDOWN and MEDIA entities, with media URLs
// resolved through media_entities.
const richArticleResult = `{ "article": { "article_results": { "result": {
  "title": "Rich",
  "media_entities": [
    { "media_id": "m1", "media_info": { "__typename": "ApiImage", "original_img_url": "https://img/x.jpg" } }
  ],
  "content_state": {
    "blocks": [
      { "type": "unstyled", "text": "Hello bold world",
        "inlineStyleRanges": [ { "offset": 6, "length": 4, "style": "Bold" } ], "entityRanges": [] },
      { "type": "atomic", "text": " ", "inlineStyleRanges": [], "entityRanges": [ { "key": 0, "length": 1, "offset": 0 } ] },
      { "type": "atomic", "text": " ", "inlineStyleRanges": [], "entityRanges": [ { "key": 1, "length": 1, "offset": 0 } ] },
      { "type": "atomic", "text": " ", "inlineStyleRanges": [], "entityRanges": [ { "key": 2, "length": 1, "offset": 0 } ] }
    ],
    "entityMap": [
      { "key": "0", "value": { "type": "DIVIDER", "data": {} } },
      { "key": "1", "value": { "type": "MARKDOWN", "data": { "markdown": "| a | b |\n|---|---|" } } },
      { "key": "2", "value": { "type": "MEDIA", "data": { "caption": "fig", "mediaItems": [ { "mediaId": "m1" } ] } } }
    ]
  }
} } } }`

func TestArticleEntitiesAndInlineMarkdown(t *testing.T) {
	tw := &models.Tweet{}
	fillArticle(tw, asMap(decode([]byte(richArticleResult))), true)
	want := "Hello **bold** world\n\n---\n\n| a | b |\n|---|---|\n\n![fig](https://img/x.jpg)"
	if tw.ArticleText != want {
		t.Errorf("markdown =\n%q\nwant\n%q", tw.ArticleText, want)
	}
}

func TestArticleEntitiesPlain(t *testing.T) {
	tw := &models.Tweet{}
	fillArticle(tw, asMap(decode([]byte(richArticleResult))), false)
	// plain: no inline marks, divider skipped, markdown + media captions kept
	want := "Hello bold world\n| a | b |\n|---|---|\nfig: https://img/x.jpg"
	if tw.ArticleText != want {
		t.Errorf("plain =\n%q\nwant\n%q", tw.ArticleText, want)
	}
}

func TestApplyInlineStylesNestedAndUnicode(t *testing.T) {
	// "a—b" has an en-dash (multi-byte in UTF-8, 1 UTF-16 unit); bold over "b".
	got := applyInlineStyles("a—b", []any{
		map[string]any{"offset": float64(2), "length": float64(1), "style": "Bold"},
	})
	if got != "a—**b**" {
		t.Errorf("got %q, want %q", got, "a—**b**")
	}
}

// sampleArticleTweet mirrors a TweetResultByRestId result for a long-form X
// Article: a normal tweet object plus an article.article_results.result holding
// a DraftJS content_state with assorted block types.
const sampleArticleTweet = `{
  "__typename": "Tweet",
  "rest_id": "456",
  "core": { "user_results": { "result": {
    "rest_id": "9",
    "core": { "name": "Jack", "screen_name": "jack" }
  } } },
  "legacy": { "full_text": "Read my article", "lang": "en" },
  "article": { "article_results": { "result": {
    "title": "My Great Article",
    "preview_text": "A short preview",
    "content_state": {
      "blocks": [
        { "type": "header-one", "text": "Intro" },
        { "type": "unstyled", "text": "First paragraph." },
        { "type": "ordered-list-item", "text": "step one" },
        { "type": "ordered-list-item", "text": "step two" },
        { "type": "unordered-list-item", "text": "a bullet" },
        { "type": "blockquote", "text": "a quote" }
      ]
    }
  } } }
}`

func TestParseArticlePlainText(t *testing.T) {
	tw := parseTweetResult(decode([]byte(sampleArticleTweet)))
	if tw == nil {
		t.Fatal("parseTweetResult returned nil")
	}
	if tw.ID != "456" {
		t.Errorf("id = %q, want 456", tw.ID)
	}
	if tw.ArticleTitle != "My Great Article" {
		t.Errorf("articleTitle = %q", tw.ArticleTitle)
	}
	want := "Intro\nFirst paragraph.\nstep one\nstep two\na bullet\na quote"
	if tw.ArticleText != want {
		t.Errorf("articleText (plain) =\n%q\nwant\n%q", tw.ArticleText, want)
	}
}

func TestRenderArticleMarkdown(t *testing.T) {
	res := asMap(deepGet(decode([]byte(sampleArticleTweet)), "article", "article_results", "result"))
	cs := articleContentState(res)
	got := renderArticle(asSlice(cs["blocks"]), nil, nil, true)
	want := "# Intro\n\nFirst paragraph.\n\n1. step one\n\n2. step two\n\n- a bullet\n\n> a quote"
	if got != want {
		t.Errorf("markdown =\n%q\nwant\n%q", got, want)
	}
}

// content_state is sometimes delivered as a JSON-encoded string; ensure we
// decode that form too.
func TestArticleContentStateAsString(t *testing.T) {
	res := map[string]any{
		"content_state": `{"blocks":[{"type":"unstyled","text":"hi"}]}`,
	}
	cs := articleContentState(res)
	if cs == nil {
		t.Fatal("expected decoded content_state, got nil")
	}
	if got := renderArticle(asSlice(cs["blocks"]), nil, nil, false); got != "hi" {
		t.Errorf("text = %q, want hi", got)
	}
}

// A tweet with no article block must leave the article fields empty.
func TestNonArticleTweetHasNoArticleFields(t *testing.T) {
	tw := parseTweetResult(decode([]byte(`{
		"rest_id": "1", "legacy": { "full_text": "plain tweet" }
	}`)))
	if tw == nil {
		t.Fatal("nil tweet")
	}
	if tw.ArticleTitle != "" || tw.ArticleText != "" {
		t.Errorf("expected empty article fields, got title=%q text=%q", tw.ArticleTitle, tw.ArticleText)
	}
}
