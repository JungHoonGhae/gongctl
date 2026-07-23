# CLAUDE.md — gongctl

data.go.kr(공공데이터포털)의 OpenAPI **활용신청·인증키 발급·호출을 AI 에이전트가 대신**하게 하는
Go CLI + MCP. 사람이 포털 UI를 한 번도 안 건드리게 하는 것이 핵심.

## 현재 상태 (2026-07-08)

- **브레인스토밍·설계 완료, 구현 착수 전.** 다음 단계 = writing-plans 로 구현 계획 작성.
- 설계 스펙(단일 진실): `docs/superpowers/specs/2026-07-08-gongctl-design.md` — **먼저 정독**.
- 코드는 아직 없음(스펙만). go.mod·스캐폴딩 미생성.

## 확정된 핵심 결정 (스펙 요약)

- **범위**: data.go.kr 전용, 깊게. 멀티포털(KOSIS·나라장터) 안 함.
- **호출 설계**: surface-only + 에이전트 주도. 도구는 결정적인 것만(로그인·활용신청·키 주입·HTTP·
  XML→JSON), 이질적 API 명세는 파싱하지 않고 에이전트에게 surface. (kvote 국정수행 PDF 교훈.)
- **인터페이스**: CLI(사람) + MCP(에이전트), 같은 백엔드. kvote 패턴.
- **MCP tool 5**: search_datasets · list_applications · apply · describe_api · call_api.
- **인증키**: data.go.kr은 **계정당 일반 인증키 하나**(첫 신청 시 발급). 엔드포인트별 매칭 불필요.
  Encoding/Decoding 키 함정 있음 — 잘못 쓰면 조용히 실패, 에러 힌트로 surface.
- **로그인**: 정부 SSO는 자동화 안 함. 사람이 브라우저 1회(`gongctl login`, CDP-attach 데몬).

## kvote에서 복사 이식할 것 (검증된 코드)

`~/workspace/projects/oss-k-vote-cli` 의 다음을 복사 이식(공유 라이브러리 추출 안 함 — §5):
- `internal/datagokr/*` (browser·apply·accounts·daemon·config) — **활용신청 CDP-attach 자동화의
  유일한 구현.** 세상에서 이거 가진 코드가 kvote뿐. 손 검증된 로직(SSO 트램펄린·JS 다이얼로그·
  `currentMyMenuId` 쿠키 전제) 그대로 가져올 것.
- `internal/nec` 의 datasets·openportal 검색 부분 → `internal/portal/search.go`.
- `internal/output`, `internal/version`, CLI/MCP 패턴, goreleaser·install.sh/ps1 파이프라인.
- NEC 전용 하드코딩 API(turnout/winners/elections)는 **가져오지 않음** — 범용 call_api로 대체.

## 신규로 짤 것 (스펙 §4)

- `internal/apicall/describe.go` — OpenAPI 상세페이지 → 엔드포인트·요청변수·가이드문서 surface.
- `internal/apicall/call.go` — 계정 인증키 주입 + HTTP GET + XML→JSON + 에러코드 surface.

## 경쟁 지형 (왜 만드나)

- `JeHwanYoo/data-go-kr`(CLI): 이미 받은 키로 호출만. ⭐1, 방치.
- `Koomook/data-go-mcp-servers`(MCP ⭐288): 수동 신청+키 붙여넣기, 6개 하드코딩, 신청 자동화 없음.
- **아무도 활용신청을 자동화 안 함** = gongctl 존재 이유. 차별화: "data-go-mcp인데 포털을 안 건드림."

## 주의

- `.github/workflows/` 커밋은 git 토큰 **workflow 스코프** 필요(kvote에서 겪음, 해결됨).
- 이건 fragile scraping — data.go.kr HTML 바뀌면 신청 자동화 깨짐. doctor 류 라이브 점검 필요.
- 배포: goreleaser + Homebrew tap + install.sh/ps1(kvote와 동일). Homebrew cask는 macOS 전용.
