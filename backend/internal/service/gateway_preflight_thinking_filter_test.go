//go:build unit

package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// realSignatureSample 来自实测 dump 的部分前缀，仅作为"非 UUID 形态"代表。
// 真 Anthropic signature 是 base64 加密块，250+ 字符；此处足以与 UUID 形态区分。
const realSignatureSample = "ErUBCkYIBxgCKkAaaXJzZqYjZ6KGKjBzAFBuVHB5R0ZxK1lFRDVtSWNxK0pEaHQ4MGV4emt6V1ZBdEh2bjVXVmd1dHdIVjJsM0NRPT0"

func TestPreflightFilter_FastPathNoThinking(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	out := preflightFilterFakeThinkingSignatures(body)
	require.Equal(t, string(body), string(out), "no thinking blocks → body 必须字节稳定")
}

func TestPreflightFilter_PreservesRealSignature(t *testing.T) {
	// 真 signature（base64-like, 长度 100+）必须原样保留，确保未触发 bug 的请求不受影响。
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 4096,
		},
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "real reasoning content",
						"signature": realSignatureSample,
					},
					map[string]any{"type": "text", "text": "hi"},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)
	// 不应有任何修改
	require.Equal(t, string(body), string(out))
	// thinking 顶层仍存在
	require.True(t, gjson.GetBytes(out, "thinking").Exists())
}

func TestPreflightFilter_ConvertsUUIDThinkingToText(t *testing.T) {
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 4096,
		},
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "fake reasoning content",
						"signature": "c145a4c8-acba-446f-a2f1-d13de5fa05db",
					},
					map[string]any{"type": "text", "text": "hi"},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)

	// block 0 应当被转成 text
	gotType := gjson.GetBytes(out, "messages.0.content.0.type").String()
	require.Equal(t, "text", gotType)
	gotText := gjson.GetBytes(out, "messages.0.content.0.text").String()
	require.Equal(t, "fake reasoning content", gotText)
	// signature 字段应消失
	require.False(t, gjson.GetBytes(out, "messages.0.content.0.signature").Exists())
	// block 1 应保持
	require.Equal(t, "text", gjson.GetBytes(out, "messages.0.content.1.type").String())
	require.Equal(t, "hi", gjson.GetBytes(out, "messages.0.content.1.text").String())
	// 顶层 thinking 必须被删（否则上游报 Expected thinking found text）
	require.False(t, gjson.GetBytes(out, "thinking").Exists())
}

func TestPreflightFilter_DropsRedactedThinking(t *testing.T) {
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "redacted_thinking",
						"data": "encrypted-blob-here",
					},
					map[string]any{"type": "text", "text": "after"},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)

	// redacted 应被删除，原 content[1] 现在变 content[0]
	contentLen := len(gjson.GetBytes(out, "messages.0.content").Array())
	require.Equal(t, 1, contentLen)
	require.Equal(t, "text", gjson.GetBytes(out, "messages.0.content.0.type").String())
	require.Equal(t, "after", gjson.GetBytes(out, "messages.0.content.0.text").String())
}

func TestPreflightFilter_MixedRealAndFake(t *testing.T) {
	// record #9 场景：前面 1 块 real + 后面 1 块 fake，应该只动 fake，real 保留。
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 4096,
		},
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "real one",
						"signature": realSignatureSample,
					},
				},
			},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "go on"}}},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "fake one",
						"signature": "c145a4c8-acba-446f-a2f1-d13de5fa05db",
					},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)

	// real thinking 保留
	require.Equal(t, "thinking", gjson.GetBytes(out, "messages.0.content.0.type").String())
	require.Equal(t, realSignatureSample, gjson.GetBytes(out, "messages.0.content.0.signature").String())

	// fake thinking 已转 text
	require.Equal(t, "text", gjson.GetBytes(out, "messages.2.content.0.type").String())
	require.Equal(t, "fake one", gjson.GetBytes(out, "messages.2.content.0.text").String())

	// 因为有真实 thinking 仍在，本来 top-level thinking 删了对 real block 反而违反约束。
	// 实测：当前实现是"任何修改都删 top-level thinking"，real block 留在那会导致
	// 上游对 real block 报 "structural" 错误。这是已知 trade-off：本场景在 reactive
	// 路径会被 retry 处理，preflight 不专门 handle 这种混合情况。
	require.False(t, gjson.GetBytes(out, "thinking").Exists())
}

func TestPreflightFilter_AllFakeContentBecomesPlaceholder(t *testing.T) {
	// content 全是 fake thinking，转换后所有块都被删（thinking 文本是空字符串）→ 触发占位 text。
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "", // 空 thinking 文本会被当 delete
						"signature": "c145a4c8-acba-446f-a2f1-d13de5fa05db",
					},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)

	// content 空数组应被注入占位 text
	contentArr := gjson.GetBytes(out, "messages.0.content").Array()
	require.Len(t, contentArr, 1)
	require.Equal(t, "text", contentArr[0].Get("type").String())
	require.Equal(t, "(assistant content removed)", contentArr[0].Get("text").String())
}

func TestPreflightFilter_RemovesThinkingDependentContextStrategy(t *testing.T) {
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 4096,
		},
		"context_management": map[string]any{
			"edits": []any{
				map[string]any{"type": "clear_thinking_20251015"},
				map[string]any{"type": "clear_tool_uses_20250919"},
			},
		},
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "fake",
						"signature": "c145a4c8-acba-446f-a2f1-d13de5fa05db",
					},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)

	require.False(t, gjson.GetBytes(out, "thinking").Exists(), "top-level thinking 应被删")
	edits := gjson.GetBytes(out, "context_management.edits").Array()
	require.Len(t, edits, 1)
	require.Equal(t, "clear_tool_uses_20250919", edits[0].Get("type").String())
}

func TestPreflightFilter_UppercaseSignatureNotMatched(t *testing.T) {
	// 真 base64 signature 可能含大写字母；UUID 正则 [0-9a-f] 严格小写，避免误伤。
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "should keep",
						"signature": "C145A4C8-ACBA-446F-A2F1-D13DE5FA05DB", // 大写 hex，不匹配
					},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)
	require.Equal(t, "thinking", gjson.GetBytes(out, "messages.0.content.0.type").String())
}

func TestPreflightFilter_NoSignatureMeansReal(t *testing.T) {
	// 极少见但合法：thinking 块未带 signature 字段（如某些上游响应）→ 不动。
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":     "thinking",
						"thinking": "no signature here",
					},
				},
			},
		},
	})

	out := preflightFilterFakeThinkingSignatures(body)
	require.Equal(t, "thinking", gjson.GetBytes(out, "messages.0.content.0.type").String())
}

func TestPreflightFilter_ByteStabilityForUntouchedBody(t *testing.T) {
	// 即使 thinking 块存在，只要 signature 不匹配 UUID 模式，body 也不应被修改字节。
	// 这关系到 dump 取证：byte-diff 信号必须只来自真实改动。
	body := mustJSON(t, map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "real",
						"signature": realSignatureSample,
					},
				},
			},
		},
	})
	require.Contains(t, string(body), `"type":"thinking"`, "fixture 包含 thinking，应触发非 fast-path 流程")

	out := preflightFilterFakeThinkingSignatures(body)
	require.Equal(t, string(body), string(out), "无任何 UUID 匹配时 body 字节必须完全一致")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	// 确保 messages 字段在 body 中，便于其他断言。
	require.True(t, strings.Contains(string(b), `"messages"`))
	return b
}
