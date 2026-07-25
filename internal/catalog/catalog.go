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
	Desc       string `json:"desc,omitempty"`
}

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
func Sync(ctx context.Context, pc *portal.Client, dType string, perPage int, progress func(int)) (*Catalog, error) {
	if perPage <= 0 {
		perPage = 200
	}
	seen := map[string]Entry{}
	for page := 1; ; page++ {
		batch, err := pc.SearchDatasets(ctx, portal.SearchOptions{
			Type: dType, Page: page, PerPage: perPage,
		})
		if err != nil {
			return nil, fmt.Errorf("%d페이지 수집 실패: %w", page, err)
		}
		added := 0
		for _, d := range batch {
			if _, ok := seen[d.PublicDataPk]; ok {
				continue
			}
			seen[d.PublicDataPk] = Entry{
				PK: d.PublicDataPk, Title: d.Title, Org: d.Org, OrgType: d.OrgType,
				Category: d.Category, ApplyCount: d.ApplyCount, ViewCount: d.ViewCount,
				ModifiedAt: d.ModifiedAt, Desc: d.Description,
			}
			added++
		}
		if progress != nil {
			progress(len(seen))
		}
		// A page that adds nothing new means the portal has stopped advancing.
		if added == 0 {
			break
		}
	}
	c := &Catalog{SyncedAt: time.Now().UTC(), Type: dType}
	for _, e := range seen {
		c.Entries = append(c.Entries, e)
	}
	sort.Slice(c.Entries, func(i, j int) bool { return c.Entries[i].ApplyCount > c.Entries[j].ApplyCount })
	return c, nil
}

// Search matches every whitespace-separated term against title, publisher and
// description (all of them must match somewhere), then ranks by how many people
// applied for the dataset — demand is the best available proxy for "this one is
// actually usable". Results are capped; the total number of matches is returned
// separately so a caller knows whether to narrow the query.
func (c *Catalog) Search(query string, limit int) (hits []Hit, total int) {
	if limit <= 0 {
		limit = 20
	}
	terms := strings.Fields(strings.ToLower(query))
	for _, e := range c.Entries {
		hay := strings.ToLower(e.Title + " " + e.Org + " " + e.Category + " " + e.Desc)
		match := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		total++
		if len(hits) < limit {
			hits = append(hits, Hit{
				PK: e.PK, Title: e.Title, Org: e.Org,
				ApplyCount: e.ApplyCount, ModifiedAt: e.ModifiedAt,
			})
		}
	}
	return hits, total
}

// Orgs counts datasets per publisher, most first — the fastest way to see who
// publishes in an area once a search has narrowed it down.
func (c *Catalog) Orgs(query string, limit int) []OrgCount {
	terms := strings.Fields(strings.ToLower(query))
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
