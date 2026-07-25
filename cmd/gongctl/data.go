package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/JungHoonGhae/gongctl/internal/apicall"
	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/JungHoonGhae/gongctl/internal/portal"
	"github.com/spf13/cobra"
)

func searchCmd() *cobra.Command {
	var dtype, org string
	var perPage, page int
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
				PerPage: perPage,
				Page:    page,
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
	c.Flags().IntVar(&perPage, "per-page", 0, "페이지당 결과 수 (기본 10)")
	c.Flags().IntVar(&page, "page", 0, "페이지 번호 (1부터)")
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
			spec, err := apicall.Describe(cmd.Context(), newFetchClient(), base, args[0])
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
		Long: `승인된 OpenAPI 엔드포인트를 호출합니다. --param k=v 로 요청변수를 전달합니다. 인증키는 로그인 세션에서 자동으로 조회하므로
--key 는 생략할 수 있습니다. 응답은 XML이면 JSON으로 변환해 출력합니다.

예) gongctl call http://apis.data.go.kr/9760000/.../getX --param numOfRows=10`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if key == "" {
				k, err := portal.APIKey(cmd.Context())
				if err != nil {
					return fmt.Errorf("인증키를 얻지 못했습니다 (--key 로 직접 지정하거나 `gongctl login` 후 재시도): %w", err)
				}
				key = k
			}
			pm := map[string]string{}
			for _, p := range params {
				kv := strings.SplitN(p, "=", 2)
				if len(kv) != 2 {
					return fmt.Errorf("--param 은 k=v 형식이어야 합니다: %q", p)
				}
				pm[kv[0]] = kv[1]
			}
			res, err := apicall.Call(cmd.Context(), newFetchClient(), args[0], pm, key)
			// The cached key may be stale (reissued in the portal). Drop it and
			// retry once with a freshly read key before reporting failure.
			if errors.Is(err, apicall.ErrKeyRejected) && !cmd.Flags().Changed("key") {
				portal.InvalidateCachedKey()
				if fresh, kerr := portal.APIKey(cmd.Context()); kerr == nil && fresh != key {
					fmt.Fprintln(cmd.ErrOrStderr(), "인증키가 거부됐습니다 — 캐시를 버리고 다시 조회해 재시도합니다.")
					res, err = apicall.Call(cmd.Context(), newFetchClient(), args[0], pm, fresh)
				}
			}
			if res != nil {
				output.WriteJSON(cmd.OutOrStdout(), res)
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err) // surface hint, don't fail hard
			}
			return nil
		},
	}
	c.Flags().StringVar(&key, "key", "", "계정 인증키 (생략 시 로그인 세션에서 자동 조회)")
	c.Flags().StringArrayVar(&params, "param", nil, "요청변수 k=v (반복 가능)")
	return c
}
