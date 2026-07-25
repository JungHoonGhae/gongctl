# data.go.kr Portal Catalog

Source of truth for the portal surface gongctl depends on. It should grow before
the Go client grows: add a row here (with how you verified it) before writing code
against a path.

Verified against the live portal on **2026-07-25** with an authenticated session
(CDP network capture + plain-HTTP probes).

## Status legend

- `public` — works with no session
- `auth` — needs a logged-in session
- `precondition` — needs `auth` **plus** something else (a cookie, a prior page)
- `driven` — gongctl does not call it directly; it drives the page's own JS
- `keyed` — needs a serviceKey, not a session

## Hosts

| Host | Role | Notes |
| --- | --- | --- |
| `www.data.go.kr` | portal web app | server-rendered; scraping target |
| `auth.data.go.kr` | SSO / login wall | never automated (government SSO) |
| `apis.data.go.kr` | OpenAPI gateway | the actual data APIs; serviceKey, no session |

## Paths

| Status | Method | Path | Purpose | gongctl mapping |
| --- | --- | --- | --- | --- |
| `public` | GET | `/sso/login.do` | login entry page | `login` opens this for the human |
| `auth` | GET | `/sso/profile.do` | **SSO trampoline** — auto-submitting form, needs JS | probe loops settle past it |
| `auth` | GET | `/iim/api/selectAcountList.do` | 활용신청 현황 list | `applications`; also the auth probe |
| `auth` | GET | `/iim/api/selectApiKeyList.do` | 인증키 발급현황 (the serviceKey) | `key` / MCP `get_api_key`; `call` auto-injects it. Parse `#pblisrCrtfcKeyPlain` (hidden input = the ACTIVE key; the table also lists superseded ones). **Note the markup has a duplicate `value` attribute — take the first.** |
| `precondition` | GET | `/tcs/dss/redirectDevAcountRequestForm.do?publicDataPk={pk}&isBusinessApply=N` | 활용신청 form | `apply`; needs cookie `currentMyMenuId=M020105`, else bounces to `index.do` |
| `driven` | POST | `/iim/api/saveDevAcountRequest.do` | 활용신청 submit (AJAX) | **not called directly** — `apply` invokes the form's `fn_save()` so the page builds/validates the payload (ADR 0001) |
| `public` | GET | `/tcs/dss/selectDataSetList.do?dType=&org=&keyword=&currentPage=&perPage=` | dataset search | `search` (plain HTTP, no browser) |
| `public` | GET | `/data/{pk}/openapi.do` | OpenAPI detail (operations, request variables, 참고문서) | `describe` (plain HTTP, no browser) |
| `keyed` | GET | `https://apis.data.go.kr/{org}/{service}/{operation}` | the actual data API | `call`; `serviceKey` query param |

### Not data-bearing (checked, ignore)

`/templates/*.hbs`, `/uim/cmm/selectMberInfo.json`, `/cmm/cmm/selectCommonCodeSelectboxList.json`,
`analytics.google.com/g/collect`. These are the only XHR/fetch requests the read
pages make — see ADR 0001: **the read paths carry no data APIs, the HTML is the
only source.**

## Preconditions worth remembering

- **`currentMyMenuId=M020105` cookie** before the apply form, or the portal
  redirects to `index.do`. Set it explicitly.
- **SSO trampoline** (`/sso/profile.do`) is an auto-submitting form, so the first
  authenticated navigation in a fresh tab must run in a browser, not plain HTTP.
  Once the session has settled, the cookies work over plain HTTP.
- **Auth cookies are session-scoped**: Chrome drops them on exit, but the values
  stay valid server-side. gongctl copies them out at login (`internal/portal/session.go`)
  and closes the window.

## Capture workflow

To add or re-verify a row:

1. Log in with `gongctl login` (this leaves a session; add `--keep-browser` if
   you need the window).
2. Attach over CDP on the debug port and enable the Network domain, then load the
   page and record only `XHR`/`Fetch` requests — that separates data calls from
   template/telemetry noise.
3. For a form action, read the handler's source (`fn_save.toString()`) instead of
   submitting, so you learn the endpoint without mutating the account.
4. Record the path, its status, the preconditions, and the date verified.

## Gateway propagation

A freshly approved API answers **403 Forbidden** at `apis.data.go.kr` for a while:
the portal auto-approves instantly, but the gateway takes minutes (up to ~1 hour)
to accept the key for that service. Verified 2026-07-25 — an API applied for
minutes earlier returned 403 while one applied ~2 hours earlier returned 200 with
the same key. `call` surfaces this as a hint so it is not mistaken for a key error.

## Specs that live only in the guide document

Some datasets document nothing on `openapi.do` — no endpoint, no 요청변수 table —
and ship the whole spec in the attached 참고문서 (hwp/xlsx/zip). Example: pk
15012005 (소상공인시장진흥공단 상가(상권)정보). `describe` surfaces this as an empty
`operations` plus a `note` and a **`guideDocUrl`** built from the page's
`fn_fileDownload('FILE_...','N')` handler:

    {baseURL}/cmm/cmm/fileDownload.do?atchFileId=FILE_...&fileDetailSn=N

Verified 2026-07-25: that URL returns the guide (HTTP 200, 8 MB zip). gongctl
never parses the document — reading it is the agent's job, by design (spec §9).
For 15012005 the zip also carried a 30 MB `주요상권현황` CSV, i.e. guide documents
sometimes contain bulk data the API itself does not expose.

## Known gaps

- **심의(manual-review) applications** are not handled — auto-approved APIs only.
  `list_applications` shows their status; approval and re-call are up to the user.
