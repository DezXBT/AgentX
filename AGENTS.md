# agentx — agent integration guide

This file tells an autonomous agent how to drive X/Twitter through agentx.

## Contract

- Drive everything through the `agentx` binary (or import `pkg/agent` in Go).
- Every command prints **one JSON object** to stdout and nothing else:
  - success: `{"ok":true,"schema_version":"1","data":...,"pagination":{"nextCursor":"..."}}`
  - failure: `{"ok":false,"schema_version":"1","error":{"code":"...","message":"..."}}`
  - process exit code is `0` on success, `1` on a handled error.
- Parse the envelope; never scrape human text. `data` shape depends on the
  command (a tweet array for timelines, a user object for `user`, etc.).

## Decision guide

| Goal | Command |
| --- | --- |
| Read my home feed | `agentx feed -n N` (add `--latest` for chronological) |
| Search | `agentx search --type Top\|Latest "<query>" -n N` |
| Look up a profile | `agentx user <handle>` |
| Read someone's tweets | `agentx posts <handle> -n N` |
| Read one tweet (no login needed) | `agentx tweet <id\|url>` |
| Read a tweet + replies | `agentx thread <id\|url>` |
| Publish | `agentx post "<text>"` |
| Reply / quote | `agentx reply <id> "<text>"` / `agentx quote <id> "<text>"` |
| Engage | `agentx like\|unlike\|retweet\|unretweet\|bookmark\|delete <id>` |

`<id|url>` accepts a numeric id or any `https://x.com/<user>/status/<id>` URL.

## Multi-account

- Register once: `agentx account add --name <n> --auth-token <t> --ct0 <c> [--cookie "<full cookie>"]`.
- Choose per call with `-a <name>` or set `AGENTX_ACCOUNT`.
- The first account becomes the default; change it with `agentx account default <name>`.
- Inspect with `agentx account list` (returns names + which is default).

Prefer supplying the **full browser cookie string** via `--cookie`: it keeps the
session looking like a real browser, which makes the cookies last much longer.

## Pagination

Timeline-style results include `pagination.nextCursor`. To page, re-issue the
same command with `--cursor <nextCursor>`. Stop when `nextCursor` is empty or
stops changing.

## Handling errors

Switch on `error.code`:

- `not_authenticated` → cookies missing/expired; ask the user to re-add the
  account, then retry.
- `rate_limited` → back off (seconds→minutes) and retry; the client already
  retries transient 429s a few times.
- `query_id_error` / repeated `not_found` on a known-good id → x.com rotated a
  GraphQL query id; report it so the operator can refresh ids.
- `invalid_input` → fix arguments; do not retry blindly.
- `network_error` → transient; retry with backoff.

## Notes for safe automation

- Writes (`post`, `like`, ...) change real account state. Confirm intent before
  bulk actions.
- Keep one stable User-Agent per account (set `--user-agent` at add time) — do
  not rotate it between requests.
