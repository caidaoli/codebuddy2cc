package handlers

import (
	"bytes"
	"codebuddy2cc/utils"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// getGoroutineID 获取当前goroutine的ID（仅用于调试）
func getGoroutineID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = b[len("goroutine "):]
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}

// 🔧 移除全局会话管理器 - 已改为请求级别独立会话
// 每个请求都创建完全独立的会话，避免任何跨请求状态污染

// extractSessionID 从请求中提取session ID用于调试追踪
// 🔧 修复：为每个请求生成唯一的会话ID，避免会话混淆

// SSEStreamState 增强的SSE流状态管理器 - 单goroutine访问，无需并发保护
// 🔧 核心优化：移除Mutex，因为每个请求都在独立goroutine中顺序访问
type SSEStreamState struct {
	messageStartSent    bool
	contentBlockStarted bool
	streamFinished      bool
	messageID           string
	messageModel        string
	currentBlockIndex   int
	toolCallsActive     bool

	// 🔧 新增：事件序列管理和验证
	eventHistory      []string                 // 已发送的事件历史
	validationEnabled bool                     // 是否启用验证
	sequenceValidator *utils.SSEEventValidator // 事件序列验证器
	lastEventTime     time.Time                // 最后事件时间
	errorCount        int                      // 错误计数

	// 🔧 性能优化：移除mutex，因为单请求单goroutine访问模式
}

// NewSSEStreamState 创建新的增强SSE流状态管理器
// 🔧 核心修复：初始化事件序列验证功能
func NewSSEStreamState() *SSEStreamState {
	return &SSEStreamState{
		messageStartSent:    false,
		contentBlockStarted: false,
		streamFinished:      false,
		messageID:           "",
		messageModel:        "",
		currentBlockIndex:   0,
		toolCallsActive:     false,

		// 🔧 新增：初始化事件序列管理
		eventHistory:      make([]string, 0, 10),
		validationEnabled: true, // 默认启用验证
		sequenceValidator: utils.NewSSEEventValidator(),
		lastEventTime:     time.Now(),
		errorCount:        0,
	}
}

// EnsureMessageStart 确保message_start事件已发送，如果未发送则发送
// 🔧 性能优化：移除mutex操作，因为单goroutine顺序访问
func (s *SSEStreamState) EnsureMessageStart(c *gin.Context, flusher http.Flusher, formatter *utils.AnthropicSSEFormatter, messageID, model string) bool {
	if s.messageStartSent {
		return false // 已发送，无需重复
	}

	if messageID == "" {
		messageID = fmt.Sprintf("msg_interim_%d", time.Now().UnixNano())
	}
	if model == "" {
		model = "claude-unknown"
	}

	s.messageID = messageID
	s.messageModel = model

	// 🔧 核心修复：在发送事件前记录到历史
	if err := s.recordEvent(utils.SSEEventMessageStart); err != nil {
		utils.DebugLog("[SSEState] Warning: message_start validation failed: %v", err)
	}

	startEvent := formatter.FormatMessageStart(messageID, model)
	c.Writer.WriteString(startEvent)
	flusher.Flush()

	s.messageStartSent = true
	utils.DebugLog("[SSEState] Sent message_start (id: %s, model: %s)", messageID, model)
	return true
}

// EnsureContentBlockStart 确保content_block_start事件已发送（用于文本内容）
// 🔧 核心修复：添加事件记录和验证
func (s *SSEStreamState) EnsureContentBlockStart(c *gin.Context, flusher http.Flusher, formatter *utils.AnthropicSSEFormatter, blockType string) bool {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）

	if s.contentBlockStarted || s.toolCallsActive {
		return false // 已有活跃的内容块或工具调用
	}

	// 🔧 核心修复：在发送事件前记录到历史
	if err := s.recordEvent(utils.SSEEventContentBlockStart); err != nil {
		utils.DebugLog("[SSEState] Warning: content_block_start validation failed: %v", err)
	}

	startEvent := formatter.FormatContentBlockStart(s.currentBlockIndex, blockType, nil)
	c.Writer.WriteString(startEvent)
	flusher.Flush()

	s.contentBlockStarted = true
	utils.DebugLog("[SSEState] Sent content_block_start (index: %d, type: %s)", s.currentBlockIndex, blockType)
	return true
}

// FinishContentBlock 完成当前内容块
// 🔧 核心修复：添加事件记录和验证
func (s *SSEStreamState) FinishContentBlock(c *gin.Context, flusher http.Flusher, formatter *utils.AnthropicSSEFormatter) bool {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）

	if !s.contentBlockStarted {
		return false // 没有活跃的内容块
	}

	// 🔧 核心修复：在发送事件前记录到历史
	if err := s.recordEvent(utils.SSEEventContentBlockStop); err != nil {
		utils.DebugLog("[SSEState] Warning: content_block_stop validation failed: %v", err)
	}

	stopEvent := formatter.FormatContentBlockStop(s.currentBlockIndex)
	c.Writer.WriteString(stopEvent)
	flusher.Flush()

	s.contentBlockStarted = false
	s.currentBlockIndex++
	utils.DebugLog("[SSEState] Sent content_block_stop (index: %d)", s.currentBlockIndex-1)
	return true
}

// ActivateToolCalls 激活工具调用模式
func (s *SSEStreamState) ActivateToolCalls() {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）

	s.toolCallsActive = true
	utils.DebugLog("[SSEState] Activated tool calls mode")
}

// FinishStream 完成整个流（发送message_delta和message_stop）
// 🔧 核心修复：添加事件记录和序列验证
func (s *SSEStreamState) FinishStream(c *gin.Context, flusher http.Flusher, formatter *utils.AnthropicSSEFormatter, stopReason string) bool {
	return s.FinishStreamWithUsage(c, flusher, formatter, stopReason, nil)
}

// FinishStreamWithUsage 完成整个流并传递usage信息
func (s *SSEStreamState) FinishStreamWithUsage(c *gin.Context, flusher http.Flusher, formatter *utils.AnthropicSSEFormatter, stopReason string, usage *utils.Usage) bool {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）

	if s.streamFinished {
		return false // 已完成
	}

	// 确保所有内容块都已关闭
	if s.contentBlockStarted {
		// 🔧 核心修复：记录自动关闭的content_block_stop事件
		if err := s.recordEvent(utils.SSEEventContentBlockStop); err != nil {
			utils.DebugLog("[SSEState] Warning: auto content_block_stop validation failed: %v", err)
		}
		stopEvent := formatter.FormatContentBlockStop(s.currentBlockIndex)
		c.Writer.WriteString(stopEvent)
		s.contentBlockStarted = false
		utils.DebugLog("[SSEState] Auto-closed content block before stream finish")
	}

	// 🔧 核心修复：记录message_delta事件
	if err := s.recordEvent(utils.SSEEventMessageDelta); err != nil {
		utils.DebugLog("[SSEState] Warning: message_delta validation failed: %v", err)
	}

	// 🔧 核心修复：发送包含usage信息的message_delta事件
	deltaEvent := formatter.FormatMessageDelta(stopReason, usage)
	c.Writer.WriteString(deltaEvent)
	flusher.Flush()

	// 🔧 核心修复：记录message_stop事件
	if err := s.recordEvent(utils.SSEEventMessageStop); err != nil {
		utils.DebugLog("[SSEState] Warning: message_stop validation failed: %v", err)
	}

	stopEvent := formatter.FormatMessageStop(nil)
	c.Writer.WriteString(stopEvent)
	flusher.Flush()

	s.streamFinished = true
	utils.DebugLog("[SSEState] Finished stream with reason: %s", stopReason)

	// 🔧 核心新增：最终验证完整序列
	if s.validationEnabled {
		if err := s.sequenceValidator.ValidateCompleteSequence(); err != nil {
			utils.DebugLog("[SSEValidation] Final sequence validation failed: %v", err)
			s.errorCount++
		} else {
			utils.DebugLog("[SSEValidation] Complete sequence validation passed")
		}
	}

	return true
}

// IsFinished 检查流是否已完成
func (s *SSEStreamState) IsFinished() bool {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）
	return s.streamFinished
}

// recordEvent 记录事件到历史并进行验证
// 🔧 核心新增：事件序列跟踪和验证
func (s *SSEStreamState) recordEvent(eventType string) error {
	// 此方法必须在已获取mutex的情况下调用
	s.eventHistory = append(s.eventHistory, eventType)
	s.lastEventTime = time.Now()

	// 如果启用验证，进行事件序列验证
	if s.validationEnabled && s.sequenceValidator != nil {
		if err := s.sequenceValidator.ValidateEvent(eventType); err != nil {
			s.errorCount++
			utils.DebugLog("[SSEValidation] Event sequence validation failed: %v (event: %s)", err, eventType)
			// 不返回错误，只记录，避免中断流
			return err
		}
	}

	utils.DebugLog("[SSESequence] Recorded event: %s (total: %d, errors: %d)",
		eventType, len(s.eventHistory), s.errorCount)
	return nil
}

// ValidateCompleteSequence 验证完整的事件序列
// 🔧 核心新增：流结束时的序列验证
func (s *SSEStreamState) ValidateCompleteSequence() error {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）

	if s.sequenceValidator == nil {
		return nil // 无验证器，跳过
	}

	return s.sequenceValidator.ValidateCompleteSequence()
}

// GetValidationReport 获取验证报告
// 🔧 核心新增：用于调试和监控事件序列
func (s *SSEStreamState) GetValidationReport() map[string]any {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）

	report := map[string]any{
		"message_start_sent":    s.messageStartSent,
		"content_block_started": s.contentBlockStarted,
		"stream_finished":       s.streamFinished,
		"tool_calls_active":     s.toolCallsActive,
		"current_block_index":   s.currentBlockIndex,
		"event_count":           len(s.eventHistory),
		"error_count":           s.errorCount,
		"last_event_time":       s.lastEventTime.Format("15:04:05.000"),
	}

	// 添加验证器报告
	if s.sequenceValidator != nil {
		validatorReport := s.sequenceValidator.GetValidationReport()
		report["validator"] = validatorReport
	}

	// 添加事件历史（最后5个事件）
	if len(s.eventHistory) > 0 {
		startIdx := len(s.eventHistory) - 5
		if startIdx < 0 {
			startIdx = 0
		}
		report["recent_events"] = s.eventHistory[startIdx:]
	}

	return report
}

// EnableValidation 启用或禁用事件序列验证
func (s *SSEStreamState) EnableValidation(enabled bool) {
	// 🔧 性能优化：移除mutex操作（单goroutine顺序访问）
	s.validationEnabled = enabled
	utils.DebugLog("[SSEValidation] Validation %s",
		func() string {
			if enabled {
				return "enabled"
			} else {
				return "disabled"
			}
		}())
}

// 统一工具调用处理器接口（SRP原则）
type ToolCallProcessor interface {
	ProcessToolCalls(choice *utils.OpenAIChoice, isStream bool) ToolProcessResult
	OutputAnthropicFormat(c *gin.Context, flusher http.Flusher) bool
	ClearSession()
	GetStats() map[string]int
}

// DefaultToolCallManager 默认工具调用管理器（统一流式和非流式处理）
type DefaultToolCallManager struct {
	session   *ToolCallsSession
	requestID string
}

// NewDefaultToolCallManager 创建工具调用管理器 - 🔧 修复：每个请求独立创建会话
func NewDefaultToolCallManager(requestID string) *DefaultToolCallManager {
	// 🔧 根本性修复：直接创建独立会话，不使用全局管理器
	// 避免会话重用和状态泄露问题
	session := newToolCallsSession(requestID)

	// 🔍 诊断：记录会话创建的详细信息
	utils.DebugLog("[SessionIsolation] Created isolated session for request: %s (session_id: %s, goroutine: g%d, address: %p)",
		requestID, session.requestID, getGoroutineID(), session)

	manager := &DefaultToolCallManager{
		session:   session,
		requestID: requestID,
	}

	// 🔍 诊断：验证管理器的独立性
	utils.DebugLog("[SessionIsolation] Manager created - requestID: %s, manager_address: %p, session_address: %p",
		requestID, manager, session)

	return manager
}

// ProcessToolCalls 统一处理工具调用逻辑
func (m *DefaultToolCallManager) ProcessToolCalls(choice *utils.OpenAIChoice, isStream bool) ToolProcessResult {
	return m.session.processToolCallsUnified(choice, isStream)
}

// OutputAnthropicFormat 输出完整的Anthropic格式（包括message_stop）
func (m *DefaultToolCallManager) OutputAnthropicFormat(c *gin.Context, flusher http.Flusher) bool {
	return m.session.convertAndOutputAnthropicToolCalls(c, flusher)
}

// OutputAnthropicToolCallsOnly 只输出工具调用内容，不发送message_stop
func (m *DefaultToolCallManager) OutputAnthropicToolCallsOnly(c *gin.Context, flusher http.Flusher) bool {
	return m.session.convertAndOutputAnthropicToolCallsOnly(c, flusher)
}

// OutputAnthropicToolCallsWithState 只输出工具调用内容，支持事件记录，不发送message_stop
// 🔧 核心新增：支持SSE事件序列记录的工具调用输出
func (m *DefaultToolCallManager) OutputAnthropicToolCallsWithState(c *gin.Context, flusher http.Flusher, streamState *SSEStreamState) bool {
	return m.session.convertAndOutputAnthropicToolCallsWithState(c, flusher, streamState)
}

// ClearSession 清理会话
func (m *DefaultToolCallManager) ClearSession() {
	m.session.clearToolCallsWithLogging()
}

// GetStats 获取统计信息
func (m *DefaultToolCallManager) GetStats() map[string]int {
	return m.session.getSessionStats()
}

// 上游URL：支持通过环境变量覆盖，便于端到端测试（DIP）
func upstreamURL() string {
	if v := strings.TrimSpace(os.Getenv("CODEBUDDY2CC_UPSTREAM_URL")); v != "" {
		return v
	}
	return "https://www.codebuddy.ai/v2/chat/completions"
}

// SSEStreamParser 真正的流式SSE解析器，支持context取消检测
type SSEStreamParser struct {
	reader   io.Reader
	buffer   []byte
	position int
	tempBuf  []byte // 重用的临时缓冲区
}

// NewSSEStreamParser 创建新的SSE流解析器
func NewSSEStreamParser(reader io.Reader) *SSEStreamParser {
	return &SSEStreamParser{
		reader:   reader,
		buffer:   make([]byte, 0, 8192),
		position: 0,
		tempBuf:  make([]byte, 1024), // 预分配重用缓冲区
	}
}

// NextEvent 读取下一个完整的SSE事件，支持context取消检测
func (p *SSEStreamParser) NextEvent(ctx context.Context) (string, error) {
	for {
		// 尝试从缓冲区解析完整事件
		if event, consumed := p.tryParseEvent(); event != "" {
			// 移除已消费的数据
			p.buffer = p.buffer[consumed:]
			return event, nil
		}

		// 🔧 关键修复：检查context状态，提前退出避免无限循环
		select {
		case <-ctx.Done():
			return "", ctx.Err() // 优雅处理context取消
		default:
			// 继续处理
		}

		// 需要更多数据，从reader读取（重用预分配缓冲区）
		n, err := p.reader.Read(p.tempBuf)
		if err != nil {
			// 🔧 特殊处理：context.Canceled不应产生噪声日志
			if err == context.Canceled {
				return "", err // 直接返回，不记录错误日志
			}
			if err == io.EOF && len(p.buffer) > 0 {
				// 处理最后的数据，优化字符串拷贝
				if len(p.buffer) > 0 {
					// 先trim字节，再转换为字符串，减少一次拷贝
					start, end := 0, len(p.buffer)
					for start < end && (p.buffer[start] == ' ' || p.buffer[start] == '\t' || p.buffer[start] == '\n' || p.buffer[start] == '\r') {
						start++
					}
					for end > start && (p.buffer[end-1] == ' ' || p.buffer[end-1] == '\t' || p.buffer[end-1] == '\n' || p.buffer[end-1] == '\r') {
						end--
					}
					if end > start {
						event := string(p.buffer[start:end])
						p.buffer = nil
						return event, nil
					}
				}
			}
			return "", err
		}

		// 追加新数据到缓冲区
		p.buffer = append(p.buffer, p.tempBuf[:n]...)
	}
}

// tryParseEvent 尝试从缓冲区解析一个完整的SSE事件
func (p *SSEStreamParser) tryParseEvent() (string, int) {
	data := p.buffer
	if len(data) == 0 {
		return "", 0
	}

	// 🎯 优化的SSE事件解析策略，支持更灵活的格式
	// 1. 优先查找标准SSE事件：data: {content}\n\n
	// 2. 兼容单行事件：data: {content}\n
	// 3. 处理空行和格式不规范的情况

	// 查找 "data: " 开始位置
	dataStart := bytes.Index(data, []byte("data: "))
	if dataStart == -1 {
		// 没有找到data标记，查找纯换行进行清理
		if newlineIdx := bytes.IndexByte(data, '\n'); newlineIdx != -1 {
			// 消费到换行符，返回空字符串继续处理
			return "", newlineIdx + 1
		}
		return "", 0
	}

	// 从data位置开始查找事件边界
	searchStart := dataStart

	// 🎯 优先查找标准双换行结束符
	if doubleNewline := bytes.Index(data[searchStart:], []byte("\n\n")); doubleNewline != -1 {
		eventEnd := searchStart + doubleNewline
		event := strings.TrimSpace(string(data[dataStart:eventEnd]))
		return event, eventEnd + 2 // +2 跳过 \n\n
	}

	// 🎯 查找单换行作为事件边界（兼容模式）
	if singleNewline := bytes.IndexByte(data[searchStart:], '\n'); singleNewline != -1 {
		eventEnd := searchStart + singleNewline
		event := strings.TrimSpace(string(data[dataStart:eventEnd]))

		// 验证这是一个完整的JSON数据行
		if strings.Contains(event, "data: {") || strings.Contains(event, "data: [DONE]") {
			return event, eventEnd + 1
		}
	}

	// 需要更多数据才能形成完整事件
	return "", 0
}

// OpenAIToolCall OpenAI工具调用结构
type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
	Index *int `json:"index"`
}

// 工具调用数量限制，防止内存攻击
const MaxToolCalls = 32

// ToolProcessResult 工具处理结果枚举
type ToolProcessResult int

const (
	ToolProcessContinue ToolProcessResult = iota // 继续处理
	ToolProcessDone                              // 处理完成
	ToolProcessError                             // 处理错误
)

// ToolCallsSession 会话级工具调用状态管理器
type ToolCallsSession struct {
	toolCallsMap   map[string]*AnthropicToolCall
	toolCallsOrder []*AnthropicToolCall
	requestID      string // 会话唯一标识
}

// AnthropicToolCall Anthropic工具调用转换器
type AnthropicToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

// newToolCallsSession 创建新的工具调用会话，使用传入的请求ID
func newToolCallsSession(requestID string) *ToolCallsSession {
	session := &ToolCallsSession{
		toolCallsMap:   make(map[string]*AnthropicToolCall),
		toolCallsOrder: make([]*AnthropicToolCall, 0, 4),
		requestID:      requestID, // 使用请求ID作为会话标识
	}

	return session
}

// processToolCallsUnified 统一工具调用处理逻辑
func (session *ToolCallsSession) processToolCallsUnified(choice *utils.OpenAIChoice, _ bool) ToolProcessResult {
	// 1. 处理工具调用数据收集
	if choice.Delta != nil && choice.Delta.ToolCalls != nil {
		// utils.DebugLog("[ToolCall] Processing %d tool calls in delta", len(choice.Delta.ToolCalls))

		for _, openaiTool := range choice.Delta.ToolCalls {
			var currentTool *AnthropicToolCall

			if openaiTool.ID != "" {
				// 检查是否是新工具
				if existing, exists := session.toolCallsMap[openaiTool.ID]; exists {
					currentTool = existing
				} else {
					// 边界检查
					if len(session.toolCallsOrder) >= MaxToolCalls {
						utils.DebugLog("Tool calls limit exceeded: %d >= %d", len(session.toolCallsOrder), MaxToolCalls)
						return ToolProcessError
					}
					// 创建新工具
					currentTool = &AnthropicToolCall{ID: openaiTool.ID}
					session.toolCallsMap[openaiTool.ID] = currentTool
					session.toolCallsOrder = append(session.toolCallsOrder, currentTool)
				}
			} else {
				// 无ID情况：延续最后一个工具
				if len(session.toolCallsOrder) > 0 {
					currentTool = session.toolCallsOrder[len(session.toolCallsOrder)-1]
				} else {
					continue
				}
			}

			// 更新工具信息
			if openaiTool.Function.Name != "" && currentTool.Name == "" {
				currentTool.Name = openaiTool.Function.Name
			}

			// 累积参数片段
			if openaiTool.Function.Arguments != "" {
				currentTool.Arguments.WriteString(openaiTool.Function.Arguments)
			}
		}

		return ToolProcessContinue
	}

	// 2. 检查是否完成工具调用
	if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
		return ToolProcessDone
	}

	return ToolProcessContinue
}

// 🎯 移除所有ID映射方法 - 改为直接透传模式简化架构

// clearToolCallsWithLogging 带日志的会话状态清理
func (session *ToolCallsSession) clearToolCallsWithLogging() {
	utils.DebugLog("[Session] Clearing session %s: tools=%d",
		session.requestID, len(session.toolCallsOrder))

	// 1. 清空map引用
	for k := range session.toolCallsMap {
		delete(session.toolCallsMap, k)
	}

	// 2. 清理slice中的指针引用（防止内存泄漏）
	for i := range session.toolCallsOrder {
		session.toolCallsOrder[i] = nil
	}

	// 3. 重置slice长度，保留容量
	session.toolCallsOrder = session.toolCallsOrder[:0]
}

// getSessionStats 获取会话统计信息
func (session *ToolCallsSession) getSessionStats() map[string]int {
	return map[string]int{
		"total_tools":  len(session.toolCallsOrder),
		"mapped_tools": len(session.toolCallsMap),
	}
}

func MessagesHandler(c *gin.Context) {
	// 🔍 诊断：记录处理器入口信息
	// handlerStartTime := time.Now()
	// goroutineID := fmt.Sprintf("g%d", getGoroutineID())
	// clientIP := c.ClientIP()
	// userAgent := c.GetHeader("User-Agent")

	// utils.DebugLog("[HandlerDiag] New request received - goroutine: %s, time: %s, IP: %s, UA: %s",
	// 	goroutineID, handlerStartTime.Format("15:04:05.000"), clientIP, userAgent)

	// 🔍 新增：检查是否有活动的SSE连接
	// utils.DebugLog("[ConnectionDiag] Request headers - Connection: %s, Accept: %s",
	// 	c.GetHeader("Connection"), c.GetHeader("Accept"))

	var req utils.AnthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request format: %v", err)})
		return
	}

	// 🔧 生成唯一的请求标识符
	requestID := generateRequestID()

	// 🔍 诊断：验证请求的唯一性
	// utils.DebugLog("[HandlerDiag] Request mapping - requestID: %s, goroutine: %s",
	// requestID, goroutineID)
	utils.DebugLog("[Request:%s] Processing request", requestID)

	// Debug: 输出客户端原始请求内容（排除tools字段以减少日志大小）
	debugClientReq := struct {
		Model       string          `json:"model"`
		Messages    []utils.Message `json:"messages"`
		Temperature *float64        `json:"temperature,omitempty"`
		MaxTokens   *int            `json:"max_tokens,omitempty"`
		Stream      bool            `json:"stream,omitempty"`
		ToolsCount  int             `json:"tools_count,omitempty"`
	}{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
		ToolsCount:  len(req.Tools),
	}
	utils.DebugLogJSON("Client Original Request", debugClientReq)

	// 在发送到 Bedrock 之前验证消息格式
	if err := utils.ValidateAndFixToolResults(&req); err != nil {
		utils.DebugLog("[ERROR] Failed to validate tool results: %v", err)
		// 尝试自动修复失败，返回错误
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Tool results validation failed: %v", err)})
		return
	}

	// 🎯 使用统一工具调用管理器（替代旧的会话管理）
	toolManager := NewDefaultToolCallManager(requestID)

	// 🔍 诊断：验证工具管理器的独立性
	// utils.DebugLog("[HandlerDiag] Created tool manager for request %s, session state: %+v",
	// 	requestID, toolManager.GetStats())

	// 🔧 强制上游使用流式，因为上游不支持非流式调用
	originalClientStream := req.Stream
	req.Stream = true

	openAIReq, err := utils.ConvertAnthropicToOpenAI(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Request conversion failed: %v", err)})
		return
	}

	// Debug: 输出转换后的OpenAI请求内容（排除tools字段以减少日志大小）
	debugReq := struct {
		Model       string                `json:"model"`
		Messages    []utils.OpenAIMessage `json:"messages"`
		Temperature *float64              `json:"temperature,omitempty"`
		MaxTokens   *int                  `json:"max_tokens,omitempty"`
		Stream      bool                  `json:"stream,omitempty"`
		ToolsCount  int                   `json:"tools_count,omitempty"`
	}{
		Model:       openAIReq.Model,
		Messages:    openAIReq.Messages,
		Temperature: openAIReq.Temperature,
		MaxTokens:   openAIReq.MaxTokens,
		Stream:      openAIReq.Stream,
		ToolsCount:  len(openAIReq.Tools),
	}
	utils.DebugLogJSON("Converted OpenAI Request", debugReq)

	reqBody, err := utils.FastMarshal(openAIReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode request"})
		return
	}

	// 🔧 关键修复：为每个请求创建独立的context，避免相互影响
	// 使用背景context + 超时，而不是直接使用gin的request context
	requestCtx, requestCancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer requestCancel() // 确保清理

	// 🔍 新增：检测context隔离性
	utils.DebugLog("[ContextIsolation] Creating request context - parent: background, timeout: 600s, requestID: %s",
		requestID)

	upstreamReq, err := http.NewRequestWithContext(requestCtx, "POST", upstreamURL(), bytes.NewBuffer(reqBody))
	if err != nil {
		utils.DebugLog("[Request:%s] [ERROR] Failed to create upstream request: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream request"})
		return
	}

	// 🔍 诊断：记录请求创建信息和context地址
	utils.DebugLog("[Request:%s] [CONCURRENCY] Created upstream request with independent context, goroutine: g%d, ctx_addr: %p",
		requestID, getGoroutineID(), requestCtx)

	// 使用单一上游API密钥
	upstreamKey := os.Getenv("CODEBUDDY2CC_KEY")
	if upstreamKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "CODEBUDDY2CC_KEY not configured"})
		return
	}

	utils.DebugLog("[Request:%s] Using configured API key", requestID)
	upstreamReq.Header.Set("Authorization", "Bearer "+upstreamKey)
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("User-Agent", "CLI/1.0.9 CodeBuddy/1.0.9")

	// 🔧 关键修复：过滤HTTP/2禁止的连接特定头部
	bannedHeaders := map[string]bool{
		"Authorization":     true,
		"Connection":        true, // HTTP/2禁止
		"Keep-Alive":        true, // HTTP/2禁止
		"Proxy-Connection":  true, // HTTP/2禁止
		"Transfer-Encoding": true, // HTTP/2禁止
		"Upgrade":           true, // HTTP/2禁止
	}

	for key, values := range c.Request.Header {
		// 使用标准化的头部键名进行比较（避免大小写问题）
		normalizedKey := http.CanonicalHeaderKey(key)
		if !bannedHeaders[normalizedKey] {
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
			}
		}
	}

	// 🔧 关键修复：优化并发连接配置
	client := &http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   10 * time.Second, // TLS握手超时
			ResponseHeaderTimeout: 30 * time.Second, // 增加响应头超时到30秒
			IdleConnTimeout:       90 * time.Second, // 增加空闲连接超时
			MaxIdleConns:          100,              // 🔧 增加最大空闲连接数，支持并发
			MaxConnsPerHost:       50,               // 🔧 增加每个主机最大连接数，支持高并发
			MaxIdleConnsPerHost:   20,               // 🔧 新增：每个主机最大空闲连接数
			DisableKeepAlives:     false,            // 确保保持连接活跃
			DisableCompression:    false,            // 启用压缩
			ExpectContinueTimeout: 1 * time.Second,  // 🔧 新增：100-continue超时
		},
	}

	resp, err := client.Do(upstreamReq)
	if err != nil {
		utils.DebugLog("[Request:%s] HTTP request failed: %v", requestID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Request failed: %v", err)})
		return
	}

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			utils.DebugLog("[Request:%s] Failed to read error response body: %v", requestID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read error response"})
			return
		}

		// 🔧 增强错误调试：输出完整的上游错误信息
		utils.DebugLog("[Request:%s] Upstream API Error - Status: %d (%s)", requestID, resp.StatusCode, http.StatusText(resp.StatusCode))
		utils.DebugLog("[Request:%s] Upstream API Error - Body: %s", requestID, string(body))

		// 如果是JSON格式的错误响应，尝试解析并输出结构化信息
		var errorResponse map[string]any
		if utils.FastUnmarshal(body, &errorResponse) == nil {
			utils.DebugLog("[Request:%s] Upstream API Error - Parsed JSON: %+v", requestID, errorResponse)
		}
		c.Data(resp.StatusCode, "application/json", body)
		return
	}

	// 🔧 成功响应：处理响应
	utils.DebugLog("[Request:%s] Successful response received", requestID)

	defer resp.Body.Close()

	// 🎯 统一处理响应，根据客户端需求决定输出格式
	responseData, err := processUnifiedResponse(resp, toolManager, requestID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Response processing failed: %v", err)})
		return
	}

	// 根据客户端需求选择输出格式
	if originalClientStream {
		writeStreamResponse(c, responseData)
	} else {
		writeNonStreamResponse(c, responseData)
	}
}

// generateRequestID 生成请求唯一标识符
func generateRequestID() string {
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	return fmt.Sprintf("req_%s_%d", hex.EncodeToString(randomBytes), time.Now().UnixNano())
}

// ResponseData 统一响应数据结构
type ResponseData struct {
	MessageID     string
	MessageModel  string
	ContentBlocks []utils.ContentBlock
	StopReason    string
	Usage         *utils.Usage
	IsToolCall    bool
}

// processUnifiedResponse 统一处理上游响应（SRP原则）
func processUnifiedResponse(resp *http.Response, toolManager *DefaultToolCallManager, requestID string) (*ResponseData, error) {
	var messageID string
	var messageModel string
	var contentBlocks []utils.ContentBlock
	var stopReason string = "end_turn"
	var usage *utils.Usage
	var isToolCall bool = false

	utils.DebugLog("[Request:%s] Processing unified response with manager stats: %+v", requestID, toolManager.GetStats())

	// 使用完全独立的context
	processCtx, processCancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer processCancel()

	streamParser := NewSSEStreamParser(resp.Body)

	for {
		event, err := streamParser.NextEvent(processCtx)
		if err != nil {
			if err == io.EOF {
				break
			}
			if err == context.Canceled || err == context.DeadlineExceeded {
				utils.DebugLog("[Request:%s] Processing context cancelled or timeout", requestID)
				break
			}
			return nil, fmt.Errorf("stream parsing failed: %v", err)
		}

		if event == "" {
			continue
		}

		// 提取上游数据
		var rawData string
		if after, ok := strings.CutPrefix(event, "data: "); ok {
			rawData = strings.TrimSpace(after)
		} else if strings.HasPrefix(event, "internal:finish_reason:") {
			rawData = strings.TrimPrefix(event, "internal:")
		} else {
			continue
		}

		// 处理流结束信号
		if rawData == "[DONE]" || strings.HasPrefix(rawData, "finish_reason:") {
			if r, found := strings.CutPrefix(rawData, "finish_reason:"); found {
				switch r {
				case "tool_calls":
					isToolCall = true
					stopReason = "tool_use"
				case "stop":
					stopReason = "end_turn"
				}
			}
			continue
		}

		// 解析OpenAI数据块
		var openAIChunk utils.OpenAIResponse
		if err := utils.FastUnmarshal([]byte(rawData), &openAIChunk); err != nil {
			continue
		}

		// 收集usage信息
		if openAIChunk.Usage != nil {
			usage = collectUsageInfo(openAIChunk.Usage)
		}

		// 设置消息基本信息
		if len(openAIChunk.Choices) > 0 && messageID == "" {
			messageID = openAIChunk.ID
			messageModel = openAIChunk.Model
		}

		// 处理choices
		if len(openAIChunk.Choices) > 0 {
			choice := openAIChunk.Choices[0]

			// 处理工具调用
			if (choice.Delta != nil && choice.Delta.ToolCalls != nil && len(choice.Delta.ToolCalls) > 0) || (choice.FinishReason != nil && *choice.FinishReason == "tool_calls") {
				toolManager.ProcessToolCalls(&choice, true)
				if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
					isToolCall = true
					stopReason = "tool_use"
				}
				continue
			}

			// 处理文本内容（非工具调用模式下）
			if choice.Delta != nil && choice.Delta.Content != nil && !isToolCall {
				if contentStr, ok := choice.Delta.Content.(string); ok && contentStr != "" {
					if len(contentBlocks) == 0 {
						contentBlocks = append(contentBlocks, utils.ContentBlock{Type: "text", Text: contentStr})
					} else {
						// 累积到最后一个文本块
						for i := len(contentBlocks) - 1; i >= 0; i-- {
							if contentBlocks[i].Type == "text" {
								contentBlocks[i].Text += contentStr
								break
							}
						}
					}
				}
			}
		}
	}

	// 处理工具调用结果
	if isToolCall && len(toolManager.session.toolCallsOrder) > 0 {
		contentBlocks = buildToolCallBlocks(toolManager)
		stopReason = "tool_use"
	}

	// 过滤空文本块并提供默认内容
	contentBlocks = filterAndDefaultContent(contentBlocks)

	// 设置默认值
	if messageID == "" {
		messageID = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	if messageModel == "" {
		messageModel = "claude-unknown"
	}

	return &ResponseData{
		MessageID:     messageID,
		MessageModel:  messageModel,
		ContentBlocks: contentBlocks,
		StopReason:    stopReason,
		Usage:         usage,
		IsToolCall:    isToolCall,
	}, nil
}

// collectUsageInfo 统一收集usage信息
func collectUsageInfo(openAIUsage *utils.Usage) *utils.Usage {
	usageMap := make(map[string]any)
	if openAIUsage.PromptTokens > 0 {
		usageMap["prompt_tokens"] = openAIUsage.PromptTokens
	}
	if openAIUsage.CompletionTokens > 0 {
		usageMap["completion_tokens"] = openAIUsage.CompletionTokens
	}
	if openAIUsage.TotalTokens > 0 {
		usageMap["total_tokens"] = openAIUsage.TotalTokens
	}
	if openAIUsage.InputTokens > 0 {
		usageMap["input_tokens"] = openAIUsage.InputTokens
	}
	if openAIUsage.OutputTokens > 0 {
		usageMap["output_tokens"] = openAIUsage.OutputTokens
	}
	if openAIUsage.CacheCreationInputTokens > 0 {
		usageMap["cache_creation_input_tokens"] = openAIUsage.CacheCreationInputTokens
	}
	if openAIUsage.CacheReadInputTokens > 0 {
		usageMap["cache_read_input_tokens"] = openAIUsage.CacheReadInputTokens
	}
	if openAIUsage.PromptCacheHitTokens > 0 {
		usageMap["prompt_cache_hit_tokens"] = openAIUsage.PromptCacheHitTokens
	}
	if openAIUsage.PromptCacheMissTokens > 0 {
		usageMap["prompt_cache_miss_tokens"] = openAIUsage.PromptCacheMissTokens
	}
	return utils.ParseUsageFromResponse(usageMap)
}

// buildToolCallBlocks 构建工具调用内容块
func buildToolCallBlocks(toolManager *DefaultToolCallManager) []utils.ContentBlock {
	var contentBlocks []utils.ContentBlock
	for _, tool := range toolManager.session.toolCallsOrder {
		if tool.Name != "" {
			var inputObj map[string]any
			argsStr := strings.TrimSpace(tool.Arguments.String())

			if argsStr == "" {
				inputObj = map[string]any{}
			} else if err := utils.FastUnmarshal([]byte(argsStr), &inputObj); err != nil {
				inputObj = map[string]any{"raw_args": argsStr}
			}

			contentBlocks = append(contentBlocks, utils.ContentBlock{
				Type:  "tool_use",
				ID:    tool.ID,
				Name:  tool.Name,
				Input: inputObj,
			})
		}
	}
	return contentBlocks
}

// filterAndDefaultContent 过滤空内容并提供默认值
func filterAndDefaultContent(contentBlocks []utils.ContentBlock) []utils.ContentBlock {
	if len(contentBlocks) > 0 {
		filtered := make([]utils.ContentBlock, 0, len(contentBlocks))
		for _, b := range contentBlocks {
			if b.Type == "text" {
				if strings.TrimSpace(b.Text) != "" {
					filtered = append(filtered, b)
				}
			} else {
				filtered = append(filtered, b)
			}
		}
		contentBlocks = filtered
	}

	if len(contentBlocks) == 0 {
		contentBlocks = []utils.ContentBlock{{Type: "text", Text: "处理完成"}}
	}
	return contentBlocks
}

// writeStreamResponse SSE流式输出（OCP原则）
func writeStreamResponse(c *gin.Context, data *ResponseData) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		utils.DebugLog("ERROR: Streaming not supported")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	// 使用原子化状态管理器
	streamState := NewSSEStreamState()
	formatter := utils.NewAnthropicSSEFormatter()

	// 确保流正确关闭
	defer func() {
		if !streamState.IsFinished() {
			streamState.FinishStream(c, flusher, formatter, data.StopReason)
		}
	}()

	// 发送message_start
	streamState.EnsureMessageStart(c, flusher, formatter, data.MessageID, data.MessageModel)

	// 处理工具调用输出
	if data.IsToolCall {
		// 直接从处理后的数据构建工具调用输出，避免重复处理
		for idx, block := range data.ContentBlocks {
			if block.Type == "tool_use" {
				// 发送content_block_start
				additional := map[string]any{
					"id":    block.ID,
					"name":  block.Name,
					"input": map[string]any{},
				}
				startLine := formatter.FormatContentBlockStart(idx, "tool_use", additional)
				c.Writer.WriteString(startLine)
				flusher.Flush()

				// 发送工具参数
				if block.Input != nil {
					if inputBytes, err := utils.FastMarshal(block.Input); err == nil {
						chunks := splitUTF8SafeChunks(string(inputBytes), 64)
						for _, chunk := range chunks {
							if chunk != "" {
								deltaLine := formatter.FormatContentBlockDelta(idx, "input_json_delta", chunk)
								c.Writer.WriteString(deltaLine)
								flusher.Flush()
							}
						}
					}
				}

				// 发送content_block_stop
				stopLine := formatter.FormatContentBlockStop(idx)
				c.Writer.WriteString(stopLine)
				flusher.Flush()
			}
		}
	} else {
		// 处理文本内容
		for idx, block := range data.ContentBlocks {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				// 发送content_block_start
				streamState.EnsureContentBlockStart(c, flusher, formatter, "text")

				// 分块发送文本内容
				chunks := splitUTF8SafeChunks(block.Text, 64)
				for _, chunk := range chunks {
					if chunk != "" {
						deltaEvent := formatter.FormatContentBlockDelta(idx, "text_delta", chunk)
						c.Writer.WriteString(deltaEvent)
						flusher.Flush()
					}
				}

				// 结束content block
				streamState.FinishContentBlock(c, flusher, formatter)
			}
		}
	}

	// 完成流
	streamState.FinishStreamWithUsage(c, flusher, formatter, data.StopReason, data.Usage)
}

// writeNonStreamResponse JSON响应输出（OCP原则）
func writeNonStreamResponse(c *gin.Context, data *ResponseData) {
	// 构建Anthropic响应
	anthResp := &utils.AnthropicResponse{
		ID:           data.MessageID,
		Type:         "message",
		Role:         "assistant",
		Content:      data.ContentBlocks,
		Model:        data.MessageModel,
		StopReason:   &data.StopReason,
		StopSequence: nil,
		Usage:        data.Usage,
	}

	c.JSON(http.StatusOK, anthResp)
}

// convertAndOutputAnthropicToolCalls 转换为Anthropic格式并输出 - 符合规范的流式格式
func (session *ToolCallsSession) convertAndOutputAnthropicToolCalls(c *gin.Context, flusher http.Flusher) bool {
	if len(session.toolCallsOrder) == 0 {
		return false
	}

	utils.DebugLog("Converting %d tool calls to Anthropic streaming format", len(session.toolCallsOrder))

	// 创建符合规范的SSE格式化器
	formatter := utils.NewAnthropicSSEFormatter()

	// 为每个工具发送符合规范的流式事件序列
	for idx, tool := range session.toolCallsOrder {
		if tool.Name == "" {
			utils.DebugLog("Skipping tool with empty name: id=%s", tool.ID)
			continue
		}

		// 🎯 KISS简化：直接使用工具ID，无需复杂映射
		// 1. 发送content_block_start事件 (仅包含基本信息，符合Anthropic规范)
		additional := map[string]any{
			"id":   tool.ID, // 直接使用原始工具ID
			"name": tool.Name,
			// 🎯 关键修复：content_block_start不包含input，符合Anthropic流式规范
		}
		startLine := formatter.FormatContentBlockStart(idx, "tool_use", additional)
		utils.DebugLog("Sending to client[tool-start]: %s", strings.TrimSpace(startLine))
		c.Writer.WriteString(startLine)

		// 2. 通过input_json_delta发送工具参数 (符合Anthropic规范的增量格式)
		argsStr := strings.TrimSpace(tool.Arguments.String())
		if argsStr == "" {
			argsStr = "{}"
		} else {
			// 验证JSON格式
			var testObj map[string]any
			if err := utils.FastUnmarshal([]byte(argsStr), &testObj); err != nil {
				utils.DebugLog("Invalid JSON for tool %s, using fallback: %v", tool.Name, err)
				argsStr = `{"raw_args":"` + strings.ReplaceAll(argsStr, `"`, `\"`) + `"}`
			}
		}

		// 分块发送JSON参数以符合Anthropic input_json_delta规范
		session.sendInputJsonDeltasWithFormatter(c, flusher, idx, argsStr, formatter)

		// 3. 发送content_block_stop事件
		stopLine := formatter.FormatContentBlockStop(idx)
		utils.DebugLog("Sending to client[tool-stop]: %s", strings.TrimSpace(stopLine))
		c.Writer.WriteString(stopLine)

		utils.DebugLog("Sent Anthropic tool_use stream: idx=%d id=%s name=%s", idx, tool.ID, tool.Name)
	}

	// 发送message完成事件
	deltaLine := formatter.FormatMessageDelta("tool_use", nil)
	utils.DebugLog("Sending to client[msg-delta]: %s", strings.TrimSpace(deltaLine))
	c.Writer.WriteString(deltaLine)
	flusher.Flush()

	stopLine := formatter.FormatMessageStop(nil)
	utils.DebugLog("Sending to client[msg-stop]: %s", strings.TrimSpace(stopLine))
	c.Writer.WriteString(stopLine)
	flusher.Flush()

	utils.DebugLog("Anthropic tool calls streaming completed: sent %d tools", len(session.toolCallsOrder))

	// 🔧 关键修复：彻底清理会话状态，避免重复发送和内存泄漏
	session.clearToolCallsWithLogging()

	return true
}

// convertAndOutputAnthropicToolCallsWithState 只输出工具调用内容，不发送message结束事件，但支持事件记录
// 🔧 核心新增：为工具调用添加事件序列记录功能
func (session *ToolCallsSession) convertAndOutputAnthropicToolCallsWithState(c *gin.Context, flusher http.Flusher, streamState *SSEStreamState) bool {
	if len(session.toolCallsOrder) == 0 {
		return false
	}

	utils.DebugLog("Converting %d tool calls to Anthropic streaming format with state tracking", len(session.toolCallsOrder))

	// 创建符合规范的SSE格式化器
	formatter := utils.NewAnthropicSSEFormatter()

	// 为每个工具发送符合规范的流式事件序列（不包括最终message事件）
	for idx, tool := range session.toolCallsOrder {
		if tool.Name == "" {
			utils.DebugLog("Skipping tool with empty name: id=%s", tool.ID)
			continue
		}

		// 🔧 核心修复：记录content_block_start事件
		if err := streamState.recordEvent(utils.SSEEventContentBlockStart); err != nil {
			utils.DebugLog("Warning: tool content_block_start validation failed: %v", err)
		}

		// 🔧 核心修复：正确设置content block状态
		streamState.contentBlockStarted = true
		streamState.currentBlockIndex = idx

		// 1. 发送content_block_start事件（包含完整的工具信息，符合Anthropic规范）
		additional := map[string]any{
			"id":    tool.ID,
			"name":  tool.Name,
			"input": map[string]any{}, // 🔧 关键修复：添加空的input字段，符合Anthropic规范
		}
		startLine := formatter.FormatContentBlockStart(idx, "tool_use", additional)
		utils.DebugLog("Sending to client[tool-start]: %s", strings.TrimSpace(startLine))
		c.Writer.WriteString(startLine)
		flusher.Flush()

		// 2. 发送工具参数
		argsStr := strings.TrimSpace(tool.Arguments.String())
		if argsStr == "" {
			argsStr = "{}"
		} else {
			// 验证JSON格式
			var testObj map[string]any
			if err := utils.FastUnmarshal([]byte(argsStr), &testObj); err != nil {
				utils.DebugLog("Invalid JSON for tool %s, using fallback: %v", tool.Name, err)
				argsStr = `{"raw_args":"` + strings.ReplaceAll(argsStr, `"`, `\"`) + `"}`
			}
		}

		// 分块发送JSON参数
		session.sendInputJsonDeltasWithFormatterAndState(c, flusher, idx, argsStr, formatter, streamState)

		// 🔧 核心修复：记录content_block_stop事件
		if err := streamState.recordEvent(utils.SSEEventContentBlockStop); err != nil {
			utils.DebugLog("Warning: tool content_block_stop validation failed: %v", err)
		}

		// 3. 发送content_block_stop事件
		stopLine := formatter.FormatContentBlockStop(idx)
		utils.DebugLog("Sending to client[tool-stop]: %s", strings.TrimSpace(stopLine))
		c.Writer.WriteString(stopLine)
		flusher.Flush()

		// 🔧 核心修复：确保状态正确更新，防止重复关闭
		streamState.contentBlockStarted = false
		streamState.currentBlockIndex++

		utils.DebugLog("Sent Anthropic tool_use stream: idx=%d id=%s name=%s", idx, tool.ID, tool.Name)
	}

	utils.DebugLog("Anthropic tool calls content streaming completed: sent %d tools (no message end events)", len(session.toolCallsOrder))

	// 🔧 关键修复：清理会话状态，但不发送message结束事件
	session.clearToolCallsWithLogging()

	return true
}

// sendInputJsonDeltasWithFormatterAndState 发送符合Anthropic规范的input_json_delta事件序列（增强UTF-8安全和事件记录）
// 🔧 核心新增：为工具参数发送添加事件记录
func (session *ToolCallsSession) sendInputJsonDeltasWithFormatterAndState(c *gin.Context, flusher http.Flusher, index int, jsonStr string, formatter *utils.AnthropicSSEFormatter, streamState *SSEStreamState) {
	// 🔧 关键修复：确保JSON字符串是有效的UTF-8编码
	if !utf8.ValidString(jsonStr) {
		utils.DebugLog("Invalid UTF-8 in JSON string, attempting to fix")
		jsonStr = strings.ToValidUTF8(jsonStr, "﷿")
	}

	// 🔧 增强：使用UTF-8安全的智能分块算法
	chunks := splitUTF8SafeChunks(jsonStr, 64) // 增大块大小并确保UTF-8安全

	for i, chunk := range chunks {
		if chunk == "" {
			continue // 跳过空块
		}

		// 验证每个块都是有效的UTF-8
		if !utf8.ValidString(chunk) {
			utils.DebugLog("Invalid UTF-8 chunk detected at index %d, skipping", i)
			continue
		}

		// 🔧 核心修复：记录每个content_block_delta事件
		if err := streamState.recordEvent(utils.SSEEventContentBlockDelta); err != nil {
			utils.DebugLog("Warning: tool json delta validation failed: %v", err)
		}

		deltaLine := formatter.FormatContentBlockDelta(index, "input_json_delta", chunk)
		utils.DebugLog("Sending to client[json-delta-%d]: %s", i, strings.TrimSpace(deltaLine))
		c.Writer.WriteString(deltaLine)
		flusher.Flush()
	}
}

// convertAndOutputAnthropicToolCallsOnly 只输出工具调用内容，不发送message结束事件（保留版本）
func (session *ToolCallsSession) convertAndOutputAnthropicToolCallsOnly(c *gin.Context, flusher http.Flusher) bool {
	if len(session.toolCallsOrder) == 0 {
		return false
	}

	utils.DebugLog("Converting %d tool calls to Anthropic streaming format (content only)", len(session.toolCallsOrder))

	// 创建符合规范的SSE格式化器
	formatter := utils.NewAnthropicSSEFormatter()

	// 为每个工具发送符合规范的流式事件序列（不包括message结束事件）
	for idx, tool := range session.toolCallsOrder {
		if tool.Name == "" {
			utils.DebugLog("Skipping tool with empty name: id=%s", tool.ID)
			continue
		}

		// 1. 发送content_block_start事件
		additional := map[string]any{
			"id":   tool.ID,
			"name": tool.Name,
		}
		startLine := formatter.FormatContentBlockStart(idx, "tool_use", additional)
		utils.DebugLog("Sending to client[tool-start]: %s", strings.TrimSpace(startLine))
		c.Writer.WriteString(startLine)
		flusher.Flush()

		// 2. 发送工具参数
		argsStr := strings.TrimSpace(tool.Arguments.String())
		if argsStr == "" {
			argsStr = "{}"
		} else {
			// 验证JSON格式
			var testObj map[string]any
			if err := utils.FastUnmarshal([]byte(argsStr), &testObj); err != nil {
				utils.DebugLog("Invalid JSON for tool %s, using fallback: %v", tool.Name, err)
				argsStr = `{"raw_args":"` + strings.ReplaceAll(argsStr, `"`, `\"`) + `"}`
			}
		}

		// 分块发送JSON参数
		session.sendInputJsonDeltasWithFormatter(c, flusher, idx, argsStr, formatter)

		// 3. 发送content_block_stop事件
		stopLine := formatter.FormatContentBlockStop(idx)
		utils.DebugLog("Sending to client[tool-stop]: %s", strings.TrimSpace(stopLine))
		c.Writer.WriteString(stopLine)
		flusher.Flush()

		utils.DebugLog("Sent Anthropic tool_use stream: idx=%d id=%s name=%s", idx, tool.ID, tool.Name)
	}

	utils.DebugLog("Anthropic tool calls content streaming completed: sent %d tools (no message end events)", len(session.toolCallsOrder))

	// 🔧 关键修复：清理会话状态，但不发送message结束事件
	session.clearToolCallsWithLogging()

	return true
}

// sendInputJsonDeltasWithFormatter 发送符合Anthropic规范的input_json_delta事件序列（增强UTF-8安全）
func (session *ToolCallsSession) sendInputJsonDeltasWithFormatter(c *gin.Context, flusher http.Flusher, index int, jsonStr string, formatter *utils.AnthropicSSEFormatter) {
	// 🔧 关键修复：确保JSON字符串是有效的UTF-8编码
	if !utf8.ValidString(jsonStr) {
		utils.DebugLog("Invalid UTF-8 in JSON string, attempting to fix")
		jsonStr = strings.ToValidUTF8(jsonStr, "�")
	}

	// 🔧 增强：使用UTF-8安全的智能分块算法
	chunks := splitUTF8SafeChunks(jsonStr, 64) // 增大块大小并确保UTF-8安全

	for i, chunk := range chunks {
		if chunk == "" {
			continue // 跳过空块
		}

		// 验证每个块都是有效的UTF-8
		if !utf8.ValidString(chunk) {
			utils.DebugLog("Invalid UTF-8 chunk detected at index %d, skipping", i)
			continue
		}

		deltaLine := formatter.FormatContentBlockDelta(index, "input_json_delta", chunk)
		utils.DebugLog("Sending to client[json-delta-%d]: %s", i, strings.TrimSpace(deltaLine))
		c.Writer.WriteString(deltaLine)
		flusher.Flush()
	}
}

// splitUTF8SafeChunks 将字符串分割为UTF-8安全的块
func splitUTF8SafeChunks(input string, maxChunkSize int) []string {
	if len(input) == 0 {
		return []string{}
	}

	var chunks []string
	inputBytes := []byte(input)
	totalBytes := len(inputBytes)

	for start := 0; start < totalBytes; {
		// 计算当前块的预期结束位置
		end := start + maxChunkSize
		if end > totalBytes {
			end = totalBytes
		}

		// 🔧 关键修复：确保不在UTF-8字符中间切割
		for end > start && end < totalBytes {
			// 检查是否是UTF-8字符的开始字节
			if utf8.RuneStart(inputBytes[end]) {
				break // 找到合适的切割点
			}
			end-- // 向前回退直到找到UTF-8字符边界
		}

		// 防止无限循环：如果回退到起始位置，强制向前移动一个字符
		if end <= start {
			// 从start位置向前查找下一个UTF-8字符边界
			_, size := utf8.DecodeRune(inputBytes[start:])
			if size == 0 {
				size = 1 // 防止无限循环
			}
			end = start + size
			if end > totalBytes {
				end = totalBytes
			}
		}

		// 提取当前块
		chunk := string(inputBytes[start:end])

		// 验证块的UTF-8有效性
		if utf8.ValidString(chunk) {
			chunks = append(chunks, chunk)
		} else {
			// 如果仍然无效，尝试修复
			chunk = strings.ToValidUTF8(chunk, "�")
			chunks = append(chunks, chunk)
			utils.DebugLog("Fixed invalid UTF-8 chunk: %d-%d", start, end)
		}

		start = end
	}

	return chunks
}
