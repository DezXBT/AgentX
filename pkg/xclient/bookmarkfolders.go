package xclient

import (
	"context"

	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

// BookmarkFolder is a named bookmark collection.
type BookmarkFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// BookmarkFolders lists the account's bookmark folders.
func (c *Client) BookmarkFolders(ctx context.Context) ([]BookmarkFolder, error) {
	data, err := c.graphqlGET(ctx, "BookmarkFoldersSlice", map[string]any{}, nil, nil)
	if err != nil {
		return nil, err
	}
	var out []BookmarkFolder
	for _, raw := range asSlice(deepGet(decode(data), "viewer", "bookmark_collections_slice", "items")) {
		m := asMap(raw)
		if id := asString(m["id"]); id != "" {
			out = append(out, BookmarkFolder{ID: id, Name: asString(m["name"])})
		}
	}
	return out, nil
}

// CreateBookmarkFolder creates a new bookmark folder and returns it.
func (c *Client) CreateBookmarkFolder(ctx context.Context, name string) (*BookmarkFolder, error) {
	data, err := c.graphqlPOST(ctx, "createBookmarkFolder", map[string]any{"name": name}, nil, true)
	if err != nil {
		return nil, err
	}
	m := asMap(deepGet(decode(data), "bookmark_collection_create"))
	id := asString(m["id"])
	if id == "" {
		id = asString(findKey(decode(data), "id"))
	}
	if id == "" {
		return nil, xerrors.New(xerrors.APIError, "bookmark folder not created: unexpected response")
	}
	n := asString(m["name"])
	if n == "" {
		n = name
	}
	return &BookmarkFolder{ID: id, Name: n}, nil
}

// DeleteBookmarkFolder deletes a bookmark folder by id.
func (c *Client) DeleteBookmarkFolder(ctx context.Context, folderID string) error {
	return c.simpleWrite(ctx, "DeleteBookmarkFolder", map[string]any{"bookmark_collection_id": folderID})
}

// BookmarkToFolder bookmarks a tweet into a specific folder.
func (c *Client) BookmarkToFolder(ctx context.Context, tweetID, folderID string) error {
	return c.simpleWrite(ctx, "bookmarkTweetToFolder", map[string]any{
		"tweet_id":               tweetID,
		"bookmark_collection_id": folderID,
	})
}

// BookmarkFolderTweets returns the tweets saved in a bookmark folder.
func (c *Client) BookmarkFolderTweets(ctx context.Context, folderID string, count int, cursor string) (*models.Page, error) {
	vars := map[string]any{
		"bookmark_collection_id": folderID,
		"count":                  clampCount(count),
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	data, err := c.graphqlGET(ctx, "BookmarkFolderTimeline", vars, nil, nil)
	if err != nil {
		return nil, err
	}
	return pageFrom(data), nil
}
