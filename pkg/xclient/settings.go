package xclient

import (
	"context"
	"encoding/base64"
	"net/url"

	"github.com/dezxbt/agentx/pkg/xerrors"
)

// Settings returns the authenticated account's settings (privacy, DM, discovery)
// via the v1.1 account/settings endpoint.
func (c *Client) Settings(ctx context.Context) (map[string]any, error) {
	body, err := c.restGet(ctx, "/1.1/account/settings.json", nil)
	if err != nil {
		return nil, err
	}
	m := asMap(decode(body))
	if m == nil {
		return nil, xerrors.New(xerrors.APIError, "could not parse settings")
	}
	// Project the privacy-relevant fields into a compact, stable view.
	view := map[string]any{
		"screenName":          m["screen_name"],
		"protected":           m["protected"],
		"discoverableByEmail": m["discoverable_by_email"],
		"discoverableByPhone": m["discoverable_by_mobile_phone"],
		"allowDmsFrom":        m["allow_dms_from"],
		"allowDmGroupsFrom":   m["allow_dm_groups_from"],
		"geoEnabled":          m["geo_enabled"],
		"language":            m["language"],
	}
	return view, nil
}

// UpdateSettings posts the given fields to account/settings (e.g. protected,
// allow_dms_from, discoverable_by_email). Only the provided keys are changed.
func (c *Client) UpdateSettings(ctx context.Context, fields map[string]string) error {
	if len(fields) == 0 {
		return xerrors.New(xerrors.InvalidInput, "no settings to update")
	}
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	_, err := c.restPostForm(ctx, "/1.1/account/settings.json", form)
	return err
}

// UpdateAvatar sets the account's profile picture from raw image bytes.
func (c *Client) UpdateAvatar(ctx context.Context, image []byte) error {
	form := url.Values{"image": {base64.StdEncoding.EncodeToString(image)}}
	_, err := c.restPostForm(ctx, "/1.1/account/update_profile_image.json", form)
	return err
}

// UpdateBanner sets the account's profile banner from raw image bytes.
func (c *Client) UpdateBanner(ctx context.Context, image []byte) error {
	form := url.Values{"banner": {base64.StdEncoding.EncodeToString(image)}}
	_, err := c.restPostForm(ctx, "/1.1/account/update_profile_banner.json", form)
	return err
}

// FollowTopic / UnfollowTopic manage the account's followed Topics.
func (c *Client) FollowTopic(ctx context.Context, topicID string) error {
	return c.simpleWrite(ctx, "TopicFollow", map[string]any{"topicId": topicID})
}

func (c *Client) UnfollowTopic(ctx context.Context, topicID string) error {
	return c.simpleWrite(ctx, "TopicUnfollow", map[string]any{"topicId": topicID})
}

// TrendLocation is a place trends are available for (WOEID + name).
type TrendLocation struct {
	Name    string `json:"name"`
	WOEID   int    `json:"woeid"`
	Country string `json:"country,omitempty"`
}

// TrendLocations lists the places (with WOEIDs) that have trending data, via the
// v1.1 trends/available endpoint.
func (c *Client) TrendLocations(ctx context.Context) ([]TrendLocation, error) {
	body, err := c.restGet(ctx, "/1.1/trends/available.json", nil)
	if err != nil {
		return nil, err
	}
	var out []TrendLocation
	for _, raw := range asSlice(decode(body)) {
		m := asMap(raw)
		out = append(out, TrendLocation{
			Name:    asString(m["name"]),
			WOEID:   asInt(m["woeid"]),
			Country: asString(m["country"]),
		})
	}
	return out, nil
}
