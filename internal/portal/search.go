package portal

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Dataset is one data.go.kr dataset from a search result. Beyond the identity
// fields, the list carries per-dataset metadata (publisher, last update, and how
// much the dataset is actually looked at and applied for) which is what makes it
// possible to judge a dataset without opening its page.
type Dataset struct {
	PublicDataPk string   `json:"publicDataPk"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Formats      []string `json:"formats,omitempty"`
	HasOpenAPI   bool     `json:"hasOpenApi"`           // true when the result links to /data/{pk}/openapi.do
	Org          string   `json:"org,omitempty"`        // 제공기관
	ModifiedAt   string   `json:"modifiedAt,omitempty"` // 수정일
	ViewCount    int      `json:"viewCount,omitempty"`  // 조회수
	ApplyCount   int      `json:"applyCount,omitempty"` // 활용신청 건수
	Category     string   `json:"category,omitempty"`   // 분류체계
	OrgType      string   `json:"orgType,omitempty"`    // 기관유형 (공공기관/지자체 등)
}

// SearchOptions filters a dataset search. Blank Org = all publishers; blank
// Type = all dataset types.
type SearchOptions struct {
	Keyword string
	Org     string
	Type    string // "FILE" | "API" | "" (all)
	// SvcType narrows OpenAPI datasets by service type as the portal's own filter
	// does: "REST" (spec published on the portal) or "LINK" (only a pointer to the
	// publisher's site). Empty means every type.
	SvcType string
	Page    int
	// PerPage is how many results one page returns (default 10, the portal's own
	// default). Raise it to sweep a large result set in fewer requests.
	PerPage int
}

var (
	rePkFile     = regexp.MustCompile(`/data/(\d+)/fileData\.do`)
	rePkAPI      = regexp.MustCompile(`/data/(\d+)/openapi\.do`)
	knownFormats = map[string]bool{"CSV": true, "XLSX": true, "XLS": true, "JSON": true, "XML": true, "HWP": true, "PDF": true, "ZIP": true}
)

// SearchDatasets scrapes /tcs/dss/selectDataSetList.do. A blank keyword lists
// the first page.
func (c *Client) SearchDatasets(ctx context.Context, opts SearchOptions) ([]Dataset, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	if opts.Type != "" {
		q.Set("dType", opts.Type)
	}
	if opts.SvcType != "" {
		q.Set("svcType", opts.SvcType)
	}
	if opts.Org != "" {
		q.Set("org", opts.Org)
	}
	q.Set("keyword", opts.Keyword)
	q.Set("currentPage", strconv.Itoa(page))
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 10
	}
	q.Set("perPage", strconv.Itoa(perPage))

	doc, err := c.getDoc(ctx, "/tcs/dss/selectDataSetList.do", q)
	if err != nil {
		return nil, err
	}

	var out []Dataset
	seen := map[string]bool{}
	// Each result is one <li> holding the <dl> (title/description) plus sibling
	// .tag-area and .info-data blocks; iterating the <li> keeps a dataset's
	// metadata attached to it instead of only reading the <dl>.
	items := doc.Find(".result-list > ul > li")
	if items.Length() == 0 {
		items = doc.Find("dl").Parent() // older/simpler markup
	}
	items.Each(func(_ int, li *goquery.Selection) {
		dl := li.Find("dl").First()
		href, _ := dl.Find(`a[href*="/data/"]`).First().Attr("href")
		var pk string
		hasAPI := false
		if m := rePkAPI.FindStringSubmatch(href); m != nil {
			pk, hasAPI = m[1], true
		} else if m := rePkFile.FindStringSubmatch(href); m != nil {
			pk = m[1]
		}
		if pk == "" || seen[pk] {
			return
		}
		seen[pk] = true
		title, formats := parseDt(dl)
		d := Dataset{
			PublicDataPk: pk,
			Title:        title,
			Description:  cleanText(dl.Find("dd").First().Text()),
			Formats:      formats,
			HasOpenAPI:   hasAPI,
			Category:     cleanText(li.Find(".tag-area .labelset.brown").First().Text()),
			OrgType:      cleanText(li.Find(".tag-area .labelset.red").First().Text()),
		}
		// .info-data is a list of label/value pairs (same shape as the 활용신청
		// 현황 list): <p><span class="tit">제공기관</span><span class="data">…</span></p>
		li.Find(".info-data p").Each(func(_ int, p *goquery.Selection) {
			switch cleanText(p.Find(".tit").Text()) {
			case "제공기관":
				d.Org = cleanText(p.Find(".data").Text())
			case "수정일":
				d.ModifiedAt = cleanText(p.Find(".data").Text())
			case "조회수":
				d.ViewCount = atoiLoose(cleanText(p.Find(".data").Text()))
			case "활용신청":
				d.ApplyCount = atoiLoose(cleanText(p.Find(".data").Text()))
			}
		})
		out = append(out, d)
	})
	return out, nil
}

func parseDt(dl *goquery.Selection) (title string, formats []string) {
	text := cleanText(dl.Find("dt").First().Text())
	toks := strings.Fields(text)
	seen := map[string]bool{}
	i := 0
	for i < len(toks) {
		up := strings.ToUpper(toks[i])
		if knownFormats[up] {
			if !seen[up] {
				seen[up] = true
				formats = append(formats, up)
			}
			i++
			continue
		}
		if toks[i] == "+" {
			i++
			continue
		}
		break
	}
	title = strings.TrimSpace(strings.TrimSuffix(strings.Join(toks[i:], " "), "미리보기"))
	return title, formats
}

// atoiLoose parses a count that may carry thousands separators, returning 0 when
// the portal renders something unexpected (never a fabricated number).
func atoiLoose(s string) int {
	n, err := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(s), ",", ""))
	if err != nil {
		return 0
	}
	return n
}

// cleanText collapses runs of whitespace to single spaces.
func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
