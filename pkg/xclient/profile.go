package xclient

import (
	"context"
	"net/url"
)

// ProfileFields are the editable profile attributes. Empty fields are omitted,
// so callers can update just the ones they pass.
type ProfileFields struct {
	Name        string // display name
	Description string // bio
	Location    string
	URL         string
}

// UpdateProfile edits the authenticated account's profile (name, bio, location,
// url) via the v1.1 account/update_profile endpoint. Only the non-empty fields
// are sent.
func (c *Client) UpdateProfile(ctx context.Context, f ProfileFields) error {
	form := url.Values{}
	if f.Name != "" {
		form.Set("name", f.Name)
	}
	if f.Description != "" {
		form.Set("description", f.Description)
	}
	if f.Location != "" {
		form.Set("location", f.Location)
	}
	if f.URL != "" {
		form.Set("url", f.URL)
	}
	_, err := c.restPostForm(ctx, "/1.1/account/update_profile.json", form)
	return err
}
