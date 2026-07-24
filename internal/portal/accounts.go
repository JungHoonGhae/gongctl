package portal

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Application is one OpenAPI 활용신청 (dev-account) the user holds, as listed on
// the 활용신청 현황 page. ExpiresAt (만료예정일) is the field the user most cares
// about — gongctl surfaces it so renewals can be tracked.
type Application struct {
	Title     string `json:"title"`     // 데이터명 (상태 접두사 제거)
	Status    string `json:"status"`    // 승인 / 신청 등 ([..] 접두사에서)
	Org       string `json:"org"`       // 제공기관
	Category  string `json:"category"`  // 분류
	Account   string `json:"account"`   // 계정 (개발/운영)
	AppliedAt string `json:"appliedAt"` // 신청일
	ExpiresAt string `json:"expiresAt"` // 만료예정일
	UDDI      string `json:"uddi"`      // 상세조회 uddi
	DetailPk  string `json:"detailPk"`  // publicDataDetailPk
}

var reFnDetail = regexp.MustCompile(`fn_detail\('([^']*)','([^']*)'`)
var reStatusPrefix = regexp.MustCompile(`^\[([^\]]+)\]\s*`)

// AccountListPath is the 활용신청 현황 page driven by the browser session.
const AccountListPath = "/iim/api/selectAcountList.do"

// parseApplications extracts the 활용신청 현황 list items from the page HTML.
// Split out as a pure function so it can be tested against a fixture without a
// live session.
func parseApplications(body string) ([]Application, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	var apps []Application
	doc.Find(".mypage-dataset-list > ul > li").Each(func(_ int, li *goquery.Selection) {
		a := Application{
			Category: strings.TrimSpace(li.Find(".tag-area .labelset.brown").First().Text()),
			Org:      strings.TrimSpace(li.Find(".tag-area .labelset.red").First().Text()),
		}

		title := strings.TrimSpace(li.Find(".title-area .title").First().Text())
		title = strings.Join(strings.Fields(title), " ") // collapse the markup whitespace
		if m := reStatusPrefix.FindStringSubmatch(title); m != nil {
			a.Status = m[1]
			title = reStatusPrefix.ReplaceAllString(title, "")
		}
		a.Title = title

		if href, ok := li.Find(".title-area a").First().Attr("href"); ok {
			if m := reFnDetail.FindStringSubmatch(href); m != nil {
				a.UDDI = m[1]
				a.DetailPk = m[2]
			}
		}

		// info-data: 계정 / 신청일 / 만료예정일 — label(.tit) → value(.data) 쌍.
		li.Find(".info-data p").Each(func(_ int, p *goquery.Selection) {
			label := strings.TrimSpace(p.Find(".tit").Text())
			value := strings.TrimSpace(p.Find(".data").Text())
			switch label {
			case "계정":
				a.Account = value
			case "신청일":
				a.AppliedAt = value
			case "만료예정일":
				a.ExpiresAt = value
			}
		})

		if a.Title != "" {
			apps = append(apps, a)
		}
	})
	return apps, nil
}
