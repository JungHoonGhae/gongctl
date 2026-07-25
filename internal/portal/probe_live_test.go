package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestProbeInReusesTab is the regression test for the login-never-detected bug.
//
// probeIn used to derive a cancellable timeout context from the tab context and
// cancel it on return. With chromedp that closes the tab, so the FIRST probe
// worked and every probe after it failed with "context canceled". Login polls
// probeIn on one reused tab, so a login that completed after the first probe was
// never detected — the command sat for 5 minutes and exited 1 while the session
// was actually live.
//
// The test drives probeIn repeatedly on a single tab, exactly as Login does.
// Pre-fix it fails on probe #2; post-fix all probes succeed.
//
// It needs a real Chrome (headless is fine) and is skipped when none is found or
// when -short is set.
func TestProbeInReusesTab(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a real browser; skipped with -short")
	}
	chrome, err := findChrome()
	if err != nil {
		t.Skipf("no Chrome-family browser available: %v", err)
	}

	// Stand in for data.go.kr's 활용신청 현황 page: served on any path, carrying
	// the marker isAuthed looks for.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Write([]byte(`<html><body><div class="mypage-dataset-list"><ul><li>ok</li></ul></div></body></html>`))
	}))
	defer srv.Close()

	// probeIn builds its URL from BaseURL; point it at the fixture server.
	orig := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = orig }()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx,
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(chrome),
			chromedp.Flag("headless", true),
			chromedp.Flag("no-first-run", true),
			chromedp.Flag("no-default-browser-check", true),
		)...)
	defer cancelAlloc()

	// ONE reused tab, exactly like Login's polling loop.
	tctx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()

	for i := 1; i <= 3; i++ {
		html, loc, err := probeIn(tctx, "/iim/api/selectAcountList.do")
		if err != nil {
			t.Fatalf("probe #%d errored (tab did not survive the previous probe): %v", i, err)
		}
		if !isAuthed(html, loc) {
			t.Fatalf("probe #%d: isAuthed false (loc=%q, len(html)=%d)", i, loc, len(html))
		}
	}
}
