package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sync/atomic"
	"unsafe"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// preflightUUIDSignaturePattern 匹配 agent-sdk 占位符 signature。
//
// 真 Anthropic signature 是 base64 加密块，250+ 字符，形如 "ErUBCkYIBxgCKkA..."。
// 假 signature (observed in agent-sdk/0.2.x) 是 36 字符 UUID，形如
// "c145a4c8-acba-446f-a2f1-d13de5fa05db"（小写 hex + 4 个短横线）。
//
// 用严格正则而非长度阈值，避免 100~250 字符边界区误判（该区间无实测数据支撑）。
var preflightUUIDSignaturePattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

// Preflight thinking filter telemetry counters。
//
// 默认 setting=false，灰度激活时需要数据验证 filter 是否产生预期效果。
// 单进程内 atomic 计数；外部 expose 端点可按需读取（参考 windowCostPrefetch* 风格）。
var (
	preflightThinkingConvertedTotal atomic.Int64 // thinking 块（UUID sig）→ text 转换次数
	preflightThinkingRedactedTotal  atomic.Int64 // redacted_thinking 块删除次数
	preflightThinkingMsgScannedTotal atomic.Int64 // 至少包含一个 thinking 类型块的请求数（fast-path 已通过）
)

// preflightFilterFakeThinkingSignatures 在入站 body 即将转发给上游前，主动清理
// agent-sdk 客户端注入的伪 thinking signature（UUID 占位符）。
//
// 行为（与 reactive FilterThinkingBlocksForRetry 策略 B 对齐）：
//   - "thinking" 块且 signature 匹配 UUID 正则 → 转 text 块（保留思考内容）
//   - "redacted_thinking" 块 → 整块删除（无 thinking 字段无法转 text）
//   - 任意修改后，删 top-level "thinking" 字段 + context_management.edits.clear_thinking_20251015
//     （否则上游会报 "Expected `thinking` or `redacted_thinking`, but found `text`"）
//   - message.content 转换后为空 → 替换为占位 text（避免 "non-empty content" 400）
//
// 真 signature（base64 加密块）的 thinking 块原样保留，确保未触发 bug 的请求不受影响。
//
// 仅当 RectifierSettings.PreflightThinkingFilterEnabled = true 时调用。
//
// 实现要求：尽量保持 body 字节稳定（其他字段、未触发块的字节不应变化），用 sjson
// 对单条 content 路径精细替换；不要 json.Unmarshal/Marshal 全 body。
func preflightFilterFakeThinkingSignatures(body []byte) []byte {
	// Fast path：没有任何 thinking 类型块直接返回，避免热路径开销。
	if !bytes.Contains(body, patternTypeThinking) &&
		!bytes.Contains(body, patternTypeThinkingSpaced) &&
		!bytes.Contains(body, patternTypeRedactedThinking) &&
		!bytes.Contains(body, patternTypeRedactedSpaced) {
		return body
	}

	preflightThinkingMsgScannedTotal.Add(1)

	jsonStr := *(*string)(unsafe.Pointer(&body))
	msgsRes := gjson.Get(jsonStr, "messages")
	if !msgsRes.Exists() || !msgsRes.IsArray() {
		return body
	}

	// 第一遍扫描：收集需要修改的位置（message index, content index, kind, 文本内容）。
	// 不在 ForEach 内直接 sjson 修改 body，否则 body 字节变化会让后续 path 失效。
	type pendingEdit struct {
		msgIdx   int
		contIdx  int
		kind     string // "convert" | "delete"
		textOnly string // for convert: 原 thinking 文本
	}
	var edits []pendingEdit

	msgsRes.ForEach(func(msgIdxRes, msg gjson.Result) bool {
		msgIdx := int(msgIdxRes.Int())
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			return true
		}
		content.ForEach(func(contIdxRes, block gjson.Result) bool {
			contIdx := int(contIdxRes.Int())
			blockType := block.Get("type").String()
			switch blockType {
			case "thinking":
				sig := block.Get("signature").String()
				if sig == "" || !preflightUUIDSignaturePattern.MatchString(sig) {
					// 真 signature 或无 signature，保留
					return true
				}
				edits = append(edits, pendingEdit{
					msgIdx:   msgIdx,
					contIdx:  contIdx,
					kind:     "convert",
					textOnly: block.Get("thinking").String(),
				})
			case "redacted_thinking":
				edits = append(edits, pendingEdit{
					msgIdx:  msgIdx,
					contIdx: contIdx,
					kind:    "delete",
				})
			}
			return true
		})
		return true
	})

	if len(edits) == 0 {
		return body
	}

	// 第二遍：反向应用，避免 delete 改变后续 index。
	// 同一 message 内按 contIdx 倒序、跨 message 按 msgIdx 倒序。
	// edits 是按正序收集的，反向遍历即可。
	out := body
	convertedCount := 0
	deletedCount := 0
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		switch e.kind {
		case "convert":
			// 用整块替换：{"type":"text","text":<thinkingText>}
			// 整块替换的 ordering 比 type-change + delete-fields + add-text 更干净。
			newBlock := map[string]any{
				"type": "text",
				"text": e.textOnly,
			}
			if e.textOnly == "" {
				// 空 thinking 文本 → 当作 delete（避免上游 "non-empty content"）
				if nb, err := sjson.DeleteBytes(out, sjsonPath("messages", e.msgIdx, "content", e.contIdx)); err == nil {
					out = nb
					deletedCount++
				}
				continue
			}
			raw, err := json.Marshal(newBlock)
			if err != nil {
				continue
			}
			if nb, err := sjson.SetRawBytes(out, sjsonPath("messages", e.msgIdx, "content", e.contIdx), raw); err == nil {
				out = nb
				convertedCount++
			}
		case "delete":
			if nb, err := sjson.DeleteBytes(out, sjsonPath("messages", e.msgIdx, "content", e.contIdx)); err == nil {
				out = nb
				deletedCount++
			}
		}
	}

	// 应用任何修改后，必须同步清理顶层 thinking 配置和依赖它的 context_management 策略。
	if topThinking := gjson.GetBytes(out, "thinking"); topThinking.Exists() {
		if nb, err := sjson.DeleteBytes(out, "thinking"); err == nil {
			out = nb
		}
	}
	out = removeThinkingDependentContextStrategies(out)

	// 删块后若有 message 的 content 完全空（"content":[]）→ 替换为占位 text。
	// 这与 reactive FilterThinkingBlocksForRetry 处理空 content 的策略一致。
	out = ensureMessagesHaveNonEmptyContent(out)

	if convertedCount > 0 {
		preflightThinkingConvertedTotal.Add(int64(convertedCount))
	}
	if deletedCount > 0 {
		preflightThinkingRedactedTotal.Add(int64(deletedCount))
	}

	return out
}

// ensureMessagesHaveNonEmptyContent 扫描 messages[]，对 content 为空数组的消息
// 注入占位 text 块（"(content removed)" / "(assistant content removed)"），
// 避免上游返回 "all messages must have non-empty content" 400。
func ensureMessagesHaveNonEmptyContent(body []byte) []byte {
	jsonStr := *(*string)(unsafe.Pointer(&body))
	msgsRes := gjson.Get(jsonStr, "messages")
	if !msgsRes.Exists() || !msgsRes.IsArray() {
		return body
	}

	out := body
	idx := 0
	msgsRes.ForEach(func(_, msg gjson.Result) bool {
		defer func() { idx++ }()
		content := msg.Get("content")
		if !content.Exists() {
			return true
		}
		if content.IsArray() && len(content.Array()) > 0 {
			return true
		}
		// 字符串 content 不动；只处理空数组。
		if !content.IsArray() {
			return true
		}

		role := msg.Get("role").String()
		placeholder := "(content removed)"
		if role == "assistant" {
			placeholder = "(assistant content removed)"
		}
		placeholderBlock := []map[string]any{{"type": "text", "text": placeholder}}
		raw, err := json.Marshal(placeholderBlock)
		if err != nil {
			return true
		}
		if nb, err := sjson.SetRawBytes(out, sjsonPath("messages", idx, "content"), raw); err == nil {
			out = nb
		}
		return true
	})
	return out
}

// sjsonPath 拼接 sjson 路径。
// e.g. sjsonPath("messages", 3, "content", 0) → "messages.3.content.0"
func sjsonPath(parts ...any) string {
	// 路径段不会包含 sjson 元字符（数组 index + 固定 key），直接 fmt.Sprint。
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "."
		}
		out += fmt.Sprint(p)
	}
	return out
}
