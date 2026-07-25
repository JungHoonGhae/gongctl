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
	r := c.Search("기온", 10)
	hits, total := r.Hits, r.Total
	if total != 1 || len(hits) != 1 || hits[0].PK != "2" {
		t.Fatalf("기온 → %d hits (total %d), first %+v", len(hits), total, hits)
	}

	r = c.Search("행정안전부", 10)
	hits, total = r.Hits, r.Total
	if total != 2 {
		t.Fatalf("행정안전부 → total %d, want 2", total)
	}
	if hits[0].ApplyCount < hits[1].ApplyCount {
		t.Errorf("not ranked by applyCount: %d then %d", hits[0].ApplyCount, hits[1].ApplyCount)
	}
}

// Precision first: when entries match every term, only those are returned and
// the result is not marked relaxed.
func TestSearchPrefersEveryTermMatching(t *testing.T) {
	r := sample().Search("행정안전부 온열질환자", 10)
	if r.Relaxed {
		t.Error("both terms matched, should not relax")
	}
	if r.Total != 1 {
		t.Errorf("both-terms search → total %d, want 1", r.Total)
	}
}

// Recall as a fallback: a query no entry fully satisfies must still answer, and
// must say it loosened the query rather than pretending the result is exact.
func TestSearchRelaxesWhenNothingMatchesEveryTerm(t *testing.T) {
	r := sample().Search("행정안전부 존재하지않는말", 10)
	if !r.Relaxed {
		t.Fatal("no entry has both terms — expected a relaxed result, not silence")
	}
	if r.Total == 0 {
		t.Fatal("relaxed search returned nothing")
	}
	if r.Hits[0].Matched != 1 {
		t.Errorf("relaxed hit should report how many terms it matched, got %d", r.Hits[0].Matched)
	}
}

// A single unmatchable word is a genuine miss, not something to widen.
func TestSearchSingleTermMissStaysEmpty(t *testing.T) {
	if r := sample().Search("존재하지않는말", 10); r.Total != 0 || r.Relaxed {
		t.Errorf("single unmatchable term → total %d relaxed %v, want 0/false", r.Total, r.Relaxed)
	}
}

// The query is written by a person or an agent, not filled into a search form:
// particles and filler words must not decide whether it finds anything.
func TestSearchHandlesNaturalLanguage(t *testing.T) {
	plain := sample().Search("폭염 인명피해", 10)
	natural := sample().Search("폭염으로 인한 인명피해 데이터 알려줘", 10)
	if natural.Total != plain.Total {
		t.Errorf("natural phrasing → %d hits, keyword phrasing → %d; should agree", natural.Total, plain.Total)
	}
	if natural.Relaxed {
		t.Errorf("natural phrasing should not need relaxing, terms=%v", natural.Terms)
	}
}

// Trimming must not eat a word: 고가 is not 고 + the particle 가.
func TestQueryTermsKeepsShortWordsIntact(t *testing.T) {
	for q, want := range map[string]string{
		"폭염에":   "폭염",
		"광진구에서": "광진구",
		"고가":    "고가",
		"실거래가":  "실거래",
	} {
		if got := queryTerms(q); len(got) != 1 || got[0] != want {
			t.Errorf("queryTerms(%q) = %v, want [%s]", q, got, want)
		}
	}
}

// The description is what makes matching work, and is exactly what must not be
// handed back — ten descriptions is thousands of characters of an agent's context.
func TestSearchDoesNotReturnDescriptions(t *testing.T) {
	hits := sample().Search("폭염", 10).Hits
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
	r := sample().Search("", 2)
	hits, total := r.Hits, r.Total
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

// A word in the dataset's name is a stronger signal than the same word buried in
// its description — otherwise a relaxed search recommends whatever popular
// dataset happens to mention the word.
func TestSearchPrefersNameMatchesOverDescriptionMatches(t *testing.T) {
	c := &Catalog{SyncedAt: time.Now(), Entries: []Entry{
		{PK: "1", Title: "인기 있는 교통 통계", ApplyCount: 9000, Desc: "지역 상권 변화도 참고할 수 있습니다"},
		{PK: "2", Title: "소상공인시장진흥공단_상권정보", ApplyCount: 10},
	}}
	r := c.Search("상권", 10)
	if r.Hits[0].PK != "2" {
		t.Errorf("name match should outrank a description mention; got pk=%s first", r.Hits[0].PK)
	}
}
