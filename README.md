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
gongctl call <endpoint> --key <인증키> --param numOfRows=5
```

```bash
# 스크래핑이 아직 살아있는지 점검 (data.go.kr HTML 변경 감지, CI용 exit 1)
gongctl doctor -f table
```

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

- **읽기**(`applications`·`search`·`describe`·`doctor`)는 브라우저 없이 순수 HTTP로 처리됩니다.
- **활용신청 제출**(`apply`)만 브라우저가 필요합니다(포털 폼의 검증 로직을 그대로 구동 — ADR 0001).
  이때 gongctl은 **headless** Chrome을 잠깐 띄워 저장된 세션을 주입하고, 끝나면 정리합니다.
  화면에는 아무것도 보이지 않습니다.
- 세션 쿠키는 `~/.config/gongctl/datagokr-session.json`(0600)에 저장됩니다 — 자격증명이므로
  작업이 끝나면 `gongctl logout`으로 지우세요. 포털 세션이 만료되면 `login`을 다시 하면 됩니다.
- 쿠키만으로 인증이 유지되지 않는 경우에는 브라우저를 열어 둔 채 CDP로 동작하도록 자동 폴백합니다
  (`--keep-browser`로 강제할 수도 있습니다).
- CDP 포트는 loopback 전용이며 `--remote-allow-origins`를 **설정하지 않습니다** — 악성 웹페이지의
  Origin 붙은 WebSocket 연결은 Chrome이 403으로 거부합니다(`proto/cdp-origin` 스파이크로 검증).
- 인증키는 로그·파일에 남기지 않습니다(전송 실패 에러 메시지에서도 마스킹).

## 라이선스

[MIT](LICENSE)
