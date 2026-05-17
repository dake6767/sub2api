package service

import (
	"hash/fnv"
)

// 指纹默认值池。值来自真实 Claude Code 客户端抓包，
// 仅用作账号首次创建指纹、且请求头未携带对应字段时的回退来源。
//
// 池通过 accountID + 字段名独立 hash 分发：同一账号对同一字段永远返回同一结果，
// 不同账号之间在池容量范围内尽量分散，从而在不破坏"同一账号长期稳定"语义的前提下，
// 避免所有缺头账号共用同一组硬编码默认值。
//
// 已缓存（历史）账号不受影响 —— 缓存命中时直接返回已有 Fingerprint，不会进入本池。

var fingerprintPoolUserAgent = []string{
	"claude-cli/2.1.140 (external, cli)",
	"claude-cli/2.1.135 (external, cli)",
	"claude-cli/2.1.128 (external, cli)",
}

var fingerprintPoolStainlessOS = []string{
	"Linux",
	"Darwin",
}

var fingerprintPoolStainlessArch = []string{
	"arm64",
	"x64",
}

var fingerprintPoolStainlessRuntimeVersion = []string{
	"v24.16.0",
	"v22.13.0",
	"v20.18.3",
}

var fingerprintPoolStainlessPackageVersion = []string{
	"0.74.0",
	"0.72.0",
}

// 恒定字段：真实抓包中这两个值没有可观察变化，保留单一常量即可。
const (
	fingerprintFixedStainlessLang    = "js"
	fingerprintFixedStainlessRuntime = "node"
)

// poolPick 按 accountID 与字段名独立 hash，从池中稳定挑选一个值。
// 注意顺序：accountID 先写入，field 最后写入 —— 让 field 的字节驱动 hash 末态，
// 这样在对小池容量取模时，不同字段才会在同一 accountID 上产生独立分布。
func poolPick(accountID int64, field string, pool []string) string {
	if len(pool) == 0 {
		return ""
	}
	h := fnv.New64a()
	var buf [8]byte
	u := uint64(accountID)
	for i := 0; i < 8; i++ {
		buf[i] = byte(u >> (8 * i))
	}
	h.Write(buf[:])
	h.Write([]byte(":"))
	h.Write([]byte(field))
	// 使用高位而非低位 mod，避免 FNV-1a 低位受最后字节主导导致字段间伴随
	return pool[(h.Sum64()>>32)%uint64(len(pool))]
}

// pickPooledFingerprint 为指定 accountID 返回一组从池中挑选的字段值，
// 作为 createFingerprintFromHeaders 中各字段的 header 缺失回退源。
// 不包含 ClientID（由 generateClientID 另行生成）与 UpdatedAt（由调用方填入）。
func pickPooledFingerprint(accountID int64) Fingerprint {
	return Fingerprint{
		UserAgent:               poolPick(accountID, "user-agent", fingerprintPoolUserAgent),
		StainlessLang:           fingerprintFixedStainlessLang,
		StainlessPackageVersion: poolPick(accountID, "x-stainless-package-version", fingerprintPoolStainlessPackageVersion),
		StainlessOS:             poolPick(accountID, "x-stainless-os", fingerprintPoolStainlessOS),
		StainlessArch:           poolPick(accountID, "x-stainless-arch", fingerprintPoolStainlessArch),
		StainlessRuntime:        fingerprintFixedStainlessRuntime,
		StainlessRuntimeVersion: poolPick(accountID, "x-stainless-runtime-version", fingerprintPoolStainlessRuntimeVersion),
	}
}
