// Package apicall surfaces data.go.kr OpenAPI specs to an agent (describe) and
// performs authenticated calls (call). It never parses a spec into claims it
// can't back with the page's own markup — uncertain structure is surfaced as
// raw HTML for the agent to read.
package apicall

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
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
	// APIType is the portal's own 'API 유형' field: REST means the page documents
	// operations and request variables; LINK means the portal only points at the
	// publisher's own site, so no spec exists here to read. Knowing which is
	// which decides whether describe→call can be driven from this page at all.
	APIType string `json:"apiType,omitempty"`
	// EndpointOnly marks a spec whose endpoint was recovered from the page but
	// whose request parameters the portal never documents.
	EndpointOnly bool `json:"endpointOnly,omitempty"`
	// LinkURL is the publisher's own page for a LINK dataset, taken from the
	// portal's URL row. The portal has it on every LINK page sampled, so telling a
	// caller to "check the publisher's documentation" without handing over the
	// address it already holds is withholding the one actionable thing on the page.
	// gongctl does not follow it: the publishers are a long tail (39 distinct hosts
	// in 70 sampled datasets, the largest 13%), each with its own registration and
	// spec format, so reading it is the agent's job — surfacing it is ours.
	LinkURL string `json:"linkUrl,omitempty"`
	// Approval is the portal's 심의유형 row: whether an application is granted
	// automatically or waits for a human at the publishing agency.
	Approval *Approval `json:"approval,omitempty"`
	// Note is set only when the spec is incomplete, to say where the rest of it
	// lives. Without it an empty Operations list is a dead end.
	Note string `json:"note,omitempty"`
}

// Approval reports the two stages the portal grades separately. gongctl applies
// for a development account, so Dev is the one that decides whether a key arrives
// immediately; Ops describes what a later move to production would face and is
// surfaced because that is a decision a caller may need to make now.
//
// Absent on datasets whose page carries no 심의유형 row (LINK datasets, mostly) —
// unknown is reported as unknown rather than assumed to be automatic.
type Approval struct {
	Dev string `json:"dev,omitempty"` // 개발단계: 자동승인 | 심의승인
	Ops string `json:"ops,omitempty"` // 운영단계: 자동승인 | 심의승인
	Raw string `json:"raw,omitempty"` // the row verbatim, in case the wording changes
}

// AutoApproved reports whether applying yields a key without human review.
func (a *Approval) AutoApproved() bool {
	return a != nil && strings.Contains(a.Dev, "자동승인")
}

// reApproval reads "개발단계 : 자동승인 / 운영단계 : 심의승인".
var reApproval = regexp.MustCompile(`개발단계\s*[::]\s*(\S+)\s*/\s*운영단계\s*[::]\s*(\S+)`)

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
	// The portal embeds an authoritative Swagger 2.0 spec on modern pages; prefer
	// it over scraping the rendered tables, which carry less and break more.
	if ops := operationsFromSwagger(doc); len(ops) > 0 {
		spec.Operations = ops
	}

	sections := doc.Find(".open-api-detail-result")
	if sections.Length() == 0 {
		sections = doc.Find(".open-api-detail")
	}
	if len(spec.Operations) > 0 {
		sections = doc.Find("__none__") // swagger already answered; skip the tables
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

	spec.Operations = dedupeOperations(spec.Operations)

	// Page-level endpoint fallback: many REST datasets carry the endpoint URL
	// somewhere on the page while documenting no 요청변수 table at all. Surfacing
	// the endpoint alone is still factual and gets a caller moving; the note below
	// makes clear the parameters are NOT documented here.
	if len(spec.Operations) == 0 {
		if html, err := doc.Html(); err == nil {
			if m := reEndpoint.FindString(html); m != "" {
				spec.Operations = append(spec.Operations, Operation{Endpoint: m})
				spec.EndpointOnly = true
			}
		}
	}

	// GuideDoc: the 참고문서 row. The file itself is never fetched or parsed here —
	// but its download URL is surfaced, because the file name alone gives an agent
	// nothing it can act on.
	// API 유형 (REST / LINK / …) — the th/td pair in the summary table.
	doc.Find("th").EachWithBreak(func(_ int, th *goquery.Selection) bool {
		if strings.Contains(cleanText(th.Text()), "API 유형") {
			spec.APIType = cleanText(th.NextFiltered("td").Text())
			return false
		}
		return true
	})

	doc.Find("th").EachWithBreak(func(_ int, th *goquery.Selection) bool {
		if strings.Contains(cleanText(th.Text()), "심의유형") {
			raw := cleanText(th.NextFiltered("td").Text())
			a := &Approval{Raw: raw}
			if m := reApproval.FindStringSubmatch(raw); m != nil {
				a.Dev, a.Ops = m[1], m[2]
			}
			spec.Approval = a
			return false
		}
		return true
	})

	// The URL row appears on LINK pages (where 심의유형 does not).
	doc.Find("th").EachWithBreak(func(_ int, th *goquery.Selection) bool {
		if cleanText(th.Text()) == "URL" {
			td := th.NextFiltered("td")
			if href, ok := td.Find("a[href]").First().Attr("href"); ok {
				spec.LinkURL = strings.TrimSpace(href)
			} else {
				spec.LinkURL = cleanText(td.Text())
			}
			return false
		}
		return true
	})

	guideRowFound := false
	doc.Find("th").EachWithBreak(func(_ int, th *goquery.Selection) bool {
		if strings.Contains(th.Text(), "참고문서") {
			guideRowFound = true
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
	if spec.EndpointOnly {
		spec.Note = "엔드포인트는 페이지에서 확인했지만 요청변수(파라미터) 표가 없습니다 — " +
			"파라미터는 포털에 문서화돼 있지 않습니다. guideDocUrl 이 있으면 그 문서를, 없으면 " +
			"제공기관 문서를 확인하세요. 파라미터를 추측해서 호출하지 마세요."
	} else if len(spec.Operations) == 0 {
		spec.Note = "이 페이지에는 상세기능·요청변수 표가 없습니다 — 명세가 참고문서(guideDocUrl)에만 있는 API입니다. " +
			"guideDocUrl 을 내려받아 읽고 엔드포인트·파라미터를 확인하세요. 파라미터를 추측해 호출하지 마세요."
		if spec.GuideDocURL == "" {
			// A 참고문서 row that carries no file (fn_fileDownload('','')) is the
			// portal saying "no document" — distinct from the row being absent,
			// which would suggest the page layout changed.
			if strings.Contains(spec.APIType, "LINK") {
				spec.Note = "이 API 는 유형이 LINK 입니다 — 포털은 명세를 싣지 않고 제공기관 사이트로 " +
					"연결만 합니다. 엔드포인트·파라미터는 포털에서 알 수 없으니 추측해서 호출하지 마세요."
				if spec.LinkURL != "" {
					spec.Note += " linkUrl(" + spec.LinkURL + ") 을 직접 열어 읽으면 명세가 거기 있습니다. " +
						"단, 대개 제공기관의 별도 회원가입·별도 인증키가 필요하며 gongctl 의 계정 인증키는 " +
						"그곳에서 쓸 수 없습니다."
				}
			} else if guideRowFound {
				spec.Note = "이 API 는 포털에 명세가 없습니다 — 상세기능·요청변수 표도, 참고문서 파일도 " +
					"제공되지 않습니다(참고문서 항목이 비어 있음). 엔드포인트와 파라미터를 알 방법이 " +
					"포털에 없으니 제공기관에 문의하거나 다른 API 를 쓰세요. 추측해서 호출하지 마세요."
			} else {
				spec.Note = "이 페이지에서 상세기능·요청변수와 참고문서 항목 자체를 찾지 못했습니다 — " +
					"페이지 구조가 바뀐 것일 수 있습니다 (gongctl doctor 로 확인). 파라미터를 추측하지 마세요."
			}
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

// reSwaggerJSON pulls the embedded spec out of the page's
// `var swaggerJson = \`{…}\`;` template literal.
var reSwaggerJSON = regexp.MustCompile("(?s)swaggerJson\\s*=\\s*`(.*?)`")

// swaggerDoc is the slice of Swagger 2.0 gongctl reads. Parameters sit at the
// PATH level on data.go.kr's specs, not under the operation, so both are read.
type swaggerDoc struct {
	Host     string   `json:"host"`
	BasePath string   `json:"basePath"`
	Schemes  []string `json:"schemes"`
	Paths    map[string]struct {
		Parameters []swaggerParam `json:"parameters"`
		Get        *swaggerOp     `json:"get"`
		Post       *swaggerOp     `json:"post"`
	} `json:"paths"`
}

type swaggerOp struct {
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	OperationID string         `json:"operationId"`
	Parameters  []swaggerParam `json:"parameters"`
}

type swaggerParam struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Example     any    `json:"example"`
}

// operationsFromSwagger reads the embedded Swagger spec, if the page carries one.
// This is the authoritative source — the HTML tables are a fallback for pages
// that predate it. Returns nil when there is no spec to read.
func operationsFromSwagger(doc *goquery.Document) []Operation {
	var raw string
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if m := reSwaggerJSON.FindStringSubmatch(s.Text()); m != nil {
			raw = m[1]
			return false
		}
		return true
	})
	if raw == "" {
		return nil
	}
	var sd swaggerDoc
	if err := json.Unmarshal([]byte(raw), &sd); err != nil || sd.Host == "" {
		return nil
	}
	scheme := "https"
	for _, s := range sd.Schemes {
		if s == "https" {
			scheme = s
			break
		}
		scheme = s
	}
	var ops []Operation
	for path, item := range sd.Paths {
		op := item.Get
		if op == nil {
			op = item.Post
		}
		if op == nil {
			continue
		}
		name := op.Summary
		if name == "" {
			name = op.OperationID
		}
		// Path-level parameters first, then any the operation adds.
		params := append(append([]swaggerParam{}, item.Parameters...), op.Parameters...)
		o := Operation{
			Name:     cleanText(name),
			Endpoint: scheme + "://" + strings.TrimRight(sd.Host, "/") + sd.BasePath + path,
		}
		for _, p := range params {
			req := "옵션"
			if p.Required {
				req = "필수"
			}
			sample := ""
			if p.Example != nil {
				sample = cleanText(fmt.Sprint(p.Example))
			}
			o.Params = append(o.Params, Param{
				Name:     p.Name,
				Required: req,
				Sample:   sample,
				Desc:     cleanText(p.Description),
			})
		}
		ops = append(ops, o)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Endpoint < ops[j].Endpoint })
	return ops
}

// dedupeOperations drops operations that are byte-identical to one already kept.
// Some portal pages render the same 상세기능 block twice — a PC table and a mobile
// one, distinguished only by presentation classes — and counting both makes a
// one-operation dataset look like two, which then reads as an ambiguous choice a
// caller has no way to resolve (both alternatives are the same call).
//
// Only exact duplicates are collapsed: same name, same endpoint, same parameter
// names in the same order. Two operations that share an endpoint but document
// different variables are genuinely different and are both kept.
func dedupeOperations(ops []Operation) []Operation {
	if len(ops) < 2 {
		return ops
	}
	seen := make(map[string]bool, len(ops))
	out := make([]Operation, 0, len(ops))
	for _, op := range ops {
		names := make([]string, 0, len(op.Params))
		for _, p := range op.Params {
			names = append(names, p.Name)
		}
		sig := op.Name + "\x00" + op.Endpoint + "\x00" + strings.Join(names, ",")
		if seen[sig] {
			continue
		}
		seen[sig] = true
		out = append(out, op)
	}
	return out
}
