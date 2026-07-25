package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

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
	var pk, op string
	var wait time.Duration
	c := &cobra.Command{
		Use:   "call [endpoint]",
		Short: "인증 API 호출 — serviceKey 주입 → GET → XML→JSON",
		Long: `승인된 OpenAPI 엔드포인트를 호출합니다. --param k=v 로 요청변수를 전달합니다. 인증키는 로그인 세션에서 자동으로 조회하므로
--key 는 생략할 수 있습니다. 응답은 XML이면 JSON으로 변환해 출력합니다.

엔드포인트 URL 대신 --pk 를 주면 포털에서 엔드포인트를 조회합니다. 경로는 추측할 수 없는
형태(HeatWaveCasualtiesRegion/getHeatWaveCasualtiesRegionList)이고 틀리면 404·500 이 오므로,
직접 타이핑하기보다 이 방식이 안전합니다. 상세기능이 여럿이면 --op 로 지정하세요.
--pk 를 쓰면 명세의 필수 요청변수가 빠졌는지도 호출 전에 확인합니다.

신청 직후에는 게이트웨이 반영에 보통 7~10분 걸려 403 이 옵니다. --wait 10m 을 주면 그때까지
1분 간격으로 재시도합니다(승인 자체는 즉시 끝나므로 다시 신청할 필요 없습니다).

예) gongctl call --pk 15077974 --param numOfRows=10
    gongctl call --pk 15077974 --wait 15m --param numOfRows=10   # 방금 신청한 API
    gongctl call http://apis.data.go.kr/9760000/.../getX --param numOfRows=10`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && pk == "" {
				return fmt.Errorf("엔드포인트 URL 또는 --pk 가 필요합니다")
			}
			if len(args) > 0 && pk != "" {
				return fmt.Errorf("엔드포인트 URL 과 --pk 는 함께 쓸 수 없습니다 — 어느 것을 호출할지 하나만 정하세요")
			}
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
			endpoint := ""
			if len(args) > 0 {
				endpoint = args[0]
			} else {
				base := flagBaseURL
				if base == "" {
					base = portal.BaseURL
				}
				resolved, rerr := apicall.Resolve(cmd.Context(), newFetchClient(), base, pk, op)
				if rerr != nil {
					return rerr
				}
				endpoint = resolved.Endpoint
				fmt.Fprintf(cmd.ErrOrStderr(), "엔드포인트: %s\n", endpoint)
				// Missing required variables usually come back as an empty result
				// rather than an error, so refuse before spending the request.
				if missing := apicall.MissingRequired(resolved, pm); len(missing) > 0 {
					return fmt.Errorf("필수 요청변수가 빠졌습니다: %s — `gongctl describe %s` 로 확인하세요",
						strings.Join(missing, ", "), pk)
				}
			}
			doCall := func(k string) (*apicall.CallResult, error) {
				if wait <= 0 {
					return apicall.Call(cmd.Context(), newFetchClient(), endpoint, pm, k)
				}
				return apicall.CallWaiting(cmd.Context(), newFetchClient(), endpoint, pm, k, wait,
					func(elapsed, remaining time.Duration) {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"\r게이트웨이 반영 대기… %s 경과 (최대 %s 더 기다립니다)", elapsed, remaining)
					})
			}
			res, err := doCall(key)
			// The cached key may be stale (reissued in the portal). Drop it and
			// retry once with a freshly read key before reporting failure.
			if errors.Is(err, apicall.ErrKeyRejected) && !cmd.Flags().Changed("key") {
				portal.InvalidateCachedKey()
				if fresh, kerr := portal.APIKey(cmd.Context()); kerr == nil && fresh != key {
					fmt.Fprintln(cmd.ErrOrStderr(), "인증키가 거부됐습니다 — 캐시를 버리고 다시 조회해 재시도합니다.")
					res, err = doCall(fresh)
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
	c.Flags().StringVar(&pk, "pk", "", "publicDataPk — 엔드포인트를 포털에서 조회 (URL 대신)")
	c.Flags().StringVar(&op, "op", "", "상세기능 이름 (엔드포인트 마지막 경로 조각). 하나뿐이면 생략 가능")
	c.Flags().DurationVar(&wait, "wait", 0, "게이트웨이 반영(403)을 이 시간까지 기다리며 재시도 (예: 10m, 최대 1h)")
	return c
}
