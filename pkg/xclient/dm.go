package xclient

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// DirectMessage is a single DM, normalised from the inbox state.
type DirectMessage struct {
	ID               string         `json:"id"`
	ConversationID   string         `json:"conversationId,omitempty"`
	Text             string         `json:"text"`
	SenderID         string         `json:"senderId"`
	SenderScreenName string         `json:"senderScreenName,omitempty"`
	RecipientID      string         `json:"recipientId,omitempty"`
	Media            []models.Media `json:"media,omitempty"` // attached photo/video/gif
	CreatedAt        string         `json:"createdAt"`       // unix millis as returned by x.com
}

// parseDMAttachment extracts attached media from a DM's message_data.attachment
// (a single photo/video/animated_gif object keyed by type).
func parseDMAttachment(att map[string]any) []models.Media {
	if att == nil {
		return nil
	}
	var out []models.Media
	for _, key := range []string{"photo", "video", "animated_gif", "media"} {
		mm := asMap(att[key])
		if mm == nil {
			continue
		}
		item := models.Media{
			Type:   asString(mm["type"]),
			URL:    asString(mm["media_url_https"]),
			Width:  asInt(deepGet(mm, "original_info", "width")),
			Height: asInt(deepGet(mm, "original_info", "height")),
		}
		if item.Type == "" {
			item.Type = key
		}
		// prefer the highest-bitrate mp4 variant for video/gif
		best, bestRate := "", -1
		for _, v := range asSlice(deepGet(mm, "video_info", "variants")) {
			vm := asMap(v)
			if asString(vm["content_type"]) == "video/mp4" {
				if r := asInt(vm["bitrate"]); r > bestRate {
					bestRate, best = r, asString(vm["url"])
				}
			}
		}
		if best != "" {
			item.URL = best
		}
		out = append(out, item)
	}
	return out
}

// SendDM sends a direct message to a recipient user id via the v1.1 DM events
// endpoint. Resolve a @handle to an id with UserByScreenName first.
func (c *Client) SendDM(ctx context.Context, recipientID, text string) error {
	if recipientID == "" || text == "" {
		return xerrors.New(xerrors.InvalidInput, "recipient and text are required")
	}
	return c.sendDMEvent(ctx, recipientID, text, "")
}

// SendDMMedia uploads media (DM category) and sends it as a DM attachment, with
// optional accompanying text.
func (c *Client) SendDMMedia(ctx context.Context, recipientID, text string, data []byte, mimeType string) error {
	if recipientID == "" {
		return xerrors.New(xerrors.InvalidInput, "recipient is required")
	}
	mediaID, err := c.UploadMedia(ctx, data, mimeType, dmMediaCategory(mimeType))
	if err != nil {
		return err
	}
	return c.sendDMEvent(ctx, recipientID, text, mediaID)
}

func (c *Client) sendDMEvent(ctx context.Context, recipientID, text, mediaID string) error {
	md := map[string]any{"text": text}
	if mediaID != "" {
		md["attachment"] = map[string]any{
			"type":  "media",
			"media": map[string]any{"id": mediaID},
		}
	}
	payload := map[string]any{
		"event": map[string]any{
			"type": "message_create",
			"message_create": map[string]any{
				"target":       map[string]any{"recipient_id": recipientID},
				"message_data": md,
			},
		},
	}
	_, err := c.restPostJSON(ctx, "/1.1/direct_messages/events/new.json", payload)
	return err
}

// DirectMessages lists recent DMs (incoming and outgoing, including message
// requests) via the rich inbox_initial_state endpoint, newest first. The older
// events/list.json endpoint omits some conversations, so this one is used.
func (c *Client) DirectMessages(ctx context.Context, count int) ([]DirectMessage, error) {
	q := url.Values{
		"include_groups":          {"true"},
		"include_inbox_timelines": {"true"},
		"supports_reactions":      {"true"},
	}
	body, err := c.restGet(ctx, "/1.1/dm/inbox_initial_state.json", q)
	if err != nil {
		return nil, err
	}
	var resp struct {
		State struct {
			Entries []map[string]any `json:"entries"`
			Users   map[string]struct {
				ScreenName string `json:"screen_name"`
			} `json:"users"`
			Conversations map[string]struct {
				Participants []struct {
					UserID string `json:"user_id"`
				} `json:"participants"`
			} `json:"conversations"`
		} `json:"inbox_initial_state"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return nil, xerrors.New(xerrors.APIError, "could not parse DM inbox")
	}
	st := resp.State

	out := make([]DirectMessage, 0, len(st.Entries))
	for _, e := range st.Entries {
		m := asMap(e["message"])
		if m == nil {
			continue
		}
		md := asMap(m["message_data"])
		sender := asString(md["sender_id"])
		convID := asString(m["conversation_id"])
		dm := DirectMessage{
			ID:               asString(m["id"]),
			ConversationID:   convID,
			Text:             asString(md["text"]),
			SenderID:         sender,
			SenderScreenName: st.Users[sender].ScreenName,
			Media:            parseDMAttachment(asMap(md["attachment"])),
			CreatedAt:        asString(m["time"]),
		}
		// recipient = the other participant of a 1:1 conversation
		for _, p := range st.Conversations[convID].Participants {
			if p.UserID != sender {
				dm.RecipientID = p.UserID
				break
			}
		}
		out = append(out, dm)
	}
	// newest first (time is fixed-width unix millis, so string sort = numeric)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out, nil
}
