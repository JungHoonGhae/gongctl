# Changelog

All notable changes to gongctl are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow
[SemVer](https://semver.org/). The release workflow uses the `## [X.Y.Z]`
section matching a `vX.Y.Z` tag as the GitHub release notes.

## [0.1.0]

First release. A Go CLI + MCP server that automates data.go.kr (공공데이터포털)
so AI agents never touch the portal UI.

### Added
- **Dataset search** (`search`, MCP `search_datasets`) — keyword search over
  data.go.kr file and OpenAPI datasets.
- **활용신청 automation** (`apply`, MCP `apply`) — submits an OpenAPI application
  through a CDP-attached browser session (auto-approved dev account → key issued).
  One-time human login via `login`; the session is kept alive and re-attached.
- **Application listing** (`applications`, MCP `list_applications`) — status,
  issued key, and expiry.
- **OpenAPI describe** (`describe`, MCP `describe_api`) — surfaces operations,
  endpoints, and request variables from the OpenAPI detail page. Surface-only:
  never fabricates a parameter; hands back raw HTML when the structure is
  uncertain.
- **Authenticated call** (`call`, MCP `call_api`) — injects the account
  serviceKey, GETs an endpoint, converts XML→JSON, and surfaces error codes
  without swallowing them.
- **`doctor`** — live drift check: drives each scraping seam and reports
  ok/drift/skipped, exiting non-zero on drift (CI-friendly).
- **MCP server** (`mcp`) — five tools plus a `gongctl://guide` resource, over
  stdio.
- Output as `json` / `jsonl` / `table`; goreleaser + Homebrew tap +
  `install.sh`/`install.ps1` distribution.

### Security
- The login daemon launches Chrome **without** `--remote-allow-origins=*`, so a
  malicious local web page cannot hijack the authenticated session over the CDP
  port (Chrome rejects foreign-Origin WebSocket upgrades). chromedp still
  attaches (it sends no Origin).
- The account serviceKey is never logged or persisted — it is redacted from
  transport-error messages.
