package apicall

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
)

// OperationName is the last path segment of an operation's endpoint — the part a
// caller can actually name. The portal's own operation titles are Korean
// sentences ("지역 및 실내·외 장소별 폭염 인명피해 통계정보를 조회한다."), which nobody
// is going to retype, while the segment (getHeatWaveCasualtiesRegionList) is short,
// unique within a dataset, and identical to what appears in the endpoint.
func OperationName(op Operation) string {
	if op.Endpoint == "" {
		return ""
	}
	e := strings.TrimRight(op.Endpoint, "/")
	if i := strings.LastIndex(e, "/"); i >= 0 {
		return e[i+1:]
	}
	return e
}

// ErrAmbiguousOperation is returned when a dataset has several operations and the
// caller named none. It lists them, because the fix is to pick one.
type ErrAmbiguousOperation struct {
	PK         string
	Operations []string
}

func (e *ErrAmbiguousOperation) Error() string {
	return fmt.Sprintf("pk=%s 에 상세기능이 %d개 있습니다 — 하나를 지정하세요: %s",
		e.PK, len(e.Operations), strings.Join(e.Operations, ", "))
}

// Resolve looks up one operation's endpoint from the portal instead of having a
// caller type a URL. Guessing an endpoint from a dataset's name is the mistake this
// exists to prevent: the paths are unguessable (HeatWaveCasualtiesRegion/
// getHeatWaveCasualtiesRegionList), and a wrong one answers 500 or 404 rather than
// saying it does not exist.
//
// name matches an operation's last path segment, case-insensitively; empty means
// "the only one there is", which fails loudly rather than picking for you when
// there is more than one.
func Resolve(ctx context.Context, f *fetch.Client, baseURL, pk, name string) (*Operation, error) {
	spec, err := Describe(ctx, f, baseURL, pk)
	if err != nil {
		return nil, err
	}
	var callable []Operation
	for _, op := range spec.Operations {
		if op.Endpoint != "" {
			callable = append(callable, op)
		}
	}
	if len(callable) == 0 {
		note := spec.Note
		if note == "" {
			note = "포털이 이 데이터셋의 엔드포인트를 싣지 않았습니다"
		}
		return nil, fmt.Errorf("pk=%s 에서 호출 가능한 엔드포인트를 찾지 못했습니다 — %s", pk, note)
	}
	if name == "" {
		if len(callable) == 1 {
			return &callable[0], nil
		}
		names := make([]string, 0, len(callable))
		for _, op := range callable {
			names = append(names, OperationName(op))
		}
		sort.Strings(names)
		return nil, &ErrAmbiguousOperation{PK: pk, Operations: names}
	}
	for i := range callable {
		if strings.EqualFold(OperationName(callable[i]), name) {
			return &callable[i], nil
		}
	}
	names := make([]string, 0, len(callable))
	for _, op := range callable {
		names = append(names, OperationName(op))
	}
	sort.Strings(names)
	return nil, fmt.Errorf("pk=%s 에 %q 상세기능이 없습니다 — 있는 것: %s", pk, name, strings.Join(names, ", "))
}

// injectedParams are supplied by gongctl itself (Call appends serviceKey), so a
// caller omitting them is not omitting anything. Specs spell it with either case.
var injectedParams = map[string]bool{"servicekey": true}

// isRequired reads the portal's 항목구분 value. The same field arrives with two
// different vocabularies depending on which source described the API — the
// embedded Swagger document yields "필수"/"옵션" while the rendered HTML table
// yields the portal's own "필"/"옵" — so this matches on the prefix. Comparing to
// "필수" alone would silently treat every HTML-described parameter as optional,
// which is exactly the kind of check that passes its tests and protects nothing.
func isRequired(v string) bool {
	v = strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(v, "필"): // 필, 필수
		return true
	case strings.EqualFold(v, "Y"), strings.EqualFold(v, "true"):
		return true
	}
	return false
}

// MissingRequired lists the operation's required request variables that params
// does not supply, ignoring the serviceKey gongctl injects. An empty result from
// data.go.kr often means a missing parameter rather than no data, and the response
// says nothing about which — so this is checked before spending the request.
//
// It reports nothing when the operation carries no documented parameters: that
// means the portal never published them, not that none are needed, and inventing
// a requirement there would block a legitimate call.
func MissingRequired(op *Operation, params map[string]string) []string {
	if op == nil || len(op.Params) == 0 {
		return nil
	}
	have := make(map[string]bool, len(params))
	for k := range params {
		have[strings.ToLower(k)] = true
	}
	var missing []string
	for _, p := range op.Params {
		n := strings.TrimSpace(p.Name)
		if n == "" || injectedParams[strings.ToLower(n)] || !isRequired(p.Required) {
			continue
		}
		if !have[strings.ToLower(n)] {
			missing = append(missing, n)
		}
	}
	return missing
}
