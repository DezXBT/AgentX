package xclient

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// grokBase is the dedicated host for Grok response streaming.
const grokBase = "https://grok.x.com"

// GrokAsk sends a single prompt to Grok and returns the assembled reply. It
// creates a fresh conversation, posts the prompt to the streaming
// /2/grok/add_response endpoint, and concatenates the streamed answer tokens.
func (c *Client) GrokAsk(ctx context.Context, prompt string) (string, error) {
	_, body, err := c.grokSend(ctx, prompt, false)
	if err != nil {
		return "", err
	}
	return parseGrokStream(body), nil
}

// grokSend creates a conversation, posts the prompt to the streaming
// add_response endpoint, and returns the conversation id and the raw stream.
// imageGen forces image generation (toolOverrides.imageGen) — without it Grok
// only generates an image when it decides to, and the stream carries no media.
func (c *Client) grokSend(ctx context.Context, prompt string, imageGen bool) (string, []byte, error) {
	data, err := c.graphqlPOST(ctx, "CreateGrokConversation", map[string]any{}, nil, true)
	if err != nil {
		return "", nil, err
	}
	convID := asString(deepGet(decode(data), "create_grok_conversation", "conversation_id"))
	if convID == "" {
		return "", nil, xerrors.New(xerrors.APIError, "could not create Grok conversation")
	}

	tools := map[string]any{}
	if imageGen {
		tools["imageGen"] = true
	}
	payload := map[string]any{
		"responses": []any{map[string]any{
			"message":         prompt,
			"sender":          1,
			"promptSource":    "",
			"fileAttachments": []any{},
		}},
		"systemPromptName":     "",
		"grokModelOptionId":    "grok-3-latest",
		"modelMode":            "MODEL_MODE_FAST",
		"conversationId":       convID,
		"returnSearchResults":  true,
		"returnCitations":      true,
		"promptMetadata":       map[string]any{"promptSource": "NATURAL", "action": "INPUT"},
		"imageGenerationCount": 4,
		"requestFeatures":      map[string]any{"eagerTweets": true, "serverHistory": true},
		"enableSideBySide":     false,
		"toolOverrides":        tools,
		"modelConfigOverride":  map[string]any{},
		"isTemporaryChat":      false,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", nil, xerrors.Wrap(xerrors.InvalidInput, err, "marshal grok request")
	}
	// add_response is served by grok.x.com (api.x.com only proxies it and
	// intermittently 503s "DNS resolution failure"); hit the real host directly.
	const path = "/2/grok/add_response.json"
	body, err := c.doRaw(ctx, http.MethodPost, grokBase+path, path, b, "text/plain;charset=UTF-8", true)
	if err != nil {
		return "", nil, err
	}
	return convID, body, nil
}

// GrokImage asks Grok to generate image(s) and returns their attachment URLs.
// With toolOverrides.imageGen the add_response stream itself carries the
// generated image cards (imageAttachment.mediaId), so the ids are read straight
// from the stream.
func (c *Client) GrokImage(ctx context.Context, prompt string) ([]string, error) {
	_, body, err := c.grokSend(ctx, prompt, true)
	if err != nil {
		return nil, err
	}
	ids := grokImageMediaIDs(body)
	if len(ids) == 0 {
		return nil, xerrors.New(xerrors.APIError, "Grok generated the image card but rendered no pixels (image_chunk null) — the daily image-generation quota is likely exhausted, or it needs a Premium/SuperGrok account")
	}
	urls := make([]string, len(ids))
	for i, id := range ids {
		urls[i] = restBase + "/2/grok/attachment.json?mediaId=" + id
	}
	return urls, nil
}

// grokMediaIDRe matches the generated-image media ids in the (JSON-escaped)
// add_response stream, e.g. `"mediaId":2063...` or `\"mediaId\":2063...`.
var grokMediaIDRe = regexp.MustCompile(`mediaId\\?":\s*"?(\d{10,})`)

// grokImageMediaIDs extracts the deduped generated-image media ids from the
// add_response stream, in order.
func grokImageMediaIDs(stream []byte) []string {
	var ids []string
	seen := map[string]bool{}
	for _, m := range grokMediaIDRe.FindAllSubmatch(stream, -1) {
		id := string(m[1])
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// parseGrokStream concatenates the answer tokens from Grok's NDJSON stream,
// skipping the chain-of-thought ("thinking") tokens.
func parseGrokStream(body []byte) string {
	var sb strings.Builder
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var chunk struct {
			Result struct {
				Message    string `json:"message"`
				IsThinking bool   `json:"isThinking"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &chunk) == nil && chunk.Result.Message != "" && !chunk.Result.IsThinking {
			sb.WriteString(chunk.Result.Message)
		}
	}
	return strings.TrimSpace(stripGrokTags(sb.String()))
}

// grokRenderTag matches Grok's inline render markup (citation/image cards),
// which clutters the plain-text answer.
var grokRenderTag = regexp.MustCompile(`(?s)<grok:render.*?</grok:render>|<grok:render[^>]*/>`)

func stripGrokTags(s string) string {
	return strings.TrimSpace(grokRenderTag.ReplaceAllString(s, ""))
}
