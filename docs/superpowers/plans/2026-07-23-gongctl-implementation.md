# gongctl Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Go CLI + MCP server that lets an AI agent search data.go.kr datasets, submit 활용신청 (application) through a live browser session, and call arbitrary approved OpenAPIs — with zero human portal-UI operation beyond a one-time browser login.

**Architecture:** One binary, two faces (CLI for humans via cobra, MCP for agents via stdio), same backend. Deterministic steps (login, apply, key injection, HTTP, XML→JSON) are automated by tools; heterogeneous API specs are *surfaced* (scraped and handed to the agent), never parsed into claims. The 활용신청 automation is a CDP-attach daemon: gongctl launches a real Chrome, the human logs in once, gongctl keeps it alive and re-attaches over the DevTools Protocol for later commands.

**Tech Stack:** Go 1.26, cobra (CLI), chromedp + cdproto (browser automation), PuerkitoBio/goquery (HTML scraping), modelcontextprotocol/go-sdk (MCP), encoding/xml (call responses). No cgo.

## Global Constraints

- **Module path:** `github.com/JungHoonGhae/gongctl` (fresh repo, no remote yet).
- **Go version floor:** `go 1.26.1`.
- **kvote source repo (port origin):** `/Users/junghoon/workspace/projects/oss-k-vote-cli` — referenced as `$KVOTE` below. Ported files are copied from here, then renamed.
- **Dependency pins (match kvote's go.mod exactly):** `github.com/PuerkitoBio/goquery v1.12.0`, `github.com/chromedp/chromedp v0.15.1`, `github.com/chromedp/cdproto v0.0.0-20260321001828-e3e3800016bc`, `github.com/modelcontextprotocol/go-sdk v1.6.1`, `github.com/spf13/cobra v1.10.2`. **Do NOT add new dependencies** — XML→JSON uses stdlib `encoding/xml`.
- **Naming rule:** every kvote-specific identifier/string ("kvote", "nec", NEC org defaults, `KVOTE_*`) is renamed to gongctl-neutral. No election-domain vocabulary survives into gongctl.
- **License:** MIT. Platforms: macOS/Linux/Windows.
- **Never log or persist a serviceKey.** The key is passed in per call and surfaced back only inside CallResult/error to the caller.
- **surface-only contract:** describe never invents a parameter that isn't in the page; call never auto-retries or rewrites a key silently — it surfaces the error with a hint.
- **Config dir:** `os.UserConfigDir()/gongctl/` holds `datagokr-cdp.json` (session state), `chrome-profile/` (persistent Chrome profile), `config.json` (preferences).

---

## File Structure

```
cmd/gongctl/
  main.go            entrypoint
  root.go            cobra root, global flags, client builders
  auth.go            login / logout / status
  data.go            search / describe / call
  apply.go           apply / applications
  mcp.go             mcp (stdio server)
  version.go         version
internal/portal/     data.go.kr automation (ported from $KVOTE/internal/datagokr + search)
  daemon.go          Chrome launch, CDP discovery, state persistence
  daemon_unix.go     setDetached (non-windows)
  daemon_windows.go  setDetached (windows)
  config.go          user preferences (AutoApply)
  browser.go         Login / Applications / Logout (CDP re-attach)
  apply.go           활용신청 form auto-submit (SSO trampoline, JS dialog, cookie precondition)
  accounts.go        parse 활용신청 현황 list (status/key-expiry/uddi)
  accounts_test.go   fixture-driven parser test (ported)
  client.go          throttled HTTP client for public scraping (search/describe)
  search.go          SearchDatasets — FILE + OpenAPI, any org
  search_test.go     fixture-driven search parser test
  testdata/          accounts.html, search-list.html
internal/apicall/    NEW — generic authenticated call surface
  describe.go        openapi.do → operations/params/guideDoc (surface-only)
  describe_test.go   fixture-driven (op-15000908.html)
  call.go            serviceKey injection + GET + XML→JSON + error surface
  call_test.go       httptest-driven
  testdata/          op-15000908.html
internal/output/     json/jsonl/table renderer (ported verbatim)
  output.go
  output_test.go     small table test (new)
internal/version/    ldflags-injected build metadata (ported, renamed)
  version.go
internal/mcpserver/  MCP (stdio)
  server.go          assemble + 5 tools + gongctl://guide resource
  server_test.go     in-memory transport round-trip
  guide.go           gongctl://guide markdown text
```

---

### Task 1: Scaffold — module, version, buildable `gongctl version`

**Files:**
- Create: `go.mod`
- Create: `internal/version/version.go`
- Create: `cmd/gongctl/main.go`
- Create: `cmd/gongctl/root.go`
- Create: `cmd/gongctl/version.go`

**Interfaces:**
- Produces: `version.String() string`; `version.Version`, `version.Commit`, `version.Date` (ldflags targets). `rootCmd` cobra command with persistent flags `--format/-f` (default `json`), `--delay` (default `700ms`), `--base-url` (default `""`). Builder stubs `resolveFormat()`, and command registration in `root.go init()`.

- [ ] **Step 1: Create the module**

Run:
```bash
cd /Users/junghoon/workspace/projects/oss-gongctl
go mod init github.com/JungHoonGhae/gongctl
go mod edit -go=1.26.1
```

- [ ] **Step 2: Port the version package**

Copy `$KVOTE/internal/version/version.go` to `internal/version/version.go`, then change **only** the `String()` function's format string from `"kvote %s ..."` to `"gongctl %s ..."`:

```go
// String renders a human-readable version line.
func String() string {
	return fmt.Sprintf("gongctl %s (commit %s, built %s)", Version, Commit, Date)
}
```

- [ ] **Step 3: Write main.go**

```go
// Command gongctl automates data.go.kr (공공데이터포털): dataset search, OpenAPI
// 활용신청, and authenticated API calls — driven from a CLI or, for AI agents,
// an MCP server. It exists so agents never have to touch the portal UI.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Write root.go (commands added in later tasks)**

```go
package main

import (
	"time"

	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

var (
	flagFormat  string
	flagDelay   time.Duration
	flagBaseURL string
)

var rootCmd = &cobra.Command{
	Use:   "gongctl",
	Short: "data.go.kr(공공데이터포털) 자동화 — 검색·활용신청·API 호출 (CLI + MCP)",
	Long: `gongctl — 공공데이터포털(data.go.kr)의 OpenAPI 활용신청·인증키·호출을 자동화합니다.

사람은 브라우저에서 한 번만 로그인(gongctl login)하면, 이후 검색·활용신청·호출을
CLI 또는 MCP(에이전트)로 처리합니다. 포털 UI를 다시 건드릴 필요가 없습니다.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&flagFormat, "format", "f", "json", "출력 형식: json | jsonl | table")
	pf.DurationVar(&flagDelay, "delay", portal.DefaultDelay, "요청 간 최소 간격 (rate limit)")
	pf.StringVar(&flagBaseURL, "base-url", "", "포털 base URL 재정의 (테스트용)")

	rootCmd.AddCommand(versionCmd())
	// auth/data/apply/mcp commands registered in later tasks.
}

func resolveFormat() (output.Format, error) {
	return output.Parse(flagFormat)
}
```

Note: `root.go` imports `output` and `portal` which don't exist yet — this task will not compile alone. Complete Task 2 and Task 6 before building. To keep Task 1 independently green, temporarily comment the `output`/`portal` imports and the two lines referencing them, then restore in Task 6. (Simpler: implement Task 1 → 2 → 6 before first `go build`.)

- [ ] **Step 5: Write version.go**

```go
package main

import (
	"fmt"

	"github.com/JungHoonGhae/gongctl/internal/version"
	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "버전 정보 출력",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
			return nil
		},
	}
}
```

- [ ] **Step 6: Commit**

```bash
git add go.mod internal/version cmd/gongctl/main.go cmd/gongctl/version.go
git commit -m "feat: scaffold gongctl module, version package, CLI root"
```

---

### Task 2: Port `internal/output` verbatim + add a table test

**Files:**
- Create: `internal/output/output.go`
- Create: `internal/output/output_test.go`

**Interfaces:**
- Produces: `output.Format` (`JSON`/`JSONL`/`Table`), `output.Parse(string) (Format, error)`, `output.WriteJSON(io.Writer, any) error`, `output.WriteJSONL(io.Writer, []any) error`, `output.WriteTable(io.Writer, headers []string, rows [][]string) error`.

- [ ] **Step 1: Write the failing test**

`internal/output/output_test.go`:
```go
package output

import (
	"strings"
	"testing"
)

func TestWriteTableCJKAlign(t *testing.T) {
	var b strings.Builder
	if err := WriteTable(&b, []string{"상태", "데이터명"}, [][]string{
		{"승인", "당선인 정보"},
		{"신청", "투표율"},
	}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "승인") || !strings.Contains(out, "당선인 정보") {
		t.Fatalf("table missing content:\n%s", out)
	}
	// header separator row of dashes must be present
	if !strings.Contains(out, "----") {
		t.Fatalf("missing separator:\n%s", out)
	}
}

func TestParseRejectsUnknown(t *testing.T) {
	if _, err := Parse("yaml"); err == nil {
		t.Fatal("expected error for unknown format")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/output/`
Expected: FAIL (package `output` has no source yet — build error).

- [ ] **Step 3: Port the implementation**

Copy `$KVOTE/internal/output/output.go` to `internal/output/output.go` **verbatim** (it has no kvote-specific identifiers — the package comment and code are domain-neutral). No edits required.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/output/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/output
git commit -m "feat: port output renderer (json/jsonl/table) from kvote"
```

---

### Task 3: Port portal daemon + config (Chrome launch, CDP state)

**Files:**
- Create: `internal/portal/daemon.go`
- Create: `internal/portal/daemon_unix.go`
- Create: `internal/portal/daemon_windows.go`
- Create: `internal/portal/config.go`

**Interfaces:**
- Produces (unexported, used by browser.go/apply.go in Task 4/5): `configDir()`, `statePath()`, `profileDir()`, `saveState(*daemonState)`, `loadState() (*daemonState, error)`, `launchBrowser(startURL string) (*exec.Cmd, error)`, `discoverWS(ctx, port, timeout) (string, error)`, `wsAlive(port int) bool`, `setDetached(*exec.Cmd)`, const `debugPort = 9333`, type `daemonState`.
- Produces (exported): `portal.ErrNotLoggedIn`, `portal.Config` with `AutoApply bool`, `portal.LoadConfig() (*Config, error)`, `(*Config).Save() error`, `portal.DefaultDelay` (add here — see step 4).

- [ ] **Step 1: Port daemon.go with renames**

Copy `$KVOTE/internal/datagokr/daemon.go` to `internal/portal/daemon.go`, change `package datagokr` → `package portal`, and apply these exact renames:
- `configDir()`: `filepath.Join(dir, "kvote")` → `filepath.Join(dir, "gongctl")`.
- `ErrNotLoggedIn` message: `"data.go.kr 세션이 없습니다 — 먼저 \`kvote api login\` 을 실행하세요"` → `"data.go.kr 세션이 없습니다 — 먼저 \`gongctl login\` 을 실행하세요"`.

Everything else (debugPort, daemonState, statePath, profileDir, saveState, loadState, findChrome, launchBrowser, discoverWS, wsAlive) is copied unchanged.

- [ ] **Step 2: Port the platform files**

Copy `$KVOTE/internal/datagokr/daemon_unix.go` and `daemon_windows.go` to `internal/portal/`, changing only `package datagokr` → `package portal`. Update the comment "kvote's exit" → "gongctl's exit" in both.

- [ ] **Step 3: Port config.go**

Copy `$KVOTE/internal/datagokr/config.go` to `internal/portal/config.go`, change `package datagokr` → `package portal`. Update comment `` `api apply` `` → `` `gongctl apply` `` on the `AutoApply` field.

- [ ] **Step 4: Add DefaultDelay constant (root.go depends on it)**

Add to `internal/portal/config.go` (or daemon.go):
```go
import "time"

// DefaultDelay is the minimum spacing between throttled portal requests.
const DefaultDelay = 700 * time.Millisecond
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/portal/`
Expected: builds (chromedp/cdproto get pulled into go.mod; run `go mod tidy` if needed).

- [ ] **Step 6: Commit**

```bash
git add internal/portal/daemon.go internal/portal/daemon_unix.go internal/portal/daemon_windows.go internal/portal/config.go
git commit -m "feat: port portal CDP daemon + config from kvote"
```

---

### Task 4: Port accounts parser + fixture test

**Files:**
- Create: `internal/portal/accounts.go`
- Create: `internal/portal/accounts_test.go`
- Create: `internal/portal/testdata/accounts.html`

**Interfaces:**
- Produces: `portal.Application` struct (Title, Status, Org, Category, Account, AppliedAt, ExpiresAt, UDDI, DetailPk), `parseApplications(body string) ([]Application, error)`, const `AccountListPath = "/iim/api/selectAcountList.do"`.

- [ ] **Step 1: Port the fixture and test**

Copy `$KVOTE/internal/datagokr/testdata/accounts.html` → `internal/portal/testdata/accounts.html` (unchanged).
Copy `$KVOTE/internal/datagokr/accounts_test.go` → `internal/portal/accounts_test.go`, change `package datagokr` → `package portal`. (The test asserts 2 apps parsed with exact fields — it is the ground-truth spec for the parser.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/portal/ -run TestParseApplications`
Expected: FAIL (parseApplications undefined).

- [ ] **Step 3: Port accounts.go**

Copy `$KVOTE/internal/datagokr/accounts.go` → `internal/portal/accounts.go`, change `package datagokr` → `package portal`. The struct doc mentions "kvote surfaces it" — change to "gongctl surfaces it". No other change (parser is domain-neutral: it reads data.go.kr's 활용신청 현황 markup). This adds the `goquery` dependency.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/portal/ -run TestParseApplications`
Expected: PASS (2 apps, first status 승인, ExpiresAt 2028-06-22).

- [ ] **Step 5: Commit**

```bash
git add internal/portal/accounts.go internal/portal/accounts_test.go internal/portal/testdata/accounts.html
git commit -m "feat: port 활용신청 현황 parser + fixture test from kvote"
```

---

### Task 5: Port browser session + apply automation

**Files:**
- Create: `internal/portal/browser.go`
- Create: `internal/portal/apply.go`

**Interfaces:**
- Consumes: daemon.go helpers (Task 3), `parseApplications` + `AccountListPath` (Task 4).
- Produces: `portal.Login(ctx, progress io.Writer) error`, `portal.Applications(ctx) ([]Application, error)`, `portal.Logout(ctx) error`, `portal.Apply(ctx, pk, purpose, category string, confirm func(ApplySummary) bool) (*ApplyResult, error)`, types `ApplySummary`/`ApplyResult`, purpose constants `PurposeWeb/App/Etc/Ref/Research`, const `BaseURL = "https://www.data.go.kr"`.

- [ ] **Step 1: Port browser.go**

Copy `$KVOTE/internal/datagokr/browser.go` → `internal/portal/browser.go`, change `package datagokr` → `package portal`. The user-facing Korean progress strings mention no product name — leave them. This is CDP code with no unit test (live-only, validated in the final E2E step).

- [ ] **Step 2: Port apply.go**

Copy `$KVOTE/internal/datagokr/apply.go` → `internal/portal/apply.go`, change `package datagokr` → `package portal`. The comments reference "kvote drives the portal's real logic" and "kvote never bulk-applies" — change "kvote" → "gongctl" in comments. The `--purpose` error message stays. All chromedp logic (SSO warm-up, `currentMyMenuId` cookie, `fn_save()`, dialog listener, list-reflection success check) is copied **verbatim** — this is the hand-verified fragile automation; do not "improve" it.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/portal/`
Expected: builds clean.

- [ ] **Step 4: Verify existing tests still pass**

Run: `go test ./internal/portal/`
Expected: PASS (accounts test still green; no new unit tests — browser/apply are live-only).

- [ ] **Step 5: Commit**

```bash
git add internal/portal/browser.go internal/portal/apply.go
git commit -m "feat: port CDP browser session + 활용신청 auto-submit from kvote"
```

---

### Task 6: Portal search — generalized from NEC (any org, FILE + OpenAPI)

**Files:**
- Create: `internal/portal/client.go`
- Create: `internal/portal/search.go`
- Create: `internal/portal/search_test.go`
- Create: `internal/portal/testdata/search-list.html`

**Interfaces:**
- Produces: `portal.Client` (throttled HTTP), `portal.New(opts ...Option) *Client`, options `WithBaseURL`/`WithDelay`/`WithHTTPClient`/`WithUserAgent`, `(*Client).SearchDatasets(ctx, SearchOptions) ([]Dataset, error)`, types `Dataset{PublicDataPk, Title, Description, Formats, HasOpenAPI}` and `SearchOptions{Keyword, Org, Type, Page}`.

Note on generalization vs kvote: kvote's `nec.Datasets` hardcodes `dType=FILE` and defaults `org=중앙선거관리위원회`. gongctl drops the NEC org default (blank = all publishers) and lets `Type` select FILE / API / (blank = all). The `<dl>` scraping and `parseDt` format-splitting are reused as-is.

- [ ] **Step 1: Capture a real search fixture**

Run:
```bash
mkdir -p internal/portal/testdata
curl -sL -A "Mozilla/5.0" \
  "https://www.data.go.kr/tcs/dss/selectDataSetList.do?dType=API&keyword=선거&currentPage=1&perPage=10" \
  -o internal/portal/testdata/search-list.html
grep -c 'fileData.do\|openapi.do' internal/portal/testdata/search-list.html
```
Expected: a non-zero count of dataset links. (This grounds the parser in real markup.)

- [ ] **Step 2: Write the failing test**

`internal/portal/search_test.go`:
```go
package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSearchDatasets(t *testing.T) {
	body, err := os.ReadFile("testdata/search-list.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tcs/dss/selectDataSetList.do" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write(body)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithDelay(0))
	ds, err := c.SearchDatasets(context.Background(), SearchOptions{Keyword: "선거"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(ds) == 0 {
		t.Fatal("expected at least one dataset from fixture")
	}
	for _, d := range ds {
		if d.PublicDataPk == "" || d.Title == "" {
			t.Errorf("dataset missing pk/title: %+v", d)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/portal/ -run TestSearchDatasets`
Expected: FAIL (New/SearchDatasets undefined).

- [ ] **Step 4: Write client.go**

Adapt `$KVOTE/internal/nec/client.go` down to just what search/scraping needs (drop the open-portal and API-gateway fields — those are NEC-specific):
```go
package portal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DefaultBaseURL is the public data portal root.
const DefaultBaseURL = "https://www.data.go.kr"

// DefaultUserAgent identifies the client honestly to the server operator.
const DefaultUserAgent = "gongctl (+https://github.com/JungHoonGhae/gongctl)"

// Client is a rate-limited HTTP client for scraping data.go.kr public pages
// (dataset search, OpenAPI detail). Authenticated actions use the CDP browser
// (browser.go), not this client.
type Client struct {
	baseURL   string
	userAgent string
	delay     time.Duration
	http      *http.Client

	mu      sync.Mutex
	lastReq time.Time
}

// Option configures a Client.
type Option func(*Client)

func WithBaseURL(u string) Option  { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }
func WithDelay(d time.Duration) Option {
	return func(c *Client) {
		if d >= 0 {
			c.delay = d
		}
	}
}

// New creates a Client with sane defaults.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		userAgent: DefaultUserAgent,
		delay:     DefaultDelay,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.delay > 0 && !c.lastReq.IsZero() {
		if wait := c.delay - time.Since(c.lastReq); wait > 0 {
			time.Sleep(wait)
		}
	}
	c.lastReq = time.Now()
}

func (c *Client) getDoc(ctx context.Context, path string, query url.Values) (*goquery.Document, error) {
	c.throttle()
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/json,*/*")
	req.Header.Set("Referer", c.baseURL+"/")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("GET %s: unexpected status %s", path, resp.Status)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}
```

- [ ] **Step 5: Write search.go**

Adapt `$KVOTE/internal/nec/datasets.go`. Keep `<dl>` scraping + `parseDt`/`bracketName`; generalize org/type; detect OpenAPI vs file link:
```go
package portal

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Dataset is one data.go.kr dataset from a search result.
type Dataset struct {
	PublicDataPk string   `json:"publicDataPk"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Formats      []string `json:"formats,omitempty"`
	HasOpenAPI   bool     `json:"hasOpenApi"` // true when the result links to /data/{pk}/openapi.do
}

// SearchOptions filters a dataset search. Blank Org = all publishers; blank
// Type = all dataset types.
type SearchOptions struct {
	Keyword string
	Org     string
	Type    string // "FILE" | "API" | "" (all)
	Page    int
}

var (
	rePkFile     = regexp.MustCompile(`/data/(\d+)/fileData\.do`)
	rePkAPI      = regexp.MustCompile(`/data/(\d+)/openapi\.do`)
	knownFormats = map[string]bool{"CSV": true, "XLSX": true, "XLS": true, "JSON": true, "XML": true, "HWP": true, "PDF": true, "ZIP": true}
)

// SearchDatasets scrapes /tcs/dss/selectDataSetList.do. A blank keyword lists
// the first page.
func (c *Client) SearchDatasets(ctx context.Context, opts SearchOptions) ([]Dataset, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	if opts.Type != "" {
		q.Set("dType", opts.Type)
	}
	if opts.Org != "" {
		q.Set("org", opts.Org)
	}
	q.Set("keyword", opts.Keyword)
	q.Set("currentPage", strconv.Itoa(page))
	q.Set("perPage", "10")

	doc, err := c.getDoc(ctx, "/tcs/dss/selectDataSetList.do", q)
	if err != nil {
		return nil, err
	}

	var out []Dataset
	seen := map[string]bool{}
	doc.Find("dl").Each(func(_ int, dl *goquery.Selection) {
		href, _ := dl.Find(`a[href*="/data/"]`).First().Attr("href")
		var pk string
		hasAPI := false
		if m := rePkAPI.FindStringSubmatch(href); m != nil {
			pk, hasAPI = m[1], true
		} else if m := rePkFile.FindStringSubmatch(href); m != nil {
			pk = m[1]
		}
		if pk == "" || seen[pk] {
			return
		}
		seen[pk] = true
		title, formats := parseDt(dl)
		out = append(out, Dataset{
			PublicDataPk: pk,
			Title:        title,
			Description:  cleanText(dl.Find("dd").First().Text()),
			Formats:      formats,
			HasOpenAPI:   hasAPI,
		})
	})
	return out, nil
}

func parseDt(dl *goquery.Selection) (title string, formats []string) {
	text := cleanText(dl.Find("dt").First().Text())
	toks := strings.Fields(text)
	seen := map[string]bool{}
	i := 0
	for i < len(toks) {
		up := strings.ToUpper(toks[i])
		if knownFormats[up] {
			if !seen[up] {
				seen[up] = true
				formats = append(formats, up)
			}
			i++
			continue
		}
		if toks[i] == "+" {
			i++
			continue
		}
		break
	}
	title = strings.TrimSpace(strings.TrimSuffix(strings.Join(toks[i:], " "), "미리보기"))
	return title, formats
}

// cleanText collapses runs of whitespace to single spaces.
func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/portal/ -run TestSearchDatasets`
Expected: PASS. If the fixture's `<dl>` structure differs from kvote's assumption and 0 datasets parse, inspect `search-list.html` and adjust the selector (`dl` / `a[href*="/data/"]`) to match — this is the one place selectors are derived from the captured fixture.

- [ ] **Step 7: Wire root.go client builder + verify full build**

Add to `cmd/gongctl/root.go`:
```go
// newPortalClient builds a portal HTTP client from the global flags.
func newPortalClient() *portal.Client {
	opts := []portal.Option{portal.WithDelay(flagDelay)}
	if flagBaseURL != "" {
		opts = append(opts, portal.WithBaseURL(flagBaseURL))
	}
	return portal.New(opts...)
}
```
Run: `go build ./... && go test ./internal/...`
Expected: whole module builds; output + portal tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/portal/client.go internal/portal/search.go internal/portal/search_test.go internal/portal/testdata/search-list.html cmd/gongctl/root.go
git commit -m "feat: generalized data.go.kr dataset search (FILE+OpenAPI, any org)"
```

---

### Task 7: `describe` — scrape openapi.do into surfaced operations/params

**Files:**
- Create: `internal/apicall/describe.go`
- Create: `internal/apicall/describe_test.go`
- Create: `internal/apicall/testdata/op-15000908.html`

**Interfaces:**
- Produces: `apicall.Describe(ctx, baseURL, pk string) (*APISpec, error)`, types `APISpec{PublicDataPk, DataName, Operations, GuideDoc}`, `Operation{Name, Endpoint, Params, RawHTML}`, `Param{Name, Required, Sample, Desc}`.

Grounding (verified against the real page `https://www.data.go.kr/data/15000908/openapi.do`, server-rendered): the page contains, per operation, an endpoint URL like `http://apis.data.go.kr/9760000/{Service}/{op}`, and a 요청변수 table whose header row is `항목명(국문) | 항목명(영문) | 항목크기 | 항목구분 | 샘플데이터 | 항목설명`. Rows map: `항목명(영문)`→Param.Name, `항목구분`(필수/옵션)→Param.Required, `샘플데이터`→Param.Sample, `항목설명`→Param.Desc. Operation containers use class `open-api-detail`; the guide doc is the `참고문서` field. All operations are pre-rendered (no AJAX needed).

- [ ] **Step 1: Copy the captured fixture into testdata**

```bash
mkdir -p internal/apicall/testdata
cp "/private/tmp/claude-501/-Users-junghoon-workspace-projects-oss-gongctl/3c826fea-54ac-4996-8e8a-32250a8ee82f/scratchpad/op-15000908.html" internal/apicall/testdata/op-15000908.html
```
If that scratchpad file is gone, re-capture:
```bash
curl -sL -A "Mozilla/5.0" "https://www.data.go.kr/data/15000908/openapi.do" -o internal/apicall/testdata/op-15000908.html
```

- [ ] **Step 2: Write the failing test**

`internal/apicall/describe_test.go`:
```go
package apicall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDescribe(t *testing.T) {
	body, err := os.ReadFile("testdata/op-15000908.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write(body)
	}))
	defer srv.Close()

	spec, err := Describe(context.Background(), srv.URL, "15000908")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(spec.Operations) == 0 {
		t.Fatal("expected at least one operation")
	}
	var withEndpoint, withParams int
	for _, op := range spec.Operations {
		if strings.Contains(op.Endpoint, "apis.data.go.kr") {
			withEndpoint++
		}
		for _, p := range op.Params {
			if p.Name == "numOfRows" {
				withParams++
			}
		}
	}
	if withEndpoint == 0 {
		t.Error("no operation surfaced an apis.data.go.kr endpoint")
	}
	if withParams == 0 {
		t.Error("expected numOfRows param surfaced in some operation")
	}
}

// A malformed page must not invent params — surface RawHTML instead of fabricating.
func TestDescribeSurfaceFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div class="open-api-detail">
			<h4>테스트기능</h4><p>표 구조가 없는 안내문</p></div></body></html>`))
	}))
	defer srv.Close()
	spec, err := Describe(context.Background(), srv.URL, "1")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(spec.Operations) != 1 {
		t.Fatalf("want 1 op, got %d", len(spec.Operations))
	}
	if len(spec.Operations[0].Params) != 0 {
		t.Error("must not fabricate params when no request-variable table exists")
	}
	if spec.Operations[0].RawHTML == "" {
		t.Error("expected RawHTML surfaced as fallback")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/apicall/ -run TestDescribe`
Expected: FAIL (Describe undefined).

- [ ] **Step 4: Write describe.go**

```go
// Package apicall surfaces data.go.kr OpenAPI specs to an agent (describe) and
// performs authenticated calls (call). It never parses a spec into claims it
// can't back with the page's own markup — uncertain structure is surfaced as
// raw HTML for the agent to read.
package apicall

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// APISpec is the surfaced view of one dataset's OpenAPI detail page.
type APISpec struct {
	PublicDataPk string      `json:"publicDataPk"`
	DataName     string      `json:"dataName"`
	Operations   []Operation `json:"operations"`
	GuideDoc     string      `json:"guideDoc,omitempty"` // 참고문서 link/name, NOT parsed
}

// Operation is one 상세기능. When the request-variable table parses cleanly,
// Params is filled; otherwise RawHTML carries the section verbatim.
type Operation struct {
	Name     string  `json:"name"`
	Endpoint string  `json:"endpoint,omitempty"`
	Params   []Param `json:"params,omitempty"`
	RawHTML  string  `json:"rawHtml,omitempty"`
}

// Param is one request variable, surfaced from the 요청변수 table.
type Param struct {
	Name     string `json:"name"`     // 항목명(영문)
	Required string `json:"required"` // 항목구분 (필수/옵션)
	Sample   string `json:"sample"`   // 샘플데이터
	Desc     string `json:"desc"`     // 항목설명
}

var reEndpoint = regexp.MustCompile(`https?://apis\.data\.go\.kr/[^\s"'<)]+`)

// Describe scrapes {baseURL}/data/{pk}/openapi.do. baseURL is overridable for
// tests; production passes portal.BaseURL.
func Describe(ctx context.Context, baseURL, pk string) (*APISpec, error) {
	url := strings.TrimRight(baseURL, "/") + "/data/" + pk + "/openapi.do"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gongctl (+https://github.com/JungHoonGhae/gongctl)")
	req.Header.Set("Accept", "text/html,*/*")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %s", url, resp.Status)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	spec := &APISpec{PublicDataPk: pk}
	spec.DataName = strings.TrimSpace(doc.Find(".open-api-title, .data-set-title").First().Text())
	spec.DataName = cleanText(spec.DataName)

	doc.Find(".open-api-detail").Each(func(_ int, sel *goquery.Selection) {
		op := Operation{Name: cleanText(sel.Find("h4, .tit").First().Text())}
		if html, err := sel.Html(); err == nil {
			if m := reEndpoint.FindString(html); m != "" {
				op.Endpoint = m
			}
		}
		op.Params = parseParams(sel)
		if len(op.Params) == 0 {
			// surface-only: no clean request-variable table → hand back raw HTML.
			if html, err := sel.Html(); err == nil {
				op.RawHTML = strings.TrimSpace(html)
			}
		}
		if op.Name != "" || op.Endpoint != "" || op.RawHTML != "" {
			spec.Operations = append(spec.Operations, op)
		}
	})

	// GuideDoc: the 참고문서 row (surface link text only; never fetch/parse it).
	doc.Find("th").EachWithBreak(func(_ int, th *goquery.Selection) bool {
		if strings.Contains(th.Text(), "참고문서") {
			spec.GuideDoc = cleanText(th.NextFiltered("td").Text())
			return false
		}
		return true
	})

	return spec, nil
}

// parseParams reads a 요청변수 table inside an operation section. It only fills
// params when it finds the exact request-variable header (항목명(영문)); response
// tables and unknown structures are left to RawHTML.
func parseParams(sel *goquery.Selection) []Param {
	var params []Param
	sel.Find("table").EachWithBreak(func(_ int, tbl *goquery.Selection) bool {
		headers := map[string]int{}
		tbl.Find("thead th, tr:first-child th").Each(func(i int, th *goquery.Selection) {
			headers[cleanText(th.Text())] = i
		})
		nameCol, ok := colIndex(headers, "항목명(영문)")
		if !ok {
			return true // not a request-variable table; keep scanning
		}
		reqCol, _ := colIndex(headers, "항목구분")
		sampleCol, _ := colIndex(headers, "샘플데이터")
		descCol, _ := colIndex(headers, "항목설명")
		tbl.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
			cells := tr.Find("td")
			if cells.Length() == 0 {
				return
			}
			get := func(idx int) string {
				if idx < 0 {
					return ""
				}
				return cleanText(cells.Eq(idx).Text())
			}
			name := get(nameCol)
			if name == "" {
				return
			}
			params = append(params, Param{
				Name:     name,
				Required: get(reqCol),
				Sample:   get(sampleCol),
				Desc:     get(descCol),
			})
		})
		return false // took the first request-variable table
	})
	return params
}

func colIndex(headers map[string]int, key string) (int, bool) {
	i, ok := headers[key]
	if !ok {
		return -1, false
	}
	return i, true
}

func cleanText(s string) string { return strings.Join(strings.Fields(s), " ") }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/apicall/ -run TestDescribe`
Expected: PASS (both TestDescribe and TestDescribeSurfaceFallback). If `TestDescribe` finds 0 endpoints/params, open `testdata/op-15000908.html`, confirm the `.open-api-detail` container and header labels match, and adjust the selectors/labels to the fixture — the fixture is ground truth.

- [ ] **Step 6: Commit**

```bash
git add internal/apicall/describe.go internal/apicall/describe_test.go internal/apicall/testdata/op-15000908.html
git commit -m "feat: describe — surface OpenAPI operations/params from openapi.do"
```

---

### Task 8: `call` — serviceKey injection, GET, XML→JSON, error surface

**Files:**
- Create: `internal/apicall/call.go`
- Create: `internal/apicall/call_test.go`

**Interfaces:**
- Produces: `apicall.Call(ctx, endpoint string, params map[string]string, key string) (*CallResult, error)`, type `CallResult{Status int, ContentType string, Body any}`, sentinel-ish error text for the key-form hint.

Design decisions (from spec §4.2):
- **serviceKey injection:** appended to the query as `serviceKey=<key>` **raw, not re-encoded** — data.go.kr issues the key in an already-URL-encoded "Encoding" form, and `url.Values.Encode()` would double-encode it. Other params ARE url-encoded. Use the key exactly as given.
- **XML→JSON:** data.go.kr returns XML by default. Convert to a nested `map[string]any` with a small stdlib `encoding/xml` token walker (no dependency). JSON bodies pass through; anything else is returned as a string.
- **Error surface:** HTTP is usually 200 even on errors; the error code lives in the body (`resultCode`/`returnReasonCode`/`cmmMsgHeader`). Never swallow it — the body (with the code) is always returned in `CallResult.Body`. Additionally, when the body signals `SERVICE_KEY_IS_NOT_REGISTERED_ERROR`, return the populated CallResult **and** a non-nil error whose message includes the Encoding/Decoding key hint. No silent retry.

- [ ] **Step 1: Write the failing test**

`internal/apicall/call_test.go`:
```go
package apicall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallXMLToJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// serviceKey must arrive verbatim (not double-encoded).
		if got := r.URL.Query().Get("serviceKey"); got != "abc+def==" {
			t.Errorf("serviceKey = %q, want verbatim abc+def==", got)
		}
		if got := r.URL.Query().Get("numOfRows"); got != "10" {
			t.Errorf("numOfRows = %q", got)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<response><header><resultCode>00</resultCode></header>` +
			`<body><items><item><name>서울</name><count>5</count></item></items></body></response>`))
	}))
	defer srv.Close()

	res, err := Call(context.Background(), srv.URL+"/svc/op", map[string]string{"numOfRows": "10"}, "abc+def==")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	m, ok := res.Body.(map[string]any)
	if !ok {
		t.Fatalf("body not a map: %T", res.Body)
	}
	if _, ok := m["header"]; !ok {
		t.Errorf("converted XML missing header: %v", m)
	}
}

func TestCallServiceKeyHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<OpenAPI_ServiceResponse><cmmMsgHeader>` +
			`<returnReasonCode>30</returnReasonCode>` +
			`<returnAuthMsg>SERVICE_KEY_IS_NOT_REGISTERED_ERROR</returnAuthMsg>` +
			`</cmmMsgHeader></OpenAPI_ServiceResponse>`))
	}))
	defer srv.Close()

	res, err := Call(context.Background(), srv.URL, nil, "wrongkey")
	if res == nil {
		t.Fatal("CallResult must still be returned (surface the body)")
	}
	if err == nil || !strings.Contains(err.Error(), "Encoding") {
		t.Fatalf("expected Encoding/Decoding key hint in error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apicall/ -run TestCall`
Expected: FAIL (Call undefined).

- [ ] **Step 3: Write call.go**

```go
package apicall

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CallResult is a surfaced API response. Body is a map (XML→JSON or JSON),
// or a string when the content isn't structured.
type CallResult struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        any    `json:"body"`
}

// Call injects serviceKey, GETs the endpoint, and surfaces the response. The
// key is used verbatim (data.go.kr's Encoding key is already URL-encoded, so
// re-encoding it would break it). It never retries: on a well-known key error
// it returns the body AND an error carrying the Encoding/Decoding hint.
func Call(ctx context.Context, endpoint string, params map[string]string, key string) (*CallResult, error) {
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	full := endpoint
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	// serviceKey appended raw; other params encoded.
	full += sep + "serviceKey=" + key
	if enc := q.Encode(); enc != "" {
		full += "&" + enc
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gongctl (+https://github.com/JungHoonGhae/gongctl)")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	res := &CallResult{Status: resp.StatusCode, ContentType: ct}
	res.Body = decodeBody(ct, raw)

	// Error surface: never swallow. If the body signals an unregistered key,
	// return the surfaced result plus a hint — the Encoding/Decoding trap.
	if strings.Contains(string(raw), "SERVICE_KEY_IS_NOT_REGISTERED_ERROR") {
		return res, fmt.Errorf("data.go.kr: SERVICE_KEY_IS_NOT_REGISTERED_ERROR — " +
			"인증키 형태(Encoding/Decoding)가 잘못됐을 수 있습니다. " +
			"활용신청 상세의 다른 키(Encoding↔Decoding)로 바꿔 다시 시도하세요")
	}
	return res, nil
}

// decodeBody converts the response into a structured value: XML→map, JSON
// passthrough, else the raw string.
func decodeBody(contentType string, raw []byte) any {
	trimmed := strings.TrimSpace(string(raw))
	if strings.Contains(contentType, "json") || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			return v
		}
	}
	if strings.HasPrefix(trimmed, "<") {
		if m, err := xmlToMap(raw); err == nil {
			return m
		}
	}
	return trimmed
}

// xmlToMap converts XML into a nested map[string]any using the stdlib token
// stream. Repeated sibling elements become a []any. Leaf text becomes a string.
// Attributes and namespaces are dropped — data.go.kr response bodies don't use
// them meaningfully. Good enough to surface; the agent reads the shape.
func xmlToMap(raw []byte) (map[string]any, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	root := map[string]any{}
	stack := []map[string]any{root}
	var textBuf strings.Builder

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			textBuf.Reset()
			child := map[string]any{}
			parent := stack[len(stack)-1]
			addChild(parent, t.Name.Local, child)
			stack = append(stack, child)
		case xml.CharData:
			textBuf.Write(t)
		case xml.EndElement:
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			text := strings.TrimSpace(textBuf.String())
			textBuf.Reset()
			if len(cur) == 0 && text != "" {
				// leaf: replace the empty map in the parent with its text
				replaceLast(stack[len(stack)-1], t.Name.Local, text)
			}
		}
	}
	return root, nil
}

// addChild inserts value under key, promoting to a slice on repeats.
func addChild(m map[string]any, key string, value any) {
	if existing, ok := m[key]; ok {
		if slice, ok := existing.([]any); ok {
			m[key] = append(slice, value)
		} else {
			m[key] = []any{existing, value}
		}
		return
	}
	m[key] = value
}

// replaceLast swaps the most recently added value under key with v (used to
// turn an empty leaf-map into its text content).
func replaceLast(m map[string]any, key string, v any) {
	if existing, ok := m[key]; ok {
		if slice, ok := existing.([]any); ok {
			slice[len(slice)-1] = v
			m[key] = slice
			return
		}
	}
	m[key] = v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/apicall/`
Expected: PASS (describe + call tests). The XML walker is non-trivial — if `TestCallXMLToJSON` fails on nesting, debug `xmlToMap` against the test's XML before moving on.

- [ ] **Step 5: Commit**

```bash
git add internal/apicall/call.go internal/apicall/call_test.go
git commit -m "feat: call — serviceKey injection, XML→JSON, error-code surface"
```

---

### Task 9: CLI commands — auth, data (search/describe/call), apply/applications

**Files:**
- Create: `cmd/gongctl/auth.go`
- Create: `cmd/gongctl/data.go`
- Create: `cmd/gongctl/apply.go`
- Modify: `cmd/gongctl/root.go` (register the new commands in `init()`)

**Interfaces:**
- Consumes: `portal.Login/Logout/Applications/Apply`, `portal.Client.SearchDatasets`, `apicall.Describe/Call`, `output.*`.
- Produces: cobra commands `login`, `logout`, `status`, `search`, `describe`, `call`, `apply`, `applications`.

- [ ] **Step 1: Write auth.go**

```go
package main

import (
	"fmt"

	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "브라우저로 data.go.kr 로그인 (세션 유지)",
		Long: `브라우저 창을 띄워 data.go.kr 에 로그인합니다. 로그인이 끝나면 gongctl 이
그 브라우저를 백그라운드로 유지하고, 이후 apply/applications 등이 그 세션에
다시 붙어 동작합니다. 키체인 비밀번호는 묻지 않습니다.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := portal.Login(cmd.Context(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "   이제 `gongctl applications` 로 활용신청 현황을 볼 수 있습니다.")
			return nil
		},
	}
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "세션 브라우저를 닫고 상태를 정리",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := portal.Logout(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "세션을 종료했습니다.")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "로그인 세션 상태 확인",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := portal.Applications(cmd.Context())
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "세션 없음 — `gongctl login` 을 실행하세요.")
				return nil
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "✅ 세션이 살아있습니다.")
			return nil
		},
	}
}
```

- [ ] **Step 2: Write apply.go (apply + applications)**

Port the confirm/prompt logic and `renderApplications` from `$KVOTE/cmd/kvote/api.go` (the `apiApplyCmd`, `apiListCmd`, `mapCategory`, `renderApplications` functions), rewired to `portal` and top-level commands:
```go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func applyCmd() *cobra.Command {
	var purpose, category string
	var yes bool
	c := &cobra.Command{
		Use:   "apply <publicDataPk>",
		Short: "OpenAPI 활용신청 (자동승인) — 목적 필수, 제출 전 확인",
		Long: `data.go.kr OpenAPI 1건의 활용신청을 자동 제출합니다(자동승인). 신청은
계정에 실제 신청을 생성하므로 **한 번에 한 건만** 처리하고 **활용목적(--purpose)을
반드시 요구**하며 제출 전 확인합니다 (투기적 대량신청 금지).

예) gongctl apply 15000908 --purpose "선거 데이터 분석" --category research`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			cat := mapCategory(category)
			cfg, _ := portal.LoadConfig()
			confirm := func(s portal.ApplySummary) bool {
				if yes || (cfg != nil && cfg.AutoApply) {
					return true
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "\n다음 내용으로 활용신청을 제출합니다:\n")
				fmt.Fprintf(cmd.ErrOrStderr(), "  데이터: %s (pk=%s)\n", s.DataName, s.PublicDataPk)
				fmt.Fprintf(cmd.ErrOrStderr(), "  상세기능: %d개  목적분류: %s\n", s.Operations, s.Category)
				fmt.Fprintf(cmd.ErrOrStderr(), "  활용목적: %s\n", s.Purpose)
				fmt.Fprint(cmd.ErrOrStderr(), "제출할까요? [y/N]: ")
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				line = strings.ToLower(strings.TrimSpace(line))
				return line == "y" || line == "yes"
			}
			res, err := portal.Apply(cmd.Context(), args[0], purpose, cat, confirm)
			if err != nil {
				if errors.Is(err, portal.ErrNotLoggedIn) {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					return nil
				}
				return err
			}
			if format == output.JSON || format == output.JSONL {
				return output.WriteJSON(cmd.OutOrStdout(), res)
			}
			if res.Submitted {
				fmt.Fprintf(cmd.ErrOrStderr(), "✅ %s — `gongctl applications` 로 확인하세요.\n", res.Message)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "⏹  %s\n", res.Message)
			}
			return nil
		},
	}
	c.Flags().StringVar(&purpose, "purpose", "", "활용목적 내용 (필수)")
	c.Flags().StringVar(&category, "category", "research", "목적분류: research|web|app|ref|etc")
	c.Flags().BoolVar(&yes, "yes", false, "확인 프롬프트 생략")
	c.MarkFlagRequired("purpose")
	return c
}

func mapCategory(c string) string {
	switch strings.ToLower(c) {
	case "web":
		return portal.PurposeWeb
	case "app":
		return portal.PurposeApp
	case "ref":
		return portal.PurposeRef
	case "etc":
		return portal.PurposeEtc
	default:
		return portal.PurposeResearch
	}
}

func applicationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "applications",
		Short: "내 OpenAPI 활용신청 현황 (상태·인증키 만료예정일)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			apps, err := portal.Applications(cmd.Context())
			if err != nil {
				if errors.Is(err, portal.ErrNotLoggedIn) {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					return nil
				}
				return err
			}
			if len(apps) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "활용신청 내역이 없습니다.")
				return nil
			}
			return renderApplications(cmd, format, apps)
		},
	}
}

func renderApplications(cmd *cobra.Command, format output.Format, apps []portal.Application) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(cmd.OutOrStdout(), apps)
	case output.JSONL:
		items := make([]any, len(apps))
		for i := range apps {
			items[i] = apps[i]
		}
		return output.WriteJSONL(cmd.OutOrStdout(), items)
	default:
		headers := []string{"상태", "계정", "데이터명", "제공기관", "신청일", "만료예정일"}
		rows := make([][]string, 0, len(apps))
		for _, a := range apps {
			rows = append(rows, []string{a.Status, a.Account, a.Title, a.Org, a.AppliedAt, a.ExpiresAt})
		}
		return output.WriteTable(cmd.OutOrStdout(), headers, rows)
	}
}
```

- [ ] **Step 3: Write data.go (search/describe/call)**

```go
package main

import (
	"fmt"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/apicall"
	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func searchCmd() *cobra.Command {
	var dtype, org string
	c := &cobra.Command{
		Use:   "search <keyword>",
		Short: "data.go.kr 데이터셋 검색 (파일 + OpenAPI)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			ds, err := newPortalClient().SearchDatasets(cmd.Context(), portal.SearchOptions{
				Keyword: strings.Join(args, " "),
				Type:    strings.ToUpper(dtype),
				Org:     org,
			})
			if err != nil {
				return err
			}
			if format == output.Table {
				headers := []string{"pk", "OpenAPI", "제목", "포맷"}
				rows := make([][]string, 0, len(ds))
				for _, d := range ds {
					api := ""
					if d.HasOpenAPI {
						api = "✓"
					}
					rows = append(rows, []string{d.PublicDataPk, api, d.Title, strings.Join(d.Formats, ",")})
				}
				return output.WriteTable(cmd.OutOrStdout(), headers, rows)
			}
			return output.WriteJSON(cmd.OutOrStdout(), ds)
		},
	}
	c.Flags().StringVar(&dtype, "type", "", "데이터 유형: file | api (기본: 전체)")
	c.Flags().StringVar(&org, "org", "", "제공기관 필터")
	return c
}

func describeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <publicDataPk>",
		Short: "OpenAPI 상세 — 상세기능·엔드포인트·요청변수 surface",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			base := flagBaseURL
			if base == "" {
				base = portal.BaseURL
			}
			spec, err := apicall.Describe(cmd.Context(), base, args[0])
			if err != nil {
				return err
			}
			return output.WriteJSON(cmd.OutOrStdout(), spec)
		},
	}
}

func callCmd() *cobra.Command {
	var key string
	var params []string
	c := &cobra.Command{
		Use:   "call <endpoint>",
		Short: "인증 API 호출 — serviceKey 주입 → GET → XML→JSON",
		Long: `승인된 OpenAPI 엔드포인트를 호출합니다. --key 로 계정 인증키를,
--param k=v 로 요청변수를 전달합니다. 응답은 XML이면 JSON으로 변환해 출력합니다.

예) gongctl call http://apis.data.go.kr/9760000/.../getX --key <KEY> --param numOfRows=10`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pm := map[string]string{}
			for _, p := range params {
				kv := strings.SplitN(p, "=", 2)
				if len(kv) != 2 {
					return fmt.Errorf("--param 은 k=v 형식이어야 합니다: %q", p)
				}
				pm[kv[0]] = kv[1]
			}
			res, err := apicall.Call(cmd.Context(), args[0], pm, key)
			if res != nil {
				output.WriteJSON(cmd.OutOrStdout(), res)
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err) // surface hint, don't fail hard
			}
			return nil
		},
	}
	c.Flags().StringVar(&key, "key", "", "계정 인증키 (serviceKey)")
	c.Flags().StringArrayVar(&params, "param", nil, "요청변수 k=v (반복 가능)")
	c.MarkFlagRequired("key")
	return c
}
```

- [ ] **Step 4: Register commands in root.go**

In `cmd/gongctl/root.go` `init()`, add after `rootCmd.AddCommand(versionCmd())`:
```go
	rootCmd.AddCommand(loginCmd(), logoutCmd(), statusCmd())
	rootCmd.AddCommand(searchCmd(), describeCmd(), callCmd())
	rootCmd.AddCommand(applyCmd(), applicationsCmd())
```

- [ ] **Step 5: Verify build + help**

Run:
```bash
go build ./... && go run ./cmd/gongctl --help && go run ./cmd/gongctl search --help
```
Expected: help lists login/logout/status/search/describe/call/apply/applications/version. No live network needed.

- [ ] **Step 6: Commit**

```bash
git add cmd/gongctl/auth.go cmd/gongctl/data.go cmd/gongctl/apply.go cmd/gongctl/root.go
git commit -m "feat: CLI commands — auth, search/describe/call, apply/applications"
```

---

### Task 10: MCP server — 5 tools + gongctl://guide resource

**Files:**
- Create: `internal/mcpserver/guide.go`
- Create: `internal/mcpserver/server.go`
- Create: `internal/mcpserver/server_test.go`
- Create: `cmd/gongctl/mcp.go`
- Modify: `cmd/gongctl/root.go` (register `mcpCmd()`)

**Interfaces:**
- Consumes: `portal.Client.SearchDatasets`, `portal.Applications`, `portal.Apply`, `apicall.Describe`, `apicall.Call`.
- Produces: `mcpserver.New(Deps) *mcp.Server`, `mcpserver.Serve(ctx, Deps) error`, `mcpserver.Deps{Portal *portal.Client, BaseURL string}`, tools `search_datasets`/`list_applications`/`apply`/`describe_api`/`call_api`, resource `gongctl://guide`.

- [ ] **Step 1: Write guide.go**

```go
package mcpserver

// GuideDoc is the gongctl://guide resource — the agent's entry point. It states
// tool order and the Encoding/Decoding serviceKey trap.
const GuideDoc = `# gongctl — data.go.kr 사용 가이드

## 도구 사용 순서
1. **search_datasets(keyword)** — 데이터셋을 찾는다. hasOpenApi=true 인 것이 API 호출 대상.
2. **list_applications()** — 이미 활용신청한 API와 그 상태·인증키 만료일을 확인.
3. **apply(pk, purpose)** — 아직 신청 안 했다면 활용신청(자동승인 개발계정 → 즉시 사용 가능).
   - 로그인 세션이 필요하다. 세션이 없으면 사람에게 \`gongctl login\` 을 안내하는 에러가 온다.
4. **describe_api(pk)** — 상세기능·엔드포인트·요청변수를 surface. 파라미터는 여기서 확인해 구성한다.
   - params 가 비어 있고 rawHtml 만 있으면, 표 구조가 불확실하다는 뜻 — rawHtml 을 직접 읽어라.
   - guideDoc(참고문서)는 링크만 준다. 필요하면 사람에게 열어보게 한다.
5. **call_api(endpoint, params, key)** — 실제 호출. key 는 계정 인증키(serviceKey) 하나로 공통.

## 인증키(serviceKey) 주의 — Encoding/Decoding 함정
- data.go.kr 은 인증키를 **Encoding / Decoding 두 형태**로 준다. 계정당 키는 하나지만 표기가 둘.
- call_api 는 받은 키를 **그대로** 쓴다. 자동으로 형태를 바꾸지 않는다.
- 응답이 SERVICE_KEY_IS_NOT_REGISTERED_ERROR 이면 **키 형태 문제일 수 있다** — 다른 형태(Encoding↔Decoding)로 재시도.

## 에러는 삼키지 않는다
- data.go.kr 은 HTTP 200 에 본문 에러코드(resultCode/returnReasonCode)를 담는다.
  call_api 결과의 body 를 확인해 성공(00)인지 판단하라.
`
```

- [ ] **Step 2: Write the failing test**

`internal/mcpserver/server_test.go`:
```go
package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchToolRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write([]byte(`<dl><dt>CSV 테스트 데이터셋 미리보기</dt>` +
			`<dd>설명</dd><a href="/data/12345/fileData.do">x</a></dl>`))
	}))
	defer srv.Close()

	server := New(Deps{
		Portal:  portal.New(portal.WithBaseURL(srv.URL), portal.WithDelay(0)),
		BaseURL: srv.URL,
	})

	ct, st := mcp.NewInMemoryTransports()
	go server.Run(context.Background(), st)

	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	sess, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_datasets",
		Arguments: map[string]any{"keyword": "테스트"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %+v", res.Content)
	}
}
```

Note: verify the exact in-memory transport + client API against the pinned SDK by reading `$KVOTE/internal/mcpserver/server_test.go` (same `modelcontextprotocol/go-sdk v1.6.1`). Match its `NewInMemoryTransports`/`Connect`/`CallTool` usage precisely.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/mcpserver/`
Expected: FAIL (New undefined).

- [ ] **Step 4: Write server.go**

```go
// Package mcpserver exposes gongctl over the Model Context Protocol (stdio):
// dataset search, 활용신청, spec surfacing, and authenticated calls as tools.
// It only assembles — the deterministic work lives in portal/apicall.
package mcpserver

import (
	"context"

	"github.com/JungHoonGhae/gongctl/internal/apicall"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Deps carries the collaborators the server needs.
type Deps struct {
	Portal  *portal.Client
	BaseURL string // data.go.kr root for describe (override in tests)
}

type searchIn struct {
	Keyword string `json:"keyword" jsonschema:"free-text query for data.go.kr datasets"`
}
type searchOut struct {
	Datasets []portal.Dataset `json:"datasets"`
}
type applyIn struct {
	PK      string `json:"pk" jsonschema:"publicDataPk of the dataset to apply for"`
	Purpose string `json:"purpose" jsonschema:"활용목적 — why you need this data (required)"`
}
type describeIn struct {
	PK string `json:"pk" jsonschema:"publicDataPk of an OpenAPI dataset"`
}
type callIn struct {
	Endpoint string            `json:"endpoint" jsonschema:"full apis.data.go.kr endpoint URL"`
	Params   map[string]string `json:"params,omitempty" jsonschema:"request variables as key/value"`
	Key      string            `json:"key" jsonschema:"account serviceKey (인증키)"`
}
type appsOut struct {
	Applications []portal.Application `json:"applications"`
}

// New builds the MCP server with all five tools and the guide resource.
func New(deps Deps) *mcp.Server {
	base := deps.BaseURL
	if base == "" {
		base = portal.BaseURL
	}
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "gongctl",
		Title:   "gongctl — 공공데이터포털(data.go.kr) 자동화",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_datasets",
		Description: "data.go.kr 데이터셋을 키워드로 검색한다. hasOpenApi=true 가 API 호출 대상. publicDataPk 를 apply/describe_api 에 넘긴다.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, *searchOut, error) {
		ds, err := deps.Portal.SearchDatasets(ctx, portal.SearchOptions{Keyword: in.Keyword})
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		return nil, &searchOut{Datasets: ds}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_applications",
		Description: "내 활용신청 현황(상태·인증키 만료일)을 조회한다. 로그인 세션 필요.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *appsOut, error) {
		apps, err := portal.Applications(ctx)
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		return nil, &appsOut{Applications: apps}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "apply",
		Description: "OpenAPI 활용신청을 제출한다(자동승인 개발계정 → 즉시 키). purpose 필수. 로그인 세션 필요.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in applyIn) (*mcp.CallToolResult, *portal.ApplyResult, error) {
		res, err := portal.Apply(ctx, in.PK, in.Purpose, portal.PurposeResearch, nil) // nil confirm = agent-driven, no prompt
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		return nil, res, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "describe_api",
		Description: "OpenAPI 상세(상세기능·엔드포인트·요청변수)를 surface 한다. params 가 비고 rawHtml 만 있으면 표 구조가 불확실하다는 뜻 — rawHtml 을 읽어라.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in describeIn) (*mcp.CallToolResult, *apicall.APISpec, error) {
		spec, err := apicall.Describe(ctx, base, in.PK)
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		return nil, spec, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "call_api",
		Description: "승인된 endpoint 를 serviceKey 주입 후 호출한다. 응답 XML 은 JSON 으로 변환. body 의 resultCode 로 성공(00) 여부 확인. 키 오류면 Encoding/Decoding 형태를 바꿔 재시도.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in callIn) (*mcp.CallToolResult, *apicall.CallResult, error) {
		res, err := apicall.Call(ctx, in.Endpoint, in.Params, in.Key)
		if err != nil {
			// surface the hint but still return the body
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, res, nil
		}
		return nil, res, nil
	})

	s.AddResource(&mcp.Resource{
		Name:        "guide",
		URI:         "gongctl://guide",
		MIMEType:    "text/markdown",
		Description: "gongctl 도구 사용 순서와 인증키 Encoding/Decoding 주의. 먼저 읽으세요.",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      "gongctl://guide",
			MIMEType: "text/markdown",
			Text:     GuideDoc,
		}}}, nil
	})

	return s
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

// Serve runs the server over stdio until the client disconnects or ctx cancels.
func Serve(ctx context.Context, deps Deps) error {
	return New(deps).Run(ctx, &mcp.StdioTransport{})
}
```

Note: the empty-input tool (`list_applications`) uses `struct{}` — confirm the SDK accepts a no-field input struct (kvote's tools all take a field). If it rejects `struct{}`, give it a dummy `type emptyIn struct{}` with no jsonschema fields, matching whatever the pinned SDK requires (check by compiling).

- [ ] **Step 5: Write cmd/gongctl/mcp.go + register**

```go
package main

import (
	"context"

	"github.com/JungHoonGhae/gongctl/internal/mcpserver"
	"github.com/spf13/cobra"
)

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP 서버 실행 (stdio) — 에이전트가 검색·활용신청·호출",
		Long: `gongctl 을 Model Context Protocol 서버로 노출합니다(stdio).
search_datasets / list_applications / apply / describe_api / call_api tool 과
gongctl://guide 리소스로 에이전트가 data.go.kr 을 다룹니다. 로그인 세션 전제.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return mcpserver.Serve(context.Background(), mcpserver.Deps{
				Portal: newPortalClient(),
			})
		},
	}
}
```
Add `rootCmd.AddCommand(mcpCmd())` to `root.go` `init()`.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/mcpserver/ && go build ./...`
Expected: PASS + full build.

- [ ] **Step 7: Commit**

```bash
git add internal/mcpserver cmd/gongctl/mcp.go cmd/gongctl/root.go
git commit -m "feat: MCP server — 5 tools + gongctl://guide resource"
```

---

### Task 11: Release pipeline — goreleaser, install scripts, CI

**Files:**
- Create: `.goreleaser.yaml`
- Create: `install.sh`
- Create: `install.ps1`
- Create: `.github/workflows/release.yml`
- Create: `LICENSE` (MIT)
- Create: `README.md`

**Interfaces:** none (build/release config). Port structure from `$KVOTE`'s equivalents, renaming kvote→gongctl, module path, and binary name.

- [ ] **Step 1: Port goreleaser + scripts**

Copy `$KVOTE/.goreleaser.yaml`, `install.sh`, `install.ps1` (if present) and rewrite: binary name `kvote`→`gongctl`, module path, repo `k-vote-cli`→`gongctl`, ldflags targets `internal/version.{Version,Commit,Date}`. Set `builds.env: [CGO_ENABLED=0]`, GOOS `darwin/linux/windows`, GOARCH `amd64/arm64`.

- [ ] **Step 2: Verify goreleaser config + snapshot build**

Run:
```bash
go install github.com/goreleaser/goreleaser/v2@latest 2>/dev/null || true
goreleaser check
goreleaser build --snapshot --clean --single-target
```
Expected: `goreleaser check` passes; a `gongctl` binary is produced under `dist/`.
Run: `./dist/**/gongctl version` → prints `gongctl <version> (...)`.

- [ ] **Step 3: Write LICENSE + README**

MIT `LICENSE` (2026, Junghoon). `README.md`: one-paragraph pitch ("data-go-mcp인데 포털을 안 건드림"), install one-liner, `gongctl login` → `search` → `apply` → `describe` → `call` walkthrough, and the MCP config snippet (`gongctl mcp` as an MCP stdio server).

- [ ] **Step 4: Commit (workflow needs token workflow scope)**

```bash
git add .goreleaser.yaml install.sh install.ps1 LICENSE README.md .github/workflows/release.yml
git commit -m "chore: release pipeline (goreleaser, install scripts, CI, docs)"
```
Note: pushing `.github/workflows/` requires a git token with **workflow** scope (kvote hit this). If the push is rejected, re-auth `gh auth refresh -s workflow` or push workflows separately.

---

## Final Live Validation (manual, not automated — spec §7)

After Task 11, run the real end-to-end path once against the actual portal with a real data.go.kr account:
```bash
go run ./cmd/gongctl login                 # browser opens; log in once
go run ./cmd/gongctl search 대기오염 --type api -f table
go run ./cmd/gongctl apply <pk> --purpose "테스트 검증" --category research
go run ./cmd/gongctl applications -f table  # confirm approved + note the key
go run ./cmd/gongctl describe <pk>
go run ./cmd/gongctl call <endpoint> --key <KEY> --param numOfRows=5
```
Expected: apply reflects in the list (auto-approved), describe surfaces operations, call returns a JSON-converted body with resultCode 00. This is the only test that exercises the CDP automation (login/apply/list) — unit tests can't.

---

## Self-Review

- **Spec coverage:** §1 pitch → README (T11). §2 architecture → file structure + all tasks. §3 MCP 5 tools + guide → T10. §4.1 describe → T7 (types + surface fallback verified against real fixture). §4.2 call → T8 (serviceKey verbatim, XML→JSON, error surface, key hint). §5 kvote port → T2–T6. §6 error handling → ErrNotLoggedIn paths (T5/T9), error-code surface (T8), tool-level errResult (T10). §7 tests → each ported/new component has a fixture/httptest; live E2E called out as manual. §8 deploy → T11. §9 YAGNI: no multiportal, no guide-doc parsing, no async 심의 polling, no shared-lib extraction, no login automation — none added.
- **Type consistency:** `portal.Client`/`SearchOptions`/`Dataset`/`Application`/`ApplySummary`/`ApplyResult`/`Purpose*` consistent T5/T6/T9/T10. `apicall.APISpec`/`Operation`/`Param`/`CallResult` consistent T7/T8/T9/T10. `Describe(ctx, baseURL, pk)` and `Call(ctx, endpoint, params, key)` signatures identical everywhere used. `mcpserver.Deps{Portal, BaseURL}` consistent T10.
- **Fixtures grounded:** describe fixture (`op-15000908.html`) already captured and verified server-rendered; search fixture captured live in T6 step 1; accounts fixture ported from kvote with its passing test.
- **Known implementation-time derivations (flagged, not placeholders):** search `<dl>` selector (T6 step 6) and describe operation-container/header labels (T7 step 5) are verified against captured fixtures at implement time — the one legitimate place selectors are tuned, since fragile scraping cannot be written blind. SDK API shapes (in-memory transport, empty-input tool) verified against pinned v1.6.1 via kvote's own test (T10 steps 2/4).
