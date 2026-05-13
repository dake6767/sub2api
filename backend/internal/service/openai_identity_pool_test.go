package service

import (
	"strings"
	"testing"
)

func TestPickCodexCLIReleaseStability(t *testing.T) {
	// 同 accountID 反复挑应返回同一 entry
	for i := int64(1); i <= 200; i++ {
		a := pickCodexCLIRelease(i)
		b := pickCodexCLIRelease(i)
		if a != b {
			t.Errorf("accountID=%d unstable: %+v vs %+v", i, a, b)
		}
	}
}

func TestPickCodexCLIReleaseBinding(t *testing.T) {
	// UA 里的版本号必须与 Version 字段匹配(防止池里有人手误配错)
	for i, e := range codexCLIReleasePool {
		want := "codex_cli_rs/" + e.Version
		if e.UserAgent != want {
			t.Errorf("pool[%d] UA/Version mismatch: ua=%q version=%q", i, e.UserAgent, e.Version)
		}
		if !strings.HasPrefix(e.UserAgent, "codex_cli_rs/") {
			t.Errorf("pool[%d] UA must start with codex_cli_rs/: %q", i, e.UserAgent)
		}
	}
}

func TestPickCodexCLIReleaseDistribution(t *testing.T) {
	// 1000 个连续 accountID 在池上的分布不应极度倾斜
	counts := make(map[string]int)
	for i := int64(1); i <= 1000; i++ {
		counts[pickCodexCLIRelease(i).Version]++
	}
	if len(counts) != len(codexCLIReleasePool) {
		t.Fatalf("expected all %d pool entries to be hit, got %d", len(codexCLIReleasePool), len(counts))
	}
	for v, n := range counts {
		// 最偏差容忍:任何一项都不应少于理论均值的 50%
		min := 1000 / len(codexCLIReleasePool) / 2
		if n < min {
			t.Errorf("version %s hit %d times, below min %d", v, n, min)
		}
	}
}

func TestPickCodexCLIReleaseZeroAccount(t *testing.T) {
	// accountID <= 0 要回退到当前生效的 codexCLIUserAgent/codexCLIVersion
	got := pickCodexCLIRelease(0)
	if got.UserAgent != codexCLIUserAgent || got.Version != codexCLIVersion {
		t.Errorf("accountID=0 should fall back to defaults, got %+v", got)
	}
	gotNeg := pickCodexCLIRelease(-1)
	if gotNeg.UserAgent != codexCLIUserAgent || gotNeg.Version != codexCLIVersion {
		t.Errorf("accountID=-1 should fall back to defaults, got %+v", gotNeg)
	}
}
