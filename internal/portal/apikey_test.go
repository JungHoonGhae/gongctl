package portal

import (
	"os"
	"strings"
	"testing"
)

// The page lists every key ever issued; only the hidden input marks the active
// one. Parsing must take that, not the first key it finds in the table.
func TestParseAPIKey(t *testing.T) {
	body, err := os.ReadFile("testdata/apikeylist.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	key, err := parseAPIKey(string(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.HasSuffix(key, "active") {
		t.Errorf("key = %q, want the active (hidden-input) key, not a stale table row", key)
	}
}

// A page without the key (no approved application yet, or markup drift) must
// error rather than hand back an empty key that would fail confusingly later.
func TestParseAPIKeyMissing(t *testing.T) {
	if _, err := parseAPIKey(`<html><body><p>인증키 없음</p></body></html>`); err == nil {
		t.Error("expected an error when no key is present")
	}
}
