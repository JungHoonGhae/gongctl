package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// 목적분류(prcusePrpos) 코드. 자동승인 개발계정 신청의 활용목적 카테고리.
const (
	PurposeWeb      = "PROS01" // 웹 사이트 개발
	PurposeApp      = "PROS02" // 앱개발
	PurposeEtc      = "PROS03" // 기타
	PurposeRef      = "PROS04" // 참고자료
	PurposeResearch = "PROS05" // 연구(논문 등)
)

// ApplySummary is shown to the user for confirmation before submitting.
type ApplySummary struct {
	PublicDataPk string `json:"publicDataPk"`
	DataName     string `json:"dataName"`
	Operations   int    `json:"operations"` // 신청할 상세기능 수
	Category     string `json:"category"`   // 목적분류 코드
	Purpose      string `json:"purpose"`    // 활용목적 내용
}

// ApplyResult reports the outcome.
type ApplyResult struct {
	Submitted bool   `json:"submitted"`
	Message   string `json:"message"`
}

// Apply fills and submits a data.go.kr OpenAPI 활용신청 (auto-approved dev
// account) for one publicDataPk, using the live browser session. It is
// deliberately single-pk and purpose-required: applications are account-scoped,
// account-mutating actions, so gongctl never bulk-applies speculatively. confirm
// is called with the filled summary; submission happens only if it returns true.
//
// The form's own fn_save() performs validation and POSTs — gongctl drives the
// portal's real logic rather than re-implementing the request, so it stays
// robust to field changes. Any validation alert() is captured as the failure.
func Apply(ctx context.Context, pk, purpose, category string, confirm func(ApplySummary) bool) (*ApplyResult, error) {
	if strings.TrimSpace(purpose) == "" {
		return nil, fmt.Errorf("활용목적이 필요합니다 (--purpose)")
	}
	if category == "" {
		category = PurposeResearch
	}
	st, err := loadState()
	if err != nil {
		return nil, err
	}
	if !wsAlive(st.Port) {
		return nil, ErrNotLoggedIn
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, st.WebSocketURL)
	defer cancelAlloc()
	tctx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()
	tctx, tcancel := context.WithTimeout(tctx, 60*time.Second)
	defer tcancel()

	// Capture any JS dialog (validation alert / success notice) and accept it.
	var dialogMu sync.Mutex
	var lastDialog string
	chromedp.ListenTarget(tctx, func(ev any) {
		if d, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			dialogMu.Lock()
			lastDialog = d.Message
			dialogMu.Unlock()
			go chromedp.Run(tctx, page.HandleJavaScriptDialog(true))
		}
	})

	// 워밍업: 새 탭의 www 세션을 먼저 인증 상태로 만든다(SSO 트램펄린 완료).
	// 이걸 안 하면 폼 진입의 첫 네비게이션이 트램펄린을 타며 리다이렉트를 잃는다.
	// tctx 직접 사용(자식 타임아웃 컨텍스트 취소가 탭을 닫는 것을 회피).
	chromedp.Run(tctx, chromedp.Navigate(BaseURL+AccountListPath))
	warmDeadline := time.Now().Add(15 * time.Second)
	var warmLoc string
	for time.Now().Before(warmDeadline) {
		chromedp.Run(tctx, chromedp.Location(&warmLoc))
		if warmLoc != "" && !strings.Contains(warmLoc, "/sso/profile.do") {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	if strings.Contains(warmLoc, "common-login") || strings.Contains(warmLoc, "auth.data.go.kr") {
		return nil, ErrNotLoggedIn
	}

	// 신청 폼 진입: currentMyMenuId 쿠키가 전제조건(없으면 index.do 로 튕김).
	formURL := fmt.Sprintf("%s/tcs/dss/redirectDevAcountRequestForm.do?publicDataPk=%s&isBusinessApply=N", BaseURL, pk)
	var loc string
	if err := chromedp.Run(tctx,
		network.SetCookie("currentMyMenuId", "M020105").WithDomain("www.data.go.kr").WithPath("/"),
		chromedp.Navigate(formURL),
	); err != nil {
		return nil, err
	}
	// settle: 폼(selectDevAcountRequestForm) 또는 index.do 로 안착할 때까지.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		chromedp.Run(tctx, chromedp.Location(&loc))
		if strings.Contains(loc, "selectDevAcountRequestForm.do") || strings.Contains(loc, "index.do") {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	if strings.Contains(loc, "common-login") || strings.Contains(loc, "auth.data.go.kr") {
		return nil, ErrNotLoggedIn
	}
	if strings.Contains(loc, "index.do") || !strings.Contains(loc, "selectDevAcountRequestForm.do") {
		return nil, fmt.Errorf("신청 폼에 접근하지 못했습니다 (pk=%s) — 이미 신청했거나 신청 불가한 데이터일 수 있습니다", pk)
	}

	// 폼 채우기 + 요약 추출 (페이지의 jQuery 사용).
	fillJS := `(function(purpose, cat){
		var f = document.getElementById('reqForm');
		if(!f) return JSON.stringify({err:'no-form'});
		var r = document.querySelector("input[name='prcusePrpos'][value='"+cat+"']");
		if(r){ r.checked = true; }
		var ta = document.getElementById('prcusePurps');
		if(ta){ ta.value = purpose; }
		var n = 0;
		document.querySelectorAll('.col-table input[type=checkbox]').forEach(function(o){
			if(!o.classList.contains('all-chk')){ o.checked = true; n++; }
		});
		var ag = document.getElementById('useScopeAgreAt');
		if(ag){ ag.checked = true; }
		var name = '';
		var tag = document.querySelector('.tagset');
		if(tag){ var box = tag.closest('div'); var t = box && box.querySelector('.tit'); if(t){ name = t.textContent.replace(/\s+/g,' ').trim(); } }
		return JSON.stringify({ops:n, name:name});
	})(` + strconv.Quote(purpose) + `,` + strconv.Quote(category) + `)`

	var raw string
	if err := chromedp.Run(tctx, chromedp.Evaluate(fillJS, &raw)); err != nil {
		return nil, fmt.Errorf("폼 채우기 실패: %w", err)
	}
	var filled struct {
		Ops  int    `json:"ops"`
		Name string `json:"name"`
		Err  string `json:"err"`
	}
	json.Unmarshal([]byte(raw), &filled)
	if filled.Err != "" {
		return nil, fmt.Errorf("신청 폼 구조를 찾지 못했습니다 (%s)", filled.Err)
	}

	summary := ApplySummary{
		PublicDataPk: pk,
		DataName:     filled.Name,
		Operations:   filled.Ops,
		Category:     category,
		Purpose:      purpose,
	}
	if confirm != nil && !confirm(summary) {
		return &ApplyResult{Submitted: false, Message: "사용자가 취소함 (제출 안 함)"}, nil
	}

	// 제출: 폼의 fn_save() 가 검증 → confirm("신청하시겠습니까?") → AJAX POST.
	// confirm/완료 알림은 위 dialog 리스너가 모두 수락한다.
	if err := chromedp.Run(tctx, chromedp.Evaluate(`(function(){ try{ fn_save(); return 'ok'; }catch(e){ return ''+e; } })()`, nil)); err != nil {
		return nil, fmt.Errorf("제출 호출 실패: %w", err)
	}
	time.Sleep(4 * time.Second) // confirm 수락 + POST + 처리 대기

	// 성공 판정 = 목록(ground truth)에 반영됐는지. 폼은 AJAX 제출이라 위치로는
	// 판별이 안 되므로 활용신청 현황을 다시 읽어 데이터명이 나타났는지 확인한다.
	dialogMu.Lock()
	dlg := lastDialog
	dialogMu.Unlock()

	if listHTML, _, lerr := probeListLenient(tctx); lerr == nil {
		if apps, perr := parseApplications(listHTML); perr == nil {
			for _, a := range apps {
				if filled.Name != "" && strings.Contains(a.Title, strings.TrimSpace(filled.Name)) {
					return &ApplyResult{Submitted: true, Message: "신청 완료 (자동승인): " + a.Status}, nil
				}
			}
		}
	}
	// 목록에 없으면 거부(검증 실패 등). dialog 메시지를 사유로.
	msg := "제출이 반영되지 않았습니다"
	if dlg != "" && !strings.Contains(dlg, "신청하시겠습니까") {
		msg += ": " + strings.ReplaceAll(dlg, "\n", " ")
	}
	return &ApplyResult{Submitted: false, Message: msg}, nil
}

// probeListLenient navigates the reused tab to the 활용신청 현황 list and returns
// its HTML, settling past the SSO trampoline.
func probeListLenient(tctx context.Context) (html, loc string, err error) {
	if e := chromedp.Run(tctx, chromedp.Navigate(BaseURL+AccountListPath)); e != nil {
		return "", "", e
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		chromedp.Run(tctx, chromedp.Location(&loc))
		if loc != "" && !strings.Contains(loc, "/sso/profile.do") {
			if chromedp.Run(tctx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)) == nil && len(html) > 1000 {
				return html, loc, nil
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	return html, loc, nil
}
