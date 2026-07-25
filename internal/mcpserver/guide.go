package mcpserver

// GuideDoc is the gongctl://guide resource — the agent's entry point. It states
// tool order and the Encoding/Decoding serviceKey trap.
const GuideDoc = `# gongctl — data.go.kr 사용 가이드

## 도구 사용 순서
0. **catalog_search(query)** — **탐색은 여기서 시작하라.** 로컬 카탈로그(전체 목록)를 한 번에
   훑으므로, 키워드를 하나씩 추측하며 포털에 반복 질의할 필요가 없다. 활용신청 많은 순으로
   오고 설명문은 오지 않는다(컨텍스트 절약). total 이 크면 검색어를 좁혀라.
   stale=true 면 최근 신설 API 가 누락될 수 있으니 사람에게 gongctl catalog sync 를 권하라.
1. **search_datasets(keyword)** — 포털 라이브 검색. 카탈로그에 없는 최신 항목을 확인할 때 쓴다.
   hasOpenApi=true 인 것이 API 호출 대상.
2. **list_applications()** — 이미 활용신청한 API와 그 상태·인증키 만료일을 확인.
3. **apply(pk, purpose)** — 아직 신청 안 했다면 활용신청(자동승인 개발계정 → 즉시 사용 가능).
   - 로그인 세션이 필요하다. 세션이 없으면 사람에게 ` + "`gongctl login`" + ` 을 안내하는 에러가 온다.
4. **describe_api(pk)** — 상세기능·엔드포인트·요청변수를 surface. 파라미터는 여기서 확인해 구성한다.
   - params 가 비어 있고 rawHtml 만 있으면, 표 구조가 불확실하다는 뜻 — rawHtml 을 직접 읽어라.
   - **operations 가 비어 있고 note 가 있으면**: 이 API 는 명세를 상세페이지에 싣지 않고 참고문서에만
     둔 경우다. guideDocUrl 을 직접 내려받아(zip/hwp/xlsx) 엔드포인트와 파라미터를 확인하라.
     **절대 파라미터를 추측해서 호출하지 마라** — 조용히 틀린 결과가 나온다.
5. **call_api(endpoint, params)** — 실제 호출. **key 는 넘기지 않아도 된다** — gongctl 이 로그인
   세션에서 계정 인증키를 자동으로 가져다 쓴다. 사람에게 인증키를 물어보지 마라.
   (키 값을 직접 봐야 하면 **get_api_key()**.)

즉 사람의 개입은 최초 gongctl login 한 번뿐이다. 검색 → 신청 → 승인 확인 → 호출까지
이 도구들로 스스로 끝내라.

## 인증키(serviceKey) 주의 — Encoding/Decoding 함정
- data.go.kr 은 인증키를 **Encoding / Decoding 두 형태**로 준다. 계정당 키는 하나지만 표기가 둘.
- 자동 조회되는 키는 Decoding(평문) 형태이며, call_api 가 전송 시 필요한 escape 를 처리한다.
- call_api 는 명시적으로 받은 키는 **그대로** 쓴다. 자동으로 형태를 바꾸지 않는다.
- 응답이 SERVICE_KEY_IS_NOT_REGISTERED_ERROR 이면 **키 형태 문제일 수 있다** — 다른 형태(Encoding↔Decoding)로 재시도.

## 방금 승인된 API가 403이면 — 키 문제가 아니다
- data.go.kr 은 자동승인을 즉시 처리하지만, **게이트웨이 반영에 수 분~1시간** 걸린다.
- 신청 직후 call_api 가 403(Forbidden) 이면 기다렸다 재시도하라. 키를 바꾸거나 다시 신청하지 마라.
- 키 자체의 유효성은 이미 오래전 승인된 다른 API 를 호출해 확인할 수 있다.

## 에러는 삼키지 않는다
- data.go.kr 은 HTTP 200 에 본문 에러코드(resultCode/returnReasonCode)를 담는다.
  call_api 결과의 body 를 확인해 성공(00)인지 판단하라.
`
