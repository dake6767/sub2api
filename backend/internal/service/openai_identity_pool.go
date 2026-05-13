package service

import (
	"hash/fnv"
)

// Codex CLI release 小池。值来自真实 openai/codex 的历史 release tag。
// 每一项是绑定的 (UserAgent, Version) 组合 —— 同 accountID 会被 hash 到同一 index,
// 保证 UA 与 Version 始终一致,避免出现 UA 是 0.125 而 Version 是 0.128 的错配。
//
// 池外暂不添加新 release,免得造出不存在的版本导致上游拒;加新版本前先确认 openai/codex
// 有对应 tag 发布。
type codexCLIReleasePoolEntry struct {
	UserAgent string
	Version   string
}

var codexCLIReleasePool = []codexCLIReleasePoolEntry{
	{UserAgent: "codex_cli_rs/0.125.0", Version: "0.125.0"},
	{UserAgent: "codex_cli_rs/0.128.0", Version: "0.128.0"},
	{UserAgent: "codex_cli_rs/0.120.0", Version: "0.120.0"},
}

// pickCodexCLIRelease 为指定 accountID 从池中稳定挑选一组 (UA, Version) 组合。
// accountID <= 0 或池为空时返回当前生效的默认值(兜底,防调用方传错)。
func pickCodexCLIRelease(accountID int64) codexCLIReleasePoolEntry {
	if accountID <= 0 || len(codexCLIReleasePool) == 0 {
		return codexCLIReleasePoolEntry{
			UserAgent: codexCLIUserAgent,
			Version:   codexCLIVersion,
		}
	}
	h := fnv.New64a()
	var buf [8]byte
	u := uint64(accountID)
	for i := 0; i < 8; i++ {
		buf[i] = byte(u >> (8 * i))
	}
	h.Write(buf[:])
	h.Write([]byte(":codex-cli-release"))
	idx := (h.Sum64() >> 32) % uint64(len(codexCLIReleasePool))
	return codexCLIReleasePool[idx]
}
