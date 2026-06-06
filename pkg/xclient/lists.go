package xclient

import (
	"context"

	"github.com/dezxbt/agentx/pkg/models"
)

// ListMembers returns the accounts that belong to a List (its members), by the
// List's numeric id.
func (c *Client) ListMembers(ctx context.Context, listID string, count int, cursor string) (*models.UserPage, error) {
	vars := map[string]any{
		"listId": listID,
		"count":  clampCount(count),
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "ListMembers", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	users, cur := extractUserTimeline(decode(data))
	return &models.UserPage{Users: users, NextCursor: cur}, nil
}

// CreateList creates a List owned by the authenticated account and returns its
// id and name.
func (c *Client) CreateList(ctx context.Context, name, description string, private bool) (map[string]any, error) {
	vars := map[string]any{
		"isPrivate":   private,
		"name":        name,
		"description": description,
	}
	data, err := c.graphqlPOST(ctx, "CreateList", vars, nil, true)
	if err != nil {
		return nil, err
	}
	l := asMap(deepGet(decode(data), "list"))
	id := asString(l["id_str"])
	if id == "" {
		id = asString(l["id"])
	}
	return map[string]any{
		"id":          id,
		"name":        asString(l["name"]),
		"description": asString(l["description"]),
		"private":     private,
	}, nil
}

// DeleteList deletes a List owned by the authenticated account.
func (c *Client) DeleteList(ctx context.Context, listID string) error {
	return c.simpleWrite(ctx, "DeleteList", map[string]any{"listId": listID})
}

// SubscribeList follows (subscribes to) a List owned by anyone, so it appears in
// the authenticated account's Lists.
func (c *Client) SubscribeList(ctx context.Context, listID string) error {
	return c.simpleWrite(ctx, "ListSubscribe", map[string]any{"listId": listID})
}

// UnsubscribeList unfollows a List the authenticated account subscribed to.
func (c *Client) UnsubscribeList(ctx context.Context, listID string) error {
	return c.simpleWrite(ctx, "ListUnsubscribe", map[string]any{"listId": listID})
}

// AddListMember adds a user (by numeric id) to a List the account owns.
func (c *Client) AddListMember(ctx context.Context, listID, userID string) error {
	return c.simpleWrite(ctx, "ListAddMember", map[string]any{"listId": listID, "userId": userID})
}

// RemoveListMember removes a user (by numeric id) from a List the account owns.
func (c *Client) RemoveListMember(ctx context.Context, listID, userID string) error {
	return c.simpleWrite(ctx, "ListRemoveMember", map[string]any{"listId": listID, "userId": userID})
}
