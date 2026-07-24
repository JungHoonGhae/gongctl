package portal

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Dataset is one data.go.kr dataset from a search result.
type Dataset struct {
	PublicDataPk string   `json:"publicDataPk"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Formats      []string `json:"formats,omitempty"`
	HasOpenAPI   bool     `json:"hasOpenApi"` // true when the result links to /data/{pk}/openapi.do
}

// SearchOptions filters a dataset search. Blank Org = all publishers; blank
// Type = all dataset types.
type SearchOptions struct {
	Keyword string
	Org     string
	Type    string // "FILE" | "API" | "" (all)
	Page    int
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
	if opts.Org != "" {
		q.Set("org", opts.Org)
	}
	q.Set("keyword", opts.Keyword)
	q.Set("currentPage", strconv.Itoa(page))
	q.Set("perPage", "10")

	doc, err := c.getDoc(ctx, "/tcs/dss/selectDataSetList.do", q)
	if err != nil {
		return nil, err
	}

	var out []Dataset
	seen := map[string]bool{}
	doc.Find("dl").Each(func(_ int, dl *goquery.Selection) {
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
		out = append(out, Dataset{
			PublicDataPk: pk,
			Title:        title,
			Description:  cleanText(dl.Find("dd").First().Text()),
			Formats:      formats,
			HasOpenAPI:   hasAPI,
		})
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

// cleanText collapses runs of whitespace to single spaces.
func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
