# gongctl 설계 — data.go.kr을 AI 에이전트가 신청·호출하게 하는 오픈소스

작성일: 2026-07-08
상태: 승인됨 (브레인스토밍 완료, 구현 계획 대기)

## 1. 목표와 차별화

한국 공공데이터포털(data.go.kr)의 OpenAPI는 데이터마다 **활용신청 → 승인 → 인증키 발급**을
사람이 UI로 일일이 해야 한다. AI 에이전트는 이 UI 벽 때문에 공공 데이터에 스스로 닿지 못한다.

`gongctl`은 그 벽을 없앤다. 에이전트가 "이런 데이터 필요해" 하면 검색 → (필요 시) 활용신청·
자동승인·키 발급 → 임의 OpenAPI 호출 → 구조화 데이터까지, **사람의 포털 UI 조작 0**으로 잇는다.

### 1.1 경쟁 지형 (2026-07 조사)

| 기존 도구 | 하는 일 | 한계 |
|---|---|---|
| `JeHwanYoo/data-go-kr` (CLI) | 이미 받은 키로 호출만 | 신청 자동화·MCP 없음, ⭐1, 2023 방치 |
| `Koomook/data-go-mcp-servers` (MCP, ⭐288) | data.go.kr API를 에이전트에 연결 | ① 사용자가 직접 신청·키 발급해 env에 붙여넣어야 함 ② 6개 API만 하드코딩 ③ 신청 자동화 전혀 없음 |

**아무도 안 하는 것 = gongctl의 존재 이유**: 활용신청·키 발급 자동화 + 임의 API 호출.
⭐288 MCP의 존재는 수요를 증명하며, 정확히 gongctl이 부수는 벽에서 멈춰 있다. 차별화 한 줄:
*"data-go-mcp인데, 포털을 한 번도 안 건드리는 버전."*

### 1.2 설계 원칙 (kvote에서 검증된 것)

- **결정적인 것만 도구가, 이질적인 것은 에이전트가 (surface-only).** 로그인·활용신청·키 주입·
  HTTP 호출은 결정적 → 도구가 자동화. API 명세(엔드포인트·파라미터)는 수천 개가 제각각이고
  대부분 첨부 문서(hwp/pdf) → **파싱하지 않고 surface**, 에이전트가 읽고 파라미터 구성.
  (kvote가 국정수행 PDF에서 얻은 교훈: 이질적 명세 파싱은 조용히 틀린다.)
- **키리스 정신의 확장**: 사용자가 인증키를 미리 준비할 필요 없음. 도구가 신청해서 받아온다.
- **탐지 회피 안 함**: CDP-attach(실제 Chrome)로 사용자 세션에 붙는다. 봇 우회가 아니라
  사용자가 이미 로그인한 브라우저를 재사용.

## 2. 아키텍처

kvote에서 검증된 **CLI(사람) + MCP(에이전트), 같은 백엔드** 패턴.

```
cmd/gongctl/         CLI (cobra)
  root.go            전역 플래그, 클라이언트 빌더
  auth.go            login / logout / status
  data.go            search / describe / call
  apply.go           apply / applications
  mcp.go             mcp (stdio 서버 실행)
internal/portal/     data.go.kr 자동화 — kvote internal/datagokr 에서 복사 이식
  browser.go         Login (CDP-attach 데몬, 세션 유지), Applications, Logout
  apply.go           활용신청 폼 자동 제출 (SSO 트램펄린·JS 다이얼로그·쿠키 전제 처리)
  accounts.go        신청 목록 파싱 (상태·인증키·만료일·uddi)
  daemon.go          Chrome 실행·WS 발견·상태 저장 (unix/windows 분기)
  search.go          데이터셋/OpenAPI 검색 (datasets + 개방포털)
  config.go          설정
internal/apicall/    신규 — 범용 인증 호출 (surface-only)
  describe.go        OpenAPI 상세페이지 → 엔드포인트·요청변수·가이드문서 surface
  call.go            인증키 주입 + HTTP GET + XML→JSON 정규화 + 에러 surface
internal/mcpserver/  MCP (stdio, modelcontextprotocol/go-sdk)
  server.go          조립 + tool 등록 + 리소스
internal/output/     json / jsonl / table 렌더러 (kvote 에서 이식)
internal/version/    ldflags 주입 버전
```

## 3. MCP tool 표면 (에이전트가 오케스트레이션)

kvote처럼 discrete tools — 에이전트가 조합. 단일 mega-tool 지양(숨기면 깨진다).

| tool | 입력 | 동작 |
|---|---|---|
| `search_datasets` | keyword | 데이터셋 검색 → publicDataPk·제목·OpenAPI 여부·제공기관 |
| `list_applications` | (없음) | 내 활용신청 목록 → 상태·**인증키**·만료일 |
| `apply` | pk, purpose | 활용신청 제출 (자동승인 개발계정 → 즉시 키). 에러 시 tool-level 반환 |
| `describe_api` | pk | OpenAPI 상세: 상세기능별 엔드포인트·요청변수 표·가이드문서를 surface (파싱 아님) |
| `call_api` | endpoint, params(map) | 계정 인증키 자동 주입 → GET → XML→JSON → {status, body} 반환 |

- 리소스 `gongctl://guide` — tool 사용 순서와 인증키 Encoding/Decoding 주의를 담은 안내(에이전트가
  먼저 읽는 진입점). kvote의 `kvote://schema` 패턴.
- 로그인은 MCP tool로 노출하지 않는다 — 사람이 브라우저에서 1회(`gongctl login`). MCP는 이미
  로그인된 세션을 전제. (미로그인 시 apply/list가 명확한 안내 에러 반환.)

## 4. 신규 컴포넌트 상세

### 4.1 describe_api (`internal/apicall/describe.go`)

- `func Describe(ctx, pk string) (*APISpec, error)` — OpenAPI 상세페이지(`/data/{pk}/openapi.do`)를
  스크래핑.
- `type APISpec struct { PublicDataPk, DataName string; Operations []Operation; GuideDoc string }`
- `type Operation struct { Name, Endpoint string; Params []Param; RawHTML string }`
- `type Param struct { Name, Required, Sample, Desc string }`
- **surface-only 계약**: 요청변수 표가 HTML에서 깨끗이 잡히면 `Params`를 채우고, 구조가 불확실하면
  `RawHTML`(해당 섹션 원문)만 채운다. 절대 없는 파라미터를 지어내지 않는다. 가이드 문서(hwp/pdf)는
  `GuideDoc`에 다운로드 링크만 — 파싱하지 않는다.

### 4.2 call_api (`internal/apicall/call.go`)

- `func Call(ctx context.Context, endpoint string, params map[string]string, key string) (*CallResult, error)`
- `type CallResult struct { Status int; ContentType string; Body any }` — Body는 XML이면 JSON으로
  변환한 map, JSON이면 그대로, 그 외는 문자열.
- **인증키 주입**: `serviceKey` 쿼리 파라미터로 주입. data.go.kr은 **계정당 일반 인증키 하나**를
  발급하며(첫 활용신청 시 발급, 이후 모든 승인 API에 공통), 호출부(CLI/MCP)가 그 계정 키를
  전달한다. 엔드포인트별 키 매칭이 필요 없다. (kvote가 `KVOTE_DATAGOKR_KEY` 단일 env를 쓰는 이유.)
- **Encoding/Decoding 키 함정**: data.go.kr은 인증키를 Encoding/Decoding 두 형태로 준다. call은
  전달받은 키를 그대로 쓰되, `SERVICE_KEY_IS_NOT_REGISTERED_ERROR` 응답을 만나면 **키 형태 문제일
  수 있다는 힌트를 에러 메시지에 포함**해 surface(자동 재시도로 조용히 틀리지 않음).
- **에러 surface**: data.go.kr은 HTTP 200에 본문 에러코드를 담는다. `resultCode`/`returnReasonCode`가
  성공(00)이 아니면 CallResult에 그대로 담아 반환 — 삼키지 않는다.

## 5. kvote 이식

- kvote `internal/datagokr/*`(browser·apply·accounts·daemon·config)와 검색(`internal/nec`의
  datasets·openportal 부분), `internal/output`을 **복사 이식**한다. 공유 라이브러리 추출은 두 repo
  릴리즈 결합 비용이 커 지금은 과함. 성숙한 스냅샷 복사 → drift가 문제되면 그때 추출.
- 이식 시 kvote-특화 명명(nec/kvote)을 gongctl 중립 명명으로 정리. NEC 전용 하드코딩 API 래퍼
  (turnout/winners/elections)는 가져오지 않는다 — gongctl은 범용 call_api로 대체.

## 6. 에러 처리

- 미로그인: apply/list/describe가 `ErrNotLoggedIn` → "gongctl login 먼저" 안내.
- 활용신청 폼 접근 실패(이미 신청/신청 불가): 명확한 메시지 + pk.
- API 호출 에러코드: 삼키지 않고 CallResult로 surface(§4.2).
- MCP tool은 모든 실패를 tool-level 에러 결과(errResult)로 — 세션을 죽이지 않는다.

## 7. 테스트 (네트워크 없음)

- `internal/portal`: kvote에서 검증된 파서 테스트 이식(accounts 파싱 등, testdata HTML 픽스처).
- `internal/apicall`:
  - describe: 실 상세페이지에서 뽑은 HTML 픽스처로 엔드포인트·요청변수 추출 + surface 폴백 검증.
  - call: XML→JSON 변환, serviceKey 주입, 에러코드 surface를 httptest 서버로 검증.
- `internal/mcpserver`: in-memory transport로 tool 왕복(빈/픽스처 응답).
- **라이브 검증(구현 최종)**: 실제 로그인 → 임의 데이터셋 apply → describe → call end-to-end
  (kvote가 이미 쓰는 data.go.kr 계정 사용).

## 8. 배포

- Go 단일 바이너리(cgo 없음 지향), MIT. macOS/Linux/Windows.
- goreleaser + GitHub Release + Homebrew tap(kvote와 동일 파이프라인). install.sh/ps1 원라이너.
- **주의**: `.github/workflows/` 커밋은 git 토큰 workflow 스코프 필요(kvote에서 겪음).

## 9. 하지 않는 것 (YAGNI)

- 멀티포털(KOSIS·나라장터·지자체) — data.go.kr 하나에 집중. 각 포털은 별도 리버스 엔지니어링 treadmill.
- API 명세(가이드 문서) 파싱 — surface만. 에이전트가 읽는다.
- 심의(수동승인) API의 비동기 대기 폴링 — v1은 자동승인 개발계정 중심. 심의건은 `list_applications`
  상태로 노출하고 승인 후 재호출은 사용자/에이전트가.
- 공유 라이브러리 추출 — 지금은 복사(§5).
- 로그인 자동화(정부 SSO) — 사람이 브라우저에서 1회.
