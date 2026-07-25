package apicall

import (
	"context"
	"github.com/JungHoonGhae/gongctl/internal/fetch"
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

	spec, err := Describe(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL, "15000908")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	// Fixture is a real 2-operation dataset (예비후보자 + 후보자), each with its
	// own apis.data.go.kr endpoint and a 7-row 요청변수 table. Both must survive
	// — the operation-switcher box (<select>+button, no data) must NOT show up
	// as a bogus third operation, and neither real operation may vanish.
	if len(spec.Operations) != 2 {
		t.Fatalf("want 2 real operations, got %d: %+v", len(spec.Operations), spec.Operations)
	}
	for i, op := range spec.Operations {
		if !strings.Contains(op.Endpoint, "apis.data.go.kr") {
			t.Errorf("op %d: expected apis.data.go.kr endpoint, got %q", i, op.Endpoint)
		}
		var hasNumOfRows bool
		for _, p := range op.Params {
			if p.Name == "numOfRows" {
				hasNumOfRows = true
			}
			if p.Name == "resultCode" || p.Name == "resultMsg" {
				t.Errorf("op %d: response-only field %q misattributed as a request param", i, p.Name)
			}
		}
		if !hasNumOfRows {
			t.Errorf("op %d: expected numOfRows request param surfaced, got %+v", i, op.Params)
		}
	}
}

// A malformed page must not invent params — surface RawHTML instead of fabricating.
func TestDescribeSurfaceFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div class="open-api-detail">
			<h4>테스트기능</h4><p>표 구조가 없는 안내문</p></div></body></html>`))
	}))
	defer srv.Close()
	spec, err := Describe(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL, "1")
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

// Some OpenAPI pages document nothing inline — no endpoint, no request-variable
// table — because the whole spec ships in an attached guide document. Returning
// an empty Operations list there is a dead end for an agent, so Describe must
// point at the guide it can actually fetch.
func TestDescribeGuideOnlyPage(t *testing.T) {
	body, err := os.ReadFile("testdata/openapi-guide-only.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write(body)
	}))
	defer srv.Close()

	spec, err := Describe(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL, "15012005")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(spec.Operations) != 0 {
		t.Errorf("must not invent operations, got %d", len(spec.Operations))
	}
	if spec.GuideDoc == "" {
		t.Error("guideDoc name missing")
	}
	// The name alone is not actionable — the agent needs a URL it can fetch.
	if !strings.Contains(spec.GuideDocURL, "FILE_000000003547578") {
		t.Errorf("guideDocUrl = %q, want a download URL carrying the atchFileId", spec.GuideDocURL)
	}
	if spec.Note == "" {
		t.Error("expected a note telling the agent where the spec actually lives")
	}
}

// A page that DOES document its operations must not gain a note.
func TestDescribeNoNoteWhenOperationsFound(t *testing.T) {
	body, err := os.ReadFile("testdata/op-15000908.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write(body)
	}))
	defer srv.Close()

	spec, err := Describe(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL, "15000908")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if spec.Note != "" {
		t.Errorf("note should be empty when operations exist, got %q", spec.Note)
	}
}

// The portal embeds the authoritative spec as Swagger 2.0 in a JS template
// literal — endpoint, per-parameter name/required/description, the lot. Scraping
// the HTML tables instead misses it entirely, which is how a fully documented API
// came back looking undocumented.
func TestDescribeReadsEmbeddedSwagger(t *testing.T) {
	body, err := os.ReadFile("testdata/openapi-swagger.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write(body)
	}))
	defer srv.Close()

	spec, err := Describe(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL, "15127057")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(spec.Operations) != 1 {
		t.Fatalf("want 1 operation from swagger, got %d", len(spec.Operations))
	}
	op := spec.Operations[0]
	if !strings.Contains(op.Endpoint, "apis.data.go.kr/1130000/ClslVioltDtl_2Service/getClslVioltDetailInfo_2") {
		t.Errorf("endpoint = %q, want host+path assembled from swagger", op.Endpoint)
	}
	if op.Name == "" {
		t.Error("operation name (swagger summary) missing")
	}
	if len(op.Params) < 8 {
		t.Fatalf("want the 8 documented params, got %d", len(op.Params))
	}
	var svcKey, bzmn *Param
	for i := range op.Params {
		switch op.Params[i].Name {
		case "serviceKey":
			svcKey = &op.Params[i]
		case "bzmnNm":
			bzmn = &op.Params[i]
		}
	}
	if svcKey == nil || bzmn == nil {
		t.Fatal("expected serviceKey and bzmnNm among params")
	}
	if svcKey.Required != "필수" {
		t.Errorf("serviceKey required = %q, want 필수", svcKey.Required)
	}
	if bzmn.Required != "옵션" {
		t.Errorf("bzmnNm required = %q, want 옵션", bzmn.Required)
	}
	if bzmn.Desc == "" {
		t.Error("param description not carried over")
	}
	// A spec this complete must not be flagged as endpoint-only or noted as missing.
	if spec.EndpointOnly || spec.Note != "" {
		t.Errorf("complete swagger spec should carry no note; got endpointOnly=%v note=%q",
			spec.EndpointOnly, spec.Note)
	}
}
