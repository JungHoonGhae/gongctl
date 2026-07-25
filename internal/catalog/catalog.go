// Package catalog keeps a local copy of data.go.kr's OpenAPI catalogue so an
// agent can find out what exists without guessing keywords at the portal one
// request at a time.
//
// Discovery was the real bottleneck: the portal only answers keyword queries, so
// finding a dataset meant inventing search terms and paging through results,
// never knowing whether a miss meant "doesn't exist" or "wrong word". A synced
// catalogue turns that into a local lookup over every dataset at once, ranked by
// how many people actually applied for each.
//
// The catalogue is deliberately split from what a caller sees: descriptions are
// stored (they make matching much better) but never returned, because ten of them
// is three thousand characters of an agent's context spent on prose it did not
// ask for. Search returns compact rows.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JungHoonGhae/gongctl/internal/portal"
)

// Entry is one catalogued dataset. Desc is used for matching and is not part of
// what Search returns.
type Entry struct {
	PK         string `json:"pk"`
	Title      string `json:"title"`
	Org        string `json:"org,omitempty"`
	OrgType    string `json:"orgType,omitempty"`
	Category   string `json:"category,omitempty"`
	ApplyCount int    `json:"applyCount,omitempty"`
	ViewCount  int    `json:"viewCount,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
	SvcType    string `json:"svcType,omitempty"` // REST | LINK | "" (portal reports something else)
	Desc       string `json:"desc,omitempty"`
}

// Service types worth labelling. REST means the portal publishes a spec, so
// describe → call can be driven from it; LINK means the portal only points at the
// publisher, so an agent that applies for one gets a key it cannot use here. Two
// in five OpenAPI datasets are LINK, which is far too many to leave unmarked.
const (
	SvcREST = "REST"
	SvcLINK = "LINK"
)

// Catalog is the synced snapshot.
type Catalog struct {
	SyncedAt time.Time `json:"syncedAt"`
	Type     string    `json:"type"` // dataset type swept ("API")
	Entries  []Entry   `json:"entries"`
}

// Hit is one search result — the compact shape callers get. No description.
type Hit struct {
	PK         string `json:"pk"`
	Title      string `json:"title"`
	Org        string `json:"org,omitempty"`
	ApplyCount int    `json:"applyCount,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
	SvcType    string `json:"svcType,omitempty"` // LINK = no spec on the portal, describe/call will not work
	Matched    int    `json:"matched,omitempty"` // terms hit — only meaningful when Result.Relaxed
}

// Result is what a search returns. Terms and Relaxed exist so a caller can tell
// what was actually searched for: a query is not always used as written.
type Result struct {
	Terms   []string `json:"terms"`             // what the query was reduced to
	Total   int      `json:"total"`             // entries matched
	Relaxed bool     `json:"relaxed,omitempty"` // true = every term matching found nothing, so terms were ORed
	Hits    []Hit    `json:"hits"`
}

// StaleAfter is when a synced catalogue should be refreshed. The portal adds and
// retires datasets continuously, so a month-old snapshot is still useful for
// orientation but should not be trusted as complete.
const StaleAfter = 14 * 24 * time.Hour

func path() (string, error) {
	dir, err := portal.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "catalog.json"), nil
}

// Load reads the synced catalogue. Returns ErrNotSynced when there is none.
func Load() (*Catalog, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, ErrNotSynced
	}
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ErrNotSynced means no catalogue has been synced yet.
var ErrNotSynced = fmt.Errorf("카탈로그가 아직 없습니다 — `gongctl catalog sync` 를 먼저 실행하세요")

// Save writes the catalogue.
func (c *Catalog) Save() error {
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// Stale reports whether the snapshot is old enough to warrant a re-sync.
func (c *Catalog) Stale() bool { return time.Since(c.SyncedAt) > StaleAfter }

// Age is how long ago the catalogue was synced.
func (c *Catalog) Age() time.Duration { return time.Since(c.SyncedAt) }

// Sync sweeps the portal's dataset list into a catalogue. perPage is honoured by
// the portal, so a large page size turns thousands of datasets into tens of
// requests. progress, when non-nil, is called with the running total.
//
// It sweeps three times, and the order matters. The unfiltered sweep comes first
// and is what defines the catalogue: it cannot miss a dataset even if the portal
// introduces a service type nobody here has heard of. The two filtered sweeps only
// *label* what the first one already found. A dataset the portal reports as
// neither REST nor LINK therefore ends up unlabelled rather than mislabelled —
// unknown is a fact worth keeping, and guessing here would send an agent to apply
// for something it cannot call.
func Sync(ctx context.Context, pc *portal.Client, dType string, perPage int, progress func(int)) (*Catalog, error) {
	if perPage <= 0 {
		perPage = 200
	}
	seen := map[string]Entry{}
	// sweep pages until the portal stops producing datasets it has not already
	// returned in this sweep. Termination is decided by pagination progress alone —
	// never by whether visit did anything with a row — so a labelling pass cannot
	// cut itself short on a page whose rows were all handled already.
	sweep := func(svcType string, visit func(portal.Dataset)) error {
		inSweep := map[string]bool{}
		for page := 1; ; page++ {
			batch, err := pc.SearchDatasets(ctx, portal.SearchOptions{
				Type: dType, SvcType: svcType, Page: page, PerPage: perPage,
			})
			if err != nil {
				return fmt.Errorf("%d페이지 수집 실패 (svcType=%q): %w", page, svcType, err)
			}
			fresh := 0
			for _, d := range batch {
				if inSweep[d.PublicDataPk] {
					continue
				}
				inSweep[d.PublicDataPk] = true
				fresh++
				visit(d)
			}
			if progress != nil {
				progress(len(seen))
			}
			if fresh == 0 {
				return nil
			}
		}
	}

	if err := sweep("", func(d portal.Dataset) {
		seen[d.PublicDataPk] = Entry{
			PK: d.PublicDataPk, Title: d.Title, Org: d.Org, OrgType: d.OrgType,
			Category: d.Category, ApplyCount: d.ApplyCount, ViewCount: d.ViewCount,
			ModifiedAt: d.ModifiedAt, Desc: d.Description,
		}
	}); err != nil {
		return nil, err
	}

	for _, svc := range []string{SvcREST, SvcLINK} {
		if err := sweep(svc, func(d portal.Dataset) {
			if e, ok := seen[d.PublicDataPk]; ok && e.SvcType == "" {
				e.SvcType = svc
				seen[d.PublicDataPk] = e
			}
		}); err != nil {
			return nil, err
		}
	}

	c := &Catalog{SyncedAt: time.Now().UTC(), Type: dType}
	for _, e := range seen {
		c.Entries = append(c.Entries, e)
	}
	sort.Slice(c.Entries, func(i, j int) bool { return c.Entries[i].ApplyCount > c.Entries[j].ApplyCount })
	return c, nil
}

// particles are Korean grammatical endings that a natural-sounding query carries
// and a catalogue title never does: nobody publishes "폭염에" or "광진구에서".
// Longest first, so 에서 is tried before 에.
var particles = []string{"에서는", "에서", "에게", "으로", "부터", "까지", "이나", "라도",
	"보다", "만큼", "처럼", "및", "의", "에", "을", "를", "은", "는", "이", "가", "와", "과", "도", "로"}

// fillers carry no domain meaning but appear in how people ask. Left in, they
// would rank titles by whether they happen to contain the word "데이터". Matching
// is on the whole token, so 대한 here never touches 대한민국.
var fillers = map[string]bool{
	"데이터": true, "자료": true, "정보를": true, "좋은": true, "관련": true, "관한": true,
	"있는": true, "찾아줘": true, "알려줘": true, "무엇": true, "어떤": true, "필요한": true,
	"목록": true, "리스트": true, "api": true, "그리고": true, "또는": true,
	// connectives — "폭염으로 인한", "청소년을 위한", "고령화에 따른"
	"인한": true, "대한": true, "위한": true, "통한": true, "따른": true, "관하여": true,
	"있나": true, "없나": true, "싶어": true, "주세요": true, "해줘": true, "좀": true,
}

// queryTerms reduces a query to the words worth matching. It is deliberately not
// a morphological analyser: stripping a trailing particle and dropping fillers is
// enough to make "폭염에 취약한 고령자" behave like "폭염 취약 고령자", and anything
// cleverer would need a dictionary this tool has no reason to carry.
func queryTerms(query string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(query)) {
		w = strings.Trim(w, ".,?!\"'()[]")
		if w == "" || fillers[w] {
			continue
		}
		for _, p := range particles {
			// Keep at least two characters: trimming 가 off 고가 leaves nothing usable.
			if trimmed := strings.TrimSuffix(w, p); trimmed != w && len([]rune(trimmed)) >= 2 {
				w = trimmed
				break
			}
		}
		out = append(out, w)
	}
	return out
}

// Search finds datasets for a query written the way people and agents actually
// write one — "폭염에 취약한 고령자", not "폭염 고령". Terms are reduced (particles
// trimmed, fillers dropped), then matched against title, publisher, category and
// description.
//
// Precision first, recall as a fallback: entries matching *every* term win, and
// only when that finds nothing are the terms ORed and ranked by how many hit.
// The fallback is reported rather than hidden — a relaxed result answers a
// different question from the one asked, and the caller has to know that.
// Within a tier, ranking is by application count: demand is the best available
// proxy for "this one is actually usable".
//
// restOnly drops everything the portal does not report as REST. For an agent that
// intends to describe and call, that is the honest default: a LINK dataset has no
// spec here, so applying for one spends a real application on a dead end.
func (c *Catalog) Search(query string, limit int, restOnly bool) Result {
	if limit <= 0 {
		limit = 20
	}
	terms := queryTerms(query)
	res := Result{Terms: terms}

	type scored struct {
		e       *Entry
		matched int
		inTitle int // terms found in the name/publisher rather than only the blurb
	}
	var all []scored
	for i := range c.Entries {
		e := &c.Entries[i]
		if restOnly && e.SvcType != SvcREST {
			continue
		}
		name := strings.ToLower(e.Title + " " + e.Org + " " + e.Category)
		desc := strings.ToLower(e.Desc)
		s := scored{e: e}
		for _, t := range terms {
			switch {
			case strings.Contains(name, t):
				s.matched++
				s.inTitle++
			case strings.Contains(desc, t):
				s.matched++
			}
		}
		if s.matched > 0 || len(terms) == 0 {
			all = append(all, s)
		}
	}

	keep := func(s scored) bool { return s.matched == len(terms) }
	strict := 0
	for _, s := range all {
		if keep(s) {
			strict++
		}
	}
	if strict == 0 && len(terms) > 1 {
		res.Relaxed = true
		keep = func(s scored) bool { return s.matched > 0 }
	}

	var kept []scored
	for _, s := range all {
		if keep(s) {
			kept = append(kept, s)
		}
	}
	// A term in the dataset's name means the dataset is about it; a term in the
	// blurb might be an aside. Without this, a relaxed search puts whatever
	// popular dataset happens to mention a word above the one actually named
	// after it.
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].matched != kept[j].matched {
			return kept[i].matched > kept[j].matched
		}
		if kept[i].inTitle != kept[j].inTitle {
			return kept[i].inTitle > kept[j].inTitle
		}
		return kept[i].e.ApplyCount > kept[j].e.ApplyCount
	})
	res.Total = len(kept)
	if len(kept) > limit {
		kept = kept[:limit]
	}
	for _, s := range kept {
		h := Hit{PK: s.e.PK, Title: s.e.Title, Org: s.e.Org,
			ApplyCount: s.e.ApplyCount, ModifiedAt: s.e.ModifiedAt, SvcType: s.e.SvcType}
		if res.Relaxed {
			h.Matched = s.matched
		}
		res.Hits = append(res.Hits, h)
	}
	return res
}

// SvcTypes counts datasets per service type, so `info` can say how much of the
// catalogue is callable from the portal at all.
func (c *Catalog) SvcTypes() map[string]int {
	out := map[string]int{}
	for _, e := range c.Entries {
		k := e.SvcType
		if k == "" {
			k = "미확인"
		}
		out[k]++
	}
	return out
}

// Orgs counts datasets per publisher, most first — the fastest way to see who
// publishes in an area once a search has narrowed it down.
func (c *Catalog) Orgs(query string, limit int) []OrgCount {
	terms := queryTerms(query)
	counts := map[string]*OrgCount{}
	for _, e := range c.Entries {
		hay := strings.ToLower(e.Title + " " + e.Org + " " + e.Category + " " + e.Desc)
		ok := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		c, exists := counts[e.Org]
		if !exists {
			c = &OrgCount{Org: e.Org}
			counts[e.Org] = c
		}
		c.Count++
		c.ApplySum += e.ApplyCount
	}
	out := make([]OrgCount, 0, len(counts))
	for _, v := range counts {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// OrgCount is one publisher's share of a query's matches.
type OrgCount struct {
	Org      string `json:"org"`
	Count    int    `json:"count"`
	ApplySum int    `json:"applySum"`
}
