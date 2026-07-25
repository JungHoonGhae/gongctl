package catalog

import (
	"strings"
	"testing"
	"time"
)

func sample() *Catalog {
	return &Catalog{
		SyncedAt: time.Now(),
		Type:     "API",
		Entries: []Entry{
			{PK: "1", Title: "행정안전부_무더위쉼터", Org: "행정안전부", ApplyCount: 2796,
				Desc: "폭염 대비 무더위쉼터 위치 정보"},
			{PK: "2", Title: "기상청_단기예보 조회서비스", Org: "기상청", ApplyCount: 63350,
				Desc: "동네예보 기온 강수 정보"},
			{PK: "3", Title: "행정안전부_폭염 인명피해", Org: "행정안전부", ApplyCount: 508,
				Desc: "온열질환자 지역별 현황"},
		},
	}
}

// Every term must match, and matches rank by demand — the agent should see the
// heavily used dataset first rather than whatever happened to be stored first.
func TestSearchRanksByDemand(t *testing.T) {
	c := sample()
	hits, total := c.Search("기온", 10)
	if total != 1 || len(hits) != 1 || hits[0].PK != "2" {
		t.Fatalf("기온 → %d hits (total %d), first %+v", len(hits), total, hits)
	}

	hits, total = c.Search("행정안전부", 10)
	if total != 2 {
		t.Fatalf("행정안전부 → total %d, want 2", total)
	}
	if hits[0].ApplyCount < hits[1].ApplyCount {
		t.Errorf("not ranked by applyCount: %d then %d", hits[0].ApplyCount, hits[1].ApplyCount)
	}
}

// Multiple terms narrow, they don't widen.
func TestSearchAllTermsMustMatch(t *testing.T) {
	if _, total := sample().Search("행정안전부 온열질환자", 10); total != 1 {
		t.Errorf("both-terms search → total %d, want 1", total)
	}
	if _, total := sample().Search("행정안전부 존재하지않는말", 10); total != 0 {
		t.Errorf("unmatchable term should yield 0, got %d", total)
	}
}

// The description is what makes matching work, and is exactly what must not be
// handed back — ten descriptions is thousands of characters of an agent's context.
func TestSearchDoesNotReturnDescriptions(t *testing.T) {
	hits, _ := sample().Search("폭염", 10)
	if len(hits) == 0 {
		t.Fatal("expected description-matched hits")
	}
	for _, h := range hits {
		if strings.Contains(h.Title, "온열질환자 지역별") {
			t.Error("description leaked into the title field")
		}
	}
	// Hit has no Desc field at all; assert the type stays that way.
	var h any = hits[0]
	if _, bad := h.(interface{ GetDesc() string }); bad {
		t.Error("Hit must not expose a description")
	}
}

// A capped result set still reports how many matched, so a caller knows to narrow.
func TestSearchCapsButReportsTotal(t *testing.T) {
	hits, total := sample().Search("", 2)
	if len(hits) != 2 || total != 3 {
		t.Errorf("limit 2 over 3 entries → %d hits, total %d", len(hits), total)
	}
}

func TestStale(t *testing.T) {
	c := sample()
	if c.Stale() {
		t.Error("just-synced catalogue must not be stale")
	}
	c.SyncedAt = time.Now().Add(-StaleAfter - time.Hour)
	if !c.Stale() {
		t.Error("old catalogue must be stale")
	}
}
