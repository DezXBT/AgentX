package xclient

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// uploadChunkSize is the per-APPEND payload size (x.com caps APPEND at 5 MB).
const uploadChunkSize = 4 << 20 // 4 MiB

const uploadPath = "/1.1/media/upload.json"

// UploadMedia performs a chunked INIT/APPEND/FINALIZE upload and returns the
// media_id to attach to a tweet. mediaType is the MIME type (e.g. image/jpeg);
// category is the x.com media_category (tweet_image / tweet_video / tweet_gif),
// derived automatically when empty. Video/gif uploads are polled to completion.
func (c *Client) UploadMedia(ctx context.Context, data []byte, mediaType, category string) (string, error) {
	if len(data) == 0 {
		return "", xerrors.New(xerrors.InvalidInput, "media is empty")
	}
	if category == "" {
		category = mediaCategory(mediaType)
	}
	mediaID, err := c.uploadInit(ctx, len(data), mediaType, category)
	if err != nil {
		return "", err
	}
	for i, off := 0, 0; off < len(data); i, off = i+1, off+uploadChunkSize {
		end := off + uploadChunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := c.uploadAppend(ctx, mediaID, i, data[off:end]); err != nil {
			return "", err
		}
	}
	return c.uploadFinalize(ctx, mediaID)
}

// SetAltText attaches accessibility alt text to an uploaded media id (call
// after UploadMedia, before posting).
func (c *Client) SetAltText(ctx context.Context, mediaID, text string) error {
	if text == "" {
		return nil
	}
	payload := map[string]any{
		"media_id": mediaID,
		"alt_text": map[string]any{"text": text},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return xerrors.Wrap(xerrors.InvalidInput, err, "marshal alt_text")
	}
	const p = "/1.1/media/metadata/create.json"
	_, err = c.doRaw(ctx, http.MethodPost, uploadBase+p, p, body, "application/json", true)
	return err
}

// dmMediaCategory maps a MIME type to the x.com DM media_category.
func dmMediaCategory(mimeType string) string {
	switch {
	case mimeType == "image/gif":
		return "dm_gif"
	case len(mimeType) >= 6 && mimeType[:6] == "video/":
		return "dm_video"
	default:
		return "dm_image"
	}
}

func (c *Client) uploadInit(ctx context.Context, totalBytes int, mediaType, category string) (string, error) {
	form := url.Values{
		"command":     {"INIT"},
		"total_bytes": {strconv.Itoa(totalBytes)},
		"media_type":  {mediaType},
	}
	if category != "" {
		form.Set("media_category", category)
	}
	body, err := c.doRaw(ctx, http.MethodPost, uploadBase+uploadPath, uploadPath, []byte(form.Encode()), "application/x-www-form-urlencoded", true)
	if err != nil {
		return "", err
	}
	var r struct {
		MediaIDString string `json:"media_id_string"`
	}
	if json.Unmarshal(body, &r) != nil || r.MediaIDString == "" {
		return "", xerrors.New(xerrors.APIError, "media INIT returned no media_id")
	}
	return r.MediaIDString, nil
}

func (c *Client) uploadAppend(ctx context.Context, mediaID string, segment int, chunk []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("command", "APPEND")
	_ = w.WriteField("media_id", mediaID)
	_ = w.WriteField("segment_index", strconv.Itoa(segment))
	fw, err := w.CreateFormFile("media", "blob")
	if err != nil {
		return xerrors.Wrap(xerrors.APIError, err, "build media chunk")
	}
	if _, err := fw.Write(chunk); err != nil {
		return xerrors.Wrap(xerrors.APIError, err, "write media chunk")
	}
	_ = w.Close()
	_, err = c.doRaw(ctx, http.MethodPost, uploadBase+uploadPath, uploadPath, buf.Bytes(), w.FormDataContentType(), true)
	return err
}

func (c *Client) uploadFinalize(ctx context.Context, mediaID string) (string, error) {
	form := url.Values{"command": {"FINALIZE"}, "media_id": {mediaID}}
	body, err := c.doRaw(ctx, http.MethodPost, uploadBase+uploadPath, uploadPath, []byte(form.Encode()), "application/x-www-form-urlencoded", true)
	if err != nil {
		return "", err
	}
	pi := parseProcessingInfo(body)
	if pi == nil || pi.State == "succeeded" || pi.State == "" {
		return mediaID, nil
	}
	// video/gif transcoding: poll STATUS until done
	return c.pollMediaStatus(ctx, mediaID, pi)
}

type processingInfo struct {
	State          string `json:"state"`
	CheckAfterSecs int    `json:"check_after_secs"`
	Error          *struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"error"`
}

func parseProcessingInfo(body []byte) *processingInfo {
	var r struct {
		ProcessingInfo *processingInfo `json:"processing_info"`
	}
	if json.Unmarshal(body, &r) != nil {
		return nil
	}
	return r.ProcessingInfo
}

func (c *Client) pollMediaStatus(ctx context.Context, mediaID string, pi *processingInfo) (string, error) {
	for tries := 0; tries < 20; tries++ {
		wait := pi.CheckAfterSecs
		if wait <= 0 {
			wait = 1
		}
		sleep(ctx, time.Duration(wait)*time.Second)
		q := url.Values{"command": {"STATUS"}, "media_id": {mediaID}}
		body, err := c.doRaw(ctx, http.MethodGet, uploadBase+uploadPath+"?"+q.Encode(), uploadPath, nil, "", false)
		if err != nil {
			return "", err
		}
		pi = parseProcessingInfo(body)
		if pi == nil || pi.State == "succeeded" {
			return mediaID, nil
		}
		if pi.State == "failed" {
			msg := "media processing failed"
			if pi.Error != nil {
				msg = pi.Error.Message
			}
			return "", xerrors.New(xerrors.APIError, "%s", msg)
		}
	}
	return "", xerrors.New(xerrors.APIError, "media processing timed out")
}

// mediaCategory maps a MIME type to the x.com media_category used at INIT.
func mediaCategory(mimeType string) string {
	switch {
	case mimeType == "image/gif":
		return "tweet_gif"
	case len(mimeType) >= 6 && mimeType[:6] == "video/":
		return "tweet_video"
	default:
		return "tweet_image"
	}
}
