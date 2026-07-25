# Changelog

All notable changes to gongctl are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow
[SemVer](https://semver.org/). The release workflow uses the `## [X.Y.Z]`
section matching a `vX.Y.Z` tag as the GitHub release notes.

## [0.3.0]

### Added

- **The catalogue now records each dataset's service type, and two in five are a
  dead end.** 4,770 of 11,932 OpenAPI datasets are `LINK`: the portal publishes no
  spec for them and only links to the publisher's site, so `describe` cannot
  produce an endpoint and an application spent on one buys a key that cannot be
  used here. Nothing surfaced that before — searching 폭염 returned a LINK dataset
  second, ahead of three callable ones, purely on application count.

  `catalog search --rest-only` (MCP `restOnly: true`) keeps only what the portal
  reports as REST; every result carries its `svcType` either way, and `catalog
  info` reports the breakdown. Verified against `describe` from the opposite
  direction: the LINK-labelled dataset reports `apiType: LINK` with zero
  operations, the REST-labelled one reports two operations with a live endpoint.

  Labels come from the portal's own `svcType` filter, and the counts it returns
  (REST 7,156 / LINK 4,770) account for the catalogue exactly. `sync` still starts
  with an unfiltered sweep that defines the catalogue, then labels what it found,
  so a service type nobody here has heard of yet leaves an entry **unlabelled
  rather than missing or mislabelled** — the 6 datasets the portal reports as
  neither show up as 미확인. Sync now takes ~235s instead of ~160s.

## [0.2.0]

### Added

- **API catalogue** (`catalog sync|search|orgs|info`, MCP `catalog_search`) — the
  portal only answers keyword queries, so finding out whether a dataset exists
  meant inventing search terms one at a time and never knowing whether a miss
  meant "doesn't exist" or "wrong word". `sync` sweeps the whole OpenAPI list
  (11,932 datasets in ~160s, 60 requests) into a local snapshot; `search` answers
  from disk instantly, ranked by application count, because demand is the best
  available proxy for "this one is actually usable".

  The gap this closes is real and was measured, not assumed: while researching
  heatwave data, four separate keyword searches missed
  기상청_생활기상지수 (5,045 applications, carries the heat-index figures) because
  none of the guessed words matched its wording. One catalogue search finds it.

  Descriptions are stored but never returned — they are what makes matching work
  and also what wrecks an agent's context (ten of them is ~3,000 characters of
  prose nobody asked for). Results are compact rows plus the total match count, so
  a caller can tell it should narrow the query rather than page blindly.
- **Queries can be written as sentences.** `"폭염에 취약한 고령자 데이터"` used to
  return nothing, because every term had to appear and particles and the word
  데이터 never do. Terms are now reduced (trailing particles trimmed, fillers
  dropped) before matching. Entries matching *every* term still win; only when
  there are none are the terms ORed — and that widening is reported as
  `relaxed`/`matched` rather than hidden, because a widened search answers a
  different question than the one asked. A term in the dataset's name outranks the
  same term buried in its blurb.
- **`catalog sync --if-stale`** — syncs only when the snapshot is old, so a cron
  entry or CI step can refresh unconditionally without anyone having to remember
  the cadence.
- **`doctor` now checks catalogue freshness**, reporting a stale snapshot as
  drift. A stale catalogue keeps answering while silently omitting everything
  published since the sync, which is exactly the kind of quiet wrongness `doctor`
  exists to make loud.

### Fixed

- **`apply` no longer burns the login session.** The portal rotates its session
  cookie during the application flow, and the rotated cookie lived only inside the
  headless browser — so every `apply` left the saved session dead and the next
  command demanded a fresh login. The rotated session is now captured before the
  browser closes (and only if it actually works, so a failed apply cannot
  overwrite a good session with a broken one).
- **`login` no longer opens a second login screen.** Polling for completion
  navigated a fresh tab to an authenticated page each time, which the portal
  bounced back to its login wall — so the user watched a second login appear on
  top of the one they were using. Login is now detected by reading cookies without
  navigating at all.
- **A rejected serviceKey is retried once with a freshly read key.** The cached
  key is normally right, but goes stale when the key is reissued on the portal;
  previously that surfaced as an authentication failure the user had to diagnose.
- Gateway propagation guidance now states the measured wait (7~10 minutes; the
  portal's own ceiling is an hour) and says explicitly that `list_applications`
  showing 승인 does not yet mean callable.
- `catalog sync` progress reported every 1,000 datasets, which left the first
  stretch of a multi-minute command silent and indistinguishable from a hang; it
  now reports every page.

## [0.1.1]

### Fixed

- **`logout` now removes the Chrome profiles**, not only the stored cookies and
  cached serviceKey. The login profile accumulates the cookies of whatever the
  human logged in *with* — an SSO provider's session, some of them persistent —
  and the headless profile holds the session gongctl injected. Leaving those
  behind after an explicit logout was the wrong boundary. Verified: 4.2 GB → 0 B,
  no cookie database left under the config directory.
- README's security section now states what is actually written to disk (with
  permissions), what `logout` removes, and the risks that remain: the files are
  `0600` but not encrypted; the local CDP port is reachable by other processes on
  the same machine while `login`/`apply` runs; MCP `apply` creates real
  applications without a human confirmation, so prompt injection through portal
  text can produce unwanted ones (the CLI's y/N confirm is the safer path); and
  the serviceKey is account-wide.

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
