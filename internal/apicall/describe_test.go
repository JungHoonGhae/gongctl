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
