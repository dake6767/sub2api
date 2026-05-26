package service

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetThinkingSigDumpForTest 重置全局状态用于隔离测试。
func resetThinkingSigDumpForTest(t *testing.T, customPath string) {
	t.Helper()
	thinkingSigDumpMu.Lock()
	thinkingSigDumpFilePath = ""
	thinkingSigDumpInitOnce = sync.Once{}
	thinkingSigDumpMu.Unlock()

	if customPath != "" {
		t.Setenv(thinkingSigDumpEnvPath, customPath)
	}
}

func TestDumpThinkingSignatureError_WritesJSONLineWithBothBodies(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "thinking-sig.log")
	resetThinkingSigDumpForTest(t, logPath)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 模拟 buildUpstreamRequest 的 stash 调用
	outbound := []byte(`{"out":"body","thinking":{"signature":"sig-XYZ"}}`)
	stashOutboundBodyForDebug(c, outbound)

	inbound := []byte(`{"in":"body","messages":[{"role":"assistant","content":[{"type":"thinking","signature":"sig-ABC"}]}]}`)

	extras := map[string]any{
		"account_id":          int64(14),
		"upstream_request_id": "req_TEST",
	}
	dumpThinkingSignatureError(c, inbound, extras)

	raw, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.NotEmpty(t, raw, "dump should have written a record")

	// 单条记录以 \x1e\n 结尾
	require.True(t, strings.HasSuffix(string(raw), "\x1e\n"))
	jsonLine := strings.TrimSuffix(string(raw), "\x1e\n")

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonLine), &got))

	assert.Equal(t, string(inbound), got["inbound_body"])
	assert.Equal(t, string(outbound), got["outbound_body"])
	// upstream_resp_body 由上游 LogUpstreamErrorBody 写 ops_error_logs.Detail,
	// 这里不再重复记录,通过 upstream_request_id 关联
	_, hasResp := got["upstream_resp_body"]
	assert.False(t, hasResp, "upstream_resp_body 应从 dumper 记录中移除")
	assert.EqualValues(t, len(inbound), got["inbound_body_len"])
	assert.EqualValues(t, len(outbound), got["outbound_body_len"])
	assert.EqualValues(t, 14, got["account_id"])
	assert.Equal(t, "req_TEST", got["upstream_request_id"])
	assert.NotEmpty(t, got["ts"])
}

func TestDumpThinkingSignatureError_NoOutboundStillDumpsInbound(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "thinking-sig.log")
	resetThinkingSigDumpForTest(t, logPath)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	// 故意不 stash 出站 body —— 模拟 stash 失败或调用顺序错的边缘 case

	inbound := []byte(`{"in":"body"}`)
	dumpThinkingSignatureError(c, inbound, nil)

	raw, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	jsonLine := strings.TrimSuffix(string(raw), "\x1e\n")
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonLine), &got))

	assert.Equal(t, string(inbound), got["inbound_body"])
	assert.Equal(t, "", got["outbound_body"])
	assert.EqualValues(t, 0, got["outbound_body_len"])
}

func TestDumpThinkingSignatureError_EmptyBodiesNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "thinking-sig.log")
	resetThinkingSigDumpForTest(t, logPath)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 入站与出站都为空,不应写文件(避免脏数据)
	dumpThinkingSignatureError(c, nil, nil)

	_, err := os.Stat(logPath)
	assert.True(t, os.IsNotExist(err), "no record should have been written for empty inbound+outbound")
}

func TestRotateThinkingSigDumpIfNeededLocked_RenamesWhenOver(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "thinking-sig.log")
	resetThinkingSigDumpForTest(t, logPath)

	// 写一个超过 maxDumpFileBytes 的假数据
	huge := make([]byte, maxDumpFileBytes+1024)
	require.NoError(t, os.WriteFile(logPath, huge, 0600))

	thinkingSigDumpMu.Lock()
	rotateThinkingSigDumpIfNeededLocked(logPath)
	thinkingSigDumpMu.Unlock()

	_, errOrig := os.Stat(logPath)
	assert.True(t, os.IsNotExist(errOrig), "original path should have been moved away")

	rotatedInfo, err := os.Stat(logPath + ".1")
	require.NoError(t, err)
	assert.Equal(t, int64(len(huge)), rotatedInfo.Size())
}
