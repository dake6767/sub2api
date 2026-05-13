package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ConfigureOpenAIFingerprint 根据配置覆盖 codex/openai 出站标识生效值。
//
// - 未配置任何字段时保持默认（与真实 Codex CLI 0.125.0 对齐）。
// - 各字段独立覆盖；空字段不会触发覆盖，保持当前默认值。
// - 不做合法性校验：此层仅搬运，语义校验交给上游 Codex 侧。
//
// 必须在 OpenAIGatewayService 构造完成前完成调用，且全进程内只应调用一次。
func ConfigureOpenAIFingerprint(cfg config.OpenAIFingerprintConfig) {
	if ua := strings.TrimSpace(cfg.UserAgent); ua != "" {
		codexCLIUserAgent = ua
	}
	if version := strings.TrimSpace(cfg.Version); version != "" {
		codexCLIVersion = version
	}
	if v := strings.TrimSpace(cfg.OriginatorOfficial); v != "" {
		openAIOriginatorOfficial = v
	}
	if v := strings.TrimSpace(cfg.OriginatorFallback); v != "" {
		openAIOriginatorFallback = v
	}
}
