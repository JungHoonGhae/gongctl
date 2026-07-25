package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Browser lifecycle. The human logs in once in a visible Chrome (government SSO
// is not automated); gongctl then copies the session cookies out (session.go) and
// closes that window, so nothing stays on screen. Reads go over plain HTTP with
// those cookies, and the one flow that still needs a browser — driving the
// 활용신청 form's own JS — starts a short-lived headless Chrome and injects them.
// A visible browser is only kept alive as a fallback, when the cookies alone turn
// out not to authenticate.

// debugPort is the fixed CDP remote-debugging port for gongctl's login browser.
const debugPort = 9333

// headlessPort is a separate port for the short-lived headless browser that
// drives 활용신청 submission, so it never collides with a login browser.
const headlessPort = 9334

// daemonState records the running browser so later commands can re-attach.
type daemonState struct {
	WebSocketURL string `json:"webSocketDebuggerUrl"`
	PID          int    `json:"pid"`
	Port         int    `json:"port"`
}

// ConfigDir is gongctl's config directory, exported so sibling packages (the
// catalogue) can store their own state beside the session files.
func ConfigDir() (string, error) { return configDir() }

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "gongctl")
	return p, os.MkdirAll(p, 0o700)
}

func statePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "datagokr-cdp.json"), nil
}

func profileDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "chrome-profile")
	return p, os.MkdirAll(p, 0o700)
}

func saveState(s *daemonState) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

func loadState() (*daemonState, error) {
	path, err := statePath()
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
	var s daemonState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ErrNotLoggedIn means no live browser session is available; run `gongctl login`.
var ErrNotLoggedIn = fmt.Errorf("data.go.kr 세션이 없습니다 — 먼저 `gongctl login` 을 실행하세요")

// findChrome locates a Chrome-family browser executable.
func findChrome() (string, error) {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}
	default: // linux
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave-browser", "microsoft-edge"} {
			if p, err := exec.LookPath(name); err == nil {
				return p, nil
			}
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("Chrome 계열 브라우저를 찾지 못했습니다 (Chrome/Chromium/Brave/Edge 설치 필요)")
}

// launchBrowser starts a detached Chrome with the remote-debugging port and
// gongctl's persistent profile. The process outlives gongctl (Setpgid + Release) so
// the session stays alive between commands. startURL is the initial page.
func launchBrowser(startURL string) (*exec.Cmd, error) {
	chrome, err := findChrome()
	if err != nil {
		return nil, err
	}
	profile, err := profileDir()
	if err != nil {
		return nil, err
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", debugPort),
		"--user-data-dir=" + profile,
		"--password-store=basic", // avoid OS keychain prompt
		"--use-mock-keychain",
		"--no-first-run",
		"--no-default-browser-check",
		// NOTE: no --remote-allow-origins=*. chromedp attaches without an Origin
		// header, which Chrome allows by default, so the flag is unnecessary —
		// and setting it to * would let any local web page open the CDP
		// WebSocket and hijack the authenticated session (spike-verified:
		// omitting the flag makes Chrome reject a foreign-Origin WS upgrade with
		// HTTP 403, while * accepts it with 101). See branch proto/cdp-origin.
		startURL,
	}
	cmd := exec.Command(chrome, args...)
	setDetached(cmd) // OS별로 gongctl 프로세스 그룹에서 분리 (browser 가 gongctl 종료 후에도 생존)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("브라우저 실행 실패: %w", err)
	}
	return cmd, nil
}

// launchHeadless starts a throwaway headless Chrome for driving the 활용신청 form.
// It uses its own profile (no login state — the caller injects the session
// cookies) and is not detached: it dies with gongctl if the caller forgets to
// close it, since nothing should outlive a single submission.
func launchHeadless() (*exec.Cmd, error) {
	chrome, err := findChrome()
	if err != nil {
		return nil, err
	}
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	profile := filepath.Join(dir, "chrome-headless")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.Command(chrome,
		fmt.Sprintf("--remote-debugging-port=%d", headlessPort),
		"--user-data-dir="+profile,
		"--headless=new",
		"--password-store=basic",
		"--use-mock-keychain",
		"--no-first-run",
		"--no-default-browser-check",
		"about:blank",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("headless 브라우저 실행 실패: %w", err)
	}
	return cmd, nil
}

// discoverWS polls the CDP HTTP endpoint until the browser's WebSocket debugger
// URL is available.
func discoverWS(ctx context.Context, port int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var v struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			if v.WebSocketDebuggerURL != "" {
				return v.WebSocketDebuggerURL, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("브라우저 디버그 포트(%d) 연결 시간 초과", port)
}

// wsAlive reports whether the recorded debugger endpoint still answers.
func wsAlive(port int) bool {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
