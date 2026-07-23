package main

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func applyCmd() *cobra.Command {
	var purpose, category string
	var yes bool
	c := &cobra.Command{
		Use:   "apply <publicDataPk>",
		Short: "OpenAPI 활용신청 (자동승인) — 목적 필수, 제출 전 확인",
		Long: `data.go.kr OpenAPI 1건의 활용신청을 자동 제출합니다(자동승인). 신청은
계정에 실제 신청을 생성하므로 **한 번에 한 건만** 처리하고 **활용목적(--purpose)을
반드시 요구**하며 제출 전 확인합니다 (투기적 대량신청 금지).

예) gongctl apply 15000908 --purpose "선거 데이터 분석" --category research`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			cat := mapCategory(category)
			cfg, _ := portal.LoadConfig()
			confirm := func(s portal.ApplySummary) bool {
				if yes || (cfg != nil && cfg.AutoApply) {
					return true
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "\n다음 내용으로 활용신청을 제출합니다:\n")
				fmt.Fprintf(cmd.ErrOrStderr(), "  데이터: %s (pk=%s)\n", s.DataName, s.PublicDataPk)
				fmt.Fprintf(cmd.ErrOrStderr(), "  상세기능: %d개  목적분류: %s\n", s.Operations, s.Category)
				fmt.Fprintf(cmd.ErrOrStderr(), "  활용목적: %s\n", s.Purpose)
				fmt.Fprint(cmd.ErrOrStderr(), "제출할까요? [y/N]: ")
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				line = strings.ToLower(strings.TrimSpace(line))
				return line == "y" || line == "yes"
			}
			res, err := portal.Apply(cmd.Context(), args[0], purpose, cat, confirm)
			if err != nil {
				if errors.Is(err, portal.ErrNotLoggedIn) {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					return nil
				}
				return err
			}
			if format == output.JSON || format == output.JSONL {
				return output.WriteJSON(cmd.OutOrStdout(), res)
			}
			if res.Submitted {
				fmt.Fprintf(cmd.ErrOrStderr(), "✅ %s — `gongctl applications` 로 확인하세요.\n", res.Message)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "⏹  %s\n", res.Message)
			}
			return nil
		},
	}
	c.Flags().StringVar(&purpose, "purpose", "", "활용목적 내용 (필수)")
	c.Flags().StringVar(&category, "category", "research", "목적분류: research|web|app|ref|etc")
	c.Flags().BoolVar(&yes, "yes", false, "확인 프롬프트 생략")
	c.MarkFlagRequired("purpose")
	return c
}

func mapCategory(c string) string {
	switch strings.ToLower(c) {
	case "web":
		return portal.PurposeWeb
	case "app":
		return portal.PurposeApp
	case "ref":
		return portal.PurposeRef
	case "etc":
		return portal.PurposeEtc
	default:
		return portal.PurposeResearch
	}
}

func applicationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "applications",
		Short: "내 OpenAPI 활용신청 현황 (상태·인증키 만료예정일)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			apps, err := portal.Applications(cmd.Context())
			if err != nil {
				if errors.Is(err, portal.ErrNotLoggedIn) {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					return nil
				}
				return err
			}
			if len(apps) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "활용신청 내역이 없습니다.")
				return nil
			}
			return renderApplications(cmd, format, apps)
		},
	}
}

func renderApplications(cmd *cobra.Command, format output.Format, apps []portal.Application) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(cmd.OutOrStdout(), apps)
	case output.JSONL:
		items := make([]any, len(apps))
		for i := range apps {
			items[i] = apps[i]
		}
		return output.WriteJSONL(cmd.OutOrStdout(), items)
	default:
		headers := []string{"상태", "계정", "데이터명", "제공기관", "신청일", "만료예정일"}
		rows := make([][]string, 0, len(apps))
		for _, a := range apps {
			rows = append(rows, []string{a.Status, a.Account, a.Title, a.Org, a.AppliedAt, a.ExpiresAt})
		}
		return output.WriteTable(cmd.OutOrStdout(), headers, rows)
	}
}
