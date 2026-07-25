package portal

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
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
func Login(ctx context.Context, progress io.Writer, keepBrowser bool) error {
	logln := func(format string, a ...any) {
		if progress != nil {
			fmt.Fprintf(progress, format+"\n", a...)
		}
	}

	st, _ := loadState()
	if st == nil || !wsAlive(st.Port) {
		logln("브라우저를 띄웁니다… 열리는 창에서 data.go.kr 에 로그인하세요 (네이버/카카오/아이디).")
		cmd, err := launchBrowser(BaseURL + "/sso/login.do")
		if err != nil {
			return err
		}
		ws, err := discoverWS(ctx, debugPort, 20*time.Second)
		if err != nil {
			return err
		}
		// Record the PID: closing the window is not enough to end the process
		// (the debug port keeps Chrome alive with zero tabs), so closeBrowser
		// needs something to signal.
		st = &daemonState{WebSocketURL: ws, Port: debugPort, PID: cmd.Process.Pid}
		if err := saveState(st); err != nil {
			return err
		}
	} else {
		logln("기존 브라우저에 연결합니다. 로그인이 안 돼 있으면 열린 창에서 로그인하세요.")
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, st.WebSocketURL)
	defer cancelAlloc()

	logln("로그인 완료를 기다리는 중… (최대 %d분)", int(LoginTimeout.Minutes()))
	deadline := time.Now().Add(LoginTimeout)
	tick := 0
	for time.Now().Before(deadline) {
		// A fresh tab per poll: each probe is bounded and disposable, so a page
		// that never finishes loading costs one iteration instead of wedging the
		// whole wait (a reused tab cannot carry a timeout — cancelling it closes
		// the tab, see probeIn).
		html, loc, err, tctx, cancelTab := probeOnce(allocCtx)
		if err == nil && isAuthed(html, loc) {
			logln("✅ 로그인 확인.")
			if err := saveState(st); err != nil {
				return err
			}
			// Copy the session out of the browser so later commands don't need a
			// window open. Verified before we close anything — if the cookies
			// don't authenticate on their own, keep the browser as the fallback.
			if keepBrowser {
				logln("   (--keep-browser: 브라우저를 열어 둡니다.)")
				return nil
			}
			sess, cerr := extractCookies(tctx)
			if cerr == nil && sessionWorks(ctx, sess) {
				if err := saveSession(sess); err != nil {
					return err
				}
				cancelTab()
				closeBrowser(ctx, st)
				logln("   세션을 저장하고 브라우저를 닫았습니다 — 이후 명령은 창 없이 동작합니다.")
				return nil
			}
			logln("   (쿠키만으로는 인증이 유지되지 않아 브라우저를 열어 둡니다.)")
			return nil
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

// closeBrowser shuts the session browser down. Best-effort: the point is to get
// the window off the user's screen once its cookies have been copied out.
//
// It sends the CDP Browser.close command rather than cancelling the chromedp
// context: with a RemoteAllocator gongctl only ATTACHED to this browser, so
// cancelling merely detaches and leaves the window on screen (the bug the old
// Logout shipped).
func closeBrowser(ctx context.Context, st *daemonState) {
	if !wsAlive(st.Port) {
		return
	}
	// Ask Chrome to close itself first so it flushes its profile. This needs a
	// tab to attach to; with zero tabs it fails harmlessly and the kill below
	// does the work.
	func() {
		allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, st.WebSocketURL)
		defer cancel()
		tctx, tcancel := chromedp.NewContext(allocCtx)
		defer tcancel()
		chromedp.Run(tctx, chromedp.ActionFunc(func(c context.Context) error {
			return browser.Close().Do(c)
		}))
	}()
	for i := 0; i < 20 && wsAlive(st.Port); i++ {
		time.Sleep(150 * time.Millisecond)
	}
	// Chrome lingers with the debug port open even after its last window closes,
	// so signal the process tree when it is still answering.
	if wsAlive(st.Port) && st.PID > 0 {
		killTree(st.PID)
		for i := 0; i < 20 && wsAlive(st.Port); i++ {
			time.Sleep(150 * time.Millisecond)
		}
	}
}

// Applications reads the 활용신청 현황 list. It prefers the saved session over
// plain HTTP (no browser needed) and falls back to driving a live browser over
// CDP when only that is available.
func Applications(ctx context.Context) ([]Application, error) {
	if sess, serr := loadSession(); serr == nil {
		if html, herr := getWithSession(ctx, sess, AccountListPath); herr == nil && isAuthed(html, "") {
			return parseApplications(html)
		}
		// Session expired or insufficient — fall through to the browser, if any.
	}

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

// Logout closes any live browser and clears the saved state and session cookies.
func Logout(ctx context.Context) error {
	if st, err := loadState(); err == nil {
		closeBrowser(ctx, st)
	}
	if err := clearSession(); err != nil {
		return err
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

// probeOnce opens a disposable tab, probes the 활용신청 현황 page with a hard
// bound, and hands the tab back so a successful caller can still read cookies
// from it. The caller MUST call cancelTab.
func probeOnce(allocCtx context.Context) (html, loc string, err error, tctx context.Context, cancelTab context.CancelFunc) {
	tabCtx, cancel := chromedp.NewContext(allocCtx)
	bounded, cancelBound := context.WithTimeout(tabCtx, 20*time.Second)
	cancelTab = func() { cancelBound(); cancel() }
	html, loc, err = probeIn(bounded, AccountListPath)
	return html, loc, err, bounded, cancelTab
}

// probeViaBrowser fetches a portal path through the live browser. Used as the
// fallback when the saved session cookies are not enough (see Applications,
// APIKey), so both share one CDP path instead of duplicating tab setup.
func probeViaBrowser(ctx context.Context, st *daemonState, path string) (string, error) {
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, st.WebSocketURL)
	defer cancelAlloc()
	tctx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()
	tctx, tcancel := context.WithTimeout(tctx, 40*time.Second)
	defer tcancel()

	html, loc, err := probeIn(tctx, path)
	if err != nil {
		return "", err
	}
	if strings.Contains(loc, "common-login") || strings.Contains(loc, "auth.data.go.kr") {
		return "", ErrNotLoggedIn
	}
	return html, nil
}
