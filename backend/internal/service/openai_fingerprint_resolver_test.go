package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// ginTestContext 构造一个带 User-Agent 头的 *gin.Context,仅用于 resolver 单测。
// 传空字符串表示不设置 UA 头。
func ginTestContext(userAgent string) *gin.Context {
	req := httptest.NewRequest("POST", "/v1/responses", nil)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestResolveOpenAIUpstreamCodexRelease_ClientCodexUAPassthrough(t *testing.T) {
	// 客户端发来官方 Codex UA —— 必须原样透传,并从中抽出 version
	c := ginTestContext("codex_cli_rs/0.131.0")
	acc := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	got := resolveOpenAIUpstreamCodexRelease(c, acc)

	if got.UserAgent != "codex_cli_rs/0.131.0" {
		t.Errorf("UA should be passed through, got %q", got.UserAgent)
	}
	if got.Version != "0.131.0" {
		t.Errorf("version should be extracted from client UA, got %q", got.Version)
	}
}

func TestResolveOpenAIUpstreamCodexRelease_NonCodexUA_AccountCustomWins(t *testing.T) {
	// 非 Codex UA + account 自定义 UA —— 用 account 的自定义 UA,version 回落默认
	c := ginTestContext("curl/8.4.0")
	acc := &Account{
		ID:          99,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"user_agent": "my-label/1.0"},
	}

	got := resolveOpenAIUpstreamCodexRelease(c, acc)

	if got.UserAgent != "my-label/1.0" {
		t.Errorf("account custom UA should win over client non-Codex UA, got %q", got.UserAgent)
	}
	if got.Version != codexCLIVersion {
		t.Errorf("version should fall back to default when custom UA is used, got %q", got.Version)
	}
}

func TestResolveOpenAIUpstreamCodexRelease_NonCodexUA_NoCustom_UsesPool(t *testing.T) {
	// 非 Codex UA + 无 account 自定义 —— 按 accountID hash 到 pool
	c := ginTestContext("curl/8.4.0")
	acc := &Account{ID: 12345, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	got := resolveOpenAIUpstreamCodexRelease(c, acc)
	want := pickCodexCLIRelease(acc.ID)

	if got != want {
		t.Errorf("should pick from pool by accountID, got %+v want %+v", got, want)
	}
}

func TestResolveOpenAIUpstreamCodexRelease_NilAccount_FallsBackToDefaults(t *testing.T) {
	// 无 account —— 回落到当前生效的默认 UA/version
	c := ginTestContext("curl/8.4.0")

	got := resolveOpenAIUpstreamCodexRelease(c, nil)

	if got.UserAgent != codexCLIUserAgent || got.Version != codexCLIVersion {
		t.Errorf("nil account should fall back to effective defaults, got %+v", got)
	}
}

func TestResolveVersionForClientUA(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want string
	}{
		{"standard codex cli", "codex_cli_rs/0.128.0", "0.128.0"},
		{"trailing space-separated token", "codex_cli_rs/0.125.0 extra", "0.125.0"},
		{"non-codex UA falls back", "curl/8.4.0", codexCLIVersion},
		{"empty UA falls back", "", codexCLIVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersionForClientUA(tc.ua); got != tc.want {
				t.Errorf("ua=%q: got %q want %q", tc.ua, got, tc.want)
			}
		})
	}
}
