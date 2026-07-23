// Command gongctl automates data.go.kr (공공데이터포털): dataset search, OpenAPI
// 활용신청, and authenticated API calls — driven from a CLI or, for AI agents,
// an MCP server. It exists so agents never have to touch the portal UI.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
