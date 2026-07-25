// Package apicall surfaces data.go.kr OpenAPI specs to an agent (describe) and
// performs authenticated calls (call). It never parses a spec into claims it
// can't back with the page's own markup — uncertain structure is surfaced as
// raw HTML for the agent to read.
package apicall

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
	"github.com/PuerkitoBio/goquery"
)

// APISpec is the surfaced view of one dataset's OpenAPI detail page.
type APISpec struct {
	PublicDataPk string      `json:"publicDataPk"`
	DataName     string      `json:"dataName"`
	Operations   []Operation `json:"operations"`
	GuideDoc     string      `json:"guideDoc,omitempty"`    // 참고문서 file name, NOT parsed
	GuideDocURL  string      `json:"guideDocUrl,omitempty"` // where to fetch that file
	// Note is set only when the page documents no operations, to say where the
	// spec actually lives. Without it an empty Operations list is a dead end.
	Note string `json:"note,omitempty"`
}

// Operation is one 상세기능. When the request-variable table parses cleanly,
// Params is filled; otherwise RawHTML carries the section verbatim.
type Operation struct {
	Name     string  `json:"name"`
	Endpoint string  `json:"endpoint,omitempty"`
	Params   []Param `json:"params,omitempty"`
	RawHTML  string  `json:"rawHtml,omitempty"`
}

// Param is one request variable, surfaced from the 요청변수 table.
type Param struct {
	Name     string `json:"name"`     // 항목명(영문)
	Required string `json:"required"` // 항목구분 (필수/옵션)
	Sample   string `json:"sample"`   // 샘플데이터
	Desc     string `json:"desc"`     // 항목설명
}

var reEndpoint = regexp.MustCompile(`https?://apis\.data\.go\.kr/[^\s"'<)]+`)

// reFileDownload pulls the two ids out of the 참고문서 link's
// fn_fileDownload('FILE_...','1') handler, which is the only place the page
// carries them.
var reFileDownload = regexp.MustCompile(`fn_fileDownload\('([^']+)'\s*,\s*'([^']+)'\)`)

// Describe scrapes {baseURL}/data/{pk}/openapi.do through the shared transport.
// baseURL is overridable for tests; production passes portal.BaseURL.
func Describe(ctx context.Context, f *fetch.Client, baseURL, pk string) (*APISpec, error) {
	url := strings.TrimRight(baseURL, "/") + "/data/" + pk + "/openapi.do"
	doc, err := f.GetDoc(ctx, url)
	if err != nil {
		return nil, err
	}

	spec := &APISpec{PublicDataPk: pk}
	spec.DataName = strings.TrimSpace(doc.Find(".open-api-title, .data-set-title").First().Text())
	spec.DataName = cleanText(spec.DataName)

	// Operation containers: real per-operation content lives in
	// .open-api-detail-result (endpoint + 요청변수/출력결과 tables). The sibling
	// .open-api-detail div is only the operation-switcher (<select>+button, no
	// data) and, on pages with broken/comment-only-closed div nesting, can end
	// up as the *only* match for .open-api-detail while swallowing unrelated
	// content via the HTML5 parser's error recovery — so prefer the result
	// containers whenever the page has any, and only fall back to
	// .open-api-detail for older/simpler single-operation pages that lack a
	// separate result div.
	sections := doc.Find(".open-api-detail-result")
	if sections.Length() == 0 {
		sections = doc.Find(".open-api-detail")
	}
	sections.Each(func(_ int, sel *goquery.Selection) {
		op := Operation{Name: cleanText(sel.Find("h4, .tit").First().Text())}
		if html, err := sel.Html(); err == nil {
			if m := reEndpoint.FindString(html); m != "" {
				op.Endpoint = m
			}
		}
		op.Params = parseParams(sel)
		if len(op.Params) == 0 {
			// surface-only: no clean request-variable table → hand back raw HTML.
			if html, err := sel.Html(); err == nil {
				op.RawHTML = strings.TrimSpace(html)
			}
		}
		if op.Name != "" || op.Endpoint != "" || op.RawHTML != "" {
			spec.Operations = append(spec.Operations, op)
		}
	})

	// GuideDoc: the 참고문서 row. The file itself is never fetched or parsed here —
	// but its download URL is surfaced, because the file name alone gives an agent
	// nothing it can act on.
	doc.Find("th").EachWithBreak(func(_ int, th *goquery.Selection) bool {
		if strings.Contains(th.Text(), "참고문서") {
			td := th.NextFiltered("td")
			spec.GuideDoc = cleanText(td.Text())
			if onclick, ok := td.Find("a[onclick]").First().Attr("onclick"); ok {
				if m := reFileDownload.FindStringSubmatch(onclick); m != nil {
					spec.GuideDocURL = fmt.Sprintf("%s/cmm/cmm/fileDownload.do?atchFileId=%s&fileDetailSn=%s",
						strings.TrimRight(baseURL, "/"), m[1], m[2])
				}
			}
			return false
		}
		return true
	})

	// Some datasets document the whole spec in the attached guide document and
	// leave the page itself empty. Say so, rather than handing back an empty list
	// that reads as "this API has no operations".
	if len(spec.Operations) == 0 {
		spec.Note = "이 페이지에는 상세기능·요청변수 표가 없습니다 — 명세가 참고문서(guideDocUrl)에만 있는 API입니다. " +
			"guideDocUrl 을 내려받아 읽고 엔드포인트·파라미터를 확인하세요. 파라미터를 추측해 호출하지 마세요."
		if spec.GuideDocURL == "" {
			spec.Note = "이 페이지에서 상세기능·요청변수를 찾지 못했고 참고문서 링크도 없습니다 — " +
				"페이지 구조가 바뀐 것일 수 있습니다 (gongctl doctor 로 확인). 파라미터를 추측하지 마세요."
		}
	}

	return spec, nil
}

// parseParams reads the 요청변수 (request parameter) table inside an operation
// section. 요청변수 and 출력결과 (response) tables share an identical header row
// (항목명(영문), ...), so column-header matching alone can't tell them apart —
// instead this walks headings and tables in document order and only parses a
// table anchored under the nearest preceding 요청변수/Request Parameter
// heading, never under 출력결과/Response. Response fields (resultCode,
// resultMsg, ...) can therefore never be misattributed as request params. If
// no heading unambiguously marks a table as the request table, it's left to
// RawHTML rather than guessed.
func parseParams(sel *goquery.Selection) []Param {
	var params []Param
	heading := ""
	sel.Find("h4, table").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if goquery.NodeName(node) == "h4" {
			heading = cleanText(node.Text())
			return true
		}
		if !isRequestHeading(heading) {
			return true // not under a 요청변수 heading (or under 출력결과); keep scanning
		}
		tbl := node
		headers := map[string]int{}
		tbl.Find("thead th, tr:first-child th").Each(func(i int, th *goquery.Selection) {
			headers[cleanText(th.Text())] = i
		})
		nameCol, ok := colIndex(headers, "항목명(영문)")
		if !ok {
			return true // heading said request, but table shape is unrecognized; keep scanning
		}
		reqCol, _ := colIndex(headers, "항목구분")
		sampleCol, _ := colIndex(headers, "샘플데이터")
		descCol, _ := colIndex(headers, "항목설명")
		tbl.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
			cells := tr.Find("td")
			if cells.Length() == 0 {
				return
			}
			get := func(idx int) string {
				if idx < 0 {
					return ""
				}
				return cleanText(cells.Eq(idx).Text())
			}
			name := get(nameCol)
			if name == "" {
				return
			}
			params = append(params, Param{
				Name:     name,
				Required: get(reqCol),
				Sample:   get(sampleCol),
				Desc:     get(descCol),
			})
		})
		return false // took the first request-variable table
	})
	return params
}

// isRequestHeading reports whether a heading text marks the following table
// as a 요청변수(Request Parameter) table rather than 출력결과(Response Element).
func isRequestHeading(h string) bool {
	if h == "" {
		return false
	}
	lower := strings.ToLower(h)
	if strings.Contains(h, "출력결과") || strings.Contains(lower, "response") {
		return false
	}
	return strings.Contains(h, "요청변수") || strings.Contains(lower, "request parameter")
}

func colIndex(headers map[string]int, key string) (int, bool) {
	i, ok := headers[key]
	if !ok {
		return -1, false
	}
	return i, true
}

func cleanText(s string) string { return strings.Join(strings.Fields(s), " ") }
