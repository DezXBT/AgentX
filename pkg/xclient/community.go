package xclient

import (
	"context"
	"encoding/json"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// CommunityTweets returns the timeline of posts in a Community. When latest is
// true the timeline is ranked by recency, otherwise by relevance (the x.com
// web defaults).
func (c *Client) CommunityTweets(ctx context.Context, communityID string, count int, cursor string, latest bool) (*models.Page, error) {
	ranking := "Relevance"
	if latest {
		ranking = "Recency"
	}
	vars := map[string]any{
		"communityId":     communityID,
		"count":           clampCount(count),
		"displayLocation": "Community",
		"rankingMode":     ranking,
		"withCommunity":   true,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "CommunityTweetsTimeline", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}

// CommunityInfo returns metadata about a Community (name, description, member
// count, join policy, role) by its numeric id.
func (c *Client) CommunityInfo(ctx context.Context, communityID string) (map[string]any, error) {
	vars := map[string]any{
		"communityId":              communityID,
		"withDmMuting":             false,
		"withSafetyModeUserFields": true,
	}
	data, err := c.graphqlGET(ctx, "CommunityByRestId", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	result := asMap(deepGet(decode(data), "communityResults", "result"))
	view := communityView(result)
	if view != nil && view["id"] == "" {
		view["id"] = communityID
	}
	return view, nil
}

// JoinCommunity joins the Community with the given id.
func (c *Client) JoinCommunity(ctx context.Context, communityID string) error {
	return c.simpleWrite(ctx, "JoinCommunity", map[string]any{"communityId": communityID})
}

// LeaveCommunity leaves the Community with the given id.
func (c *Client) LeaveCommunity(ctx context.Context, communityID string) error {
	return c.simpleWrite(ctx, "LeaveCommunity", map[string]any{"communityId": communityID})
}

// CreateCommunity creates a new Community with the given name and returns its
// id and name. NOTE: creating a Community is a permanent, public action and is
// not undoable via this client. x.com also gates creation by account standing:
// ineligible accounts get a CommunityCreateActionUnavailable result, surfaced
// here as an error (verified live — a fresh burner cannot create Communities,
// so no Community is actually created in that case).
func (c *Client) CreateCommunity(ctx context.Context, name string) (map[string]any, error) {
	data, err := c.graphqlPOST(ctx, "CreateCommunity", map[string]any{"name": name}, nil, true)
	if err != nil {
		return nil, err
	}
	// The mutation returns a union under community_create_v2 (older clients used
	// community_create): a success variant carries a `community`, an unavailable
	// variant only a __typename (e.g. CommunityCreateActionUnavailable).
	root := decode(data)
	res := asMap(deepGet(root, "community_create_v2"))
	if len(res) == 0 {
		res = asMap(deepGet(root, "community_create"))
	}
	community := asMap(res["community"])
	if len(community) == 0 {
		community = asMap(findKey(root, "community"))
	}
	if len(community) == 0 {
		typename := asString(res["__typename"])
		if typename == "" {
			typename = "unknown response"
		}
		return nil, xerrors.New(xerrors.APIError, "community not created (%s) — the account is not eligible to create Communities", typename)
	}
	view := communityView(community)
	if view == nil {
		view = map[string]any{}
	}
	if asString(view["name"]) == "" {
		view["name"] = name
	}
	return view, nil
}

// communityView projects the raw communityResults.result into a stable, compact
// shape for the JSON envelope.
func communityView(r map[string]any) map[string]any {
	if len(r) == 0 {
		return nil
	}
	id := asString(r["rest_id"])
	if id == "" {
		id = asString(r["id_str"])
	}
	out := map[string]any{
		"id":          id,
		"name":        asString(r["name"]),
		"description": asString(r["description"]),
		"memberCount": numString(r["member_count"]),
		"role":        asString(r["role"]),
		"joinPolicy":  asString(r["join_policy"]),
		"isMember":    asString(r["role"]) != "" && asString(r["role"]) != "NonMember",
	}
	return out
}

// numString coerces a JSON number/string member count to a plain value, leaving
// the original (json.Number-ish) form intact for the envelope.
func numString(v any) any {
	switch t := v.(type) {
	case json.Number:
		return t
	case float64:
		return int64(t)
	case string:
		return t
	default:
		return v
	}
}
