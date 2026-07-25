package main

import (
	"fmt"

	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func loginCmd() *cobra.Command {
	var keepBrowser bool
	c := &cobra.Command{
		Use:   "login",
		Short: "브라우저로 data.go.kr 로그인 (세션 유지)",
		Long: `브라우저 창을 띄워 data.go.kr 에 로그인합니다. 로그인이 끝나면 gongctl 이
그 브라우저를 백그라운드로 유지하고, 이후 apply/applications 등이 그 세션에
다시 붙어 동작합니다. 키체인 비밀번호는 묻지 않습니다.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := portal.Login(cmd.Context(), cmd.ErrOrStderr(), keepBrowser); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "   이제 `gongctl applications` 로 활용신청 현황을 볼 수 있습니다.")
			return nil
		},
	}
	c.Flags().BoolVar(&keepBrowser, "keep-browser", false, "로그인 후 브라우저를 닫지 않음 (디버깅용)")
	return c
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "세션 종료 — 브라우저·쿠키·인증키·브라우저 프로파일 전부 삭제",
		Long: `로그인 세션을 완전히 정리합니다. 다음을 모두 삭제합니다:

  · 저장된 data.go.kr 세션 쿠키와 캐시된 인증키
  · gongctl 이 만든 Chrome 프로파일 2개(로그인용·headless용)
    — 로그인 프로파일에는 사람이 로그인에 사용한 SSO 제공자(네이버 등)의
      쿠키도 함께 쌓이므로, 로그아웃 시 같이 지웁니다.
  · 실행 중이던 세션 브라우저`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := portal.Logout(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "세션을 종료했습니다 — 쿠키·인증키·브라우저 프로파일을 삭제했습니다.")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "로그인 세션 상태 확인",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := portal.Applications(cmd.Context())
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "세션 없음 — `gongctl login` 을 실행하세요.")
				return nil
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "✅ 세션이 살아있습니다.")
			return nil
		},
	}
}
