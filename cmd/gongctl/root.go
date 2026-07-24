package main

import (
	"time"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

var (
	flagFormat  string
	flagDelay   time.Duration
	flagBaseURL string
)

var rootCmd = &cobra.Command{
	Use:   "gongctl",
	Short: "data.go.kr(공공데이터포털) 자동화 — 검색·활용신청·API 호출 (CLI + MCP)",
	Long: `gongctl — 공공데이터포털(data.go.kr)의 OpenAPI 활용신청·인증키·호출을 자동화합니다.

사람은 브라우저에서 한 번만 로그인(gongctl login)하면, 이후 검색·활용신청·호출을
CLI 또는 MCP(에이전트)로 처리합니다. 포털 UI를 다시 건드릴 필요가 없습니다.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&flagFormat, "format", "f", "json", "출력 형식: json | jsonl | table")
	pf.DurationVar(&flagDelay, "delay", fetch.DefaultDelay, "요청 간 최소 간격 (rate limit)")
	pf.StringVar(&flagBaseURL, "base-url", "", "포털 base URL 재정의 (테스트용)")

	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(loginCmd(), logoutCmd(), statusCmd())
	rootCmd.AddCommand(searchCmd(), describeCmd(), callCmd())
	rootCmd.AddCommand(applyCmd(), applicationsCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(mcpCmd())
}

// resolveFormat parses the global --format flag into an output.Format.
func resolveFormat() (output.Format, error) {
	return output.Parse(flagFormat)
}

// newFetchClient builds the shared HTTP transport from the global flags.
func newFetchClient() *fetch.Client {
	return fetch.New(fetch.WithDelay(flagDelay))
}

// newPortalClient builds a portal search client over the shared transport.
func newPortalClient() *portal.Client {
	opts := []portal.Option{}
	if flagBaseURL != "" {
		opts = append(opts, portal.WithBaseURL(flagBaseURL))
	}
	return portal.New(newFetchClient(), opts...)
}
