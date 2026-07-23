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
