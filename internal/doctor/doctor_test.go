package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
)

// fixtureServer serves the real captured search + openapi.do markup (reused from
// the sibling packages' testdata) so the drift checks pass against ground truth.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	search, err := os.ReadFile("../portal/testdata/search-list.html")
	if err != nil {
		t.Fatalf("read search fixture: %v", err)
	}
	openapi, err := os.ReadFile("../apicall/testdata/op-15000908.html")
	if err != nil {
		t.Fatalf("read openapi fixture: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		switch r.URL.Path {
		case "/tcs/dss/selectDataSetList.do":
			w.Write(search)
		case "/data/" + CanaryPK + "/openapi.do":
			w.Write(openapi)
		default:
			http.NotFound(w, r)
		}
	}))
}

func statusOf(checks []Check, name string) Status {
	for _, c := range checks {
		if c.Name == name {
			return c.Status
		}
	}
	return ""
}

// Against live-shaped markup, both scraping checks report ok.
func TestRunHealthy(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	checks := Run(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL)
	if got := statusOf(checks, "search"); got != StatusOK {
		t.Errorf("search check = %q, want ok", got)
	}
	if got := statusOf(checks, "describe"); got != StatusOK {
		t.Errorf("describe check = %q, want ok", got)
	}
}

// When the portal returns markup our parsers no longer understand, the checks
// report drift loudly instead of silently passing.
func TestRunDetectsDrift(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>redesigned, nothing our selectors match</body></html>`))
	}))
	defer srv.Close()

	checks := Run(context.Background(), fetch.New(fetch.WithDelay(0)), srv.URL)
	if got := statusOf(checks, "search"); got != StatusDrift {
		t.Errorf("search check = %q, want drift", got)
	}
	if got := statusOf(checks, "describe"); got != StatusDrift {
		t.Errorf("describe check = %q, want drift", got)
	}
}
