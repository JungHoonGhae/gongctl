# Changelog

All notable changes to gongctl are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow
[SemVer](https://semver.org/). The release workflow uses the `## [X.Y.Z]`
section matching a `vX.Y.Z` tag as the GitHub release notes.

## [0.1.0]

First release. A Go CLI + MCP server that automates data.go.kr (공공데이터포털)
so an AI agent can go from "I need this data" to actual API responses without a
human touching the portal.

**A person logs in once in a browser. Everything after that — searching,
applying, confirming approval, fetching the issued key, calling the API — the
agent does itself.** No key is ever copied and pasted; after login no browser
window stays on screen.

Verified end to end on a real data.go.kr account: search → apply (auto-approved)
→ approval confirmed → serviceKey fetched → API called, with zero visible
browsers throughout.

### Added

- **Dataset search** (`search`, MCP `search_datasets`) — keyword search over
  data.go.kr's file and OpenAPI datasets. Results carry each dataset's publisher,
  last-modified date, view count and application count, so a dataset can be
  judged without opening its page. `--per-page` sweeps large result sets in few
  requests (the full OpenAPI catalogue — 11,932 datasets — in 60).
- **활용신청 automation** (`apply`, MCP `apply`) — fills and submits the
  application form by driving the portal's own `fn_save()`, so the portal builds
  and validates the payload. Auto-approved dev accounts get a key immediately.
- **Application list** (`applications`, MCP `list_applications`) — status, account
  type, and expiry. Paginated, so accounts with more than ten applications are
  reported in full.
- **serviceKey retrieval** (`key`, MCP `get_api_key`) — reads the account key the
  portal issues on first approval. `call` uses it automatically when `--key` is
  omitted, which is what closes the loop for an agent.
- **OpenAPI describe** (`describe`, MCP `describe_api`) — surfaces operations,
  endpoints and request variables. Reads the Swagger 2.0 document the portal
  embeds in the page when present (the authoritative source) and falls back to
  the rendered tables. Reports the portal's `API 유형`: REST pages carry a spec,
  LINK pages only point at the publisher's site. Surface-only — it never invents
  a parameter, and says where the spec actually lives when the page has none.
- **Authenticated call** (`call`, MCP `call_api`) — injects the serviceKey,
  converts XML responses to JSON, and surfaces error codes rather than swallowing
  them. A 403 right after approval is explained as gateway propagation (minutes,
  not a key problem) instead of being left to guesswork.
- **`doctor`** — drives each scraping seam live and reports ok/drift/skipped,
  exiting non-zero on drift. This project scrapes fragile HTML by necessity, so
  drift is made loud rather than absorbed.
- **MCP server** (`mcp`) — six tools plus a `gongctl://guide` resource, over
  stdio.
- Output as `json` / `jsonl` / `table`; goreleaser + Homebrew tap +
  `install.sh` / `install.ps1` distribution.

### Security

- The login browser is closed once its session cookies have been copied out, so
  no window and no browser process is left running. Reads then go over plain
  HTTP; only 활용신청 submission needs a browser, and that one runs **headless**
  with the session injected.
- Chrome is launched **without** `--remote-allow-origins=*`, so a malicious local
  web page cannot open the CDP port and drive the authenticated session (Chrome
  rejects foreign-Origin WebSocket upgrades; verified by spike). chromedp still
  attaches, as it sends no Origin header.
- The serviceKey is never logged or persisted in the clear: it is redacted from
  transport-error messages, and the cached copy is written `0600`. `logout`
  clears both the cookies and the cached key.

### Known limitations

- Only auto-approved applications work; 심의(manual-review) datasets are listed
  with their status but not driven to approval.
- A freshly approved API answers 403 at the gateway for minutes (about 8 in
  testing, up to an hour by the portal's own guidance) before it can be called.
- LINK-type datasets have no spec on the portal, so `describe` can only say so
  and point at the publisher.
- The portal's HTML can change at any time; `doctor` exists to detect that.
