// Package doctor is a liveness check for gongctl's fragile scraping. data.go.kr
// can redesign its HTML at any time, and the parsers degrade to *empty* results
// rather than crashing — so drift is otherwise silent. doctor drives each
// scraping seam against the live portal and reports whether it still yields data,
// turning silent drift into a loud, checkable signal (arch review candidate B).
package doctor

import (
	"context"
	"errors"
	"fmt"

	"github.com/JungHoonGhae/gongctl/internal/apicall"
	"github.com/JungHoonGhae/gongctl/internal/catalog"
	"github.com/JungHoonGhae/gongctl/internal/fetch"
	"github.com/JungHoonGhae/gongctl/internal/portal"
)

// CanaryPK is a stable, long-lived OpenAPI dataset (중앙선거관리위원회
// PofelcddInfoInqireService) used to probe the describe scraper. If data.go.kr
// ever retires it, the describe check will report drift — update this pk then.
const CanaryPK = "15000908"

// Status is the outcome of one check.
type Status string

const (
	StatusOK      Status = "ok"      // seam still yields data
	StatusDrift   Status = "drift"   // seam parsed to nothing — markup likely changed
	StatusSkipped Status = "skipped" // precondition missing (e.g. not logged in)
)

// Check is one diagnostic result.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Run drives the read-only scraping seams (dataset search, OpenAPI describe)
// against baseURL and reports whether each still yields data. It needs no login;
// the session-scoped seams (활용신청 현황) are checked separately by the caller.
func Run(ctx context.Context, fc *fetch.Client, baseURL string) []Check {
	return []Check{
		searchCheck(ctx, fc, baseURL),
		describeCheck(ctx, fc, baseURL),
		catalogCheck(),
	}
}

// catalogCheck reports the local catalogue's freshness. A stale snapshot is the
// quiet failure mode of local discovery: searches keep succeeding while silently
// missing everything published since the last sync, so doctor — the command that
// exists to make quiet problems loud — is where it belongs.
func catalogCheck() Check {
	cat, err := catalog.Load()
	if errors.Is(err, catalog.ErrNotSynced) {
		return Check{"catalog", StatusSkipped, "카탈로그 없음 — `gongctl catalog sync` 로 만들면 검색이 즉시 처리됩니다"}
	}
	if err != nil {
		return Check{"catalog", StatusDrift, "카탈로그를 읽지 못했습니다: " + err.Error()}
	}
	days := cat.Age().Hours() / 24
	if cat.Stale() {
		return Check{"catalog", StatusDrift, fmt.Sprintf(
			"%.0f일 전 스냅샷 (%d건) — 그 이후 신설된 API 가 검색에서 누락됩니다. `gongctl catalog sync`",
			days, len(cat.Entries))}
	}
	return Check{"catalog", StatusOK, fmt.Sprintf("%.1f일 전 수집 (%d건)", days, len(cat.Entries))}
}

func searchCheck(ctx context.Context, fc *fetch.Client, baseURL string) Check {
	pc := portal.New(fc, portal.WithBaseURL(baseURL))
	ds, err := pc.SearchDatasets(ctx, portal.SearchOptions{})
	switch {
	case err != nil:
		return Check{"search", StatusDrift, "요청 실패: " + err.Error()}
	case len(ds) == 0:
		return Check{"search", StatusDrift, "데이터셋 0건 파싱 — selectDataSetList.do 마크업이 바뀌었을 수 있음"}
	default:
		return Check{"search", StatusOK, fmt.Sprintf("%d개 데이터셋 파싱", len(ds))}
	}
}

func describeCheck(ctx context.Context, fc *fetch.Client, baseURL string) Check {
	spec, err := apicall.Describe(ctx, fc, baseURL, CanaryPK)
	switch {
	case err != nil:
		return Check{"describe", StatusDrift, "요청 실패: " + err.Error()}
	case len(spec.Operations) == 0:
		return Check{"describe", StatusDrift, "상세기능 0건 파싱 — openapi.do 마크업이 바뀌었을 수 있음 (pk=" + CanaryPK + ")"}
	default:
		return Check{"describe", StatusOK, fmt.Sprintf("%d개 상세기능 파싱 (pk=%s)", len(spec.Operations), CanaryPK)}
	}
}
