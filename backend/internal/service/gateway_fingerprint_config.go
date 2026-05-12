package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ConfigureFingerprint 根据配置覆盖 fingerprintSalt / cchSeed 生效值。
//
// - 未配置任何字段时保持默认（与真实 Claude Code CLI 对齐）。
// - salt 接受任意非空字符串，cch_seed 接受 "0x…" 或十进制字符串。
// - 解析 cch_seed 失败时返回错误，调用方应让启动中断，避免悄悄以默认值运行。
//
// 必须在 GatewayService 构造完成前完成调用，且全进程内只应调用一次。
func ConfigureFingerprint(cfg config.FingerprintConfig) error {
	salt := strings.TrimSpace(cfg.Salt)
	if salt != "" {
		fingerprintSalt = salt
	}

	rawSeed := strings.TrimSpace(cfg.CCHSeed)
	if rawSeed != "" {
		seed, err := parseCCHSeed(rawSeed)
		if err != nil {
			return fmt.Errorf("fingerprint.cch_seed invalid: %w", err)
		}
		cchSeed = seed
	}
	return nil
}

func parseCCHSeed(raw string) (uint64, error) {
	base := 10
	digits := raw
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		base = 16
		digits = raw[2:]
	}
	return strconv.ParseUint(digits, base, 64)
}
