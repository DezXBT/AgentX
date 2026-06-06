// Command agentx is the agent-facing control surface for the agentx library.
//
// Every command emits a single JSON envelope on stdout:
//
//	{"ok":true,"schema_version":"1","data":...,"pagination":{"nextCursor":"..."}}
//	{"ok":false,"schema_version":"1","error":{"code":"...","message":"..."}}
//
// so that any agent runtime can drive X/Twitter by shelling out and parsing one
// predictable structure. Multi-account is first class via the --account flag
// (or AGENTX_ACCOUNT); credentials live in ~/.agentx/accounts.json.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/dezxbt/agentx/pkg/account"
	"github.com/dezxbt/agentx/pkg/agent"
	"github.com/dezxbt/agentx/pkg/models"
	"github.com/dezxbt/agentx/pkg/xclient"
	"github.com/dezxbt/agentx/pkg/xerrors"
)

const schemaVersion = "1"

// pretty controls JSON output: compact single-line by default (token-efficient
// for agents), pretty-printed when --pretty is passed (human-readable).
var pretty bool

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	argv := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		if a == "--pretty" {
			pretty = true
			continue
		}
		argv = append(argv, a)
	}
	cmd, args := splitCommand(argv)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	h, ok := commands[cmd]
	if !ok {
		switch cmd {
		case "-h", "--help", "help":
			usage()
			return
		default:
			fail(xerrors.New(xerrors.InvalidInput, "unknown command %q (try `agentx help`)", cmd))
		}
	}
	h(ctx, args)
}

type handler func(ctx context.Context, args []string)

// globalValueFlags are the value-taking global flags that may appear before the
// subcommand; splitCommand skips them (and their values) when locating it.
var globalValueFlags = map[string]bool{
	"-a": true, "--account": true,
	"-n": true, "--count": true,
	"--cursor": true,
}

// splitCommand extracts the subcommand token, tolerating global flags placed
// before it (e.g. `agentx -a alice user jack`). The command is removed; every
// other token (global flags and positionals) is returned in order for the
// handler, which re-parses the global flags via parseCommon.
func splitCommand(args []string) (string, []string) {
	cmd := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if cmd != "" {
			rest = append(rest, a)
			continue
		}
		if globalValueFlags[a] {
			rest = append(rest, a)
			if i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		cmd = a // first non-(global-flag) token is the command (incl. -h/--help)
	}
	return cmd, rest
}

var commands = map[string]handler{
	"account":       cmdAccount,
	"feed":          cmdFeed,
	"search":        cmdSearch,
	"user":          cmdUser,
	"posts":         cmdPosts,
	"tweet":         cmdTweet,
	"analytics":     cmdAnalytics,
	"thread":        cmdThread,
	"article":       cmdArticle,
	"bookmarks":     cmdBookmarks,
	"likes":         cmdLikes,
	"followers":     cmdFollowers,
	"following":     cmdFollowing,
	"list":          cmdList,
	"community":     cmdCommunity,
	"me":            cmdMe,
	"media":         cmdMedia,
	"trends":        cmdTrends,
	"retweeters":    cmdRetweeters,
	"likers":        cmdLikers,
	"schedule":      cmdSchedule,
	"scheduled":     cmdScheduled,
	"unschedule":    cmdUnschedule,
	"pin":           simpleAction("pin"),
	"unpin":         simpleAction("unpin"),
	"mentions":      cmdMentions,
	"notifications": cmdNotifications,
	"notif":         cmdNotifications,
	"unread":        cmdUnread,
	"dm":            cmdDM,
	"download":      cmdDownload,
	"watch":         cmdWatch,
	"follow":        userAction("follow"),
	"unfollow":      userAction("unfollow"),
	"mute":          userAction("mute"),
	"unmute":        userAction("unmute"),
	"block":         userAction("block"),
	"unblock":       userAction("unblock"),
	"post":          cmdPost,
	"poll":          cmdPoll,
	"edit":          cmdEdit,
	"draft":         cmdDraft,
	"drafts":        cmdDrafts,
	"vote":          cmdVote,
	"grok":          cmdGrok,
	"profile":       cmdProfile,
	"settings":      cmdSettings,
	"avatar":        cmdAvatar,
	"banner":        cmdBanner,
	"topic":         cmdTopic,
	"reply":         cmdReply,
	"quote":         cmdQuote,
	"like":          simpleAction("like"),
	"unlike":        simpleAction("unlike"),
	"retweet":       simpleAction("retweet"),
	"unretweet":     simpleAction("unretweet"),
	"delete":        simpleAction("delete"),
	"bookmark":      cmdBookmark,
	"unbookmark":    simpleAction("unbookmark"),
}

// --- envelope ---

type envelope struct {
	OK            bool               `json:"ok"`
	SchemaVersion string             `json:"schema_version"`
	Data          any                `json:"data,omitempty"`
	Pagination    *pagination        `json:"pagination,omitempty"`
	Meta          *xclient.RateLimit `json:"rateLimit,omitempty"`
	Error         *errObj            `json:"error,omitempty"`
}

type pagination struct {
	NextCursor string `json:"nextCursor,omitempty"`
}

type errObj struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func emit(data any, page *pagination) {
	enc := json.NewEncoder(os.Stdout)
	if pretty {
		enc.SetIndent("", "  ")
	}
	_ = enc.Encode(envelope{
		OK:            true,
		SchemaVersion: schemaVersion,
		Data:          data,
		Pagination:    page,
		Meta:          xclient.LastRateLimit(), // auto-surface last-seen rate limit
	})
}

func fail(err error) {
	enc := json.NewEncoder(os.Stdout)
	if pretty {
		enc.SetIndent("", "  ")
	}
	_ = enc.Encode(envelope{
		OK:            false,
		SchemaVersion: schemaVersion,
		Error:         &errObj{Code: string(xerrors.CodeOf(err)), Message: errMessage(err)},
	})
	os.Exit(1)
}

func errMessage(err error) string {
	if e, ok := err.(*xerrors.Error); ok {
		return e.Message
	}
	return err.Error()
}

// --- shared flag parsing ---

type commonFlags struct {
	account string
	count   int
	cursor  string
}

// parseCommon pulls --account/--count/--cursor (and -n) out of args, returning
// the remaining positional args. A tiny hand-rolled parser keeps each command's
// surface obvious without a flag library per subcommand.
func parseCommon(args []string) (commonFlags, []string) {
	cf := commonFlags{account: os.Getenv("AGENTX_ACCOUNT")}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--account", "-a":
			i++
			if i < len(args) {
				cf.account = args[i]
			}
		case "--count", "-n":
			i++
			if i < len(args) {
				cf.count = atoi(args[i])
			}
		case "--cursor":
			i++
			if i < len(args) {
				cf.cursor = args[i]
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return cf, rest
}

func flagValue(args []string, name string) (string, []string) {
	var rest []string
	val := ""
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			i++
			if i < len(args) {
				val = args[i]
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return val, rest
}

// flagValues collects every occurrence of a repeatable flag (e.g. --media a
// --media b), returning the values and the remaining args.
func flagValues(args []string, name string) ([]string, []string) {
	var vals, rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			i++
			if i < len(args) {
				vals = append(vals, args[i])
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return vals, rest
}

// uploadMediaFiles uploads each local file and returns the resulting media ids,
// detecting the MIME type from the file contents. alts[i], when present, is set
// as the accessibility alt text for paths[i].
func uploadMediaFiles(ctx context.Context, ag *agent.Agent, account string, paths, alts []string) []string {
	var ids []string
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			fail(xerrors.New(xerrors.InvalidInput, "read media %q: %v", p, err))
		}
		id, err := ag.UploadMedia(ctx, account, data, http.DetectContentType(data))
		if err != nil {
			fail(err)
		}
		if i < len(alts) && alts[i] != "" {
			if err := ag.SetAltText(ctx, account, id, alts[i]); err != nil {
				fail(err)
			}
		}
		ids = append(ids, id)
	}
	return ids
}

func hasFlag(args []string, name string) ([]string, bool) {
	var rest []string
	found := false
	for _, a := range args {
		if a == name {
			found = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, found
}

func newAgent() *agent.Agent {
	store, err := account.Open(account.DefaultPath())
	if err != nil {
		fail(err)
	}
	return agent.New(store)
}

// --- read commands ---

func cmdFeed(ctx context.Context, args []string) {
	args, latest := hasFlag(args, "--latest")
	cf, _ := parseCommon(args)
	page, err := newAgent().Feed(ctx, cf.account, latest, cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdSearch(ctx context.Context, args []string) {
	product, args := flagValue(args, "--type")
	cf, rest := parseCommon(args)
	query := strings.Join(rest, " ")
	if query == "" {
		fail(xerrors.New(xerrors.InvalidInput, "search query is required"))
	}
	page, err := newAgent().Search(ctx, cf.account, query, product, cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdUser(ctx context.Context, args []string) {
	id, args := flagValue(args, "--id")
	cf, rest := parseCommon(args)
	ag := newAgent()
	if id != "" {
		u, err := ag.UserByID(ctx, cf.account, id)
		if err != nil {
			fail(err)
		}
		emit(u, nil)
		return
	}
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx user <screen_name> | user --id <user_id>"))
	}
	u, err := ag.User(ctx, cf.account, strings.TrimPrefix(rest[0], "@"))
	if err != nil {
		fail(err)
	}
	emit(u, nil)
}

// cmdMedia handles `media <screen_name|id>` — a user's photos/videos tab.
func cmdMedia(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx media <screen_name|user_id>"))
	}
	page, err := newAgent().UserMedia(ctx, cf.account, rest[0], cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdRetweeters(ctx context.Context, args []string) { engagersCmd(ctx, args, true) }
func cmdLikers(ctx context.Context, args []string)     { engagersCmd(ctx, args, false) }

func engagersCmd(ctx context.Context, args []string, rt bool) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx retweeters|likers <id|url>"))
	}
	id := parseTweetID(rest[0])
	ag := newAgent()
	var page *models.UserPage
	var err error
	if rt {
		page, err = ag.Retweeters(ctx, cf.account, id, cf.count, cf.cursor)
	} else {
		page, err = ag.Favoriters(ctx, cf.account, id, cf.count, cf.cursor)
	}
	if err != nil {
		fail(err)
	}
	emit(page.Users, &pagination{NextCursor: page.NextCursor})
}

// cmdPoll posts a poll: `poll <text...> --option A --option B [--option C]
// [--option D] [--duration 1440]` (duration in minutes, default 1 day).
func cmdPoll(ctx context.Context, args []string) {
	options, args := flagValues(args, "--option")
	durStr, args := flagValue(args, "--duration")
	cf, rest := parseCommon(args)
	if len(rest) == 0 || len(options) < 2 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx poll <text...> --option A --option B [--option C] [--option D] [--duration minutes]"))
	}
	duration := atoi(durStr)
	t, err := newAgent().PostPoll(ctx, cf.account, strings.Join(rest, " "), options, duration)
	if err != nil {
		fail(err)
	}
	emit(t, nil)
}

// cmdEdit edits an existing tweet: `edit <id|url> <text...>` (Premium-gated).
func cmdEdit(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) < 2 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx edit <id|url> <text...>"))
	}
	id := parseTweetID(rest[0])
	t, err := newAgent().EditTweet(ctx, cf.account, id, strings.Join(rest[1:], " "))
	if err != nil {
		fail(err)
	}
	emit(t, nil)
}

// cmdDraft handles `draft <text...>` (create), `draft delete <id>`.
func cmdDraft(ctx context.Context, args []string) {
	if len(args) > 0 && args[0] == "delete" {
		cf, rest := parseCommon(args[1:])
		if len(rest) == 0 {
			fail(xerrors.New(xerrors.InvalidInput, "usage: agentx draft delete <draft_id>"))
		}
		if err := newAgent().DeleteDraft(ctx, cf.account, rest[0]); err != nil {
			fail(err)
		}
		emit(map[string]any{"action": "draft delete", "draftId": rest[0], "done": true}, nil)
		return
	}
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx draft <text...> | draft delete <id>"))
	}
	id, err := newAgent().CreateDraft(ctx, cf.account, strings.Join(rest, " "))
	if err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "draft", "draftId": id, "done": true}, nil)
}

// cmdDrafts lists saved drafts.
func cmdDrafts(ctx context.Context, args []string) {
	cf, _ := parseCommon(args)
	list, err := newAgent().Drafts(ctx, cf.account)
	if err != nil {
		fail(err)
	}
	emit(list, nil)
}

// cmdAnalytics reports a tweet's metrics + engagement rate: `analytics <id|url>`.
func cmdAnalytics(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx analytics <id|url>"))
	}
	a, err := newAgent().Analytics(ctx, cf.account, parseTweetID(rest[0]))
	if err != nil {
		fail(err)
	}
	emit(a, nil)
}

// cmdGrok asks Grok a question (`grok <prompt...>`) or generates an image
// (`grok image <prompt...> [--out file]`).
func cmdGrok(ctx context.Context, args []string) {
	out, args := flagValue(args, "--out")
	cf, rest := parseCommon(args)
	if len(rest) > 0 && rest[0] == "image" {
		grokImage(ctx, cf.account, out, rest[1:])
		return
	}
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx grok <prompt...> | grok image <prompt...> [--out file]"))
	}
	reply, err := newAgent().GrokAsk(ctx, cf.account, strings.Join(rest, " "))
	if err != nil {
		fail(err)
	}
	emit(map[string]any{"reply": reply}, nil)
}

// grokImage generates image(s) with Grok and saves them locally.
func grokImage(ctx context.Context, account, out string, rest []string) {
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx grok image <prompt...> [--out file]"))
	}
	ag := newAgent()
	urls, err := ag.GrokImage(ctx, account, strings.Join(rest, " "))
	if err != nil {
		fail(err)
	}
	var saved []map[string]any
	for i, u := range urls {
		data, ct, derr := ag.DownloadMedia(ctx, account, u)
		if derr != nil {
			continue
		}
		name := out
		if name == "" || len(urls) > 1 {
			name = fmt.Sprintf("grok_image_%d%s", i+1, extForContentType(ct))
		}
		if werr := os.WriteFile(name, data, 0o644); werr != nil {
			fail(xerrors.New(xerrors.APIError, "write %q: %v", name, werr))
		}
		saved = append(saved, map[string]any{"file": name, "bytes": len(data), "contentType": ct})
	}
	if len(saved) == 0 {
		fail(xerrors.New(xerrors.APIError, "no image could be downloaded"))
	}
	emit(saved, nil)
}

func extForContentType(ct string) string {
	switch {
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	default:
		return ".img"
	}
}

// cmdVote votes on a poll: `vote <id|url> <choice>` (choice is 1..N).
func cmdVote(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) < 2 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx vote <id|url> <choice 1-4>"))
	}
	choice := atoi(rest[1])
	if choice < 1 || choice > 4 {
		fail(xerrors.New(xerrors.InvalidInput, "choice must be 1-4"))
	}
	id := parseTweetID(rest[0])
	if err := newAgent().VotePoll(ctx, cf.account, id, choice); err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "vote", "tweetId": id, "choice": choice, "done": true}, nil)
}

// cmdProfile edits the account's profile:
// `profile [--name N] [--bio B] [--location L] [--url U]`.
func cmdProfile(ctx context.Context, args []string) {
	name, args := flagValue(args, "--name")
	bio, args := flagValue(args, "--bio")
	loc, args := flagValue(args, "--location")
	link, args := flagValue(args, "--url")
	cf, _ := parseCommon(args)
	if name == "" && bio == "" && loc == "" && link == "" {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx profile [--name N] [--bio B] [--location L] [--url U]"))
	}
	f := xclient.ProfileFields{Name: name, Description: bio, Location: loc, URL: link}
	if err := newAgent().UpdateProfile(ctx, cf.account, f); err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "profile", "updated": true}, nil)
}

// cmdSettings reads account settings, or updates privacy fields when flags are
// given: `settings [--protected b] [--dms-from all|following] [--discoverable-email b] [--geo b]`.
func cmdSettings(ctx context.Context, args []string) {
	protected, args := flagValue(args, "--protected")
	dms, args := flagValue(args, "--dms-from")
	email, args := flagValue(args, "--discoverable-email")
	geo, args := flagValue(args, "--geo")
	cf, _ := parseCommon(args)
	ag := newAgent()
	fields := map[string]string{}
	for k, v := range map[string]string{
		"protected": protected, "allow_dms_from": dms,
		"discoverable_by_email": email, "geo_enabled": geo,
	} {
		if v != "" {
			fields[k] = v
		}
	}
	if len(fields) > 0 {
		if err := ag.UpdateSettings(ctx, cf.account, fields); err != nil {
			fail(err)
		}
	}
	s, err := ag.Settings(ctx, cf.account)
	if err != nil {
		fail(err)
	}
	emit(s, nil)
}

func cmdAvatar(ctx context.Context, args []string) { profileImageCmd(ctx, args, false) }
func cmdBanner(ctx context.Context, args []string) { profileImageCmd(ctx, args, true) }

func profileImageCmd(ctx context.Context, args []string, banner bool) {
	cf, rest := parseCommon(args)
	what := "avatar"
	if banner {
		what = "banner"
	}
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx %s <image_file>", what))
	}
	data, err := os.ReadFile(rest[0])
	if err != nil {
		fail(xerrors.New(xerrors.InvalidInput, "read %s: %v", rest[0], err))
	}
	ag := newAgent()
	if banner {
		err = ag.UpdateBanner(ctx, cf.account, data)
	} else {
		err = ag.UpdateAvatar(ctx, cf.account, data)
	}
	if err != nil {
		fail(err)
	}
	emit(map[string]any{"action": what, "updated": true}, nil)
}

// cmdTopic handles `topic follow <id>` and `topic unfollow <id>`.
func cmdTopic(ctx context.Context, args []string) {
	if len(args) < 1 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx topic follow|unfollow <topic_id>"))
	}
	verb := args[0]
	cf, rest := parseCommon(args[1:])
	if len(rest) == 0 || (verb != "follow" && verb != "unfollow") {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx topic follow|unfollow <topic_id>"))
	}
	id := rest[0]
	ag := newAgent()
	var err error
	if verb == "follow" {
		err = ag.FollowTopic(ctx, cf.account, id)
	} else {
		err = ag.UnfollowTopic(ctx, cf.account, id)
	}
	if err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "topic " + verb, "topicId": id, "done": true}, nil)
}

// cmdBookmark bookmarks a tweet, optionally into a folder (--folder <id>).
func cmdBookmark(ctx context.Context, args []string) {
	folder, args := flagValue(args, "--folder")
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx bookmark <id|url> [--folder <folder_id>]"))
	}
	id := parseTweetID(rest[0])
	ag := newAgent()
	if folder != "" {
		if err := ag.BookmarkToFolder(ctx, cf.account, id, folder); err != nil {
			fail(err)
		}
		emit(map[string]any{"action": "bookmark", "tweetId": id, "folderId": folder, "done": true}, nil)
		return
	}
	if err := ag.Bookmark(ctx, cf.account, id); err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "bookmark", "tweetId": id, "done": true}, nil)
}

// cmdSchedule handles `schedule <when> <text...>` where <when> is a unix
// timestamp (seconds) or a relative offset like +30m, +2h, +1d.
func cmdSchedule(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) < 2 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx schedule <unixSeconds|+30m|+2h|+1d> <text...>"))
	}
	executeAt, err := parseWhen(rest[0])
	if err != nil {
		fail(err)
	}
	text := strings.Join(rest[1:], " ")
	id, err := newAgent().ScheduleTweet(ctx, cf.account, text, executeAt)
	if err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "schedule", "scheduledTweetId": id, "executeAt": executeAt, "text": text}, nil)
}

func cmdScheduled(ctx context.Context, args []string) {
	cf, _ := parseCommon(args)
	list, err := newAgent().ScheduledTweets(ctx, cf.account)
	if err != nil {
		fail(err)
	}
	emit(list, nil)
}

func cmdUnschedule(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx unschedule <scheduled_tweet_id>"))
	}
	if err := newAgent().DeleteScheduledTweet(ctx, cf.account, rest[0]); err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "unschedule", "scheduledTweetId": rest[0], "done": true}, nil)
}

// parseWhen converts a unix-seconds string or a relative offset (+30m, +2h,
// +1d) into an absolute unix timestamp in seconds.
func parseWhen(s string) (int64, error) {
	if strings.HasPrefix(s, "+") {
		off := strings.TrimPrefix(s, "+")
		// time.ParseDuration has no day unit; expand a trailing "d" to hours.
		if strings.HasSuffix(off, "d") {
			if days := atoi(strings.TrimSuffix(off, "d")); days > 0 {
				return time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix(), nil
			}
		}
		d, err := time.ParseDuration(off)
		if err != nil {
			return 0, xerrors.New(xerrors.InvalidInput, "bad offset %q (use +30m, +2h, +1d)", s)
		}
		return time.Now().Add(d).Unix(), nil
	}
	n := int64(atoi(s))
	if n <= 0 {
		return 0, xerrors.New(xerrors.InvalidInput, "bad time %q (unix seconds or +offset)", s)
	}
	return n, nil
}

// cmdTrends handles `trends [woeid]` (default worldwide) and `trends locations`
// (places with WOEIDs, e.g. Indonesia = 23424846).
func cmdTrends(ctx context.Context, args []string) {
	if len(args) > 0 && args[0] == "locations" {
		cf, _ := parseCommon(args[1:])
		locs, err := newAgent().TrendLocations(ctx, cf.account)
		if err != nil {
			fail(err)
		}
		emit(locs, nil)
		return
	}
	cf, rest := parseCommon(args)
	woeid := ""
	if len(rest) > 0 {
		woeid = rest[0]
	}
	trends, err := newAgent().Trends(ctx, cf.account, woeid)
	if err != nil {
		fail(err)
	}
	emit(trends, nil)
}

func cmdPosts(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx posts <screen_name>"))
	}
	page, err := newAgent().UserPosts(ctx, cf.account, strings.TrimPrefix(rest[0], "@"), cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdTweet(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx tweet <id|url>"))
	}
	t, err := newAgent().Tweet(ctx, cf.account, parseTweetID(rest[0]))
	if err != nil {
		fail(err)
	}
	emit(t, nil)
}

func cmdThread(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx thread <id|url>"))
	}
	page, err := newAgent().Thread(ctx, cf.account, parseTweetID(rest[0]))
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdArticle(ctx context.Context, args []string) {
	args, markdown := hasFlag(args, "--markdown")
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx article <id|url> [--markdown]"))
	}
	t, err := newAgent().Article(ctx, cf.account, parseTweetID(rest[0]), markdown)
	if err != nil {
		fail(err)
	}
	emit(t, nil)
}

// cmdBookmarks handles `bookmarks` (all), plus `bookmarks folders`,
// `bookmarks folder <id>`, `bookmarks folder create <name>`, and
// `bookmarks folder delete <id>`.
func cmdBookmarks(ctx context.Context, args []string) {
	if len(args) > 0 && args[0] == "folders" {
		list, err := newAgent().BookmarkFolders(ctx, mustAccount(args[1:]))
		if err != nil {
			fail(err)
		}
		emit(list, nil)
		return
	}
	if len(args) > 0 && args[0] == "folder" {
		cmdBookmarkFolder(ctx, args[1:])
		return
	}
	cf, _ := parseCommon(args)
	page, err := newAgent().Bookmarks(ctx, cf.account, cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdBookmarkFolder(ctx context.Context, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "create":
			cf, rest := parseCommon(args[1:])
			if len(rest) == 0 {
				fail(xerrors.New(xerrors.InvalidInput, "usage: agentx bookmarks folder create <name>"))
			}
			f, err := newAgent().CreateBookmarkFolder(ctx, cf.account, strings.Join(rest, " "))
			if err != nil {
				fail(err)
			}
			emit(f, nil)
			return
		case "delete":
			cf, rest := parseCommon(args[1:])
			if len(rest) == 0 {
				fail(xerrors.New(xerrors.InvalidInput, "usage: agentx bookmarks folder delete <folder_id>"))
			}
			if err := newAgent().DeleteBookmarkFolder(ctx, cf.account, rest[0]); err != nil {
				fail(err)
			}
			emit(map[string]any{"action": "bookmark folder delete", "folderId": rest[0], "done": true}, nil)
			return
		}
	}
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx bookmarks folder <folder_id> | create <name> | delete <id>"))
	}
	page, err := newAgent().BookmarkFolderTweets(ctx, cf.account, rest[0], cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

// mustAccount returns the account selected by global flags in args.
func mustAccount(args []string) string {
	cf, _ := parseCommon(args)
	return cf.account
}

func cmdLikes(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx likes <screen_name>"))
	}
	page, err := newAgent().Likes(ctx, cf.account, strings.TrimPrefix(rest[0], "@"), cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdFollowers(ctx context.Context, args []string) { userListCmd(ctx, args, "followers") }
func cmdFollowing(ctx context.Context, args []string) { userListCmd(ctx, args, "following") }

func userListCmd(ctx context.Context, args []string, which string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx %s <screen_name>", which))
	}
	name := strings.TrimPrefix(rest[0], "@")
	ag := newAgent()
	var page *models.UserPage
	var err error
	if which == "followers" {
		page, err = ag.Followers(ctx, cf.account, name, cf.count, cf.cursor)
	} else {
		page, err = ag.Following(ctx, cf.account, name, cf.count, cf.cursor)
	}
	if err != nil {
		fail(err)
	}
	emit(page.Users, &pagination{NextCursor: page.NextCursor})
}

// cmdList handles `list <id|url>` (tweets in a List) plus the subcommands
// `list members <id|url>`, `list create <name>`, and `list delete <id|url>`.
func cmdList(ctx context.Context, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "members":
			cmdListMembers(ctx, args[1:])
			return
		case "create":
			cmdListCreate(ctx, args[1:])
			return
		case "delete":
			cmdListDelete(ctx, args[1:])
			return
		case "subscribe", "follow", "unsubscribe", "unfollow":
			cmdListSubscribe(ctx, args[0], args[1:])
			return
		case "add", "remove":
			cmdListMember(ctx, args[0], args[1:])
			return
		}
	}
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx list <list_id|url> | list members <id> | list subscribe <id> | list create <name> | list delete <id>"))
	}
	page, err := newAgent().ListTweets(ctx, cf.account, parseTweetID(rest[0]), cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

func cmdListMembers(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx list members <list_id|url>"))
	}
	page, err := newAgent().ListMembers(ctx, cf.account, parseTweetID(rest[0]), cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Users, &pagination{NextCursor: page.NextCursor})
}

func cmdListCreate(ctx context.Context, args []string) {
	desc, args := flagValue(args, "--desc")
	args, private := hasFlag(args, "--private")
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx list create <name> [--desc text] [--private]"))
	}
	name := strings.Join(rest, " ")
	out, err := newAgent().CreateList(ctx, cf.account, name, desc, private)
	if err != nil {
		fail(err)
	}
	emit(out, nil)
}

func cmdListSubscribe(ctx context.Context, verb string, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx list %s <list_id|url>", verb))
	}
	id := parseTweetID(rest[0])
	off := verb == "unsubscribe" || verb == "unfollow"
	ag := newAgent()
	var err error
	if off {
		err = ag.UnsubscribeList(ctx, cf.account, id)
	} else {
		err = ag.SubscribeList(ctx, cf.account, id)
	}
	if err != nil {
		fail(err)
	}
	action := "list subscribe"
	if off {
		action = "list unsubscribe"
	}
	emit(map[string]any{"action": action, "listId": id, "done": true}, nil)
}

func cmdListMember(ctx context.Context, verb string, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) < 2 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx list %s <list_id|url> <screen_name|user_id>", verb))
	}
	listID := parseTweetID(rest[0])
	member := rest[1]
	ag := newAgent()
	var err error
	if verb == "add" {
		err = ag.AddListMember(ctx, cf.account, listID, member)
	} else {
		err = ag.RemoveListMember(ctx, cf.account, listID, member)
	}
	if err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "list " + verb, "listId": listID, "member": member, "done": true}, nil)
}

func cmdListDelete(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx list delete <list_id|url>"))
	}
	id := parseTweetID(rest[0])
	if err := newAgent().DeleteList(ctx, cf.account, id); err != nil {
		fail(err)
	}
	emit(map[string]any{"action": "list delete", "listId": id, "done": true}, nil)
}

// cmdCommunity handles `community posts <id>` (with --latest), `community info
// <id>`, `community join <id>`, and `community leave <id>`.
func cmdCommunity(ctx context.Context, args []string) {
	if len(args) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx community <posts|info|join|leave> <id|url>"))
	}
	sub := args[0]
	if sub == "create" {
		cmdCommunityCreate(ctx, args[1:])
		return
	}
	rest, latest := hasFlag(args[1:], "--latest")
	cf, rest := parseCommon(rest)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx community %s <community_id|url>", sub))
	}
	id := parseTweetID(rest[0])
	ag := newAgent()
	switch sub {
	case "posts", "tweets":
		page, err := ag.CommunityTweets(ctx, cf.account, id, cf.count, cf.cursor, latest)
		if err != nil {
			fail(err)
		}
		emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
	case "info":
		info, err := ag.CommunityInfo(ctx, cf.account, id)
		if err != nil {
			fail(err)
		}
		emit(info, nil)
	case "join":
		if err := ag.JoinCommunity(ctx, cf.account, id); err != nil {
			fail(err)
		}
		emit(map[string]any{"action": "join", "communityId": id, "done": true}, nil)
	case "leave":
		if err := ag.LeaveCommunity(ctx, cf.account, id); err != nil {
			fail(err)
		}
		emit(map[string]any{"action": "leave", "communityId": id, "done": true}, nil)
	default:
		fail(xerrors.New(xerrors.InvalidInput, "unknown community subcommand %q (use posts|info|join|leave|create)", sub))
	}
}

func cmdCommunityCreate(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx community create <name>"))
	}
	name := strings.Join(rest, " ")
	out, err := newAgent().CreateCommunity(ctx, cf.account, name)
	if err != nil {
		fail(err)
	}
	emit(out, nil)
}

// --- write commands ---

func cmdPost(ctx context.Context, args []string) {
	media, args := flagValues(args, "--media")
	alts, args := flagValues(args, "--alt")
	args, thread := hasFlag(args, "--thread")
	cf, rest := parseCommon(args)
	text := strings.Join(rest, " ")
	ag := newAgent()
	ids := uploadMediaFiles(ctx, ag, cf.account, media, alts)
	if thread {
		chunks := splitForThread(text, 250)
		tws, err := ag.PostThread(ctx, cf.account, chunks, ids)
		if err != nil {
			fail(err)
		}
		emit(tws, nil)
		return
	}
	t, err := ag.Post(ctx, cf.account, text, xclient.PostOptions{MediaIDs: ids})
	if err != nil {
		fail(err)
	}
	emit(t, nil)
}

// splitForThread breaks text into <=limit-rune chunks on word boundaries
// (hard-splitting any single word longer than the limit).
func splitForThread(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= limit {
		return []string{text}
	}
	var chunks []string
	cur := ""
	flush := func() {
		for len([]rune(cur)) > limit {
			r := []rune(cur)
			chunks = append(chunks, string(r[:limit]))
			cur = string(r[limit:])
		}
	}
	for _, w := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = w
		case len([]rune(cur))+1+len([]rune(w)) <= limit:
			cur += " " + w
		default:
			flush()
			chunks = append(chunks, cur)
			cur = w
		}
		flush()
	}
	if cur != "" {
		chunks = append(chunks, cur)
	}
	return chunks
}

// cmdDownload saves media from a tweet (id/url) or a direct media URL.
func cmdDownload(ctx context.Context, args []string) {
	out, args := flagValue(args, "--out")
	args, all := hasFlag(args, "--all")
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx download <tweet|url> [--out file] [--all]"))
	}
	target := rest[0]
	ag := newAgent()

	var urls []string
	if strings.HasPrefix(target, "http") && !strings.Contains(target, "/status/") {
		urls = []string{target} // already a direct media URL
	} else {
		t, err := ag.Tweet(ctx, cf.account, parseTweetID(target))
		if err != nil {
			fail(err)
		}
		for _, m := range t.Media {
			urls = append(urls, m.URL)
		}
		if len(urls) == 0 {
			fail(xerrors.New(xerrors.NotFound, "tweet has no downloadable media"))
		}
		if !all {
			urls = urls[:1]
		}
	}

	var saved []map[string]any
	for i, u := range urls {
		data, ct, err := ag.DownloadMedia(ctx, cf.account, u)
		if err != nil {
			fail(err)
		}
		name := out
		if name == "" || len(urls) > 1 {
			name = mediaFilename(u, ct)
		}
		if err := os.WriteFile(name, data, 0o644); err != nil {
			fail(xerrors.New(xerrors.APIError, "write %q: %v", name, err))
		}
		saved = append(saved, map[string]any{"saved": name, "bytes": len(data), "contentType": ct})
		_ = i
	}
	emit(saved, nil)
}

// cmdWatch polls mentions or a search on an interval, emitting each new tweet as
// a compact NDJSON line. With --once it emits the current matches and exits.
func cmdWatch(ctx context.Context, args []string) {
	intervalStr, args := flagValue(args, "--interval")
	args, once := hasFlag(args, "--once")
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx watch <mentions|search <query...>> [--interval 60s] [--once]"))
	}
	mode := rest[0]
	query := strings.Join(rest[1:], " ")
	if mode == "search" && query == "" {
		fail(xerrors.New(xerrors.InvalidInput, "watch search needs a query"))
	}
	interval := 60 * time.Second
	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			interval = d
		}
	}

	ag := newAgent()
	enc := json.NewEncoder(os.Stdout) // compact NDJSON (one object per line)
	seen := map[string]bool{}
	bg := context.Background()
	first := true

	poll := func() ([]models.Tweet, error) {
		pctx, cancel := context.WithTimeout(bg, 90*time.Second)
		defer cancel()
		switch mode {
		case "mentions":
			p, err := ag.Mentions(pctx, cf.account, cf.count, "")
			if err != nil {
				return nil, err
			}
			return p.Tweets, nil
		case "search":
			p, err := ag.Search(pctx, cf.account, query, "Latest", cf.count, "")
			if err != nil {
				return nil, err
			}
			return p.Tweets, nil
		default:
			return nil, xerrors.New(xerrors.InvalidInput, "watch mode must be mentions or search")
		}
	}

	for {
		tweets, err := poll()
		if err != nil {
			_ = enc.Encode(map[string]any{"ok": false, "error": errMessage(err)})
		} else {
			// oldest -> newest so the stream is chronological
			for i := len(tweets) - 1; i >= 0; i-- {
				t := tweets[i]
				if seen[t.ID] {
					continue
				}
				seen[t.ID] = true
				if first && !once {
					continue // seed backlog silently; only stream genuinely new items
				}
				_ = enc.Encode(map[string]any{"ok": true, "watch": mode, "tweet": t})
			}
		}
		first = false
		if once {
			return
		}
		time.Sleep(interval)
	}
}

func cmdReply(ctx context.Context, args []string) {
	media, args := flagValues(args, "--media")
	alts, args := flagValues(args, "--alt")
	cf, rest := parseCommon(args)
	if len(rest) == 0 || (len(rest) < 2 && len(media) == 0) {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx reply <id|url> <text...> [--media file]"))
	}
	id := parseTweetID(rest[0])
	text := strings.Join(rest[1:], " ")
	ag := newAgent()
	ids := uploadMediaFiles(ctx, ag, cf.account, media, alts)
	t, err := ag.Post(ctx, cf.account, text, xclient.PostOptions{ReplyToID: id, MediaIDs: ids})
	if err != nil {
		fail(err)
	}
	emit(t, nil)
}

func cmdQuote(ctx context.Context, args []string) {
	media, args := flagValues(args, "--media")
	alts, args := flagValues(args, "--alt")
	cf, rest := parseCommon(args)
	if len(rest) == 0 || (len(rest) < 2 && len(media) == 0) {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx quote <id|url> <text...> [--media file]"))
	}
	id := parseTweetID(rest[0])
	text := strings.Join(rest[1:], " ")
	ag := newAgent()
	ids := uploadMediaFiles(ctx, ag, cf.account, media, alts)
	t, err := ag.Post(ctx, cf.account, text, xclient.PostOptions{QuoteTweetID: id, MediaIDs: ids})
	if err != nil {
		fail(err)
	}
	emit(t, nil)
}

func cmdMentions(ctx context.Context, args []string) {
	cf, _ := parseCommon(args)
	page, err := newAgent().Mentions(ctx, cf.account, cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Tweets, &pagination{NextCursor: page.NextCursor})
}

// cmdUnread reports unread counts (notifications, DMs, XChat).
func cmdUnread(ctx context.Context, args []string) {
	cf, _ := parseCommon(args)
	b, err := newAgent().Badges(ctx, cf.account)
	if err != nil {
		fail(err)
	}
	emit(b, nil)
}

// cmdNotifications handles `notifications [all|mentions|verified]`.
func cmdNotifications(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	tab := "all"
	if len(rest) > 0 {
		tab = rest[0]
	}
	page, err := newAgent().Notifications(ctx, cf.account, tab, cf.count, cf.cursor)
	if err != nil {
		fail(err)
	}
	emit(page.Items, &pagination{NextCursor: page.NextCursor})
}

// cmdDM handles `dm list` and `dm send <screen_name> <text...>`.
func cmdDM(ctx context.Context, args []string) {
	cf, rest := parseCommon(args)
	if len(rest) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx dm list | dm send <screen_name> <text...>"))
	}
	ag := newAgent()
	switch rest[0] {
	case "list":
		msgs, err := ag.DirectMessages(ctx, cf.account, cf.count)
		if err != nil {
			fail(err)
		}
		emit(msgs, nil)
	case "send":
		mediaFiles, r := flagValues(rest, "--media")
		minArgs := 3
		if len(mediaFiles) > 0 {
			minArgs = 2 // text optional when media is attached
		}
		if len(r) < minArgs {
			fail(xerrors.New(xerrors.InvalidInput, "usage: agentx dm send <screen_name> <text...> [--media file]"))
		}
		target := strings.TrimPrefix(r[1], "@")
		text := strings.Join(r[2:], " ")
		var err error
		if len(mediaFiles) > 0 {
			data, rerr := os.ReadFile(mediaFiles[0])
			if rerr != nil {
				fail(xerrors.New(xerrors.InvalidInput, "read media %q: %v", mediaFiles[0], rerr))
			}
			err = ag.SendDMMedia(ctx, cf.account, target, text, data, http.DetectContentType(data))
		} else {
			err = ag.SendDM(ctx, cf.account, target, text)
		}
		if err != nil {
			fail(err)
		}
		emit(map[string]any{"action": "dm", "to": target, "done": true}, nil)
	case "download":
		out, dargs := flagValue(rest[1:], "--out")
		if len(dargs) == 0 {
			fail(xerrors.New(xerrors.InvalidInput, "usage: agentx dm download <media_url> [--out file]"))
		}
		mediaURL := dargs[0]
		data, contentType, err := ag.DownloadMedia(ctx, cf.account, mediaURL)
		if err != nil {
			fail(err)
		}
		if out == "" {
			out = mediaFilename(mediaURL, contentType)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			fail(xerrors.New(xerrors.APIError, "write %q: %v", out, err))
		}
		emit(map[string]any{"saved": out, "bytes": len(data), "contentType": contentType}, nil)
	default:
		fail(xerrors.New(xerrors.InvalidInput, "unknown dm subcommand %q (use list|send|download)", rest[0]))
	}
}

func cmdMe(ctx context.Context, args []string) {
	cf, _ := parseCommon(args)
	u, err := newAgent().Me(ctx, cf.account)
	if err != nil {
		fail(err)
	}
	emit(u, nil)
}

// userAction builds a handler for the social-graph write commands that operate
// on a target @handle (follow/mute/block and their inverses).
func userAction(name string) handler {
	return func(ctx context.Context, args []string) {
		cf, rest := parseCommon(args)
		if len(rest) == 0 {
			fail(xerrors.New(xerrors.InvalidInput, "usage: agentx %s <screen_name>", name))
		}
		target := strings.TrimPrefix(rest[0], "@")
		ag := newAgent()
		var err error
		switch name {
		case "follow":
			err = ag.Follow(ctx, cf.account, target)
		case "unfollow":
			err = ag.Unfollow(ctx, cf.account, target)
		case "mute":
			err = ag.Mute(ctx, cf.account, target)
		case "unmute":
			err = ag.Unmute(ctx, cf.account, target)
		case "block":
			err = ag.Block(ctx, cf.account, target)
		case "unblock":
			err = ag.Unblock(ctx, cf.account, target)
		}
		if err != nil {
			fail(err)
		}
		emit(map[string]any{"action": name, "user": target, "done": true}, nil)
	}
}

func simpleAction(name string) handler {
	return func(ctx context.Context, args []string) {
		cf, rest := parseCommon(args)
		if len(rest) == 0 {
			fail(xerrors.New(xerrors.InvalidInput, "usage: agentx %s <id|url>", name))
		}
		id := parseTweetID(rest[0])
		ag := newAgent()
		var err error
		switch name {
		case "like":
			err = ag.Like(ctx, cf.account, id)
		case "unlike":
			err = ag.Unlike(ctx, cf.account, id)
		case "retweet":
			err = ag.Retweet(ctx, cf.account, id)
		case "unretweet":
			err = ag.Unretweet(ctx, cf.account, id)
		case "delete":
			err = ag.Delete(ctx, cf.account, id)
		case "bookmark":
			err = ag.Bookmark(ctx, cf.account, id)
		case "unbookmark":
			err = ag.Unbookmark(ctx, cf.account, id)
		case "pin":
			err = ag.PinTweet(ctx, cf.account, id)
		case "unpin":
			err = ag.UnpinTweet(ctx, cf.account, id)
		}
		if err != nil {
			fail(err)
		}
		emit(map[string]any{"action": name, "tweetId": id, "done": true}, nil)
	}
}

// --- account management ---

func cmdAccount(ctx context.Context, args []string) {
	if len(args) == 0 {
		fail(xerrors.New(xerrors.InvalidInput, "usage: agentx account <add|list|remove|default>"))
	}
	store, err := account.Open(account.DefaultPath())
	if err != nil {
		fail(err)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "add":
		acc := &account.Account{}
		acc.Name, rest = flagValue(rest, "--name")
		acc.AuthToken, rest = flagValue(rest, "--auth-token")
		acc.CT0, rest = flagValue(rest, "--ct0")
		acc.CookieString, rest = flagValue(rest, "--cookie")
		acc.UserAgent, rest = flagValue(rest, "--user-agent")
		acc.Proxy, _ = flagValue(rest, "--proxy")
		// env fallbacks
		if acc.AuthToken == "" {
			acc.AuthToken = os.Getenv("TWITTER_AUTH_TOKEN")
		}
		if acc.CT0 == "" {
			acc.CT0 = os.Getenv("TWITTER_CT0")
		}
		if err := store.Add(acc); err != nil {
			fail(err)
		}
		emit(map[string]any{"added": acc.Name, "default": store.Default()}, nil)

	case "list":
		def := store.Default()
		views := make([]accountView, 0)
		for _, a := range store.List() {
			views = append(views, redactedAccount(a, a.Name == def))
		}
		emit(map[string]any{"accounts": views, "default": def}, nil)

	case "login":
		name, rest2 := flagValue(rest, "--name")
		username, rest2 := flagValue(rest2, "--username")
		password, rest2 := flagValue(rest2, "--password")
		proxy, _ := flagValue(rest2, "--proxy")
		if username == "" {
			username = os.Getenv("TWITTER_USERNAME")
		}
		if password == "" {
			password = os.Getenv("TWITTER_PASSWORD")
		}
		if proxy == "" {
			proxy = os.Getenv("AGENTX_PROXY")
		}
		if name == "" || username == "" || password == "" {
			fail(xerrors.New(xerrors.InvalidInput, "usage: agentx account login --name N --username U [--password P] [--proxy URL]  (password also via $TWITTER_PASSWORD)"))
		}
		res, err := xclient.Login(ctx, username, password, proxy)
		if err != nil {
			fail(err)
		}
		// Keep the proxy on the account: cookies minted behind a residential IP
		// can be flagged if then used from a datacenter IP.
		acc := &account.Account{Name: name, AuthToken: res.AuthToken, CT0: res.CT0, Proxy: proxy}
		if err := store.Add(acc); err != nil {
			fail(err)
		}
		emit(map[string]any{"added": name, "default": store.Default(), "loggedIn": true}, nil)

	case "remove":
		if len(rest) == 0 {
			fail(xerrors.New(xerrors.InvalidInput, "usage: agentx account remove <name>"))
		}
		if err := store.Remove(rest[0]); err != nil {
			fail(err)
		}
		emit(map[string]any{"removed": rest[0], "default": store.Default()}, nil)

	case "default":
		if len(rest) == 0 {
			emit(map[string]any{"default": store.Default()}, nil)
			return
		}
		if err := store.SetDefault(rest[0]); err != nil {
			fail(err)
		}
		emit(map[string]any{"default": store.Default()}, nil)

	case "check":
		name := ""
		if len(rest) > 0 {
			name = rest[0]
		}
		u, err := agent.New(store).Check(ctx, name)
		if err != nil {
			emit(map[string]any{"valid": false, "reason": errMessage(err)}, nil)
			return
		}
		emit(map[string]any{"valid": true, "screenName": u.ScreenName, "id": u.ID}, nil)

	default:
		fail(xerrors.New(xerrors.InvalidInput, "unknown account subcommand %q", sub))
	}
}

// --- account list view (never expose raw credentials) ---

// accountView is the redacted shape emitted by `account list`. The raw
// auth_token / ct0 are session secrets and must never be printed in full to an
// agent's stdout/logs, so only a short suffix and presence flags are shown.
type accountView struct {
	Name            string    `json:"name"`
	ScreenName      string    `json:"screenName,omitempty"`
	AuthToken       string    `json:"authToken"` // redacted, e.g. "…a7c37"
	CT0             string    `json:"ct0"`       // redacted
	HasCookieString bool      `json:"hasCookieString,omitempty"`
	UserAgent       string    `json:"userAgent,omitempty"`
	Proxy           string    `json:"proxy,omitempty"`
	AddedAt         time.Time `json:"addedAt"`
	LastUsed        time.Time `json:"lastUsed,omitempty"`
	Default         bool      `json:"default,omitempty"`
}

func redactedAccount(a *account.Account, isDefault bool) accountView {
	return accountView{
		Name:            a.Name,
		ScreenName:      a.ScreenName,
		AuthToken:       redactSecret(a.AuthToken),
		CT0:             redactSecret(a.CT0),
		HasCookieString: strings.TrimSpace(a.CookieString) != "",
		UserAgent:       a.UserAgent,
		Proxy:           a.Proxy,
		AddedAt:         a.AddedAt,
		LastUsed:        a.LastUsed,
		Default:         isDefault,
	}
}

// redactSecret keeps only the last 4 characters of a secret so an account can
// be eyeballed without leaking the credential.
func redactSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return "…" + s[len(s)-4:]
}

// mediaFilename derives a local filename from a media URL, appending an
// extension from the Content-Type when the URL path has none.
func mediaFilename(rawURL, contentType string) string {
	name := rawURL
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	name = path.Base(name)
	if name == "" || name == "." || name == "/" {
		name = "dm_media"
	}
	if !strings.Contains(name, ".") {
		switch {
		case strings.Contains(contentType, "jpeg"):
			name += ".jpg"
		case strings.Contains(contentType, "png"):
			name += ".png"
		case strings.Contains(contentType, "gif"):
			name += ".gif"
		case strings.Contains(contentType, "mp4"):
			name += ".mp4"
		}
	}
	return name
}

// --- helpers ---

var tweetIDRegex = regexp.MustCompile(`(\d{6,})`)

func parseTweetID(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		if m := tweetIDRegex.FindAllString(s, -1); len(m) > 0 {
			return m[len(m)-1]
		}
	}
	return s
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func usage() {
	fmt.Fprint(os.Stderr, `agentx - agent-friendly X/Twitter client (multi-account)

USAGE
  agentx <command> [flags] [args]

READ
  feed [--latest] [-n N] [--cursor C]      home timeline (for-you / following)
  search [--type Top|Latest] <query...>    advanced search
  user <screen_name>                       user profile
  me                                       the logged-in account's profile
  posts <screen_name> [-n N]               a user's tweets
  tweet <id|url>                           single tweet (falls back to FxTwitter)
  analytics <id|url>                       tweet metrics + engagement rate
  thread <id|url>                          tweet + conversation
  article <id|url> [--markdown]            long-form X Article (title + body)
  bookmarks [-n N]                         your bookmarked tweets
  bookmarks folders                        your bookmark folders
  bookmarks folder <id> [-n N]             tweets in a bookmark folder
  likes <screen_name> [-n N]               tweets a user has liked
  mentions [-n N]                          tweets mentioning you
  notifications [all|mentions|verified]    your notifications timeline
  unread                                   unread counts (notifications, DMs, XChat)
  followers <screen_name> [-n N]           a user's followers
  following <screen_name> [-n N]           accounts a user follows
  user --id <user_id>                      profile by numeric id (UserByRestId)
  media <screen_name|id> [-n N]            a user's media (photos/videos) tab
  trends [woeid]                           trending topics (default worldwide)
  trends locations                         places with WOEIDs (e.g. Indonesia 23424846)
  retweeters <id|url> [-n N]               accounts that retweeted a tweet
  likers <id|url> [-n N]                   accounts that liked a tweet
  scheduled                                your pending scheduled tweets
  list <list_id|url> [-n N]                latest tweets of a List
  list members <list_id|url> [-n N]        accounts that belong to a List
  community posts <id|url> [--latest]      posts in a Community (relevance/recency)
  community info <id|url>                  Community metadata (name, members, role)

WRITE
  post <text...> [--media f [--alt txt]]   create a tweet (media + alt text)
  poll <text...> --option A --option B [--duration min]   post a poll (2-4 options)
  edit <id|url> <text...>                  edit an existing tweet (Premium)
  draft <text...> | draft delete <id> | drafts   manage draft tweets
  vote <id|url> <choice>                   vote on a poll (choice 1-4)
  grok <prompt...>                         ask Grok and print the reply
  grok image <prompt...> [--out file]      generate image(s) with Grok and save
  profile [--name N] [--bio B] [--location L] [--url U]   edit your profile
  avatar <image> | banner <image>          set profile picture / header
  settings [--protected b] [--dms-from all|following] [--geo b]   privacy settings
  topic follow|unfollow <topic_id>         follow / unfollow a Topic
  reply <id|url> <text...> [--media f]      reply to a tweet
  quote <id|url> <text...> [--media f]      quote-tweet
  like|unlike|retweet|unretweet|delete|bookmark|unbookmark <id|url>
  bookmark <id|url> [--folder <id>]        bookmark into a folder
  bookmarks folder create <name> | delete <id>   manage bookmark folders
  follow|unfollow|mute|unmute|block|unblock <screen_name>
  community join|leave <id|url>            join or leave a Community
  community create <name>                  create a Community (permanent!)
  post <text...>                           >280 chars auto-uses long-form (Premium)
  list subscribe|unsubscribe <id|url>      follow / unfollow someone's List
  list create <name> [--desc text] [--private]   create a List
  list delete <list_id|url>                delete a List you own
  list add|remove <list_id|url> <screen_name|id>   edit a List's members
  pin|unpin <id|url>                       pin / unpin a tweet to your profile
  schedule <unix|+30m|+2h|+1d> <text...>   schedule a tweet for later
  unschedule <scheduled_tweet_id>          cancel a scheduled tweet
  dm send <screen_name> <text...> [--media f]   send a direct message (+ media)
  dm list [-n N]                           recent DMs (incl. media attachments)
  dm download <media_url> [--out file]     download DM media bytes (cookie-auth)

UTILITIES
  post --thread <text...>                  auto-split long text into a thread
  download <tweet|url> [--out f] [--all]   save tweet media (or a direct URL)
  watch <mentions|search <q>> [--interval 60s] [--once]   stream new tweets (NDJSON)
  account check [name]                     verify an account's cookies are valid

ACCOUNTS
  account add --name N --auth-token T --ct0 C [--cookie ...] [--user-agent ...] [--proxy ...]
  account login --name N --username U [--password P] [--proxy URL]   log in (user/pass -> cookies)
  account list                             (credentials are redacted)
  account remove <name>
  account default [name]

GLOBAL FLAGS
  -a, --account NAME    select account (default: store default / $AGENTX_ACCOUNT)
  -n, --count N         result count
      --cursor C        pagination cursor
      --pretty          pretty-print JSON (default is compact, token-efficient)

ENV
  AGENTX_ACCOUNT   default account name
  AGENTX_HOME      store location (default ~/.agentx)
  AGENTX_UTLS=1    use a Chrome-like TLS (JA3) fingerprint for requests
  AGENTX_QID_<Op>  override a GraphQL query id (e.g. when x.com rotates one)

Output is always a single JSON envelope: {"ok":bool,"data":...,"error":...}.
Read responses also carry a "rateLimit" object when x.com reports one.
`)
}
