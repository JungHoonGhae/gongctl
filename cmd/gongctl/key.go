package main

import (
	"errors"
	"fmt"

	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func keyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key",
		Short: "계정 인증키(serviceKey) 조회",
		Long: `data.go.kr 계정의 일반 인증키를 가져옵니다. 계정당 키는 하나이며 첫 활용신청이
승인될 때 발급되어 모든 승인 API에 공통으로 쓰입니다.

` + "`gongctl call`" + ` 은 --key 를 생략하면 이 키를 자동으로 사용합니다.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			key, err := portal.APIKey(cmd.Context())
			if err != nil {
				if errors.Is(err, portal.ErrNotLoggedIn) {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					return nil
				}
				return err
			}
			if format == output.Table {
				fmt.Fprintln(cmd.OutOrStdout(), key)
				return nil
			}
			return output.WriteJSON(cmd.OutOrStdout(), map[string]string{"serviceKey": key})
		},
	}
}
