package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPoolPick_Stable 同一 accountID + 字段在任意调用次数下都应返回同一结果。
func TestPoolPick_Stable(t *testing.T) {
	pool := []string{"a", "b", "c", "d", "e"}
	for _, id := range []int64{1, 42, 100, 99999, -7} {
		first := poolPick(id, "x-stainless-os", pool)
		for i := 0; i < 20; i++ {
			got := poolPick(id, "x-stainless-os", pool)
			assert.Equal(t, first, got, "accountID=%d 在多次调用中必须稳定", id)
		}
	}
}

// TestPoolPick_DifferentAccountsDistributed
// 在池容量 >=2 时，连续 N 个 accountID 的挑选结果应至少覆盖 2 个不同值，
// 否则说明 hash 分布塌陷。
func TestPoolPick_DifferentAccountsDistributed(t *testing.T) {
	pool := []string{"Linux", "Darwin"}
	seen := make(map[string]struct{})
	for id := int64(1); id <= 50; id++ {
		seen[poolPick(id, "x-stainless-os", pool)] = struct{}{}
	}
	assert.GreaterOrEqual(t, len(seen), 2, "连续 50 个 accountID 应能挑到至少 2 个不同值")
}

// TestPoolPick_FieldSaltIndependent 同一 accountID 在不同字段上应独立分布，
// 不会出现"选了 Linux 的账号一定选 arm64"的伴随规律。
func TestPoolPick_FieldSaltIndependent(t *testing.T) {
	poolOS := []string{"Linux", "Darwin"}
	poolArch := []string{"arm64", "x64"}
	// 收集 (os, arch) 组合
	combos := make(map[string]struct{})
	for id := int64(1); id <= 80; id++ {
		os := poolPick(id, "x-stainless-os", poolOS)
		arch := poolPick(id, "x-stainless-arch", poolArch)
		combos[os+"|"+arch] = struct{}{}
	}
	// 80 个账号分到 2x2=4 种组合时，至少能见到 3 种才算独立
	assert.GreaterOrEqual(t, len(combos), 3,
		"80 个 accountID 应在 (OS, Arch) 组合上命中至少 3 种，否则 hash 在字段间不独立")
}

// TestPoolPick_EmptyPool 空池返回空串，不 panic。
func TestPoolPick_EmptyPool(t *testing.T) {
	assert.Equal(t, "", poolPick(1, "whatever", nil))
	assert.Equal(t, "", poolPick(1, "whatever", []string{}))
}

// TestPickPooledFingerprint_Fixed 固定字段必须来自 fingerprintFixed* 常量，
// 而非池挑选 —— 抓包观察这两个字段无变化，保持恒定更贴近真实客户端。
func TestPickPooledFingerprint_Fixed(t *testing.T) {
	for _, id := range []int64{1, 2, 3, 100} {
		fp := pickPooledFingerprint(id)
		assert.Equal(t, fingerprintFixedStainlessLang, fp.StainlessLang)
		assert.Equal(t, fingerprintFixedStainlessRuntime, fp.StainlessRuntime)
	}
}

// TestPickPooledFingerprint_AllFieldsNonEmpty 每个字段都应有值，防止未来误把池置空。
func TestPickPooledFingerprint_AllFieldsNonEmpty(t *testing.T) {
	fp := pickPooledFingerprint(42)
	assert.NotEmpty(t, fp.UserAgent)
	assert.NotEmpty(t, fp.StainlessLang)
	assert.NotEmpty(t, fp.StainlessPackageVersion)
	assert.NotEmpty(t, fp.StainlessOS)
	assert.NotEmpty(t, fp.StainlessArch)
	assert.NotEmpty(t, fp.StainlessRuntime)
	assert.NotEmpty(t, fp.StainlessRuntimeVersion)
}

// TestCreateFingerprintFromHeaders_FallsBackToPoolWhenHeadersMissing
// 请求头缺失时，createFingerprintFromHeaders 应回退到池（按 accountID）而非硬编码常量。
// 断言方式：同一 accountID 两次调用结果一致，且值必须出现在池中。
func TestCreateFingerprintFromHeaders_FallsBackToPoolWhenHeadersMissing(t *testing.T) {
	svc := &IdentityService{}
	const id int64 = 4242

	fp := svc.createFingerprintFromHeaders(id, http.Header{})

	assert.Contains(t, fingerprintPoolUserAgent, fp.UserAgent)
	assert.Contains(t, fingerprintPoolStainlessOS, fp.StainlessOS)
	assert.Contains(t, fingerprintPoolStainlessArch, fp.StainlessArch)
	assert.Contains(t, fingerprintPoolStainlessPackageVersion, fp.StainlessPackageVersion)
	assert.Contains(t, fingerprintPoolStainlessRuntimeVersion, fp.StainlessRuntimeVersion)
	assert.Equal(t, fingerprintFixedStainlessLang, fp.StainlessLang)
	assert.Equal(t, fingerprintFixedStainlessRuntime, fp.StainlessRuntime)

	// 稳定性：同一 accountID 再次调用结果一致
	fp2 := svc.createFingerprintFromHeaders(id, http.Header{})
	assert.Equal(t, fp.UserAgent, fp2.UserAgent)
	assert.Equal(t, fp.StainlessOS, fp2.StainlessOS)
	assert.Equal(t, fp.StainlessArch, fp2.StainlessArch)
}

// TestCreateFingerprintFromHeaders_ClientHeadersOverridePool
// 请求头中有值时应优先使用请求头值，不被池替换。
func TestCreateFingerprintFromHeaders_ClientHeadersOverridePool(t *testing.T) {
	svc := &IdentityService{}
	headers := http.Header{
		"User-Agent":                  []string{"claude-cli/9.9.9 (test)"},
		"X-Stainless-Os":              []string{"FreeBSD"},
		"X-Stainless-Arch":            []string{"riscv64"},
		"X-Stainless-Runtime-Version": []string{"v99.0.0"},
	}
	fp := svc.createFingerprintFromHeaders(1, headers)

	assert.Equal(t, "claude-cli/9.9.9 (test)", fp.UserAgent)
	assert.Equal(t, "FreeBSD", fp.StainlessOS)
	assert.Equal(t, "riscv64", fp.StainlessArch)
	assert.Equal(t, "v99.0.0", fp.StainlessRuntimeVersion)
}
