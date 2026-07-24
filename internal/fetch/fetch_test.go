package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Get surfaces status/content-type/body and does NOT treat non-200 as an error
// (data.go.kr returns 200 — and sometimes non-200 — with a meaningful body the
// caller must see).
func TestGetSurfacesNon200Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("<err>nope</err>"))
	}))
	defer srv.Close()

	res, err := New(WithDelay(0)).Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Get errored on non-200: %v", err)
	}
	if res.Status != 500 {
		t.Errorf("status = %d, want 500", res.Status)
	}
	if res.ContentType != "application/xml" {
		t.Errorf("contentType = %q", res.ContentType)
	}
	if string(res.Body) != "<err>nope</err>" {
		t.Errorf("body = %q", res.Body)
	}
}

// Get stamps the configured User-Agent.
func TestGetSetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	New(WithDelay(0), WithUserAgent("test-agent/1.0")).Get(context.Background(), srv.URL)
	if gotUA != "test-agent/1.0" {
		t.Errorf("User-Agent = %q, want test-agent/1.0", gotUA)
	}
}

// Two Gets on one client are spaced by at least the throttle delay.
func TestThrottleSpacesRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	c := New(WithDelay(200 * time.Millisecond))
	start := time.Now()
	c.Get(context.Background(), srv.URL)
	c.Get(context.Background(), srv.URL)
	elapsed := time.Since(start)
	if elapsed < 180*time.Millisecond {
		t.Errorf("two throttled Gets took %v, want >= ~200ms spacing", elapsed)
	}
}

// GetDoc returns a parseable document on 200 and errors on non-200 (parser
// pages are only useful when served 200 with HTML).
func TestGetDoc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1 class="t">hi</h1></body></html>`))
	}))
	defer srv.Close()

	c := New(WithDelay(0))
	doc, err := c.GetDoc(context.Background(), srv.URL+"/ok")
	if err != nil {
		t.Fatalf("GetDoc 200: %v", err)
	}
	if got := doc.Find("h1.t").Text(); got != "hi" {
		t.Errorf("parsed text = %q, want hi", got)
	}
	if _, err := c.GetDoc(context.Background(), srv.URL+"/bad"); err == nil {
		t.Error("GetDoc on 404 should error")
	}
}
