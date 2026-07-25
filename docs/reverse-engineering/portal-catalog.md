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
| `auth` | GET | `/iim/api/selectApiKeyList.do` | 인증키 발급현황 (the serviceKey) | not wired yet — see gap below |
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

## Known gaps

- **serviceKey is not read programmatically.** It lives on
  `/iim/api/selectApiKeyList.do`, but `applications` does not parse it, so
  `call --key` still needs the key pasted by hand. Wiring this would close the
  last manual step in the search → apply → describe → call chain.
