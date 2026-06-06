package xclient

import "testing"

func TestParseV11Tweets(t *testing.T) {
	body := []byte(`[
	  { "id_str":"100","full_text":"hey @me","lang":"en","favorite_count":3,"retweet_count":1,
	    "user":{"id_str":"7","name":"Al","screen_name":"al","verified":true},
	    "entities":{"urls":[{"expanded_url":"https://e.com"}]},
	    "extended_entities":{"media":[{"type":"photo","media_url_https":"https://img/a.jpg"}]} }
	]`)
	tws := parseV11Tweets(body)
	if len(tws) != 1 {
		t.Fatalf("got %d, want 1", len(tws))
	}
	tw := tws[0]
	if tw.ID != "100" || tw.Text != "hey @me" || tw.Author.ScreenName != "al" {
		t.Errorf("tweet = %+v", tw)
	}
	if tw.Metrics.Likes != 3 || len(tw.URLs) != 1 || len(tw.Media) != 1 {
		t.Errorf("metrics/urls/media = %+v %v %v", tw.Metrics, tw.URLs, tw.Media)
	}
	if tw.Source != "rest_v1" {
		t.Errorf("source = %q", tw.Source)
	}
}

func TestDecStr(t *testing.T) {
	if got := decStr("2062561213532471707"); got != "2062561213532471706" {
		t.Errorf("decStr = %q", got)
	}
	if got := decStr("notanumber"); got != "" {
		t.Errorf("decStr(bad) = %q, want empty", got)
	}
}
