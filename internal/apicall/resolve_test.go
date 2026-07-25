package apicall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
)

// describeFromHTML runs Describe against a page body, for assertions about a single
// row of the summary table.
func describeFromHTML(t *testing.T, body string) *APISpec {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body>" + body + "</body></html>"))
	}))
	t.Cleanup(srv.Close)
	spec, err := Describe(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL, "1")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	return spec
}

// The same field arrives with two vocabularies depending on which source described
// the API: the embedded Swagger document says 필수/옵션, the rendered HTML table
// says the portal's own 필/옵. A check written against one of them silently passes
// everything from the other.
func TestIsRequiredAcceptsBothVocabularies(t *testing.T) {
	for v, want := range map[string]bool{
		"필수": true, "필": true, "  필  ": true, "Y": true,
		"옵션": false, "옵": false, "": false, "N": false,
	} {
		if got := isRequired(v); got != want {
			t.Errorf("isRequired(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestMissingRequired(t *testing.T) {
	op := &Operation{Params: []Param{
		{Name: "ServiceKey", Required: "필"}, // gongctl injects this one
		{Name: "pageNo", Required: "필"},
		{Name: "numOfRows", Required: "필수"},
		{Name: "bas_yy", Required: "옵"},
	}}
	missing := MissingRequired(op, map[string]string{"pageNo": "1"})
	if len(missing) != 1 || missing[0] != "numOfRows" {
		t.Errorf("missing = %v, want [numOfRows]", missing)
	}
	if m := MissingRequired(op, map[string]string{"pageNo": "1", "NUMOFROWS": "10"}); len(m) != 0 {
		t.Errorf("parameter names should match case-insensitively, still missing %v", m)
	}
	if m := MissingRequired(op, map[string]string{"pageNo": "1", "numOfRows": "10", "bas_yy": "2019"}); len(m) != 0 {
		t.Errorf("all required supplied, still missing %v", m)
	}
}

// A dataset whose parameters the portal never published must stay callable: an
// undocumented spec is not evidence that nothing is required.
func TestMissingRequiredSilentWhenSpecHasNoParams(t *testing.T) {
	if m := MissingRequired(&Operation{}, nil); m != nil {
		t.Errorf("undocumented operation should not block a call, got %v", m)
	}
	if m := MissingRequired(nil, nil); m != nil {
		t.Errorf("nil operation should not block a call, got %v", m)
	}
}

func TestOperationName(t *testing.T) {
	for endpoint, want := range map[string]string{
		"http://apis.data.go.kr/1741000/HeatWaveCasualtiesRegion/getHeatWaveCasualtiesRegionList": "getHeatWaveCasualtiesRegionList",
		"http://apis.data.go.kr/a/b/getX/": "getX",
		"":                                 "",
	} {
		if got := OperationName(Operation{Endpoint: endpoint}); got != want {
			t.Errorf("OperationName(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

// The portal renders some 상세기능 blocks twice (a PC table and a mobile one), which
// made a single-operation dataset report two identical operations — and then refuse
// to call it as "ambiguous", listing the same name twice as the choices.
func TestDedupeOperationsCollapsesIdenticalDuplicates(t *testing.T) {
	dup := Operation{
		Name:     "지역별 폭염 인명피해를 조회한다.",
		Endpoint: "http://apis.data.go.kr/1741000/HeatWaveCasualtiesRegion/getHeatWaveCasualtiesRegionList",
		Params:   []Param{{Name: "pageNo", Required: "필"}, {Name: "numOfRows", Required: "필"}},
	}
	if got := dedupeOperations([]Operation{dup, dup}); len(got) != 1 {
		t.Errorf("identical duplicates → %d operations, want 1", len(got))
	}
	// Same endpoint, different documented variables: genuinely two operations.
	other := dup
	other.Params = []Param{{Name: "pageNo", Required: "필"}}
	if got := dedupeOperations([]Operation{dup, other}); len(got) != 2 {
		t.Errorf("operations differing in parameters → %d, want 2", len(got))
	}
}

// gongctl applies for a development account, and the portal grades the two stages
// separately: every sampled dataset auto-approves at 개발단계 while a third of them
// require review at 운영단계. Conflating the two would either promise a key that
// needs a human or warn about review that never applies here.
func TestApprovalParsing(t *testing.T) {
	spec := describeFromHTML(t, `<table><tr>
		<th>심의유형</th><td> 개발단계 : 자동승인 / 운영단계 : 심의승인 </td>
	</tr></table>`)
	if spec.Approval == nil {
		t.Fatal("심의유형 row not surfaced")
	}
	if spec.Approval.Dev != "자동승인" || spec.Approval.Ops != "심의승인" {
		t.Errorf("dev=%q ops=%q, want 자동승인/심의승인", spec.Approval.Dev, spec.Approval.Ops)
	}
	if !spec.Approval.AutoApproved() {
		t.Error("dev 자동승인 should report AutoApproved")
	}
	if spec.Approval.Raw == "" {
		t.Error("raw row should be kept in case the wording changes")
	}
}

// A page without the row must not be reported as auto-approving.
func TestApprovalAbsentIsNotAssumedAutomatic(t *testing.T) {
	spec := describeFromHTML(t, `<table><tr><th>제공기관</th><td>어딘가</td></tr></table>`)
	if spec.Approval != nil {
		t.Errorf("no 심의유형 row → approval should stay nil, got %+v", spec.Approval)
	}
	if spec.Approval.AutoApproved() {
		t.Error("unknown approval must not read as automatic")
	}
}

// A review stage the portal words differently must not silently read as automatic.
func TestApprovalUnparsedKeepsRaw(t *testing.T) {
	spec := describeFromHTML(t, `<table><tr><th>심의유형</th><td>전면 심의 대상</td></tr></table>`)
	if spec.Approval == nil || spec.Approval.Raw != "전면 심의 대상" {
		t.Fatalf("unexpected approval %+v", spec.Approval)
	}
	if spec.Approval.AutoApproved() {
		t.Error("unrecognised wording must not read as auto-approved")
	}
}

// Every LINK dataset sampled (70/70) carries the publisher's address in the URL
// row, so a note telling the caller to consult the publisher without handing over
// that address withholds the only actionable thing on the page.
func TestLinkURLSurfacedAndNamedInNote(t *testing.T) {
	spec := describeFromHTML(t, `<table>
		<tr><th>API 유형</th><td>LINK</td></tr>
		<tr><th>URL</th><td><a href="https://www.safetydata.go.kr/disaster-data/view?dataSn=1326">https://www.safetydata.go.kr/disaster-data/view?dataSn=1326</a></td></tr>
		<tr><th>참고문서</th><td></td></tr>
	</table>`)
	if spec.LinkURL != "https://www.safetydata.go.kr/disaster-data/view?dataSn=1326" {
		t.Fatalf("linkUrl = %q, want the publisher's href", spec.LinkURL)
	}
	if !strings.Contains(spec.Note, spec.LinkURL) {
		t.Errorf("note should hand over the address, got: %s", spec.Note)
	}
	// The publishers are a long tail with their own credentials; promising the
	// account key works there would send a caller down a dead end.
	if !strings.Contains(spec.Note, "인증키") {
		t.Errorf("note should warn the account key does not work there, got: %s", spec.Note)
	}
}

// A REST page has no URL row, and must not acquire an empty linkUrl.
func TestLinkURLAbsentOnRestPage(t *testing.T) {
	spec := describeFromHTML(t, `<table><tr><th>API 유형</th><td>REST</td></tr></table>`)
	if spec.LinkURL != "" {
		t.Errorf("linkUrl = %q, want empty", spec.LinkURL)
	}
}
