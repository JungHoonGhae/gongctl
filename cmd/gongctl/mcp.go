package main

import (
	"context"

	"github.com/JungHoonGhae/gongctl/internal/mcpserver"
	"github.com/spf13/cobra"
)

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP 서버 실행 (stdio) — 에이전트가 검색·활용신청·호출",
		Long: `gongctl 을 Model Context Protocol 서버로 노출합니다(stdio).
search_datasets / list_applications / apply / describe_api / call_api tool 과
gongctl://guide 리소스로 에이전트가 data.go.kr 을 다룹니다. 로그인 세션 전제.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return mcpserver.Serve(context.Background(), mcpserver.Deps{
				Portal: newPortalClient(),
			})
		},
	}
}
