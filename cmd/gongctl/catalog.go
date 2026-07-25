package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JungHoonGhae/gongctl/internal/catalog"
	"github.com/JungHoonGhae/gongctl/internal/output"
	"github.com/spf13/cobra"
)

func catalogCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "catalog",
		Short: "API 카탈로그 — 무엇이 존재하는지 로컬에서 즉시 검색",
		Long: `data.go.kr 의 오픈API 목록을 로컬에 한 번 받아두고, 이후 검색을 네트워크 없이
즉시 처리합니다. 포털은 키워드 검색만 제공하므로, 카탈로그가 없으면 "이런 데이터가
있나?"를 확인하려면 검색어를 하나씩 추측해 볼 수밖에 없습니다.

  gongctl catalog sync            전체 목록 수집 (수십 초)
  gongctl catalog sync --if-stale 오래됐을 때만 수집 — cron/CI 로 주기 갱신할 때
  gongctl catalog search 폭염     로컬 검색 — 활용신청 많은 순
  gongctl catalog orgs 폭염       그 주제를 개방한 기관 순위
  gongctl catalog info            언제 수집했는지 / 몇 건인지`,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	c.AddCommand(catalogSyncCmd(), catalogSearchCmd(), catalogOrgsCmd(), catalogInfoCmd())
	return c
}

func catalogSyncCmd() *cobra.Command {
	var dtype string
	var perPage int
	var ifStale bool
	c := &cobra.Command{
		Use:   "sync",
		Short: "포털에서 전체 목록을 받아 로컬 카탈로그 갱신",
		Long: `포털의 전체 오픈API 목록을 수집해 로컬 카탈로그를 갱신합니다.

--if-stale 은 카탈로그가 아직 신선하면 아무것도 하지 않고 성공합니다. 갱신 주기를
판단하는 일을 사람이 기억하지 않아도 되도록, cron 이나 CI 가 조건 없이 걸어두는 용도입니다:

  0 4 * * *  gongctl catalog sync --if-stale`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if ifStale {
				// Only an existing, still-fresh catalogue is a reason to skip. A
				// missing or unreadable one means sync is exactly what's needed.
				if cur, err := catalog.Load(); err == nil && !cur.Stale() {
					fmt.Fprintf(cmd.ErrOrStderr(), "카탈로그가 아직 신선합니다 (%.0f일 전, %d건) — 건너뜁니다\n",
						cur.Age().Hours()/24, len(cur.Entries))
					return nil
				}
			}
			start := time.Now()
			// Report every page, not every thousand: this runs for minutes, and
			// silence for the first stretch is indistinguishable from a hang.
			cat, err := catalog.Sync(cmd.Context(), newPortalClient(), dtype, perPage, func(n int) {
				fmt.Fprintf(cmd.ErrOrStderr(), "\r  수집 중… %d건", n)
			})
			if err != nil {
				return err
			}
			if err := cat.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "\r카탈로그 %d건 저장 (%s, %.0f초)\n",
				len(cat.Entries), dtype, time.Since(start).Seconds())
			return nil
		},
	}
	c.Flags().StringVar(&dtype, "type", "API", "데이터 유형: API | FILE")
	c.Flags().IntVar(&perPage, "per-page", 200, "페이지당 요청 건수")
	c.Flags().BoolVar(&ifStale, "if-stale", false, "카탈로그가 오래됐을 때만 수집 (cron/CI 용)")
	return c
}

func catalogSearchCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "search <검색어…>",
		Short: "로컬 카탈로그 검색 (모든 단어가 포함된 것, 활용신청 많은 순)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			cat, err := loadCatalog(cmd)
			if err != nil {
				return err
			}
			q := ""
			for i, a := range args {
				if i > 0 {
					q += " "
				}
				q += a
			}
			res := cat.Search(q, limit)
			hits, total := res.Hits, res.Total
			if format != output.Table {
				return output.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"terms": res.Terms, "relaxed": res.Relaxed,
					"total": total, "shown": len(hits), "hits": hits,
				})
			}
			if total == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "일치하는 데이터가 없습니다.")
				return nil
			}
			if res.Relaxed {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"모든 단어를 포함하는 결과가 없어 일부만 일치하는 것까지 보여줍니다 (검색어: %s)\n\n",
					strings.Join(res.Terms, " "))
			}
			headers := []string{"pk", "활용신청", "수정일", "제공기관", "데이터명"}
			rows := make([][]string, 0, len(hits))
			for _, h := range hits {
				rows = append(rows, []string{h.PK, fmt.Sprintf("%d", h.ApplyCount), h.ModifiedAt, h.Org, h.Title})
			}
			if err := output.WriteTable(cmd.OutOrStdout(), headers, rows); err != nil {
				return err
			}
			if total > len(hits) {
				fmt.Fprintf(cmd.ErrOrStderr(), "\n총 %d건 중 %d건 표시 — 검색어를 좁히거나 --limit 을 올리세요.\n", total, len(hits))
			}
			return nil
		},
	}
	c.Flags().IntVar(&limit, "limit", 20, "표시할 최대 건수")
	return c
}

func catalogOrgsCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "orgs [검색어…]",
		Short: "주제를 개방한 기관 순위 (검색어 생략 시 전체)",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := resolveFormat()
			if err != nil {
				return err
			}
			cat, err := loadCatalog(cmd)
			if err != nil {
				return err
			}
			q := ""
			for i, a := range args {
				if i > 0 {
					q += " "
				}
				q += a
			}
			orgs := cat.Orgs(q, limit)
			if format != output.Table {
				return output.WriteJSON(cmd.OutOrStdout(), orgs)
			}
			headers := []string{"API수", "활용신청 합", "제공기관"}
			rows := make([][]string, 0, len(orgs))
			for _, o := range orgs {
				rows = append(rows, []string{fmt.Sprintf("%d", o.Count), fmt.Sprintf("%d", o.ApplySum), o.Org})
			}
			return output.WriteTable(cmd.OutOrStdout(), headers, rows)
		},
	}
	c.Flags().IntVar(&limit, "limit", 15, "표시할 기관 수")
	return c
}

func catalogInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "카탈로그 수집 시각·건수",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cat, err := loadCatalog(cmd)
			if err != nil {
				return err
			}
			format, ferr := resolveFormat()
			if ferr != nil {
				return ferr
			}
			if format != output.Table {
				return output.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"syncedAt": cat.SyncedAt, "type": cat.Type,
					"entries": len(cat.Entries), "stale": cat.Stale(),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "수집 %s (%.0f시간 전) · %s 유형 · %d건%s\n",
				cat.SyncedAt.Local().Format("2006-01-02 15:04"), cat.Age().Hours(),
				cat.Type, len(cat.Entries),
				map[bool]string{true: " · ⚠ 오래됨, sync 권장", false: ""}[cat.Stale()])
			return nil
		},
	}
}

// loadCatalog reads the catalogue and nudges when it is old, so a stale snapshot
// never silently passes for a complete one.
func loadCatalog(cmd *cobra.Command) (*catalog.Catalog, error) {
	cat, err := catalog.Load()
	if errors.Is(err, catalog.ErrNotSynced) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if cat.Stale() {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"⚠ 카탈로그가 %.0f일 전 것입니다 — `gongctl catalog sync` 로 갱신하세요.\n", cat.Age().Hours()/24)
	}
	return cat, nil
}
