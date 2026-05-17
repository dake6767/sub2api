package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEffectiveConcurrency_NonAnthropicReturnsRaw 验证非 Anthropic OAuth 账号
// 不受 extra.max_sessions 影响,行为与改造前一致。
func TestEffectiveConcurrency_NonAnthropicReturnsRaw(t *testing.T) {
	cases := []struct {
		name    string
		account Account
		want    int
	}{
		{
			name: "openai oauth ignores max_sessions",
			account: Account{
				Platform:    "openai",
				Type:        AccountTypeOAuth,
				Concurrency: 5,
				Extra:       map[string]any{"max_sessions": 2},
			},
			want: 5,
		},
		{
			name: "anthropic api key ignores max_sessions",
			account: Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Concurrency: 8,
				Extra:       map[string]any{"max_sessions": 2},
			},
			want: 8,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.account.EffectiveConcurrency())
		})
	}
}

// TestEffectiveConcurrency_AnthropicOAuthClampsByMaxSessions 验证 Anthropic OAuth
// 账号在 max_sessions < Concurrency 时被向下夹紧。
func TestEffectiveConcurrency_AnthropicOAuthClampsByMaxSessions(t *testing.T) {
	a := Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 10,
		Extra:       map[string]any{"max_sessions": 2},
	}
	assert.Equal(t, 2, a.EffectiveConcurrency(),
		"max_sessions=2 应当夹紧 admin 误配的 concurrency=10,与 Anthropic 真实上限对齐")
}

// TestEffectiveConcurrency_AnthropicOAuthKeepsConcurrencyWhenLowerOrAbsent 验证当
// max_sessions 缺失或大于 Concurrency 时,保留原 Concurrency 值。
func TestEffectiveConcurrency_AnthropicOAuthKeepsConcurrencyWhenLowerOrAbsent(t *testing.T) {
	cases := []struct {
		name    string
		account Account
		want    int
	}{
		{
			name: "max_sessions absent",
			account: Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				Concurrency: 3,
				Extra:       map[string]any{}, // 无 max_sessions
			},
			want: 3,
		},
		{
			name: "max_sessions higher than concurrency",
			account: Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				Concurrency: 3,
				Extra:       map[string]any{"max_sessions": 10},
			},
			want: 3,
		},
		{
			name: "max_sessions zero treated as unset",
			account: Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				Concurrency: 4,
				Extra:       map[string]any{"max_sessions": 0},
			},
			want: 4,
		},
		{
			name: "setup_token account also clamped",
			account: Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeSetupToken,
				Concurrency: 6,
				Extra:       map[string]any{"max_sessions": 1},
			},
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.account.EffectiveConcurrency())
		})
	}
}

// TestEffectiveConcurrency_ZeroConcurrencyRespectsMaxSessions 当 admin 把
// concurrency 设为 0(无限)而 Anthropic 上游给了真实上限时,生效值取上限。
func TestEffectiveConcurrency_ZeroConcurrencyRespectsMaxSessions(t *testing.T) {
	a := Account{
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 0,
		Extra:       map[string]any{"max_sessions": 2},
	}
	assert.Equal(t, 2, a.EffectiveConcurrency(),
		"concurrency=0 通常表示无限,但 Anthropic 真实上限存在时应被夹紧")
}
