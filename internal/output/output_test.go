package output

import (
	"strings"
	"testing"
)

func TestWriteTableCJKAlign(t *testing.T) {
	var b strings.Builder
	if err := WriteTable(&b, []string{"상태", "데이터명"}, [][]string{
		{"승인", "당선인 정보"},
		{"신청", "투표율"},
	}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "승인") || !strings.Contains(out, "당선인 정보") {
		t.Fatalf("table missing content:\n%s", out)
	}
	// header separator row of dashes must be present
	if !strings.Contains(out, "----") {
		t.Fatalf("missing separator:\n%s", out)
	}
}

func TestParseRejectsUnknown(t *testing.T) {
	if _, err := Parse("yaml"); err == nil {
		t.Fatal("expected error for unknown format")
	}
}
