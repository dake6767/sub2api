package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestConfigureOpenAIFingerprint(t *testing.T) {
	// 测试前后恢复默认值，避免跨测试污染
	origUA, origVer := codexCLIUserAgent, codexCLIVersion
	origOfficial, origFallback := openAIOriginatorOfficial, openAIOriginatorFallback
	t.Cleanup(func() {
		codexCLIUserAgent = origUA
		codexCLIVersion = origVer
		openAIOriginatorOfficial = origOfficial
		openAIOriginatorFallback = origFallback
	})

	t.Run("empty config preserves defaults", func(t *testing.T) {
		codexCLIUserAgent = defaultCodexCLIUserAgent
		codexCLIVersion = defaultCodexCLIVersion
		openAIOriginatorOfficial = defaultOpenAIOriginatorOfficial
		openAIOriginatorFallback = defaultOpenAIOriginatorFallback

		ConfigureOpenAIFingerprint(config.OpenAIFingerprintConfig{})

		if codexCLIUserAgent != defaultCodexCLIUserAgent {
			t.Errorf("user_agent drifted: got %q want %q", codexCLIUserAgent, defaultCodexCLIUserAgent)
		}
		if codexCLIVersion != defaultCodexCLIVersion {
			t.Errorf("version drifted: got %q want %q", codexCLIVersion, defaultCodexCLIVersion)
		}
		if openAIOriginatorOfficial != defaultOpenAIOriginatorOfficial {
			t.Errorf("originator_official drifted: got %q", openAIOriginatorOfficial)
		}
		if openAIOriginatorFallback != defaultOpenAIOriginatorFallback {
			t.Errorf("originator_fallback drifted: got %q", openAIOriginatorFallback)
		}
	})

	t.Run("each field overrides independently", func(t *testing.T) {
		codexCLIUserAgent = defaultCodexCLIUserAgent
		codexCLIVersion = defaultCodexCLIVersion
		openAIOriginatorOfficial = defaultOpenAIOriginatorOfficial
		openAIOriginatorFallback = defaultOpenAIOriginatorFallback

		ConfigureOpenAIFingerprint(config.OpenAIFingerprintConfig{
			UserAgent:          "codex_cli_rs/0.128.0",
			Version:            "0.128.0",
			OriginatorOfficial: "codex_cli_rs",
			OriginatorFallback: "my-private-label",
		})

		if got, want := codexCLIUserAgent, "codex_cli_rs/0.128.0"; got != want {
			t.Errorf("user_agent: got %q want %q", got, want)
		}
		if got, want := codexCLIVersion, "0.128.0"; got != want {
			t.Errorf("version: got %q want %q", got, want)
		}
		if got, want := openAIOriginatorFallback, "my-private-label"; got != want {
			t.Errorf("originator_fallback: got %q want %q", got, want)
		}
	})

	t.Run("blank fields keep existing values", func(t *testing.T) {
		codexCLIUserAgent = "prev-ua"
		codexCLIVersion = "prev-ver"
		openAIOriginatorOfficial = "prev-official"
		openAIOriginatorFallback = "prev-fallback"

		ConfigureOpenAIFingerprint(config.OpenAIFingerprintConfig{
			UserAgent: "   ",
			Version:   "",
		})

		if codexCLIUserAgent != "prev-ua" {
			t.Errorf("blank ua should not overwrite, got %q", codexCLIUserAgent)
		}
		if codexCLIVersion != "prev-ver" {
			t.Errorf("empty version should not overwrite, got %q", codexCLIVersion)
		}
		if openAIOriginatorOfficial != "prev-official" || openAIOriginatorFallback != "prev-fallback" {
			t.Errorf("omitted fields must not drift")
		}
	})

	t.Run("defaults match hardcoded byte values", func(t *testing.T) {
		// 防止有人不小心改了 default* 常量但忘了改测试基线
		if defaultCodexCLIUserAgent != "codex_cli_rs/0.125.0" {
			t.Errorf("default UA drifted: %q", defaultCodexCLIUserAgent)
		}
		if defaultCodexCLIVersion != "0.125.0" {
			t.Errorf("default version drifted: %q", defaultCodexCLIVersion)
		}
		if defaultOpenAIOriginatorOfficial != "codex_cli_rs" {
			t.Errorf("default official originator drifted: %q", defaultOpenAIOriginatorOfficial)
		}
		if defaultOpenAIOriginatorFallback != "opencode" {
			t.Errorf("default fallback originator drifted: %q", defaultOpenAIOriginatorFallback)
		}
	})
}
