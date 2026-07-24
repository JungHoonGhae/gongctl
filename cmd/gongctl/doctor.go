package main

import (
	"errors"
	"fmt"

	"github.com/JungHoonGhae/gongctl/internal/doctor"
	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "스크래핑 상태 점검 — data.go.kr 마크업 변경(drift) 감지",
		Long: `gongctl 의 fragile scraping 이 아직 동작하는지 라이브로 점검합니다.
data.go.kr 이 HTML 을 바꾸면 파서가 조용히 빈 결과를 내므로, doctor 가 각 seam
(검색·describe·활용신청 현황)을 실제로 호출해 데이터가 나오는지 확인합니다.
drift 가 하나라도 있으면 종료코드 1 을 반환합니다(CI 용).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			base := flagBaseURL
			if base == "" {
				base = portal.BaseURL
			}

			checks := doctor.Run(cmd.Context(), newFetchClient(), base)
			checks = append(checks, sessionCheck(cmd))

			if err := renderChecks(cmd, format, checks); err != nil {
				return err
			}
			for _, c := range checks {
				if c.Status == doctor.StatusDrift {
					return fmt.Errorf("drift 감지 — 파서 점검 필요")
				}
			}
			return nil
		},
	}
}

// sessionCheck probes the login-gated 활용신청 현황 seam. Without a session it is
// reported skipped rather than drift — the parser can't be exercised.
func sessionCheck(cmd *cobra.Command) doctor.Check {
	_, err := portal.Applications(cmd.Context())
	switch {
	case errors.Is(err, portal.ErrNotLoggedIn):
		return doctor.Check{Name: "applications", Status: doctor.StatusSkipped, Detail: "세션 없음 — `gongctl login` 후 재점검"}
	case err != nil:
		return doctor.Check{Name: "applications", Status: doctor.StatusDrift, Detail: "요청 실패: " + err.Error()}
	default:
		return doctor.Check{Name: "applications", Status: doctor.StatusOK, Detail: "활용신청 현황 파싱 성공"}
	}
}

func renderChecks(cmd *cobra.Command, format output.Format, checks []doctor.Check) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(cmd.OutOrStdout(), checks)
	case output.JSONL:
		items := make([]any, len(checks))
		for i := range checks {
			items[i] = checks[i]
		}
		return output.WriteJSONL(cmd.OutOrStdout(), items)
	default:
		headers := []string{"점검", "상태", "상세"}
		rows := make([][]string, 0, len(checks))
		for _, c := range checks {
			rows = append(rows, []string{c.Name, string(c.Status), c.Detail})
		}
		return output.WriteTable(cmd.OutOrStdout(), headers, rows)
	}
}
