// Package mcpserver exposes gongctl over the Model Context Protocol (stdio):
// dataset search, 활용신청, spec surfacing, and authenticated calls as tools.
// It only assembles — the deterministic work lives in portal/apicall.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JungHoonGhae/gongctl/internal/apicall"
	"github.com/JungHoonGhae/gongctl/internal/catalog"
	"github.com/JungHoonGhae/gongctl/internal/fetch"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Deps carries the collaborators the server needs.
type Deps struct {
	Fetch   *fetch.Client
	BaseURL string // data.go.kr root for search/describe (override in tests)
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
	Endpoint string            `json:"endpoint,omitempty" jsonschema:"full apis.data.go.kr endpoint URL. Prefer pk instead — endpoint paths cannot be guessed, and a wrong one answers 404 or 500 rather than saying so"`
	PK       string            `json:"pk,omitempty" jsonschema:"publicDataPk — gongctl looks the endpoint up from the portal instead of you typing or guessing a URL. Use this unless you already hold an endpoint from describe_api"`
	Op       string            `json:"op,omitempty" jsonschema:"which operation, named by the last path segment of its endpoint (e.g. getHeatWaveCasualtiesRegionList). Omit when the dataset has only one"`
	Params   map[string]string `json:"params,omitempty" jsonschema:"request variables as key/value"`
	// Bounded well below apicall.MaxPropagationWait: a tool call that blocks for an
	// hour looks like a hung agent, and the caller can simply ask again.
	WaitSeconds int    `json:"waitSeconds,omitempty" jsonschema:"wait up to this many seconds for a just-approved API to propagate, retrying the 403 (max 300). Use it right after apply; omit it otherwise"`
	Key         string `json:"key,omitempty" jsonschema:"account serviceKey — omit it and gongctl fetches the account key from the login session"`
}
type catalogIn struct {
	Query string `json:"query" jsonschema:"space-separated terms; every term must appear in the title, publisher or description"`
	Limit int    `json:"limit,omitempty" jsonschema:"max rows to return (default 20) — keep it small, the total match count comes back separately"`
	// Default false so a caller sees the whole catalogue unless it says otherwise;
	// the tool description tells an agent that intends to call to set it.
	RESTOnly bool `json:"restOnly,omitempty" jsonschema:"true = only datasets whose spec is published on the portal (REST). Set this when you intend to describe and call, since a LINK dataset cannot be called from here"`
}
type catalogOut struct {
	Terms    []string      `json:"terms"`             // what the query was reduced to
	Relaxed  bool          `json:"relaxed,omitempty"` // true = no entry had every term, so any-term matches are shown
	Total    int           `json:"total"`             // matches found
	Shown    int           `json:"shown"`             // rows returned
	SyncedAt string        `json:"syncedAt"`          // when the catalogue was built
	Stale    bool          `json:"stale"`             // true = re-sync, results may be incomplete
	Hits     []catalog.Hit `json:"hits"`
}
type keyOut struct {
	ServiceKey string `json:"serviceKey"`
}
type appsOut struct {
	Applications []portal.Application `json:"applications"`
}

// maxToolWait caps how long call_api will block. Propagation can take longer than
// this; the agent is told to call again rather than have a tool hold the session.
const maxToolWait = 5 * time.Minute

// emptyIn is the input type for tools that take no arguments. mcp.AddTool
// infers a JSON schema from the struct even with zero fields, so this is
// just a named `struct{}` for readability at call sites.
type emptyIn struct{}

// New builds the MCP server with all five tools and the guide resource.
func New(deps Deps) *mcp.Server {
	base := deps.BaseURL
	if base == "" {
		base = portal.BaseURL
	}
	// One shared transport → one throttle across search/describe/call.
	pc := portal.New(deps.Fetch, portal.WithBaseURL(base))
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "gongctl",
		Title:   "gongctl — 공공데이터포털(data.go.kr) 자동화",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_datasets",
		Description: "data.go.kr 데이터셋을 키워드로 검색한다. hasOpenApi=true 가 API 호출 대상. publicDataPk 를 apply/describe_api 에 넘긴다.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, *searchOut, error) {
		ds, err := pc.SearchDatasets(ctx, portal.SearchOptions{Keyword: in.Keyword})
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		return nil, &searchOut{Datasets: ds}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "catalog_search",
		Description: "로컬 카탈로그에서 어떤 API 가 존재하는지 찾는다. 포털 검색과 달리 전체 목록을 한 번에 " +
			"훑으므로 '이런 데이터가 있나?'를 키워드를 추측해가며 여러 번 물을 필요가 없다. " +
			"query 는 자연어로 그대로 써도 된다('폭염에 취약한 고령자'처럼) — 조사와 불필요한 말은 " +
			"알아서 걸러진다. 결과는 활용신청 많은 순(=실제로 쓰이는 순)이고, 설명문은 반환하지 않는다" +
			"(컨텍스트 절약). 데이터 탐색은 이 도구로 시작하라. " +
			"svcType 이 LINK 면 포털에 명세가 없어 describe_api/call_api 로 갈 수 없다(전체의 약 40%%가 LINK다) — " +
			"호출까지 갈 생각이면 restOnly=true 를 주고 검색하라. svcType 이 비어 있으면 유형이 확인되지 않은 것이다. " +
			"relaxed=true 면 모든 단어를 포함하는 데이터가 없어 일부만 일치하는 것까지 보여준 것이므로 " +
			"matched 가 낮은 결과는 무관할 수 있다. terms 로 실제 검색된 단어를 확인하라. " +
			"stale=true 면 스냅샷이 오래되어 최근 신설 API 가 누락될 수 있다. " +
			"카탈로그가 없으면 사람에게 `gongctl catalog sync` 를 안내하라.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in catalogIn) (*mcp.CallToolResult, *catalogOut, error) {
		cat, err := catalog.Load()
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		res := cat.Search(in.Query, in.Limit, in.RESTOnly)
		hits := res.Hits
		return nil, &catalogOut{
			Terms: res.Terms, Relaxed: res.Relaxed,
			Total: res.Total, Shown: len(hits),
			SyncedAt: cat.SyncedAt.Format("2006-01-02"), Stale: cat.Stale(),
			Hits: hits,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_applications",
		Description: "내 활용신청 현황(상태·인증키 만료일)을 조회한다. 로그인 세션 필요.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *appsOut, error) {
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
		Description: "OpenAPI 상세(상세기능·엔드포인트·요청변수)를 surface 한다. params 가 비고 rawHtml 만 있으면 표 구조가 불확실하다는 뜻 — rawHtml 을 읽어라. operations 가 비고 note 가 있으면 명세가 참고문서에만 있는 API이므로 guideDocUrl 을 내려받아 읽어라 (파라미터 추측 금지).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in describeIn) (*mcp.CallToolResult, *apicall.APISpec, error) {
		spec, err := apicall.Describe(ctx, deps.Fetch, base, in.PK)
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		return nil, spec, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "call_api",
		Description: "승인된 API 를 호출한다. **endpoint URL 을 지어내지 마라** — pk 를 주면 " +
			"gongctl 이 포털에서 엔드포인트를 조회하고, 명세의 필수 요청변수가 빠졌는지 호출 전에 " +
			"확인한다(빠지면 data.go.kr 은 에러 대신 빈 결과를 주므로 스스로 알아채기 어렵다). " +
			"상세기능이 여럿이면 op 로 지정하라(엔드포인트 마지막 경로 조각). " +
			"**방금 apply 한 API 라면 waitSeconds=300 을 줘라** — 승인은 즉시지만 게이트웨이 반영에 " +
			"보통 7~10분 걸려 403 이 오고, gongctl 이 그 동안 1분 간격으로 재시도한다. " +
			"그래도 403 이면 실패가 아니라 아직 반영 전이니 잠시 후 다시 호출하라(키를 바꾸거나 " +
			"다시 신청하지 마라). key 를 생략하면 로그인 세션에서 계정 인증키를 자동으로 가져다 쓴다 — 사람에게 키를 물어볼 필요가 없다. 응답 XML 은 JSON 으로 변환. body 의 resultCode 로 성공(00) 여부 확인.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in callIn) (*mcp.CallToolResult, *apicall.CallResult, error) {
		endpoint := in.Endpoint
		if endpoint == "" {
			if in.PK == "" {
				return errResult("endpoint 또는 pk 중 하나가 필요합니다"), nil, nil
			}
			resolved, rerr := apicall.Resolve(ctx, deps.Fetch, base, in.PK, in.Op)
			if rerr != nil {
				return errResult(rerr.Error()), nil, nil
			}
			endpoint = resolved.Endpoint
			if missing := apicall.MissingRequired(resolved, in.Params); len(missing) > 0 {
				return errResult(fmt.Sprintf("필수 요청변수가 빠졌습니다: %s — describe_api(pk=%s) 로 확인하세요",
					strings.Join(missing, ", "), in.PK)), nil, nil
			}
		}
		key := in.Key
		if key == "" {
			k, kerr := portal.APIKey(ctx)
			if kerr != nil {
				return errResult("인증키를 얻지 못했습니다: " + kerr.Error()), nil, nil
			}
			key = k
		}
		doCall := func(k string) (*apicall.CallResult, error) {
			if in.WaitSeconds <= 0 {
				return apicall.Call(ctx, deps.Fetch, endpoint, in.Params, k)
			}
			w := time.Duration(in.WaitSeconds) * time.Second
			if w > maxToolWait {
				w = maxToolWait
			}
			return apicall.CallWaiting(ctx, deps.Fetch, endpoint, in.Params, k, w, nil)
		}
		res, err := doCall(key)
		// A rejected key may just be a stale cached copy (the user reissued it).
		// Drop it and read the key again — once, so a genuinely bad key still fails.
		if errors.Is(err, apicall.ErrKeyRejected) && in.Key == "" {
			portal.InvalidateCachedKey()
			if fresh, kerr := portal.APIKey(ctx); kerr == nil && fresh != key {
				res, err = doCall(fresh)
			}
		}
		if err != nil {
			// surface the hint but still return the body
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, res, nil
		}
		return nil, res, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_api_key",
		Description: "계정 인증키(serviceKey)를 조회한다. 계정당 하나이며 첫 활용신청 승인 시 발급되어 모든 승인 API에 공통. call_api 는 이 키를 자동으로 쓰므로, 키를 직접 보고 싶을 때만 호출하면 된다.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *keyOut, error) {
		key, err := portal.APIKey(ctx)
		if err != nil {
			return errResult(err.Error()), nil, nil
		}
		return nil, &keyOut{ServiceKey: key}, nil
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
