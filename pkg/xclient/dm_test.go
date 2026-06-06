package xclient

import "testing"

func TestParseDMAttachmentPhoto(t *testing.T) {
	att := asMap(decode([]byte(`{"photo":{"type":"photo","media_url_https":"https://ton/x.jpg","original_info":{"width":400,"height":400}}}`)))
	m := parseDMAttachment(att)
	if len(m) != 1 || m[0].Type != "photo" || m[0].URL != "https://ton/x.jpg" || m[0].Width != 400 {
		t.Fatalf("photo attachment = %+v", m)
	}
}

func TestParseDMAttachmentVideoVariant(t *testing.T) {
	att := asMap(decode([]byte(`{"video":{"type":"video","media_url_https":"https://ton/thumb.jpg",
		"video_info":{"variants":[
			{"content_type":"video/mp4","bitrate":256,"url":"https://v/low.mp4"},
			{"content_type":"video/mp4","bitrate":2048,"url":"https://v/high.mp4"},
			{"content_type":"application/x-mpegURL","url":"https://v/x.m3u8"}]}}}`)))
	m := parseDMAttachment(att)
	if len(m) != 1 || m[0].URL != "https://v/high.mp4" {
		t.Fatalf("video attachment picked wrong variant: %+v", m)
	}
}

func TestParseDMAttachmentNil(t *testing.T) {
	if parseDMAttachment(nil) != nil {
		t.Error("nil attachment should yield nil media")
	}
}
