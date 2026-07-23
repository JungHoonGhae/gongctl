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

## 라이선스

[MIT](LICENSE)
