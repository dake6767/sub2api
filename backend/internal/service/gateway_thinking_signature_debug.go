package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// thinking signature debug dumper —— 当上游返回 "Invalid `signature` in `thinking` block" /
// "thinking ... cannot be modified" 一类 400 错误时,把这次请求的入站 body(sub2api 修改前)
// 与出站 body(sub2api 即将发给上游) 字节级落到一份**专用日志文件**,供事后 diff 定位
// 是哪一步 transform 破坏了 thinking 块的字节稳定性。
//
// 设计:
//   - 仅在 isThinkingBlockSignatureError 命中时触发,**不影响正常请求路径**
//   - 文件 mode 600,owner 由进程身份决定;不写 stdout/journalctl
//   - per-file 流式追加,内部用 mutex 序列化;预期 prod 触发频率 < 5/min,
//     不需要额外节流
//   - 防文件无限增长:超过 maxDumpFileBytes (50 MB) 时,自动轮转一次到
//     {path}.1,新写到原路径;**只保留两代**,旧的 .1 会被新一次轮转覆盖
//   - body 完整记录(因为 thinking 块字节级 diff 必须看全量);
//     单条记录用 \x1e (record separator) 分隔,字段用 JSON one-line
//
// 与上游 LogUpstreamErrorBody 的分工:
//   - 上游 cfg.Gateway.LogUpstreamErrorBody=true 已经把 response body(截断)
//     写进 ops_error_logs 的 Detail 字段,response body 不再在本 dumper 里重复
//   - 本 dumper 的差异化价值 = 入站 body + 出站 body 字节级 diff
//     (用于定位 sub2api 哪一步 transform 改坏了 thinking 块),
//     这是上游 LogUpstreamErrorBody 覆盖不到的

const (
	thinkingSigDumpEnvPath  = "SUB2API_THINKING_SIG_DUMP_PATH"
	thinkingSigDumpDefault  = "/var/log/sub2api/thinking-signature-debug.log"
	thinkingSigDumpFallback = "/tmp/sub2api-thinking-signature-debug.log"
	maxDumpFileBytes        = 50 * 1024 * 1024
)

// ginContextKeyOutboundBody 出站 body 通过 gin context 在 buildUpstreamRequest
// 与 signature error 检测点之间传递。仅在 thinking signature debug 启用时设置。
const ginContextKeyOutboundBody = "sub2api:last_outbound_body"

var (
	thinkingSigDumpMu       sync.Mutex
	thinkingSigDumpFilePath string
	thinkingSigDumpInitOnce sync.Once
)

// stashOutboundBodyForDebug 在 buildUpstreamRequest 完成所有 transform 之后调用,
// 把最终出站 body 暂存到 gin context,供后续 signature error 检测点取出 diff。
// 复制 body 字节,避免 gin context 持有后被外部 mutate。
func stashOutboundBodyForDebug(c *gin.Context, body []byte) {
	if c == nil || len(body) == 0 {
		return
	}
	dup := make([]byte, len(body))
	copy(dup, body)
	c.Set(ginContextKeyOutboundBody, dup)
}

// dumpThinkingSignatureError 当 isThinkingBlockSignatureError 命中时,把这次请求的
// 入站 body + 出站 body 写到专用日志文件,供事后字节级 diff 定位是哪一步 transform
// 改坏了 thinking 块。上游响应 body 不在这里记录(由上游 LogUpstreamErrorBody 写入
// ops_error_logs.Detail),可通过 extras 里的 upstream_request_id 关联。
//
// 入参语义:
//   - inboundBody:Forward() 在 StripEmptyTextBlocks 后、buildUpstreamRequest 之前
//     的 body 字节,即 mimic/RewriteUserID/syncBilling/CCH 改写之前的状态
//   - extra:用于关联的上下文字段(account_id, client_request_id, upstream_request_id, ...)
func dumpThinkingSignatureError(c *gin.Context, inboundBody []byte, extra map[string]any) {
	outboundBody := outboundBodyFromContext(c)

	// inboundBody / outboundBody 至少一份有内容才值得写
	if len(inboundBody) == 0 && len(outboundBody) == 0 {
		return
	}

	thinkingSigDumpInitOnce.Do(initThinkingSigDumpPath)
	path := thinkingSigDumpFilePath
	if path == "" {
		return
	}

	thinkingSigDumpMu.Lock()
	defer thinkingSigDumpMu.Unlock()

	rotateThinkingSigDumpIfNeededLocked(path)

	record := map[string]any{
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"inbound_body":      string(inboundBody),  // 入站(StripEmptyTextBlocks 后,mimic/identity/billing 改写前)
		"outbound_body":     string(outboundBody), // 出站(所有 transform 完成,即将发给 Anthropic)
		"inbound_body_len":  len(inboundBody),
		"outbound_body_len": len(outboundBody),
	}
	for k, v := range extra {
		record[k] = v
	}

	line, err := json.Marshal(record)
	if err != nil {
		logger.LegacyPrintf("service.gateway.thinking_sig_dump", "marshal failed: %v", err)
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		logger.LegacyPrintf("service.gateway.thinking_sig_dump", "open %s failed: %v", path, err)
		return
	}
	defer f.Close()

	// \x1e (record separator) 在 JSON 内安全(不会出现在合法 JSON 中),便于事后 split
	_, _ = f.WriteString(string(line) + "\x1e\n")
}

func outboundBodyFromContext(c *gin.Context) []byte {
	if c == nil {
		return nil
	}
	v, ok := c.Get(ginContextKeyOutboundBody)
	if !ok {
		return nil
	}
	b, ok := v.([]byte)
	if !ok {
		return nil
	}
	return b
}

func initThinkingSigDumpPath() {
	candidate := os.Getenv(thinkingSigDumpEnvPath)
	if candidate == "" {
		candidate = thinkingSigDumpDefault
	}

	// 确保目录存在;如果创建失败,fallback 到 /tmp 让 dev 也能工作
	dir := filepath.Dir(candidate)
	if err := os.MkdirAll(dir, 0700); err != nil {
		logger.LegacyPrintf("service.gateway.thinking_sig_dump", "mkdir %s failed, fallback to %s: %v",
			dir, thinkingSigDumpFallback, err)
		candidate = thinkingSigDumpFallback
		_ = os.MkdirAll(filepath.Dir(candidate), 0700)
	}

	// 预创建一次,失败也允许 —— OpenFile 会再试一次
	if f, err := os.OpenFile(candidate, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err == nil {
		_ = f.Close()
	}
	thinkingSigDumpFilePath = candidate
	logger.LegacyPrintf("service.gateway.thinking_sig_dump", "thinking signature debug dump path = %s", candidate)
}

// rotateThinkingSigDumpIfNeededLocked 在持锁状态下检查文件尺寸,超过则 rename → .1。
// 失败不阻塞写入。
func rotateThinkingSigDumpIfNeededLocked(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Size() < maxDumpFileBytes {
		return
	}
	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		logger.LegacyPrintf("service.gateway.thinking_sig_dump", "rotate %s -> %s failed: %v", path, rotated, err)
		return
	}
	logger.LegacyPrintf("service.gateway.thinking_sig_dump", "rotated %s -> %s (was %d bytes)", path, rotated, info.Size())
}

// buildDumpExtras 把检测到 signature error 时的上下文打包到 extras map 供 dump 写入。
func buildDumpExtras(account *Account, c *gin.Context, upstreamReqID string) map[string]any {
	extras := map[string]any{}
	if account != nil {
		extras["account_id"] = account.ID
		extras["account_name"] = account.Name
		extras["platform"] = account.Platform
		extras["type"] = account.Type
	}
	if c != nil && c.Request != nil {
		if reqID := c.GetHeader("x-request-id"); reqID != "" {
			extras["client_request_id"] = reqID
		}
		extras["client_user_agent"] = c.Request.Header.Get("User-Agent")
		extras["path"] = c.Request.URL.Path
	}
	if upstreamReqID != "" {
		extras["upstream_request_id"] = upstreamReqID
	}
	extras["stage"] = fmt.Sprintf("invalid_thinking_signature_first_400")
	return extras
}
