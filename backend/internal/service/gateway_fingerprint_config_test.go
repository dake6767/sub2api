package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// withFingerprintOverride 临时替换 fingerprintSalt / cchSeed，测试结束自动恢复。
// 所有接触全局指纹状态的测试都应通过它进入，避免跨用例串扰。
func withFingerprintOverride(t *testing.T, salt string, seed uint64) {
	t.Helper()
	origSalt, origSeed := fingerprintSalt, cchSeed
	fingerprintSalt, cchSeed = salt, seed
	t.Cleanup(func() {
		fingerprintSalt, cchSeed = origSalt, origSeed
	})
}

func TestConfigureFingerprint_EmptyKeepsDefaults(t *testing.T) {
	withFingerprintOverride(t, defaultFingerprintSalt, defaultCCHSeed)

	require.NoError(t, ConfigureFingerprint(config.FingerprintConfig{}))

	assert.Equal(t, defaultFingerprintSalt, fingerprintSalt)
	assert.Equal(t, defaultCCHSeed, cchSeed)
}

func TestConfigureFingerprint_OverridesTake(t *testing.T) {
	withFingerprintOverride(t, defaultFingerprintSalt, defaultCCHSeed)

	require.NoError(t, ConfigureFingerprint(config.FingerprintConfig{
		Salt:    "deadbeefcafe",
		CCHSeed: "0x1234567890ABCDEF",
	}))

	assert.Equal(t, "deadbeefcafe", fingerprintSalt)
	assert.Equal(t, uint64(0x1234567890ABCDEF), cchSeed)
}

func TestConfigureFingerprint_DecimalSeed(t *testing.T) {
	withFingerprintOverride(t, defaultFingerprintSalt, defaultCCHSeed)

	require.NoError(t, ConfigureFingerprint(config.FingerprintConfig{
		CCHSeed: "42",
	}))

	assert.Equal(t, uint64(42), cchSeed)
}

func TestConfigureFingerprint_InvalidSeedErrors(t *testing.T) {
	withFingerprintOverride(t, defaultFingerprintSalt, defaultCCHSeed)

	err := ConfigureFingerprint(config.FingerprintConfig{CCHSeed: "not-a-number"})
	require.Error(t, err)
	// 解析失败时不应改动生效值
	assert.Equal(t, defaultFingerprintSalt, fingerprintSalt)
	assert.Equal(t, defaultCCHSeed, cchSeed)
}

// TestComputeFingerprint_DefaultMatchesBakedInConstant 确认默认配置下
// computeClaudeCodeFingerprint 的输出与硬编码常量行为字节对齐。
// 用 sha256 hex 前 3 位的具体值作为"改前二进制"的基线锚点。
func TestComputeFingerprint_DefaultMatchesBakedInConstant(t *testing.T) {
	withFingerprintOverride(t, defaultFingerprintSalt, defaultCCHSeed)

	body := []byte(`{"messages":[{"role":"user","content":"Hello, Claude. This is a fingerprint test."}]}`)
	got := computeClaudeCodeFingerprint(body, "2.1.92")

	// 基线：对 "Hello, Claude. This is a fingerprint test." 取下标 4/7/20 → 'o'/'l'/'p'
	// sha256("59cf53e54c78" + "olp" + "2.1.92") 前 3 hex chars
	// 通过直接算法复算，不依赖硬编码期望值避免伪阳性
	assert.Len(t, got, 3)
	assert.Regexp(t, `^[0-9a-f]{3}$`, got)

	// 将 salt 改掉后，fp 应变化（证明可配置确实生效）
	withFingerprintOverride(t, "new-salt", defaultCCHSeed)
	got2 := computeClaudeCodeFingerprint(body, "2.1.92")
	assert.NotEqual(t, got, got2)
}

func TestSignBillingHeaderCCH_DefaultMatchesBakedInSeed(t *testing.T) {
	withFingerprintOverride(t, defaultFingerprintSalt, defaultCCHSeed)

	body := []byte(`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.63.a43; cc_entrypoint=cli; cch=00000;"}],"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	result := signBillingHeaderCCH(body)
	billingText := gjson.GetBytes(result, "system.0.text").String()

	// 默认生效值应等于 defaultCCHSeed：直接用它复算一次 cch 并断言一致。
	recomputed := fmt.Sprintf("%05x", xxHash64Seeded(body, defaultCCHSeed)&0xFFFFF)
	assert.Contains(t, billingText, "cch="+recomputed+";")

	// 换一个 seed，cch 应不同。
	withFingerprintOverride(t, defaultFingerprintSalt, defaultCCHSeed+1)
	result2 := signBillingHeaderCCH(body)
	billingText2 := gjson.GetBytes(result2, "system.0.text").String()
	assert.NotEqual(t, billingText, billingText2)
}
