---
name: agentx
description: Drive X/Twitter from the CLI (read, post, DM, lists, communities, Grok, scheduling, analytics). Use when an agent needs to read or act on X/Twitter. Every command prints one JSON envelope.
---

# agentx — X/Twitter agent control surface

`agentx` is a single Go binary. Every command prints **one JSON envelope** to stdout, so parse stdout as JSON and check `.ok`. Build: `go build -o agentx ./cmd/agentx`.

## Envelope (always this shape)
```json
{"ok":true,"schema_version":"1","data":<any>,"pagination":{"nextCursor":"…"},"rateLimit":{…}}
{"ok":false,"schema_version":"1","error":{"code":"…","message":"…"}}
```
Rules: check `.ok` first. List data → `.data` is an array + `.pagination.nextCursor`. Single object → `.data` is an object. On error read `.error.code` (machine) + `.error.message` (human). Exit code ≠0 on error. Output is **compact single-line JSON by default** (token-efficient); add `--pretty` only when a human reads it.

## Setup (once)
```
agentx account add --name NAME --auth-token <auth_token> --ct0 <ct0>
```
Cookies come from a logged-in browser (`auth_token` + `ct0`). Env fallback: `TWITTER_AUTH_TOKEN`, `TWITTER_CT0`. Store: `$AGENTX_HOME/accounts.json` (default `~/.agentx`, mode 0600). Multi-account: add several, pick with `-a NAME`. `account login --name N --username U` (pass via `$TWITTER_PASSWORD`) does user/pass→cookies but X blocks login from datacenter IPs — prefer cookies.

## Global flags (may precede or follow the subcommand)
`-a NAME` account · `-n N` max items · `--cursor C` page cursor · `--pretty` human-readable JSON · `--account`/`--count` long forms.
IDs: any `<id>` also accepts a full tweet/list/community URL. Handles: with or without `@`.

## Commands (one line each)

READ
```
feed [--latest] [-n N] [--cursor C]      home timeline
search [--type Top|Latest] <query>       search
user <handle>  |  user --id <id>         profile
me                                       own profile
posts <handle> [-n N]                    a user's tweets
media <handle|id> [-n N]                 user's photos/videos
tweet <id>                               single tweet (zero-auth FxTwitter fallback)
thread <id>                              tweet + replies
article <id> [--markdown]                long-form X Article
analytics <id>                           metrics + engagement rate
likes <handle> | bookmarks | mentions    tweet lists
followers <handle> | following <handle>  user lists
retweeters <id> | likers <id>            who RT'd/liked (likers: author-only by X)
list <id> | list members <id>            List tweets / members
community posts <id> [--latest] | community info <id>
notifications [all|mentions|verified] | unread     (alias: notif)
trends [woeid] | trends locations        trending (Indonesia woeid=23424846)
scheduled | drafts                       your queued/draft tweets
settings                                 your privacy settings
```

WRITE (state-changing; reversible ones noted)
```
post <text> [--media f [--alt txt]] [--thread]    tweet (>280 → long-form, Premium)
reply <id> <text> [--media f] | quote <id> <text> [--media f]
poll <text> --option A --option B [--option C|D] [--duration MIN]
vote <id> <choice 1-4>
like/unlike  retweet/unretweet  bookmark/unbookmark  delete  <id>
pin/unpin <id>                                    pin tweet to profile
bookmark <id> --folder <id>                       bookmark into folder
follow/unfollow  mute/unmute  block/unblock  <handle>
edit <id> <text>                                  Premium
draft <text> | draft delete <id>
schedule <unix|+30m|+2h|+1d> <text> | unschedule <id>
profile [--name N] [--bio B] [--location L] [--url U]
avatar <img> | banner <img>
settings [--protected b] [--dms-from all|following] [--discoverable-email b] [--geo b]
topic follow|unfollow <topic_id>
list create <name> [--desc T] [--private] | list delete <id>
list add|remove <listid> <handle|id> | list subscribe|unsubscribe <id>
community join|leave <id> | community create <name>   (create is permanent)
bookmarks folder create <name> | delete <id>          (Premium)
dm send <handle> <text> [--media f]
```

GROK (AI)
```
grok <prompt>                            text answer (live web search built in)
grok image <prompt> [--out file]         generate image(s), download to file(s)
```

UTILITIES
```
download <id|url> [--out f] [--all]       save tweet media / a direct URL
watch <mentions|search <q>> [--interval 60s] [--once]   NDJSON stream of new tweets
account list|remove <n>|default [n]|check [n]
```

## Pagination
List commands return `.pagination.nextCursor`. Pass it back via `--cursor` for the next page. `-n N` caps items in the response (note: the cursor still points past the full fetched page, so for exact paging use the cursor, not a tiny `-n`).

## Error codes (`.error.code`)
`not_authenticated` bad/expired cookies · `not_found` · `invalid_input` · `rate_limited` (see `.rateLimit`) · `query_id_error` X rotated a GraphQL id → set `AGENTX_QID_<Operation>=<id>` env or update constants · `network_error` · `api_error` x.com rejected (message has detail) · `account_error`.

## Gotchas
- **Premium-gated** (work only on X Premium accounts; routing is correct, x.com returns a clear error otherwise): `edit`, note/long-form `post` (>280), `bookmarks folder` writes.
- **Account-eligibility**: `community create` needs an eligible account.
- **likers**: X returns the liker list only to the tweet's author; empty for others (retweeters is public).
- **grok image**: needs image-gen quota (free tier is limited/daily); errors clearly when the quota is spent.
- **DM**: only classic (unencrypted) DMs via `dm send/list/download`. X's new **XChat is end-to-end encrypted and out of scope** (keys are device-only, non-extractable).
- **Rate limit**: `.rateLimit` carries `x-rate-limit-*`; back off when `remaining` is low.
- `AGENTX_UTLS=1` sends a Chrome JA3 TLS fingerprint (HTTP/2) for stealth.
- Writes are real and public — confirm intent before posting/following/etc.

## Typical flows
- Read a user's latest: `agentx posts elonmusk -n 20`
- Reply: `agentx reply <id> "nice"` · Quote with image: `agentx quote <id> "look" --media pic.jpg`
- Thread: `agentx post --thread "long text…"` · Poll: `agentx poll "Q?" --option A --option B`
- Schedule: `agentx schedule +2h "later"` then `agentx scheduled`
- Ask Grok: `agentx grok "USD to IDR today?"` · Image: `agentx grok image "a dragon" --out d.png`
- Paginate: `agentx feed -n 40` → take `nextCursor` → `agentx feed --cursor <c>`
