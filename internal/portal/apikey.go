package portal

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// APIKeyListPath is the 인증키 발급현황 page, which carries the account's active
// serviceKey.
const APIKeyListPath = "/iim/api/selectApiKeyList.do"

// APIKey returns the account's serviceKey (일반 인증키). data.go.kr issues one key
// per account on the first approved application and reuses it for every API, so
// this is all a caller needs to start calling.
//
// This is what closes the loop for an agent: it can search, apply, see the
// approval, and then fetch the key itself instead of asking a human to copy it
// out of the portal.
func APIKey(ctx context.Context) (string, error) {
	// Why the session path failed matters — a stale cookie and a changed page
	// need different fixes — so it is reported rather than swallowed when the
	// browser fallback is also unavailable.
	var sessErr error
	if sess, err := loadSession(); err == nil {
		html, herr := getWithSession(ctx, sess, APIKeyListPath)
		if herr != nil {
			sessErr = fmt.Errorf("세션으로 인증키 페이지를 받지 못했습니다: %w", herr)
		} else if key, kerr := parseAPIKey(html); kerr != nil {
			sessErr = kerr
		} else {
			return key, nil
		}
	} else {
		sessErr = err
	}

	st, err := loadState()
	if err != nil || !wsAlive(st.Port) {
		return "", sessErr
	}
	html, err := probeViaBrowser(ctx, st, APIKeyListPath)
	if err != nil {
		return "", err
	}
	return parseAPIKey(html)
}

// parseAPIKey pulls the active key out of the 인증키 발급현황 page.
//
// The page carries it twice: a hidden input holding the current key, and a table
// listing every key ever issued (신규발급 plus any 재발급). Only the hidden input
// identifies which one is *active* — a reissued key invalidates the original, and
// the table does not say which row won — so that is the single source read here.
// The value is the plain (Decoding) form; apicall.Call escapes it as needed.
func parseAPIKey(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}
	key, _ := doc.Find("#pblisrCrtfcKeyPlain").First().Attr("value")
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("인증키를 찾지 못했습니다 — 활용신청이 승인된 뒤 발급됩니다 (또는 페이지 구조 변경: gongctl doctor 로 확인)")
	}
	return key, nil
}
