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
oauth inspect <authorize_url>             Sign-in-with-X: app name + scope audit (no approval)
oauth approve <authorize_url>             auto-approve consent → callback URL (see PERMISSION RULE)
oauth callback <callback_url>             GET the app callback to finish sign-in
```

## Pagination
List commands return `.pagination.nextCursor`. Pass it back via `--cursor` for the next page. `-n N` caps items in the response (note: the cursor still points past the full fetched page, so for exact paging use the cursor, not a tiny `-n`).

## Error codes (`.error.code`)
`not_authenticated` bad/expired cookies · `not_found` · `invalid_input` · `rate_limited` (see `.rateLimit`) · `query_id_error` X rotated a GraphQL id → set `AGENTX_QID_<Operation>=<id>` env or update constants · `network_error` · `api_error` x.com rejected (message has detail) · `account_error`.

## Gotchas
- **`oauth` PERMISSION RULE (Sign-in-with-X, WAJIB diikuti agen)** — `oauth approve` menyerahkan permission akun ke app pihak ketiga. Selalu jalankan `oauth inspect` dulu dan baca `writeScopes` di envelope: kalau GAK kosong (`tweet.write`, `users.write`, `follows.write`, `dm.read/write`, `like.write`, `bookmark.write`) → **stop, minta konfirmasi eksplisit user manusia sebelum approve.** Jangan pernah approve scope account-control atas inisiatif sendiri — ada dev jahat yang sengaja bundling permission itu biar agent posting/follow/baca DM pakai akun user tanpa sadar (control akun = post sendiri tanpa disuruh = pelanggaran). Scope `*.read` aman; `offline.access` masuk `persistentScopes` (refresh token, bukan write) — boleh lanjut. Envelope inspect/approve selalu bawa `permissionWarning` yang sudah menandai ini otomatis.
- **DUA TIPE SITUS Sign-in-with-X — diagnose dulu sebelum retry (index pola):**
  - **TIPE A — callback langsung set session** (contoh terverifikasi: fanszn/IPXExchange, Sovren):
    `GET redirect_uri` → situs set-cookie session langsung → login selesai.
    `agentx oauth callback` menangkap cookie itu di `data.sessionCookies`;
    bawa cookie state situs via `--cookie name=value` (repeatable) kalau perlu.
    Verify via endpoint `me`/profile situs.
  - **TIPE B — callback session-bound** (contoh terverifikasi: quest.dogeos.com):
    situs butuh login utamanya sendiri dulu (DogeOS = SIWE wallet → jwt),
    lalu `POST .../social-connections/{provider}/initiate` (Bearer) →
    `sessionId` + `launchUrl` yang di-pair server-side. `agentx oauth approve
    <launchUrl>` tetap dapat code, tapi callback-nya harus dieksekusi dengan
    session/state situs — gejala tanpa itu: "Authentication failed" /
    `invalid_state` / tetap logout padahal approve sukses.
    **Fix: tool tipis spesifik situs** (~100 baris: login situs → initiate →
    `agentx oauth approve` → GET callback backend → poll status async
    `pending → connected`). Jangan re-initiate saat pending — macet.
  - **Diagnosa cepat:** approve sukses + X kasih code, tapi situs tetap
    logout/error → pasti Tipe B; cari endpoint `initiate`/`confirm` di JS
    bundle situs, jangan buang waktu retry callback.
  - Referensi lengkap: skill `signin-with-x` (Endpoint Discovery +
    Session-bound callbacks) dan skill `dogeos-connect-x` (pola Tipe B utuh).
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
- Sign-in-with-X: `agentx oauth inspect <url>` (cek permissionWarning) → `agentx oauth approve <url>` → GET/`oauth callback` pada `data.redirect_uri` → verifikasi session via endpoint "me" situs target
