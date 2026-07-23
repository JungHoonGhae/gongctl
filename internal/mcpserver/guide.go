package mcpserver

// GuideDoc is the gongctl://guide resource — the agent's entry point. It states
// tool order and the Encoding/Decoding serviceKey trap.
const GuideDoc = `# gongctl — data.go.kr 사용 가이드

## 도구 사용 순서
1. **search_datasets(keyword)** — 데이터셋을 찾는다. hasOpenApi=true 인 것이 API 호출 대상.
2. **list_applications()** — 이미 활용신청한 API와 그 상태·인증키 만료일을 확인.
3. **apply(pk, purpose)** — 아직 신청 안 했다면 활용신청(자동승인 개발계정 → 즉시 사용 가능).
   - 로그인 세션이 필요하다. 세션이 없으면 사람에게 ` + "`gongctl login`" + ` 을 안내하는 에러가 온다.
4. **describe_api(pk)** — 상세기능·엔드포인트·요청변수를 surface. 파라미터는 여기서 확인해 구성한다.
   - params 가 비어 있고 rawHtml 만 있으면, 표 구조가 불확실하다는 뜻 — rawHtml 을 직접 읽어라.
   - guideDoc(참고문서)는 링크만 준다. 필요하면 사람에게 열어보게 한다.
5. **call_api(endpoint, params, key)** — 실제 호출. key 는 계정 인증키(serviceKey) 하나로 공통.

## 인증키(serviceKey) 주의 — Encoding/Decoding 함정
- data.go.kr 은 인증키를 **Encoding / Decoding 두 형태**로 준다. 계정당 키는 하나지만 표기가 둘.
- call_api 는 받은 키를 **그대로** 쓴다. 자동으로 형태를 바꾸지 않는다.
- 응답이 SERVICE_KEY_IS_NOT_REGISTERED_ERROR 이면 **키 형태 문제일 수 있다** — 다른 형태(Encoding↔Decoding)로 재시도.

## 에러는 삼키지 않는다
- data.go.kr 은 HTTP 200 에 본문 에러코드(resultCode/returnReasonCode)를 담는다.
  call_api 결과의 body 를 확인해 성공(00)인지 판단하라.
`
