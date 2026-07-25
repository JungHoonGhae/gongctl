# gongctl

**data.go.kr(공공데이터포털) 활용신청·인증키 발급·API 호출을 AI 에이전트가 대신합니다.**
사람이 포털 UI를 한 번도 안 건드리고, 데이터셋 검색부터 활용신청·호출까지 CLI/MCP 한 경로로 끝냅니다.

기존 도구들은 이미 발급받은 키로 호출만 하거나(`data-go-kr` CLI), 활용신청은 여전히 사람이
포털에서 수동으로 눌러야 했습니다(`data-go-mcp`류 MCP 서버들). **gongctl은 활용신청 자체를
자동화하는 유일한 도구입니다** — "data-go-mcp인데 포털을 안 건드림."

> [!WARNING]
> 활용신청 자동화는 data.go.kr의 HTML 구조에 의존하는 fragile scraping입니다. 포털 개편 시
> 깨질 수 있습니다. 정부 SSO 로그인 자체는 자동화하지 않습니다 — 브라우저로 사람이 1회 로그인하면
> 이후 세션을 재사용합니다.

## 설치

```sh
curl -fsSL https://raw.githubusercontent.com/JungHoonGhae/gongctl/main/install.sh | sh
```

Windows:

```powershell
irm https://raw.githubusercontent.com/JungHoonGhae/gongctl/main/install.ps1 | iex
```

또는 Go가 있다면:

```sh
go install github.com/JungHoonGhae/gongctl/cmd/gongctl@latest
```

## 사용법

```sh
# 1. 브라우저가 한 번 열립니다 — data.go.kr에 로그인하세요 (SSO는 자동화하지 않음)
gongctl login

# 2. 데이터셋 검색
gongctl search 대기오염 --type api -f table

# 3. 활용신청 (계정당 인증키는 첫 신청 시 자동 발급되고 이후 재사용됩니다)
gongctl apply <PK> --purpose "대기질 분석 프로젝트" --category research

# 4. 승인 확인 및 인증키 조회
gongctl applications -f table

# 5. API 상세 스펙 확인 (요청변수·가이드문서 surface)
gongctl describe <PK>

# 6. 실제 호출 (XML 응답도 JSON으로 변환해 돌려줍니다)
gongctl call <endpoint> --param numOfRows=5   # 인증키 자동
```

```bash
# 무엇이 존재하는지 먼저 훑기 — 로컬 카탈로그(한 번 sync 후 즉시 검색)
gongctl catalog sync              # 전체 오픈API 목록 수집 (약 2~3분)
gongctl catalog search 폭염 온열   # 활용신청 많은 순, 설명문 없이 간결하게
gongctl catalog search "폭염에 취약한 고령자 데이터"   # 자연어로 그대로 물어도 됩니다
gongctl catalog search 폭염 --rest-only   # 실제로 호출 가능한(REST) 것만
gongctl catalog info               # 수집 시각 + 유형 분포
gongctl catalog orgs 폭염          # 그 주제를 개방한 기관 순위
```

> 포털은 키워드 검색만 제공합니다. 카탈로그가 없으면 "이런 데이터가 있나?"를 확인하려면
> 검색어를 하나씩 추측해볼 수밖에 없고, 못 찾았을 때 *없는 것*인지 *단어가 틀린 것*인지
> 알 수 없습니다. `catalog`는 전체를 로컬에 두고 한 번에 훑습니다.
> `doctor`가 카탈로그가 오래됐는지도 함께 점검합니다.
>
> **오픈API의 약 40%(11,932개 중 4,770개)는 `LINK` 유형**으로, 포털에 명세가 없고 제공기관
> 사이트로만 연결됩니다. 즉 `describe`가 엔드포인트를 줄 수 없어 신청해도 호출할 수 없습니다.
> 카탈로그는 각 항목의 유형을 표시하고, `--rest-only`로 걸러낼 수 있습니다 — 그냥 인기순으로
> 고르면 데드엔드에 활용신청을 쓰게 됩니다(`폭염` 검색 2위가 LINK입니다).
>
> 검색어는 문장으로 써도 됩니다 — 조사(`~에서`, `~으로`)와 군더더기(`데이터`, `알려줘`)는
> 걸러집니다. 모든 단어를 포함하는 결과가 없으면 조용히 0건을 주는 대신 일부만 일치하는
> 것까지 보여주고, 그렇게 넓혔다는 사실을 `relaxed`로 알려줍니다.

```bash
# 호출 — 엔드포인트 URL 을 타이핑하지 않습니다
gongctl call --pk 15077974 --param pageNo=1 --param numOfRows=3 --param type=xml
```

> **승인 조건은 `describe`의 `approval`에 나옵니다.** 포털은 개발단계와 운영단계를 따로
> 심의하는데, gongctl이 쓰는 개발계정 경로는 조사한 데이터셋에서 사실상 모두 자동승인이었습니다
> (무작위 90건 표본에 개발단계 심의는 0건). 운영단계는 약 1/3이 심의승인이므로, 나중에 상용으로
> 옮길 계획이면 미리 확인할 값입니다. 그 행이 없는 데이터셋(대개 LINK)은 자동승인으로
> 가정하지 않고 `approval` 없음으로 보고합니다.
>
> 엔드포인트 경로는 추측할 수 없는 형태(`HeatWaveCasualtiesRegion/getHeatWaveCasualtiesRegionList`)이고,
> 틀리면 "없다"가 아니라 404·500이 옵니다. `--pk` 를 주면 포털에서 조회하고, **명세의 필수
> 요청변수가 빠졌는지 호출 전에 확인**합니다 — 빠진 채로 부르면 data.go.kr은 에러가 아니라
> 빈 결과를 주기 때문에 데이터가 없는 것과 구분할 수 없습니다.

```bash
# 계정 인증키 조회 (call 은 생략 시 자동으로 이 키를 씁니다)
gongctl key

# 스크래핑이 아직 살아있는지 점검 (data.go.kr HTML 변경 감지, CI용 exit 1)
gongctl doctor -f table
```

> **사람의 개입은 `gongctl login` 한 번뿐입니다.** 검색 → 활용신청 → 승인 확인 → 인증키 획득 →
> 호출까지 에이전트가 스스로 끝냅니다. 인증키를 사람이 복사해 붙여넣을 필요가 없습니다.
>
> **신청 직후 403은 정상입니다.** 승인은 즉시 끝나지만 게이트웨이 반영에 시간이 걸립니다 —
> 실측 **7~10분**, 포털 안내상 최대 1시간. `list_applications`에 '승인'으로 보여도 아직
> 호출이 안 될 수 있습니다. 1~2분 간격으로 재시도하면 되고, 키를 바꾸거나 다시 신청할 필요는
> 없습니다. 여러 개를 쓸 계획이면 **먼저 다 신청해두고 함께 기다리는 편이 빠릅니다.**

`gongctl status` / `gongctl logout` / `gongctl version`도 있습니다.

## MCP 서버로 쓰기

`gongctl mcp`는 stdio MCP 서버로 동작하며 5개 도구(`search_datasets`, `list_applications`,
`apply`, `describe_api`, `call_api`)를 노출합니다. 에이전트가 API 명세를 직접 읽고 호출을
판단하도록 surface-only로 설계되어 있습니다 — gongctl이 API 의미를 대신 해석하지 않습니다.

Claude Desktop / 기타 MCP 클라이언트 설정 예시:

```json
{
  "mcpServers": {
    "gongctl": {
      "command": "gongctl",
      "args": ["mcp"]
    }
  }
}
```

## 보안 주의

`gongctl login`은 브라우저 창을 한 번 띄웁니다(정부 SSO는 자동화하지 않습니다). 로그인이 확인되면
gongctl이 세션 쿠키를 복사해 저장하고 **그 브라우저를 종료합니다** — 이후 명령은 창 없이 동작합니다.

**동작 방식**

- **읽기**(`applications`·`key`·`search`·`describe`·`doctor`)는 브라우저 없이 순수 HTTP로 처리됩니다.
- **활용신청 제출**(`apply`)만 브라우저가 필요합니다(포털 폼의 검증 로직을 그대로 구동 — ADR 0001).
  이때 **headless** Chrome을 잠깐 띄워 저장된 세션을 주입하고, 끝나면 정리합니다.
- Chrome은 `--remote-allow-origins`를 **설정하지 않고** 실행됩니다. 악성 웹페이지가 보내는
  Origin 헤더 붙은 WebSocket 연결은 Chrome이 403으로 거부합니다(스파이크로 검증). chromedp는
  Origin 없이 붙으므로 재부착에는 영향이 없습니다.
- 인증키는 로그·파일에 남기지 않습니다. 전송 실패 에러 메시지에서도 마스킹합니다.

**디스크에 저장되는 것** (`~/.config/gongctl`, 디렉터리 `0700`)

| 파일 | 내용 | 권한 |
| --- | --- | --- |
| `datagokr-session.json` | data.go.kr 세션 쿠키 | `0600` |
| `datagokr-apikey` | 계정 인증키(serviceKey) | `0600` |
| `chrome-profile/` | 로그인용 Chrome 프로파일 | `0700` |
| `chrome-headless/` | 신청 제출용 headless 프로파일 | `0700` |

**`gongctl logout`은 위 네 가지를 모두 삭제합니다.** 로그인 프로파일에는 사람이 로그인에 사용한
SSO 제공자(네이버 등)의 쿠키도 함께 쌓이기 때문에, 쿠키 파일만 지우는 것으로는 충분하지 않습니다.
작업이 끝나면 `logout`을 실행하세요.

**남는 위험 — 알고 쓰세요**

- **평문 저장**: 위 파일들은 `0600`이지만 암호화되지 않습니다. 같은 사용자 권한으로 실행되는
  악성 프로그램이나 백업 사본은 읽을 수 있습니다.
- **로컬 CDP 포트**: `login`과 `apply` 동안 Chrome이 `127.0.0.1`의 디버깅 포트를 엽니다.
  Chrome의 Origin 검사는 *브라우저에서 오는* 연결만 막으므로, **같은 머신의 다른 프로세스**는
  그 창이 살아 있는 동안 붙을 수 있습니다. 노출 구간은 짧지만 0은 아닙니다 —
  공용/다중 사용자 머신에서는 사용을 피하세요.
- **MCP `apply`는 사람 확인 없이 실제 신청을 생성합니다**(에이전트 주도가 목적). 에이전트는
  포털에서 읽은 텍스트를 근거로 행동하므로, 프롬프트 인젝션으로 원치 않는 신청이 만들어질
  수 있습니다. 신청은 포털에서 취소할 수 있고 파괴적이지 않지만, **계정에 실제 변경을 남깁니다.**
  민감한 계정에서는 MCP 대신 CLI(`apply`는 기본적으로 y/N 확인)를 쓰세요.
- **인증키는 계정 단위**입니다. 유출되면 승인된 모든 API가 함께 노출됩니다.

## 라이선스

[MIT](LICENSE)
