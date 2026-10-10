// Package models holds the shared, backend-agnostic data types returned by the
// agentx client. Every fetch backend (authenticated GraphQL, FxTwitter, ...)
// normalises its raw payload into these types so agents see one stable schema.
package models

// Author is the user that produced a tweet.
type Author struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ScreenName      string `json:"screenName"`
	ProfileImageURL string `json:"profileImageUrl,omitempty"`
	Verified        bool   `json:"verified,omitempty"`
}

// Metrics holds public engagement counters.
type Metrics struct {
	Likes     int `json:"likes"`
	Retweets  int `json:"retweets"`
	Replies   int `json:"replies"`
	Quotes    int `json:"quotes"`
	Views     int `json:"views"`
	Bookmarks int `json:"bookmarks"`
}

// Media is an attached photo, video or animated gif.
type Media struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Tweet is the normalised representation of a single tweet.
type Tweet struct {
	ID            string   `json:"id"`
	Text          string   `json:"text"`
	Author        Author   `json:"author"`
	Metrics       Metrics  `json:"metrics"`
	CreatedAt     string   `json:"createdAt,omitempty"`
	Media         []Media  `json:"media,omitempty"`
	URLs          []string `json:"urls,omitempty"`
	IsRetweet     bool     `json:"isRetweet,omitempty"`
	RetweetedBy   string   `json:"retweetedBy,omitempty"`
	IsReply       bool     `json:"isReply,omitempty"`
	InReplyToID   string   `json:"inReplyToId,omitempty"`
	InReplyToUser string   `json:"inReplyToUser,omitempty"`
	// ConversationID is the root tweet of the thread. Equal to ID for a tweet
	// that starts a thread, so it is what tells a client which thread a reply
	// landed in without fetching the parent.
	ConversationID string `json:"conversationId,omitempty"`
	Lang           string `json:"lang,omitempty"`
	QuotedTweet    *Tweet `json:"quotedTweet,omitempty"`
	ArticleTitle   string `json:"articleTitle,omitempty"`
	ArticleText    string `json:"articleText,omitempty"`
	Source         string `json:"source,omitempty"` // which backend produced this tweet
}

// UserProfile is the normalised representation of a user.
type UserProfile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ScreenName      string `json:"screenName"`
	Bio             string `json:"bio,omitempty"`
	Location        string `json:"location,omitempty"`
	URL             string `json:"url,omitempty"`
	FollowersCount  int    `json:"followersCount"`
	FollowingCount  int    `json:"followingCount"`
	TweetsCount     int    `json:"tweetsCount"`
	Verified        bool   `json:"verified,omitempty"`
	ProfileImageURL string `json:"profileImageUrl,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
}

// Page is a paginated slice of tweets with an optional continuation cursor.
type Page struct {
	Tweets     []Tweet `json:"tweets"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

// UserPage is a paginated slice of users (followers/following) with an optional
// continuation cursor.
type UserPage struct {
	Users      []UserProfile `json:"users"`
	NextCursor string        `json:"nextCursor,omitempty"`
}
