package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

// defaultFingerprintSalt 是计算 cc_version 后缀指纹的内置默认盐值。
//
// 来源：与 Parrot src/transform/cc_mimicry.py 的 FINGERPRINT_SALT 完全一致；
// 这是真实 Claude Code CLI 抓包推导出的常量，不改动时 fp 与 CLI 对齐。
//
// 自部署可通过 config.yaml 的 fingerprint.salt 覆盖，消除"全网 sub2api 共用同一水印"的指纹；
// 改动后 fp 与真实 CLI 不再一致，注意观察 Anthropic 的第三方检测反应。
const defaultFingerprintSalt = "59cf53e54c78"

// fingerprintSalt 为生效的盐值，启动时由 ConfigureFingerprint 根据配置覆盖，未配置时等于 defaultFingerprintSalt。
var fingerprintSalt = defaultFingerprintSalt

// computeClaudeCodeFingerprint 复刻真实 Claude Code CLI 的 cc_version 指纹算法：
//
//  1. 取 messages 中第一条 role=user 的纯文本（首块 text）
//  2. 取该文本的第 4、7、20 字符（不足以 '0' 补齐）
//  3. SHA256(SALT + chars + cc_version) 取 hex 前 3 字符
//
// 算法来自 Parrot src/transform/cc_mimicry.py:compute_fingerprint，与官方 CLI 字节对齐。
// 任何偏差都会导致 cc_version=X.Y.Z.{fp} 在上游侧与真实 CLI 不一致。
func computeClaudeCodeFingerprint(body []byte, version string) string {
	firstText := extractFirstUserText(body)
	indices := []int{4, 7, 20}
	chars := make([]byte, 0, 3)
	for _, i := range indices {
		if i < len(firstText) {
			chars = append(chars, firstText[i])
		} else {
			chars = append(chars, '0')
		}
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + string(chars) + version))
	return hex.EncodeToString(sum[:])[:3]
}

// extractFirstUserText 提取 messages 中第一条 user 消息的首段 text 内容。
// 兼容 string 和 []block 两种 content 格式。
func extractFirstUserText(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return ""
	}
	first := ""
	messages.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() != "user" {
			return true
		}
		content := msg.Get("content")
		if content.Type == gjson.String {
			first = content.String()
			return false
		}
		if content.IsArray() {
			content.ForEach(func(_, block gjson.Result) bool {
				if block.Get("type").String() == "text" {
					first = block.Get("text").String()
					return false
				}
				return true
			})
			return false
		}
		return false
	})
	return first
}

// buildBillingAttributionBlockJSON 构造 system 数组的 billing attribution block。
//
// 形态严格对齐真实 Claude Code CLI：
//
//	{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.140.{fp}; cc_entrypoint=cli; cch=00000;"}
//
// cch=00000 是签名占位符，由 signBillingHeaderCCH 在 buildUpstreamRequest 阶段
// 替换为基于完整 body 的 xxhash64 5 位十六进制摘要。
//
// 此 block 不带 cache_control（与真实 CLI 一致；cache breakpoint 由后续的
// Claude Code prompt block 承担）。
func buildBillingAttributionBlockJSON(body []byte, cliVersion string) ([]byte, error) {
	if cliVersion == "" {
		return nil, fmt.Errorf("cliVersion required")
	}
	fp := computeClaudeCodeFingerprint(body, cliVersion)
	text := fmt.Sprintf(
		"x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli; cch=00000;",
		cliVersion, fp,
	)
	return json.Marshal(map[string]string{
		"type": "text",
		"text": text,
	})
}
