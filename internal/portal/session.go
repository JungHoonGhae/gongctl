package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Session is the authenticated data.go.kr session, extracted from the browser
// once at login so later commands can talk to the portal over plain HTTP.
//
// The portal's auth cookies are session-scoped — Chrome discards them when it
// exits — but the values stay valid server-side until the session expires. So
// gongctl copies them out after login and closes the browser, instead of keeping
// a window open for the rest of the day. Reads (활용신청 현황) then need no
// browser at all; the one flow that still does (활용신청 submit, which drives the
// portal's own form JS) relaunches Chrome headless and injects these cookies, so
// nothing ever appears on screen after login.
type Session struct {
	Cookies     map[string]string `json:"cookies"`
	RetrievedAt time.Time         `json:"retrievedAt"`
}

func sessionPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "datagokr-session.json"), nil
}

// saveSession writes the session 0600 — these cookies are credentials.
func saveSession(s *Session) error {
	path, err := sessionPath()
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

// loadSession reads the saved session, or returns ErrNotLoggedIn if none exists.
func loadSession() (*Session, error) {
	path, err := sessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if len(s.Cookies) == 0 {
		return nil, ErrNotLoggedIn
	}
	return &s, nil
}

// clearSession removes the stored cookies and the cached serviceKey (logout).
// Both are credentials, so logout must take both.
func clearSession() error {
	path, err := sessionPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if dir, derr := configDir(); derr == nil {
		if err := os.Remove(filepath.Join(dir, keyCacheFile)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// header renders the cookies as a Cookie request header.
func (s *Session) header() string {
	pairs := make([]string, 0, len(s.Cookies))
	for k, v := range s.Cookies {
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, "; ")
}

// extractCookies copies data.go.kr's cookies out of the live browser tab.
func extractCookies(tctx context.Context) (*Session, error) {
	var jar []*network.Cookie
	err := chromedp.Run(tctx, chromedp.ActionFunc(func(c context.Context) error {
		var e error
		jar, e = network.GetCookies().WithURLs([]string{BaseURL}).Do(c)
		return e
	}))
	if err != nil {
		return nil, err
	}
	s := &Session{Cookies: map[string]string{}, RetrievedAt: time.Now().UTC()}
	for _, c := range jar {
		s.Cookies[c.Name] = c.Value
	}
	if len(s.Cookies) == 0 {
		return nil, fmt.Errorf("브라우저에서 쿠키를 얻지 못했습니다")
	}
	return s, nil
}

// getWithSession fetches a portal path over plain HTTP using the session cookies.
func getWithSession(ctx context.Context, s *Session, path string) (html string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", s.header())
	req.Header.Set("User-Agent", fetch.DefaultUserAgent)
	req.Header.Set("Accept", "text/html,*/*")
	req.Header.Set("Referer", BaseURL+"/")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	return string(body), nil
}

// getAuthed fetches a path with the session and retries once if the portal serves
// the login wall. Observed on a live, valid session: data.go.kr intermittently
// answers an authenticated request with the login page (HTTP 200), and treating
// that as "logged out" makes callers demand a needless re-login.
func getAuthed(ctx context.Context, s *Session, path string) (string, error) {
	html, err := getWithSession(ctx, s, path)
	if err != nil {
		return "", err
	}
	if !isLoginWall(html, "") {
		return html, nil
	}
	time.Sleep(800 * time.Millisecond)
	html, err = getWithSession(ctx, s, path)
	if err != nil {
		return "", err
	}
	if isLoginWall(html, "") {
		return "", ErrNotLoggedIn
	}
	return html, nil
}

// sessionWorks reports whether the extracted cookies actually authenticate over
// plain HTTP. Login checks this before closing the browser: if the portal needs
// something the cookies alone don't carry, gongctl keeps the browser alive and
// falls back to driving it over CDP.
func sessionWorks(ctx context.Context, s *Session) bool {
	html, err := getWithSession(ctx, s, AccountListPath)
	return err == nil && isAuthed(html, "")
}

// browserForApply returns a browser to drive the 활용신청 form with. If a session
// browser is already running it is reused (sess == nil, nothing to inject).
// Otherwise a headless Chrome is started and the saved session is returned for
// the caller to inject — that instance belongs to the caller, which closes it.
func browserForApply(ctx context.Context) (*daemonState, *Session, error) {
	if st, err := loadState(); err == nil && wsAlive(st.Port) {
		return st, nil, nil
	}
	sess, err := loadSession()
	if err != nil {
		return nil, nil, ErrNotLoggedIn
	}
	if _, err := launchHeadless(); err != nil {
		return nil, nil, err
	}
	ws, err := discoverWS(ctx, headlessPort, 25*time.Second)
	if err != nil {
		return nil, nil, err
	}
	return &daemonState{WebSocketURL: ws, Port: headlessPort}, sess, nil
}

// injectSession sets the saved cookies on a fresh browser so it is authenticated
// without a re-login, then parks it on the portal root to settle the session.
func injectSession(tctx context.Context, s *Session) error {
	actions := []chromedp.Action{network.Enable()}
	for name, value := range s.Cookies {
		actions = append(actions, network.SetCookie(name, value).
			WithDomain("www.data.go.kr").WithPath("/"))
	}
	actions = append(actions, chromedp.Navigate(BaseURL+"/"))
	if err := chromedp.Run(tctx, actions...); err != nil {
		return err
	}
	time.Sleep(1500 * time.Millisecond) // let the portal settle the injected session
	return nil
}
