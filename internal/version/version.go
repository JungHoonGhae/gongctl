// Package version holds build metadata injected via -ldflags, with a
// runtime/debug fallback so `go install …@latest` builds (no ldflags) still
// report the module version instead of "dev".
package version

import (
	"fmt"
	"runtime/debug"
)

var (
	// Version is the semantic version, set at build time.
	Version = "dev"
	// Commit is the short git SHA, set at build time.
	Commit = "unknown"
	// Date is the build timestamp (RFC3339), set at build time.
	Date = "unknown"
)

func init() {
	if Version != "dev" {
		return // ldflags 주입됨 (goreleaser/make build) — 그대로 사용
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// go install mod@vX.Y.Z 는 모듈 버전을 빌드 정보에 심는다.
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		Version = v
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				Commit = s.Value[:7]
			}
		case "vcs.time":
			Date = s.Value
		}
	}
}

// String renders a human-readable version line.
func String() string {
	return fmt.Sprintf("gongctl %s (commit %s, built %s)", Version, Commit, Date)
}
