<div align="center">
  <img src="assets/logo.png" alt="AgentX" width="420">

  <h1>agentx</h1>
  <p><b>An agent-first X / Twitter client in Go.</b><br>
  One binary, one JSON envelope per command — so any agent runtime can drive X.</p>

  <p>
    <img alt="Go" src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white">
    <img alt="status" src="https://img.shields.io/badge/status-production-brightgreen">
    <img alt="interface" src="https://img.shields.io/badge/interface-JSON%20envelope-blue">
    <img alt="accounts" src="https://img.shields.io/badge/multi--account-yes-success">
  </p>
</div>

---

`agentx` is a from-scratch Go library **+** CLI built for **autonomous agents**. Every command prints a single predictable JSON object, supports **multiple accounts**, and keeps **`auth_token` cookies alive** by making traffic look like a real browser.

## ✨ Why cookies last longer here

X flags/expires `auth_token` cookies aggressively when traffic doesn't look human. agentx signs **every** authenticated request with:

- a freshly generated **`x-client-transaction-id`** — the exact algorithm x.com's web app runs (golden-tested),
- the **full browser header set** (bearer, `x-csrf-token`, `x-twitter-*`, `sec-fetch-*`, stable User-Agent),
- the complete cookie string,
- optional **uTLS** Chrome JA3 fingerprint over **HTTP/2** (`AGENTX_UTLS=1`).

## 🚀 Quick start

```bash
go build -o agentx ./cmd/agentx

# Register an account (cookies from a logged-in browser)
./agentx account add --name main --auth-token <auth_token> --ct0 <ct0>

# Read
./agentx feed -n 20
./agentx user jack
./agentx search --type Latest "golang" -n 20
./agentx analytics <tweet_id>

# Write
./agentx post "hello from agentx 👋"
./agentx reply <id> "nice!" --media pic.jpg
./agentx poll "Tabs or spaces?" --option Tabs --option Spaces

# Grok (AI)
./agentx grok "USD to IDR today?"
./agentx grok image "a majestic dragon" --out dragon.png
```

Every command prints **one JSON envelope**:

```json
{ "ok": true,  "schema_version": "1", "data": …, "pagination": {"nextCursor": "…"}, "rateLimit": {…} }
{ "ok": false, "schema_version": "1", "error": {"code": "…", "message": "…"} }
```

Check `.ok`, read `.data`, page with `.pagination.nextCursor`. Exit code is `0` on success, `1` on a handled error. Output is **compact single-line JSON by default** (token-efficient for agents) — add `--pretty` for human-readable output.

## 🧩 Features

| Area | What you get |
| --- | --- |
| **Read** | feed (for-you / following), search, user / posts / tweet / thread / **article**, media tab, bookmarks (+ folders), likes, followers / following, **lists** + members, **communities**, mentions, **notifications** + unread, **trends** (+ locations), **analytics** (engagement rate), retweeters / likers |
| **Write** | post, reply, quote, **polls** (+ vote), like / retweet / bookmark, follow / mute / block, **pin**, media upload (+ alt text), auto-**threads**, **scheduled** tweets, **drafts**, profile / avatar / banner / **settings**, **topics**, list & community management |
| **Grok** | text answers with **live web search**, and **image generation** (download to file) |
| **DM** | classic DM send / list / media / download |
| **Infra** | multi-account, zero-auth **FxTwitter** fallback, uTLS + HTTP/2, rate-limit surfacing, query-id override for X rotations, `watch` streaming, media `download` |

See **[SKILL.md](SKILL.md)** for the compact, token-efficient command reference and **[AGENTS.md](AGENTS.md)** for the full agent integration guide.

## 🏛️ Architecture

```
cmd/agentx              CLI — one JSON envelope per command
pkg/agent               High-level facade (account selection, client cache, fallbacks)
pkg/xclient             Authenticated x.com GraphQL/REST client (+ transaction signing)
pkg/transaction         x-client-transaction-id generator   [golden-tested]
pkg/account             Multi-account credential store (~/.agentx, 0600)   [tested]
pkg/backends/fxtwitter  Zero-auth single-tweet fallback
pkg/models              Backend-agnostic Tweet / UserProfile / Page
pkg/xerrors             Stable machine-readable error codes
```

## 🔌 Use as a Go library

```go
import "github.com/dezxbt/agentx/pkg/agent"

ag := agent.New(store)
page, err := ag.Feed(ctx, "main", false, 20, "")
```

## ⚠️ Notes

- **Premium-gated** (work on X Premium accounts; the client routes correctly and surfaces a clear error otherwise): long-form `post` (>280), `edit`, bookmark-folder writes.
- **likers** are returned by X only to the tweet's author (retweeters is public).
- **grok image** needs available image-generation quota.
- X's new **XChat is end-to-end encrypted and out of scope** — keys are device-only and non-extractable by design. Classic DMs work.
- Writes are **real and public** — confirm intent before acting.

## 📄 License

MIT — see [LICENSE](LICENSE).

---

## 🔍 Keywords

Twitter API, X API, Twitter CLI, X CLI, Twitter bot, X bot, Twitter automation, X automation, Twitter agent, X agent, Go Twitter client, Golang Twitter, GraphQL Twitter, Twitter scraper, X scraper, Twitter multi-account, cookie auth Twitter, anti-detection Twitter, autonomous agent Twitter, AI agent Twitter, Grok API, Grok CLI, Twitter DM, tweet automation, Twitter search, social media automation, social media bot, Twitter posting, automated tweets, Twitter scheduling, AI-powered Twitter, LLM Twitter, agent framework, Twitter toolkit, X toolkit, headless Twitter, browser automation Twitter, JA3 fingerprint, uTLS Twitter, rate limit Twitter, FxTwitter, x-client-transaction-id, agent skill, AI tool, MCP server, tool use, function calling, agent tool, social media agent skill, Twitter skill, X skill, ChatGPT Twitter plugin, Claude Twitter tool, AI coding agent, agent orchestration, agentic workflow, Hermes agent, LangChain Twitter, AutoGPT Twitter, OpenAI function calling, tool-augmented LLM, RAG social media, prompt engineering Twitter, AI agent toolkit, developer tools, open source agent, self-hosted agent, API wrapper, Twitter API wrapper, X API wrapper, REST API Twitter, GraphQL API Twitter
