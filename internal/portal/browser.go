package portal

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// BaseURL is the portal root. It is a var, not a const, so tests can point the
// CDP probe at a local fixture server (see probe_live_test.go).
var BaseURL = "https://www.data.go.kr"

// LoginTimeout bounds the interactive login wait.
const LoginTimeout = 5 * time.Minute

// Login ensures a live, authenticated browser session exists. It launches (or
// reuses) gongctl's detached Chrome, opens the login page, and waits until the
// session can actually load an authenticated page. The browser is left running
// so later commands re-attach to it. progress receives status lines (may be nil).
func Login(ctx context.Context, progress io.Writer) error {
	logln := func(format string, a ...any) {
		if progress != nil {
			fmt.Fprintf(progress, format+"\n", a...)
		}
	}

	st, _ := loadState()
	if st == nil || !wsAlive(st.Port) {
		logln("브라우저를 띄웁니다… 열리는 창에서 data.go.kr 에 로그인하세요 (네이버/카카오/아이디).")
		if _, err := launchBrowser(BaseURL + "/sso/login.do"); err != nil {
			return err
		}
		ws, err := discoverWS(ctx, debugPort, 20*time.Second)
		if err != nil {
			return err
		}
		st = &daemonState{WebSocketURL: ws, Port: debugPort}
		if err := saveState(st); err != nil {
			return err
		}
	} else {
		logln("기존 브라우저에 연결합니다. 로그인이 안 돼 있으면 열린 창에서 로그인하세요.")
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, st.WebSocketURL)
	defer cancelAlloc()
	// One reused background tab for probing — avoids flicker while the user logs
	// in on their own tab.
	tctx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()

	logln("로그인 완료를 기다리는 중… (최대 %d분)", int(LoginTimeout.Minutes()))
	deadline := time.Now().Add(LoginTimeout)
	tick := 0
	for time.Now().Before(deadline) {
		html, loc, err := probeIn(tctx, AccountListPath)
		if err == nil && isAuthed(html, loc) {
			logln("✅ 로그인 확인 — 세션이 살아있습니다.")
			return saveState(st)
		}
		tick++
		if tick%5 == 0 {
			logln("[대기] 아직 로그인 전입니다… 열린 창에서 로그인해주세요.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("로그인 시간 초과 (%d분) — 열린 창에서 로그인 후 다시 시도하세요", int(LoginTimeout.Minutes()))
}

// Applications re-attaches to the live browser and reads the 활용신청 현황 list.
func Applications(ctx context.Context) ([]Application, error) {
	st, err := loadState()
	if err != nil {
		return nil, err
	}
	if !wsAlive(st.Port) {
		return nil, ErrNotLoggedIn
	}
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, st.WebSocketURL)
	defer cancelAlloc()
	tctx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()
	// Hard bound on the whole probe. Safe here (unlike inside probeIn) because
	// this tab is single-use: the timeout closing it is exactly what we want.
	tctx, tcancel := context.WithTimeout(tctx, 40*time.Second)
	defer tcancel()

	html, loc, err := probeIn(tctx, AccountListPath)
	if err != nil {
		return nil, err
	}
	if !isAuthed(html, loc) {
		return nil, ErrNotLoggedIn
	}
	return parseApplications(html)
}

// Logout closes the live browser and clears saved state.
func Logout(ctx context.Context) error {
	st, err := loadState()
	if err != nil {
		return nil // nothing to do
	}
	if wsAlive(st.Port) {
		allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, st.WebSocketURL)
		defer cancel()
		tctx, tcancel := chromedp.NewContext(allocCtx)
		defer tcancel()
		// best-effort browser close
		chromedp.Run(tctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Cancel(tctx)
		}))
	}
	path, _ := statePath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// probeIn navigates the given (reused) tab to path, lets the SSO trampoline
// settle, and returns the final HTML + URL.
//
// It uses tctx DIRECTLY and never derives a cancellable child from it: with
// chromedp, cancelling (or expiring) a context derived from a tab context closes
// the tab, so a per-probe timeout would kill the very tab the caller wants to
// reuse. Login polls this in a loop — a child cancel here left every probe after
// the first failing with "context canceled", so a successful login was never
// detected. Callers that need a hard bound put the timeout on the tab context at
// creation time instead (see Applications, Apply).
func probeIn(tctx context.Context, path string) (html, loc string, err error) {
	if err = chromedp.Run(tctx, chromedp.Navigate(BaseURL+path)); err != nil {
		return "", "", err
	}
	// Settle: the portal may bounce through /sso/profile.do (an auto-submitting
	// form). Wait until the URL is no longer that trampoline, then read.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if e := chromedp.Run(tctx, chromedp.Location(&loc)); e != nil {
			return "", "", e
		}
		if !strings.Contains(loc, "/sso/profile.do") {
			if e := chromedp.Run(tctx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); e == nil {
				return html, loc, nil
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	return html, loc, nil
}

// isAuthed reports whether a probed page is the authenticated 활용신청 현황 list
// rather than the login wall.
func isAuthed(html, loc string) bool {
	if strings.Contains(loc, "common-login") || strings.Contains(loc, "auth.data.go.kr") {
		return false
	}
	if strings.Contains(html, "통합 로그인") || strings.Contains(html, "로그인 중 입니다") {
		return false
	}
	return strings.Contains(html, "mypage-dataset-list") || strings.Contains(html, "활용신청 현황")
}
