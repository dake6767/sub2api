package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

// buildMimicReq 构造一个最小 *http.Request 供 applyClaudeCodeMimicHeaders 测试使用。
func buildMimicReq(initial http.Header) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	for k, vs := range initial {
		for _, v := range vs {
			req.Header[k] = append(req.Header[k], v)
		}
	}
	return req
}

// TestApplyClaudeCodeMimicHeaders_StainlessRetryCount_PassthroughWhenClientSet
// 客户端已提供 X-Stainless-Retry-Count 时，mimic 不应覆盖为默认 "0"。
func TestApplyClaudeCodeMimicHeaders_StainlessRetryCount_PassthroughWhenClientSet(t *testing.T) {
	req := buildMimicReq(http.Header{"X-Stainless-Retry-Count": []string{"3"}})

	applyClaudeCodeMimicHeaders(req, false)

	assert.Equal(t, "3", getHeaderRaw(req.Header, "X-Stainless-Retry-Count"),
		"应保留客户端提供的 retry-count")
}

// TestApplyClaudeCodeMimicHeaders_StainlessTimeout_PassthroughWhenClientSet
// 客户端已提供 X-Stainless-Timeout 时，mimic 不应覆盖为默认 "600"。
func TestApplyClaudeCodeMimicHeaders_StainlessTimeout_PassthroughWhenClientSet(t *testing.T) {
	req := buildMimicReq(http.Header{"X-Stainless-Timeout": []string{"120"}})

	applyClaudeCodeMimicHeaders(req, false)

	assert.Equal(t, "120", getHeaderRaw(req.Header, "X-Stainless-Timeout"),
		"应保留客户端提供的 timeout")
}

// TestApplyClaudeCodeMimicHeaders_StainlessHeaders_DefaultWhenMissing
// 客户端未提供时，mimic 应填入 DefaultHeaders 的内置默认值。
func TestApplyClaudeCodeMimicHeaders_StainlessHeaders_DefaultWhenMissing(t *testing.T) {
	req := buildMimicReq(http.Header{})

	applyClaudeCodeMimicHeaders(req, false)

	assert.Equal(t, claude.DefaultHeaders["X-Stainless-Retry-Count"],
		getHeaderRaw(req.Header, "X-Stainless-Retry-Count"),
		"缺失时应填充 DefaultHeaders 中的默认值")
	assert.Equal(t, claude.DefaultHeaders["X-Stainless-Timeout"],
		getHeaderRaw(req.Header, "X-Stainless-Timeout"),
		"缺失时应填充 DefaultHeaders 中的默认值")
}

// TestApplyClaudeCodeMimicHeaders_NonClientDrivenStillForced
// 其他 stainless 头（如 Lang / OS / Runtime）仍应强制覆盖为 DefaultHeaders 值，
// 避免客户端伪造破坏整体伪装一致性。
func TestApplyClaudeCodeMimicHeaders_NonClientDrivenStillForced(t *testing.T) {
	req := buildMimicReq(http.Header{
		"X-Stainless-Lang":    []string{"python"},
		"X-Stainless-OS":      []string{"Windows"},
		"X-Stainless-Runtime": []string{"cpython"},
	})

	applyClaudeCodeMimicHeaders(req, false)

	assert.Equal(t, claude.DefaultHeaders["X-Stainless-Lang"],
		getHeaderRaw(req.Header, "X-Stainless-Lang"),
		"非客户端驱动字段应强制覆盖")
	assert.Equal(t, claude.DefaultHeaders["X-Stainless-OS"],
		getHeaderRaw(req.Header, "X-Stainless-OS"))
	assert.Equal(t, claude.DefaultHeaders["X-Stainless-Runtime"],
		getHeaderRaw(req.Header, "X-Stainless-Runtime"))
}

// TestIsClientDrivenStainlessHeader 覆盖辅助函数的大小写与未知 key 场景。
func TestIsClientDrivenStainlessHeader(t *testing.T) {
	assert.True(t, isClientDrivenStainlessHeader("X-Stainless-Retry-Count"))
	assert.True(t, isClientDrivenStainlessHeader("x-stainless-retry-count"))
	assert.True(t, isClientDrivenStainlessHeader("X-Stainless-Timeout"))
	assert.False(t, isClientDrivenStainlessHeader("X-Stainless-Lang"))
	assert.False(t, isClientDrivenStainlessHeader("User-Agent"))
}
