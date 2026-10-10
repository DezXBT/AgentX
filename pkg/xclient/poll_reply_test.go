package xclient

import (
	"encoding/json"
	"testing"
)

// pollTweetResult is the exact shape x.com returned for a live 2-choice poll
// (captured 2026-10-10). The poll lives in card.legacy.binding_values, not in a
// typed object, and the viewer's own vote shows up as selected_choice.
const pollTweetResult = `{
  "__typename": "Tweet",
  "rest_id": "2108815659823976832",
  "card": { "legacy": {
    "binding_values": [
      { "key": "choice1_label", "value": { "string_value": "Merah", "type": "STRING" } },
      { "key": "choice2_label", "value": { "string_value": "Biru", "type": "STRING" } },
      { "key": "selected_choice", "value": { "string_value": "2", "type": "STRING" } },
      { "key": "end_datetime_utc", "value": { "string_value": "2026-10-11T07:03:27Z", "type": "STRING" } },
      { "key": "counts_are_final", "value": { "boolean_value": false, "type": "BOOLEAN" } },
      { "key": "choice2_count", "value": { "string_value": "0", "type": "STRING" } },
      { "key": "choice1_count", "value": { "string_value": "0", "type": "STRING" } },
      { "key": "duration_minutes", "value": { "string_value": "1440", "type": "STRING" } }
    ],
    "name": "poll2choice_text_only"
  }, "rest_id": "card://2108815377182408704" },
  "core": { "user_results": { "result": {
    "__typename": "User", "rest_id": "2060552336028962816",
    "core": { "name": "kawin", "screen_name": "kawinvrai" },
    "legacy": { "profile_image_url_https": "https://img/k.png" }
  } } },
  "legacy": {
    "full_text": "poll readback verify",
    "created_at": "Sat Oct 10 07:03:27 +0000 2026",
    "favorite_count": 0, "retweet_count": 0, "reply_count": 0,
    "quote_count": 0, "bookmark_count": 0, "lang": "in"
  }
}`

func TestParsePollFromCard(t *testing.T) {
	var result any
	if err := json.Unmarshal([]byte(pollTweetResult), &result); err != nil {
		t.Fatal(err)
	}
	tw := parseTweetResult(result)
	if tw == nil {
		t.Fatal("parseTweetResult returned nil")
	}
	p := tw.Poll
	if p == nil {
		t.Fatal("poll not parsed from the card")
	}
	if len(p.Choices) != 2 {
		t.Fatalf("got %d choices, want 2", len(p.Choices))
	}
	if p.Choices[0].Label != "Merah" || p.Choices[1].Label != "Biru" {
		t.Fatalf("labels wrong: %+v", p.Choices)
	}
	if p.Selected != 2 {
		t.Errorf("selected = %d, want 2", p.Selected)
	}
	if p.DurationMinutes != 1440 {
		t.Errorf("durationMinutes = %d, want 1440", p.DurationMinutes)
	}
	if p.EndsAt != "2026-10-11T07:03:27Z" {
		t.Errorf("endsAt = %q", p.EndsAt)
	}
	if p.Final {
		t.Error("final should be false (counts_are_final=false)")
	}
}

// A non-poll card (there are several: summary, player, ...) must not produce a poll.
func TestParsePollIgnoresOtherCards(t *testing.T) {
	cases := []string{
		`{"name":"summary_large_image","binding_values":[{"key":"title","value":{"string_value":"x"}}]}`,
		`{"name":"poll2choice_text_only"}`,
	}
	for _, c := range cases {
		var result any
		if err := json.Unmarshal([]byte(`{"card":{"legacy":`+c+`}}`), &result); err != nil {
			t.Fatal(err)
		}
		if p := parseTweetResult(result); p != nil && p.Poll != nil {
			t.Errorf("unexpected poll for card %s: %+v", c, p.Poll)
		}
	}
}

// Reply linkage: a reply reports its parent and thread root; a thread starter
// reports only conversationId == its own id.
func TestParseReplyLinkage(t *testing.T) {
	reply := `{
	  "rest_id": "2108812435834761569",
	  "core": { "user_results": { "result": { "__typename": "User", "rest_id": "2060552336028962816",
	    "core": { "name": "kawin", "screen_name": "kawinvrai" }, "legacy": {} } } },
	  "legacy": {
	    "full_text": "@brian_armstrong the compounding half is the part everyone underrates",
	    "in_reply_to_status_id_str": "2108609897059238091",
	    "in_reply_to_screen_name": "brian_armstrong",
	    "conversation_id_str": "2108609897059238091"
	  }
	}`
	var result any
	if err := json.Unmarshal([]byte(reply), &result); err != nil {
		t.Fatal(err)
	}
	tw := parseTweetResult(result)
	if tw == nil || !tw.IsReply {
		t.Fatalf("isReply not set: %+v", tw)
	}
	if tw.InReplyToID != "2108609897059238091" || tw.InReplyToUser != "brian_armstrong" {
		t.Errorf("reply linkage wrong: %+v", tw)
	}
	if tw.ConversationID != tw.InReplyToID {
		t.Errorf("conversationId = %q, want the root %q", tw.ConversationID, tw.InReplyToID)
	}

	root := `{
	  "rest_id": "2108812413537792187",
	  "core": { "user_results": { "result": { "__typename": "User", "rest_id": "2060552336028962816",
	    "core": { "name": "kawin", "screen_name": "kawinvrai" }, "legacy": {} } } },
	  "legacy": { "full_text": "standalone", "conversation_id_str": "2108812413537792187" }
	}`
	if err := json.Unmarshal([]byte(root), &result); err != nil {
		t.Fatal(err)
	}
	tw = parseTweetResult(result)
	if tw.IsReply {
		t.Error("thread starter must not be flagged as a reply")
	}
	if tw.ConversationID != tw.ID {
		t.Errorf("conversationId = %q, want own id %q", tw.ConversationID, tw.ID)
	}
}
