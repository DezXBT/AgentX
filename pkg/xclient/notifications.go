package xclient

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// BadgeCount holds the unread counters x.com shows on its nav badges.
type BadgeCount struct {
	Notifications int `json:"notifications"`
	DM            int `json:"dm"`
	XChat         int `json:"xchat"`
	Total         int `json:"total"`
}

// Badges returns the account's unread counts (notifications, DMs, XChat) via the
// v2 badge_count endpoint.
func (c *Client) Badges(ctx context.Context) (*BadgeCount, error) {
	q := url.Values{"supports_ntab_urt": {"1"}, "include_xchat_count": {"1"}}
	body, err := c.restGet(ctx, "/2/badge_count/badge_count.json", q)
	if err != nil {
		return nil, err
	}
	var r struct {
		Ntab  int `json:"ntab_unread_count"`
		DM    int `json:"dm_unread_count"`
		XChat int `json:"xchat_unread_count"`
		Total int `json:"total_unread_count"`
	}
	if json.Unmarshal(body, &r) != nil {
		return nil, xerrors.New(xerrors.APIError, "could not parse badge counts")
	}
	return &BadgeCount{Notifications: r.Ntab, DM: r.DM, XChat: r.XChat, Total: r.Total}, nil
}

// NotificationItem is one row of the notifications timeline: either a
// notification message ("X and 3 others liked your post") or a tweet surfaced in
// the tab (e.g. a mention/reply).
type NotificationItem struct {
	Kind      string        `json:"kind"` // "notification" or "tweet"
	ID        string        `json:"id,omitempty"`
	Message   string        `json:"message,omitempty"`
	Icon      string        `json:"icon,omitempty"` // template icon, e.g. heart_icon
	Timestamp string        `json:"timestampMs,omitempty"`
	Tweet     *models.Tweet `json:"tweet,omitempty"`
}

// NotificationPage is a page of notifications plus a pagination cursor.
type NotificationPage struct {
	Items      []NotificationItem `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

// Notifications fetches the v2 notifications timeline. tab is one of "all",
// "mentions", or "verified" (defaults to "all").
func (c *Client) Notifications(ctx context.Context, tab string, count int, cursor string) (*NotificationPage, error) {
	switch tab {
	case "", "all":
		tab = "all"
	case "mentions", "verified":
	default:
		return nil, xerrors.New(xerrors.InvalidInput, "unknown notifications tab %q (use all|mentions|verified)", tab)
	}
	q := url.Values{
		"count":      {strconv.Itoa(clampCount(count))},
		"tweet_mode": {"extended"},
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	body, err := c.restGet(ctx, "/2/notifications/"+tab+".json", q)
	if err != nil {
		return nil, err
	}
	return parseNotifications(body), nil
}

// parseNotifications walks the legacy globalObjects + timeline structure the v2
// notifications endpoints return, resolving notification messages and tweet
// references in display order.
func parseNotifications(body []byte) *NotificationPage {
	root := asMap(decode(json.RawMessage(body)))
	objs := asMap(deepGet(root, "globalObjects"))
	notifs := asMap(objs["notifications"])
	tweets := asMap(objs["tweets"])
	users := asMap(objs["users"])

	page := &NotificationPage{}
	for _, ins := range asSlice(deepGet(root, "timeline", "instructions")) {
		for _, e := range asSlice(deepGet(asMap(ins), "addEntries", "entries")) {
			content := asMap(asMap(e)["content"])

			if cur := asMap(deepGet(content, "operation", "cursor")); cur != nil {
				if asString(cur["cursorType"]) == "Bottom" {
					page.NextCursor = asString(cur["value"])
				}
				continue
			}

			item := asMap(deepGet(content, "item", "content"))
			if n := asMap(item["notification"]); n != nil {
				nd := asMap(notifs[asString(n["id"])])
				page.Items = append(page.Items, NotificationItem{
					Kind:      "notification",
					ID:        asString(nd["id"]),
					Message:   asString(deepGet(nd, "message", "text")),
					Icon:      asString(deepGet(nd, "icon", "id")),
					Timestamp: asString(nd["timestampMs"]),
				})
				continue
			}
			if tw := asMap(item["tweet"]); tw != nil {
				td := asMap(tweets[asString(tw["id"])])
				if t := parseV11Tweet(injectUser(td, users)); t != nil {
					page.Items = append(page.Items, NotificationItem{Kind: "tweet", Tweet: t})
				}
			}
		}
	}
	return page
}

// injectUser attaches the full user object (from globalObjects.users) onto a
// legacy tweet that only carries user_id_str, so parseV11Tweet can fill Author.
func injectUser(tw, users map[string]any) map[string]any {
	if tw == nil {
		return nil
	}
	if _, ok := tw["user"]; !ok {
		if u := asMap(users[asString(tw["user_id_str"])]); u != nil {
			tw["user"] = u
		}
	}
	return tw
}
