---
status: accepted
---

# HTML scraping with loud drift detection, not API discovery or self-healing selectors

gongctl reads data.go.kr by scraping HTML, which looks fragile enough that
"surely there's a better way" keeps coming up — an adaptive-selector library
(Scrapling), a crawler framework (goscrapy), an AI browser agent (Browser Use
et al), or discovering the JSON APIs beneath the pages (Unbrowse's approach). We
investigated the last one against the live portal and it settled the rest:
data.go.kr's read paths are **pure server-render**, so there are no underlying
data APIs to prefer. We therefore keep HTML scraping, and manage its fragility
with a *loud* drift signal (`gongctl doctor`) rather than a clever parser.

## Evidence (2026-07-25, live authenticated session)

XHR/fetch captured via CDP while loading each page:

| Page | Data-bearing XHR/fetch |
|---|---|
| `selectDataSetList.do` (search) | none — only a `.hbs` template and Google Analytics |
| `openapi.do` (describe) | none — only `selectMberInfo.json` (member info) |
| `selectAcountList.do` (applications) | none — only `selectCommonCodeSelectboxList.json` (select-box codes) |
| apply submit | **`POST /iim/api/saveDevAcountRequest.do`** — a real endpoint |

## Considered options

- **API discovery for reads** — rejected: nothing to discover; the data is only
  ever in the server-rendered HTML.
- **`POST /iim/api/saveDevAcountRequest.do` directly for apply** — rejected even
  though the endpoint exists. Driving the form's own `fn_save()` makes the portal
  build and validate the payload; hand-rolling the POST means reproducing every
  field (plus session/CSRF preconditions) and silently creating malformed
  applications when a field changes.
- **Adaptive/self-healing selectors (Scrapling)** — rejected: Python (breaks the
  single-binary distribution) and *probabilistic* — relocating an element by
  similarity can silently match the wrong thing. gongctl's contract is
  surface-only: fail loudly, never fabricate.
- **Crawler framework (goscrapy)** — rejected: gongctl hits three known
  endpoints; it is not a crawler, and a framework wouldn't make selectors any
  less brittle.
- **AI browser agents (Browser Use, Stagehand, agent-browser)** — rejected: an
  LLM deciding what to click is probabilistic, and `apply` creates a real
  application on the user's government account. That path must stay
  deterministic.
- **Swapping chromedp for another driver (Rod, Playwright-Go)** — not adopted:
  every Go option is CDP underneath, so this changes the API surface, not the
  substance, at the cost of re-validating hand-verified automation.

## Consequences

- Markup drift is inevitable, so it must be *detected*, not absorbed: parsers
  degrade to empty results, and `gongctl doctor` drives each seam live and exits
  non-zero on drift (CI-friendly).
- CDP stays. The one-time human SSO login plus a long-lived re-attachable session
  is the requirement that rules out managed/remote browser services anyway.
