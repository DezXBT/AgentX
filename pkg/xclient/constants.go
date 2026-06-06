package xclient

// bearerToken is x.com's public web bearer token. It is not a secret; the real
// authentication is the auth_token cookie. Ported from twitter-cli.
const bearerToken = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

const graphqlBase = "https://x.com/i/api/graphql"

// restBase is the host for x.com's legacy v1.1 REST API, still used for media
// upload, the social graph mutations (follow/mute/block), DMs and a few account
// endpoints that have no GraphQL equivalent.
const restBase = "https://api.x.com"

// uploadBase is the dedicated host for chunked media upload.
const uploadBase = "https://upload.x.com"

// queryIDs maps GraphQL operation names to their query ids. These rotate
// occasionally; the values below are the twitter-cli fallback set and can be
// refreshed at runtime via Client.SetQueryID.
var queryIDs = map[string]string{
	"HomeTimeline":             "c-CzHF1LboFilMpsx4ZCrQ",
	"HomeLatestTimeline":       "BKB7oi212Fi7kQtCBGE4zA",
	"UserByScreenName":         "1VOOyvKkiI3FMmkeDNxM9A",
	"UserTweets":               "q6xj5bs0hapm9309hexA_g",
	"TweetDetail":              "xd_EMdYvB9hfZsZ6Idri0w",
	"Likes":                    "lIDpu_NWL7_VhimGGt0o6A",
	"SearchTimeline":           "VhUd6vHVmLBcw0uX-6jMLA",
	"Bookmarks":                "2neUNDqrrFzbLui8yallcQ",
	"ListLatestTweetsTimeline": "K77PSxWq_St4HLusAV9nVg",
	"Followers":                "IOh4aS6UdGWGJUYTqliQ7Q",
	"Following":                "zx6e-TLzRkeDO_a7p4b3JQ",
	"CreateTweet":              "IID9x6WsdMnTlXnzXGq8ng",
	"DeleteTweet":              "VaenaVgh5q5ih7kvyVjgtg",
	"FavoriteTweet":            "lI07N6Otwv1PhnEgXILM7A",
	"UnfavoriteTweet":          "ZYKSe-w7KEslx3JhSIk5LA",
	"CreateRetweet":            "ojPdsZsimiJrUGLR1sjUtA",
	"DeleteRetweet":            "iQtK4dl5hBmXewYZuEOKVw",
	"CreateBookmark":           "aoDbu3RHznuiSkQ9aNM67Q",
	"DeleteBookmark":           "Wlmlj2-xzyS1GN3a6cj-mQ",
	"TweetResultByRestId":      "7xflPyRiUxGVbJd4uWmbfg",

	// Communities (read + join/leave) and List management (members, create).
	// Extracted from the live x.com main.js bundle; rotate via SetQueryID /
	// AGENTX_QID_<Operation> if x.com bumps them.
	"CommunityTweetsTimeline": "pXYASW5kVylF3YMrGJovLg",
	"CommunityByRestId":       "vLS7mhOqMLtGZdXqFP1DEg",
	"JoinCommunity":           "TQ-ErN9XPSjNkSY4ZB7W6Q",
	"LeaveCommunity":          "q9LMMKLXMQ5t9AdHYjm7Ew",
	"ListMembers":             "EM7YRaM3gCnzDESmchA7RA",
	"ListByRestId":            "t9AbdyHaJVfjL9jsODwgpQ",
	"CreateList":              "UQRa0jJ9doxGEIQRea1Y0w",
	"DeleteList":              "UnN9Th1BDbeLjpgjGSpL3Q",
	"ListSubscribe":           "Gpws7iVbAR7ebO3qCCYmPw",
	"ListUnsubscribe":         "-diULb6PX5grQ_MvItGiJQ",

	// Tier-1 additions: user-by-id, media tab, pin/unpin, list membership edits.
	"UserByRestId":     "VQfQ9wwYdk6j_u2O4vt64Q",
	"UserMedia":        "g_rGPF0fLON-M9cyVjXuzA",
	"PinTweet":         "VIHsNu89pK-kW35JpHq7Xw",
	"UnpinTweet":       "BhKei844ypCyLYCg0nwigw",
	"ListAddMember":    "vWPi0CTMoPFsjsL6W4IynQ",
	"ListRemoveMember": "cAGvZIu7SW0YlLYynz3VYA",

	// Long-form ("note") tweet >280 (Premium-gated) and Community creation.
	"CreateNoteTweet": "yeInFtqpUoABoBE_YWPYgA",
	"CreateCommunity": "uL--Q0pdGxf9qKuHQpKXdw",

	// Tier-2: engagement viewers, scheduled tweets, bookmark folders. These ids
	// live in lazy-loaded chunks (not main.js); discovered by resolving the
	// webpack manifest in the app HTML and grepping the named chunks.
	"Retweeters":             "_ahUdWCUiqTx9q6y84KlLQ",
	"Favoriters":             "PnjIsiRPJfoa8_zdI8tipg",
	"CreateScheduledTweet":   "LCVzRQGxOaGnOnYH01NQXg",
	"FetchScheduledTweets":   "r0GFZK18BfWs5SdRG64t4Q",
	"DeleteScheduledTweet":   "CTOVqej0JBXAZSwkp1US0g",
	"BookmarkFoldersSlice":   "i78YDd0Tza-dV4SYs58kRg",
	"BookmarkFolderTimeline": "-idjPcYMpPyHajagUpLHsw",
	"createBookmarkFolder":   "6Xxqpq8TM_CREYiuof_h5w",
	"DeleteBookmarkFolder":   "2UTTsO-6zs93XqlEUZPsSg",
	"bookmarkTweetToFolder":  "4KHZvvNbHNf07bsgnL9gWA",

	// Grok chat (conversation create; messages go through /2/grok/add_response).
	"CreateGrokConversation":        "vvC5uy7pWWHXS2aDi1FZeA",
	"GrokConversationItemsByRestId": "3U1JqiRWjvPaKIRBhwo-Rg",

	// Draft tweets. (Editing a tweet reuses CreateTweet with edit_options.)
	"CreateDraftTweet": "cH9HZWz_EW9gnswvA4ZRiQ",
	"FetchDraftTweets": "MUx6DV5a61M9CtcQNkyP3A",
	"DeleteDraftTweet": "bkh9G3FGgTldS9iTKWWYYw",

	// Topics (follow/unfollow + info).
	"TopicFollow":   "ElqSLWFmsPL4NlZI5e1Grg",
	"TopicUnfollow": "srwjU6JM_ZKTj_QMfUGNcw",
	"TopicByRestId": "4OUZZOonV2h60I0wdlQb_w",
}

// defaultFeatures is the baseline feature-flag set sent with most operations.
// Only true flags are forwarded (x.com defaults missing flags to false), which
// also keeps GET URLs short.
var defaultFeatures = map[string]bool{
	"responsive_web_graphql_exclude_directive_enabled":                  true,
	"verified_phone_label_enabled":                                      false,
	"creator_subscriptions_tweet_preview_api_enabled":                   true,
	"responsive_web_graphql_timeline_navigation_enabled":                true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled": false,
	"c9s_tweet_anatomy_moderator_badge_enabled":                         true,
	"tweetypie_unmention_optimization_enabled":                          true,
	"responsive_web_edit_tweet_api_enabled":                             true,
	"graphql_is_translatable_rweb_tweet_is_translatable_enabled":        true,
	"view_counts_everywhere_api_enabled":                                true,
	"longform_notetweets_consumption_enabled":                           true,
	"responsive_web_twitter_article_tweet_consumption_enabled":          true,
	"tweet_awards_web_tipping_enabled":                                  false,
	"longform_notetweets_rich_text_read_enabled":                        true,
	"longform_notetweets_inline_media_enabled":                          true,
	"rweb_video_timestamps_enabled":                                     true,
	"responsive_web_media_download_video_enabled":                       true,
	"freedom_of_speech_not_reach_fetch_enabled":                         true,
	"standardized_nudges_misinfo":                                       true,
	"responsive_web_enhance_cards_enabled":                              false,
	"rweb_tipjar_consumption_enabled":                                   true,
	"premium_content_api_read_enabled":                                  false,
	"communities_web_enable_tweet_community_results_fetch":              true,
	"responsive_web_jetfuel_frame":                                      false,
	"responsive_web_grok_analyze_button_fetch_trends_enabled":           false,
	"responsive_web_grok_analyze_post_followups_enabled":                false,
	"responsive_web_grok_share_attachment_enabled":                      true,
	"articles_preview_enabled":                                          true,
	"creator_subscriptions_quote_tweet_preview_enabled":                 false,
}
