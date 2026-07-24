package main

import (
	"fmt"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/apicall"
	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func searchCmd() *cobra.Command {
	var dtype, org string
	c := &cobra.Command{
		Use:   "search <keyword>",
		Short: "data.go.kr 데이터셋 검색 (파일 + OpenAPI)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			ds, err := newPortalClient().SearchDatasets(cmd.Context(), portal.SearchOptions{
				Keyword: strings.Join(args, " "),
				Type:    strings.ToUpper(dtype),
				Org:     org,
			})
			if err != nil {
				return err
			}
			if format == output.Table {
				headers := []string{"pk", "OpenAPI", "제목", "포맷"}
				rows := make([][]string, 0, len(ds))
				for _, d := range ds {
					api := ""
					if d.HasOpenAPI {
						api = "✓"
					}
					rows = append(rows, []string{d.PublicDataPk, api, d.Title, strings.Join(d.Formats, ",")})
				}
				return output.WriteTable(cmd.OutOrStdout(), headers, rows)
			}
			return output.WriteJSON(cmd.OutOrStdout(), ds)
		},
	}
	c.Flags().StringVar(&dtype, "type", "", "데이터 유형: file | api (기본: 전체)")
	c.Flags().StringVar(&org, "org", "", "제공기관 필터")
	return c
}

func describeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <publicDataPk>",
		Short: "OpenAPI 상세 — 상세기능·엔드포인트·요청변수 surface",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			base := flagBaseURL
			if base == "" {
				base = portal.BaseURL
			}
			spec, err := apicall.Describe(cmd.Context(), base, args[0])
			if err != nil {
				return err
			}
			return output.WriteJSON(cmd.OutOrStdout(), spec)
		},
	}
}

func callCmd() *cobra.Command {
	var key string
	var params []string
	c := &cobra.Command{
		Use:   "call <endpoint>",
		Short: "인증 API 호출 — serviceKey 주입 → GET → XML→JSON",
		Long: `승인된 OpenAPI 엔드포인트를 호출합니다. --key 로 계정 인증키를,
--param k=v 로 요청변수를 전달합니다. 응답은 XML이면 JSON으로 변환해 출력합니다.

예) gongctl call http://apis.data.go.kr/9760000/.../getX --key <KEY> --param numOfRows=10`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pm := map[string]string{}
			for _, p := range params {
				kv := strings.SplitN(p, "=", 2)
				if len(kv) != 2 {
					return fmt.Errorf("--param 은 k=v 형식이어야 합니다: %q", p)
				}
				pm[kv[0]] = kv[1]
			}
			res, err := apicall.Call(cmd.Context(), args[0], pm, key)
			if res != nil {
				output.WriteJSON(cmd.OutOrStdout(), res)
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err) // surface hint, don't fail hard
			}
			return nil
		},
	}
	c.Flags().StringVar(&key, "key", "", "계정 인증키 (serviceKey)")
	c.Flags().StringArrayVar(&params, "param", nil, "요청변수 k=v (반복 가능)")
	c.MarkFlagRequired("key")
	return c
}
