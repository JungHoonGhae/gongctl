package portal

import (
	"os"
	"testing"
)

func TestParseApplications(t *testing.T) {
	body, err := os.ReadFile("testdata/accounts.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	apps, err := parseApplications(string(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2", len(apps))
	}

	a := apps[0]
	if a.Status != "승인" {
		t.Errorf("status = %q, want 승인", a.Status)
	}
	if a.Title != "중앙선거관리위원회_당선인 정보" {
		t.Errorf("title = %q (상태접두사·공백 정리 실패)", a.Title)
	}
	if a.Org != "중앙선거관리위원회" {
		t.Errorf("org = %q", a.Org)
	}
	if a.Category != "공공행정" {
		t.Errorf("category = %q", a.Category)
	}
	if a.Account != "개발" {
		t.Errorf("account = %q", a.Account)
	}
	if a.AppliedAt != "2026-06-22" {
		t.Errorf("appliedAt = %q", a.AppliedAt)
	}
	if a.ExpiresAt != "2028-06-22" {
		t.Errorf("expiresAt = %q, want 2028-06-22", a.ExpiresAt)
	}
	if a.DetailPk != "119222395" {
		t.Errorf("detailPk = %q", a.DetailPk)
	}
	if a.UDDI != "uddi:aaaa-1111" {
		t.Errorf("uddi = %q", a.UDDI)
	}

	if apps[1].Title != "중앙선거관리위원회_투·개표 정보" {
		t.Errorf("apps[1].Title = %q", apps[1].Title)
	}
}

// The list is paginated at 10; the "총 N건" header is what tells Applications
// there are more pages. If this stops parsing, applications past the tenth
// vanish silently and an agent's "did I already apply?" check goes wrong.
func TestParseTotalCount(t *testing.T) {
	if got := parseTotalCount(`<p>총 <span class="num">16</span>건</p>`); got != 16 {
		t.Errorf("parseTotalCount = %d, want 16", got)
	}
	if got := parseTotalCount(`<p>총 <b>1,234</b>건</p>`); got != 1234 {
		t.Errorf("parseTotalCount with comma = %d, want 1234", got)
	}
	// No header → 0, so the caller reads a single page instead of looping.
	if got := parseTotalCount(`<p>목록이 없습니다</p>`); got != 0 {
		t.Errorf("parseTotalCount without header = %d, want 0", got)
	}
}
