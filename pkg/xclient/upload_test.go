package xclient

import "testing"

func TestMediaCategory(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": "tweet_image",
		"image/png":  "tweet_image",
		"image/gif":  "tweet_gif",
		"video/mp4":  "tweet_video",
		"":           "tweet_image",
	}
	for mime, want := range cases {
		if got := mediaCategory(mime); got != want {
			t.Errorf("mediaCategory(%q)=%q want %q", mime, got, want)
		}
	}
}
