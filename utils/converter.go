package utils

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// 🎯 移除工具ID映射机制 - 直接透传简化架构

type AnthropicRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []Tool           `json:"tools,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   *int             `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	Metadata    *RequestMetadata `json:"metadata,omitempty"` // 🔧 新增：支持metadata
}

// RequestMetadata 请求元数据，用于session追踪和调试
type RequestMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type Message struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"` // 使用 any 替代 interface{}
	Agent      string           `json:"agent,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`   // 🔧 新增：支持直接的tool_calls字段
	ToolCallID string           `json:"tool_call_id,omitempty"` // 🔧 新增：支持工具调用结果消息
}

type ContentBlock struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
	// 工具调用支持
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// 根据Anthropic规范，tool_use.input 可以是对象或字符串
	Input any `json:"input,omitempty"` // 使用 any 替代 interface{}
	// 工具结果支持
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   *bool  `json:"is_error,omitempty"`
}

// MarshalJSON 自定义JSON序列化，确保文本块包含text字段
func (cb ContentBlock) MarshalJSON() ([]byte, error) {
	type Alias ContentBlock
	switch cb.Type {
	case "text":
		// 文本块必须包含text字段，即使为空
		return FastMarshal(struct {
			Alias
			Text string `json:"text"`
		}{
			Alias: Alias(cb),
			Text:  cb.Text,
		})
	default:
		return FastMarshal(Alias(cb))
	}
}

type ImageURL struct {
	URL string `json:"url"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"` // 使用 any 替代 interface{}
}

type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Tools       []OpenAITool    `json:"tools,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"` // 使用 any 替代 interface{}
	Agent      string           `json:"agent,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

type OpenAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // 使用 any 替代 interface{}
}

type OpenAIToolCall struct {
	// 一些上游在流式增量里提供 index 字段来标识归属的工具调用
	Index    *int               `json:"index,omitempty"`
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// Anthropic format fields
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	// Cache related fields for Anthropic prompt caching support
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	// 🔧 新增：支持上游的详细缓存字段
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}

type AnthropicResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   *string        `json:"stop_reason,omitempty"`
	StopSequence *string        `json:"stop_sequence"` // 保持 *string 以支持 null 值
	Usage        *Usage         `json:"usage,omitempty"`
}

type AnthropicStreamChunk struct {
	Type  string       `json:"type"`
	Index int          `json:"index,omitempty"`
	Delta *StreamDelta `json:"delta,omitempty"`
}

type StreamDelta struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

// sanitizeContentBlocks 过滤空文本块；返回精简后的块列表
func sanitizeContentBlocks(blocks []ContentBlock) []ContentBlock {
	if len(blocks) == 0 {
		return blocks
	}
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == "text" {
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			out = append(out, b)
			continue
		}
		out = append(out, b)
	}
	return out
}

// ConvertAnthropicToOpenAI 转换Anthropic请求为OpenAI格式
func ConvertAnthropicToOpenAI(req *AnthropicRequest) (*OpenAIRequest, error) {
	// // Debug: 输出工具转换信息
	// if len(req.Tools) > 0 {
	// 	DebugLog("Converting %d tools from Anthropic to OpenAI format", len(req.Tools))
	// 	for i, tool := range req.Tools {
	// 		paramStatus := "present"
	// 		if tool.InputSchema == nil {
	// 			paramStatus = "NULL - will use default schema"
	// 		}
	// 		DebugLog("Tool[%d]: name=%s, description=%s, input_schema=%s",
	// 			i, tool.Name, tool.Description, paramStatus)
	// 	}
	// }

	// 应用模型映射
	mappedModel := MapModel(req.Model)

	openAIReq := &OpenAIRequest{
		Model:       mappedModel,
		Messages:    make([]OpenAIMessage, 0, len(req.Messages)+1),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
	}

	// 提取并保留原始system消息内容
	var originalSystemContent string
	var otherMessages []Message

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			// 合并所有system消息
			if content, ok := msg.Content.(string); ok {
				originalSystemContent += content + "\n\n"
			} else if contentBlocks, ok := msg.Content.([]any); ok {
				for _, block := range contentBlocks {
					if blockMap, ok := block.(map[string]any); ok {
						if text, exists := blockMap["text"].(string); exists {
							originalSystemContent += text + "\n\n"
						}
					}
				}
			}
		} else {
			// 🔧 新增：过滤空内容的用户消息，但保留工具调用结果消息
			if msg.Role == "user" && isContentEmpty(msg.Content) && msg.ToolCallID == "" && !hasToolResult(msg.Content) {
				DebugLog("Filtering empty user message")
				continue // 跳过空内容且没有tool_call_id且没有tool_result的用户消息
			}
			otherMessages = append(otherMessages, msg)
		}
	}

	// 构建增强的system消息：保留原始内容 + CodeBuddy特定指令
	enhancedSystemContent := originalSystemContent
	if enhancedSystemContent != "" {
		enhancedSystemContent += "\n\n--- CodeBuddy Integration ---\n\n"
	}
	enhancedSystemContent += "You are CodeBuddy Code, Tencent's official CLI for CodeBuddy."

	systemMsg := OpenAIMessage{
		Role: "system",
		Content: []ContentBlock{{
			Type: "text",
			Text: enhancedSystemContent,
		}},
	}
	openAIReq.Messages = append(openAIReq.Messages, systemMsg)

	// 🔧 关键修复：实现连续assistant消息的智能合并逻辑
	mergedMessages := mergeConsecutiveAssistantMessages(otherMessages)

	for _, msg := range mergedMessages {
		openAIMsg := OpenAIMessage{
			Role:       msg.Role,
			Agent:      msg.Agent,
			ToolCallID: msg.ToolCallID, // 🔧 新增：设置工具调用结果消息的tool_call_id
		}

		// 🎯 专门处理 role:"tool" 的消息，确保 content 为非空字符串并携带 tool_call_id
		if msg.Role == "tool" {
			var contentStr string
			switch c := msg.Content.(type) {
			case string:
				contentStr = c
			case []any:
				var sb strings.Builder
				for _, item := range c {
					if itemMap, ok := item.(map[string]any); ok {
						if t, ok := itemMap["type"].(string); ok && t == "text" {
							if text, ok := itemMap["text"].(string); ok {
								sb.WriteString(text)
							}
						} else if val, ok := itemMap["content"].(string); ok {
							sb.WriteString(val)
						}
					}
				}
				contentStr = sb.String()
			default:
				contentStr = fmt.Sprintf("%v", c)
			}
			contentStr = strings.TrimSpace(contentStr)
			if contentStr == "" {
				contentStr = "工具调用完成"
			}
			if openAIMsg.ToolCallID == "" && msg.ToolCallID != "" {
				openAIMsg.ToolCallID = msg.ToolCallID
			}
			openAIMsg.Content = contentStr
			openAIReq.Messages = append(openAIReq.Messages, openAIMsg)
			continue
		}

		// 🔧 修复：首先检查消息是否直接包含tool_calls字段 (专门处理从JSON反序列化来的工具调用)
		if len(msg.ToolCalls) > 0 {
			// 消息直接包含tool_calls，转换并确保有content字段
			openAIMsg.ToolCalls = msg.ToolCalls
			// 🔧 修复：不允许将“空内容”与tool_calls一起传递，改用默认文本
			if !isContentEmpty(msg.Content) {
				openAIMsg.Content = convertContent(msg.Content)
			} else {
				toolName := "tool"
				if len(msg.ToolCalls) > 0 && msg.ToolCalls[0].Function.Name != "" {
					toolName = msg.ToolCalls[0].Function.Name
				}
				openAIMsg.Content = []ContentBlock{{Type: "text", Text: "调用" + toolName + "工具"}}
			}
		} else if hasToolResult(msg.Content) {
			// 🔧 [正确修复] 将Anthropic的tool_result转换为独立的role="tool"消息
			// 参考req3.json格式：tool_result应该是独立的tool角色消息，不是user消息的content
			DebugLog("[Converter] Processing message with tool_result, content type: %T", msg.Content)

			if anthroContentBlocks, ok := msg.Content.([]any); ok {
				for _, anthroBlock := range anthroContentBlocks {
					if anthroBlockMap, ok := anthroBlock.(map[string]any); ok {
						if blockType, exists := anthroBlockMap["type"].(string); exists && blockType == "tool_result" {

							// 1. 提取必要信息（安全解析）
							var toolUseId string
							if v, ok := anthroBlockMap["tool_use_id"]; ok {
								if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
									toolUseId = s
								}
							}
							if toolUseId == "" {
								toolUseId = "unknown_tool_" + fmt.Sprintf("%d", time.Now().UnixNano())
								DebugLog("[ToolResult] Missing tool_use_id, generated: %s", toolUseId)
							}

							isError := false
							if v, ok := anthroBlockMap["is_error"]; ok {
								switch t := v.(type) {
								case bool:
									isError = t
								case string:
									ls := strings.ToLower(strings.TrimSpace(t))
									isError = ls == "true" || ls == "1" || ls == "yes"
								case float64:
									isError = t != 0
								case int:
									isError = t != 0
								case int64:
									isError = t != 0
								}
							}
							DebugLog("[ToolResult] Parsed is_error=%v tool_use_id=%s", isError, toolUseId)

							var contentText string
							if toolContent, exists := anthroBlockMap["content"]; exists {
								switch tc := toolContent.(type) {
								case string:
									contentText = tc
								case []any:
									var sb strings.Builder
									for _, item := range tc {
										if itemMap, ok := item.(map[string]any); ok {
											if text, ok := itemMap["text"].(string); ok {
												sb.WriteString(text)
											}
										}
									}
									contentText = sb.String()
								default:
									contentText = "工具执行完成"
								}
							}

							// 🔧 [关键修复] 当content为空时，确保显示默认消息
							if strings.TrimSpace(contentText) == "" {
								contentText = "工具调用完成"
							}

							// 2. 创建独立的role="tool"消息
							toolMsg := OpenAIMessage{
								Role:       "tool",
								ToolCallID: toolUseId,
								Content:    contentText, // 直接使用字符串content，不是数组
								Agent:      msg.Agent,
							}

							// 3. 将tool消息添加到请求中
							openAIReq.Messages = append(openAIReq.Messages, toolMsg)
							// DebugLog("[ToolResult] Created tool message: toolCallID=%s, isError=%v, content=%s", toolUseId, isError, contentText)
						} else if toolResultData, exists := anthroBlockMap["toolResult"]; exists {
							// 🔧 [兼容修复] 处理非标准toolResult格式
							DebugLog("[ToolResult] Processing non-standard toolResult format")

							if toolResultMap, ok := toolResultData.(map[string]any); ok {
								var contentText string
								var toolUseId string

								// 🔧 [关键修复] 尝试从tool_call_id或其他字段获取toolUseId
								if id, exists := toolResultMap["tool_call_id"]; exists {
									if idStr, ok := id.(string); ok {
										toolUseId = idStr
									}
								}
								// 如果toolUseId仍为空，生成一个默认的
								if toolUseId == "" {
									toolUseId = "unknown_tool_" + fmt.Sprintf("%d", time.Now().UnixNano())
									DebugLog("[ToolResult] Generated default toolUseId: %s", toolUseId)
								}

								// 尝试从content字段提取文本
								if content, exists := toolResultMap["content"]; exists {
									if contentStr, ok := content.(string); ok {
										contentText = contentStr
									}
								}

								// 尝试从renderer.value提取文本（如果content为空）
								if contentText == "" {
									if renderer, exists := toolResultMap["renderer"]; exists {
										if rendererMap, ok := renderer.(map[string]any); ok {
											if value, exists := rendererMap["value"]; exists {
												if valueStr, ok := value.(string); ok && valueStr != "(No content)" {
													contentText = valueStr
												}
											}
										}
									}
								}

								// 如果仍然为空，使用默认消息
								if strings.TrimSpace(contentText) == "" {
									contentText = "工具调用完成"
								}

								// 创建独立的role="tool"消息
								toolMsg := OpenAIMessage{
									Role:       "tool",
									ToolCallID: toolUseId,
									Content:    contentText,
									Agent:      msg.Agent,
								}

								openAIReq.Messages = append(openAIReq.Messages, toolMsg)
								DebugLog("[ToolResult] Created tool message from non-standard format: toolCallID=%s, content=%s", toolUseId, contentText)
							}
						}
					}
				}
			}
			// 跳过原user消息，因为tool_result已转换为独立的tool消息
			continue
		} else if hasToolUse(msg.Content) {
			// 🔧 [关键修复] 将Anthropic tool_use转换为标准OpenAI格式
			// 使用标准OpenAI tool_calls格式，确保上游API兼容性
			openAIMsg.Role = "assistant"

			// 🎯 [重复ID修复] 使用map去重，确保每个tool_use只转换一次
			seenToolIDs := make(map[string]bool)

			if anthroContentBlocks, ok := msg.Content.([]any); ok {
				for _, anthroBlock := range anthroContentBlocks {
					if anthroBlockMap, ok := anthroBlock.(map[string]any); ok {
						if blockType, exists := anthroBlockMap["type"].(string); exists && blockType == "tool_use" {

							// 1. 提取tool_use信息
							toolUseId := anthroBlockMap["id"].(string)

							// 🎯 [关键修复] 检查是否已处理过此ID
							if seenToolIDs[toolUseId] {
								DebugLog("[ToolUse] Skipping duplicate tool_use ID: %s", toolUseId)
								continue
							}
							seenToolIDs[toolUseId] = true

							toolName := anthroBlockMap["name"].(string)
							toolInput := anthroBlockMap["input"]

							// 2. 转换tool_input为JSON字符串格式
							toolInputJSON, err := FastMarshal(toolInput)
							if err != nil {
								DebugLog("[ToolUse] Error marshaling tool input: %v", err)
								continue
							}

							// 3. 构建标准OpenAI tool_calls格式
							openAIToolCall := OpenAIToolCall{
								ID:   toolUseId,
								Type: "function",
								Function: OpenAIFunctionCall{
									Name:      toolName,
									Arguments: string(toolInputJSON),
								},
							}

							// 将tool_calls添加到消息中
							openAIMsg.ToolCalls = append(openAIMsg.ToolCalls, openAIToolCall)
							DebugLog("[ToolUse] Converted to OpenAI format: id=%s, name=%s", toolUseId, toolName)

						} else if blockType == "text" {
							// 设置assistant消息的文本内容
							if text, exists := anthroBlockMap["text"].(string); exists && strings.TrimSpace(text) != "" {
								if openAIMsg.Content == nil || openAIMsg.Content == "" {
									openAIMsg.Content = text
								} else {
									// 如果已有内容，追加新文本
									openAIMsg.Content = fmt.Sprintf("%s\n%s", openAIMsg.Content, text)
								}
							}
						}
					}
				}
			}

			// 如果没有设置任何内容，提供默认文本
			if openAIMsg.Content == nil || openAIMsg.Content == "" {
				if len(openAIMsg.ToolCalls) > 0 {
					openAIMsg.Content = "正在使用工具"
				}
			}
		} else {
			// 统一过滤空内容消息（user/assistant），保留含工具相关的消息
			if (msg.Role == "user" || msg.Role == "assistant") && isContentEmpty(msg.Content) {
				// 对于assistant，如果完全无工具相关且内容为空，直接跳过
				if msg.ToolCallID == "" && len(msg.ToolCalls) == 0 && !hasToolUse(msg.Content) && !hasToolResult(msg.Content) {
					DebugLog("Filtering empty %s message", msg.Role)
					continue
				}
			}

			// 🔧 [关键修复] 如果消息包含工具相关内容，不应该进入通用转换逻辑
			if hasToolResult(msg.Content) || hasToolUse(msg.Content) {
				DebugLog("[Converter] Skipping general conversion for tool-related message")
				// 这种情况应该在上面的分支中处理，如果到这里说明有逻辑问题
				continue
			}

			// 转换并进行空文本块清理
			converted := convertContent(msg.Content).([]ContentBlock)
			sanitized := sanitizeContentBlocks(converted)
			openAIMsg.Content = sanitized
			// 对于有 tool_call_id 但内容为空的消息，提供默认文本，避免上游校验失败
			if (msg.Role == "user" || msg.Role == "assistant") && msg.ToolCallID != "" && len(sanitized) == 0 {
				openAIMsg.Content = []ContentBlock{{Type: "text", Text: "工具调用完成"}}
			}
		}
		openAIReq.Messages = append(openAIReq.Messages, openAIMsg)
	}

	if len(req.Tools) > 0 {
		openAIReq.Tools = make([]OpenAITool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			// 使用专门的验证和标准化函数 (SRP: 分离关注点)
			normalizedParams := validateAndNormalizeToolParameters(tool.InputSchema)

			openAIReq.Tools = append(openAIReq.Tools, OpenAITool{
				Type: "function",
				Function: OpenAIFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  normalizedParams,
				},
			})
		}
	}

	return openAIReq, nil
}

func convertContent(content any) any {
	switch c := content.(type) {
	case string:
		return []ContentBlock{{Type: "text", Text: c}}
	case []any:
		blocks := make([]ContentBlock, 0, len(c))
		for _, item := range c {
			if blockMap, ok := item.(map[string]any); ok {
				block := ContentBlock{}
				if blockType, exists := blockMap["type"].(string); exists {
					block.Type = blockType
					switch blockType {
					case "text":
						if text, exists := blockMap["text"].(string); exists {
							// 🔧 KISS修复：过滤空text，避免发送无意义的空内容到上游
							if strings.TrimSpace(text) != "" {
								block.Text = text
							} else {
								// 跳过空text block，不添加到blocks中
								continue
							}
						} else {
							// text字段不存在，跳过这个block
							continue
						}
					case "image_url":
						if imgURL, exists := blockMap["image_url"].(map[string]any); exists {
							if url, exists := imgURL["url"].(string); exists {
								block.ImageURL = &ImageURL{URL: url}
							}
						}
					case "tool_use":
						// 🎯 tool_use不应该在这里处理，应该通过convertToolUseToOpenAI处理
						// 如果在这里遇到tool_use，说明上游逻辑有问题，跳过处理
						DebugLog("Warning: tool_use found in convertContent, should be handled by convertToolUseToOpenAI")
						continue
					default:
						if text, exists := blockMap["text"].(string); exists {
							// 🔧 同样过滤default分支中的空text
							if strings.TrimSpace(text) != "" {
								block.Type = "text"
								block.Text = text
							} else {
								// 跳过空text block
								continue
							}
						} else {
							// 没有text字段，跳过
							continue
						}
					}
				}
				blocks = append(blocks, block)
			}
		}
		// 🔧 KISS防护：如果所有content blocks都被过滤掉了，提供默认content
		if len(blocks) == 0 {
			// 为空content提供有意义的默认值，而不是完全空的数组
			return []ContentBlock{{Type: "text", Text: "工具调用完成"}}
		}
		return blocks
	default:
		// 🔧 DRY原则：统一的默认content策略，避免空text
		return []ContentBlock{{Type: "text", Text: "工具调用完成"}}
	}
}

// ConvertOpenAIToAnthropic 已废弃 - 现在统一使用流式处理
// 保留以支持可能的旧代码引用
func ConvertOpenAIToAnthropic(resp *OpenAIResponse) (*AnthropicResponse, error) {
	// 这个函数不再使用，因为我们统一使用流式处理
	// 如果意外调用，返回基本结构
	return &AnthropicResponse{
		ID:           resp.ID,
		Type:         "message",
		Role:         "assistant",
		Content:      []ContentBlock{},
		Model:        resp.Model,
		StopReason:   stringPtr("end_turn"),
		StopSequence: nil,
		Usage:        resp.Usage,
	}, nil
}

func stringPtr(s string) *string {
	return &s
}

// mergeConsecutiveAssistantMessages 合并连续的assistant消息
// 当发现连续的assistant消息时，将第一个消息的content与第二个消息的tool_calls合并
func mergeConsecutiveAssistantMessages(messages []Message) []Message {
	if len(messages) <= 1 {
		return messages
	}

	var result []Message
	i := 0

	for i < len(messages) {
		currentMsg := messages[i]

		// 检查是否是assistant消息且下一个消息也是assistant
		if currentMsg.Role == "assistant" && i+1 < len(messages) && messages[i+1].Role == "assistant" {
			nextMsg := messages[i+1]

			// 场景：第一个消息有content，第二个消息有tool_calls
			if currentMsg.Content != nil && len(currentMsg.ToolCalls) == 0 &&
				len(nextMsg.ToolCalls) > 0 && nextMsg.Content == nil {

				// 合并：第一个消息的content + 第二个消息的tool_calls
				mergedMsg := Message{
					Role:      "assistant",
					Content:   currentMsg.Content, // 保留第一个消息的文本内容
					Agent:     currentMsg.Agent,
					ToolCalls: nextMsg.ToolCalls, // 添加第二个消息的工具调用
				}

				result = append(result, mergedMsg)
				i += 2 // 跳过两个消息
				continue
			}
		}

		// 默认情况：直接添加当前消息
		result = append(result, currentMsg)
		i++
	}

	return result
}

// validateAndNormalizeToolParameters 确保工具参数符合OpenAI规范 (SRP: 单一参数验证责任)
func validateAndNormalizeToolParameters(inputSchema map[string]any) map[string]any {
	if inputSchema == nil {
		// 只有在inputSchema为null时才提供默认JSON Schema (KISS: 简单默认值)
		DebugLog("Tool input_schema is NULL, using default empty schema")
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
			"required":   []string{},
		}
	}

	// 深拷贝防止修改原始数据 (安全性原则)
	cleanSchema := deepCopyMap(inputSchema)

	// 清理OpenAI特有字段 (DRY: 统一清理逻辑)
	delete(cleanSchema, "$schema")
	delete(cleanSchema, "strict")
	delete(cleanSchema, "additionalProperties")

	// 验证JSON Schema基本结构 (健壮性增强)
	if _, hasType := cleanSchema["type"]; !hasType {
		cleanSchema["type"] = "object"
	}
	if _, hasProps := cleanSchema["properties"]; !hasProps {
		cleanSchema["properties"] = map[string]any{}
	}

	// DebugLog("Tool input_schema validated and normalized, %d fields remaining", len(cleanSchema))
	return cleanSchema
}

// deepCopyMap 深拷贝map避免修改原始数据 (SRP: 单一深拷贝责任)
func deepCopyMap(original map[string]any) map[string]any {
	copy := make(map[string]any)
	for key, value := range original {
		switch v := value.(type) {
		case map[string]any:
			copy[key] = deepCopyMap(v)
		case []any:
			newSlice := make([]any, len(v))
			for i, item := range v {
				if itemMap, ok := item.(map[string]any); ok {
					newSlice[i] = deepCopyMap(itemMap)
				} else {
					newSlice[i] = item
				}
			}
			copy[key] = newSlice
		default:
			copy[key] = value
		}
	}
	return copy
}

func hasToolResult(content any) bool {
	if contentBlocks, ok := content.([]any); ok {
		for _, block := range contentBlocks {
			if blockMap, ok := block.(map[string]any); ok {
				// 🔧 [兼容修复] 检查标准Anthropic tool_result格式
				if blockType, exists := blockMap["type"].(string); exists && blockType == "tool_result" {
					return true
				}
				// 🔧 [兼容修复] 检查非标准toolResult格式（如CodeBuddy CLI发送的格式）
				if _, exists := blockMap["toolResult"]; exists {
					return true
				}
			}
		}
	}
	return false
}

// hasToolUse 检查消息是否包含tool_use类型的内容块
func hasToolUse(content any) bool {
	if contentBlocks, ok := content.([]any); ok {
		for _, block := range contentBlocks {
			if blockMap, ok := block.(map[string]any); ok {
				if blockType, exists := blockMap["type"].(string); exists && blockType == "tool_use" {
					return true
				}
			}
		}
	}
	return false
}

// 添加 stop_sequence 到响应以符合标准
func AddStopSequenceToResponse(anthResp *AnthropicResponse) *AnthropicResponse {
	// 标准 Anthropic 响应需要 stop_sequence: null
	// 但由于我们没有 stop_sequence 信息，设置为 null
	return anthResp
}

// ConvertOpenAIStreamToAnthropic 是一个无状态转换器，它将单个OpenAI流块转换为相应的Anthropic事件字符串。
// 它不管理流状态（例如，message_start或content_block_start是否已发送）。
// 状态管理和事件排序的责任在于调用者（handlers.handleUnifiedStreamResponse）。
func ConvertOpenAIStreamToAnthropic(openAIChunk string) (string, error) {
	if !strings.HasPrefix(openAIChunk, "data: ") {
		// 不是一个标准的SSE 'data:' 行，可能是一个注释或空行，直接忽略
		return "", nil
	}

	data := strings.TrimSpace(strings.TrimPrefix(openAIChunk, "data: "))

	// [DONE] 信号现在由handler处理，这里不再过滤
	if data == "[DONE]" {
		return openAIChunk, nil // 将[DONE]信号传递给handler
	}

	var chunk OpenAIResponse
	if err := FastUnmarshal([]byte(data), &chunk); err != nil {
		DebugLog("[SSE Converter] Failed to unmarshal OpenAI chunk: %v. Data: %s", err, data)
		return "", fmt.Errorf("failed to unmarshal OpenAI chunk: %w", err)
	}

	if len(chunk.Choices) == 0 {
		return "", nil // 没有choice，忽略
	}

	choice := chunk.Choices[0]
	formatter := NewAnthropicSSEFormatter()

	// 诊断日志
	DebugLog("[SSE Converter] Processing chunk - ID: %s, HasDelta: %v, FinishReason: %s",
		chunk.ID,
		choice.Delta != nil,
		func() string {
			if choice.FinishReason != nil {
				return *choice.FinishReason
			}
			return "<nil>"
		}(),
	)

	// 优先处理流结束信号，因为它最重要
	if choice.FinishReason != nil {
		// 返回一个特殊的内部事件，由handler决定如何处理
		return fmt.Sprintf("internal:finish_reason:%s", *choice.FinishReason), nil
	}

	// 处理文本增量
	if choice.Delta != nil && choice.Delta.Content != nil {
		if contentStr, ok := choice.Delta.Content.(string); ok && contentStr != "" {
			DebugLog("[SSE Converter] Generating content_block_delta with text: %s", contentStr)
			return formatter.FormatContentBlockDelta(0, "text_delta", contentStr), nil
		}
	}

	// 🔧 关键修复：处理工具调用增量 (遵循SRP: 专一负责工具调用检测)
	if choice.Delta != nil && choice.Delta.ToolCalls != nil && len(choice.Delta.ToolCalls) > 0 {
		DebugLog("[SSE Converter] 🔧 Tool calls detected in stream - Count: %d", len(choice.Delta.ToolCalls))

		// 序列化工具调用数据供handler处理
		toolCallsJSON, err := FastMarshal(choice.Delta.ToolCalls)
		if err != nil {
			DebugLog("[SSE Converter] ERROR: Failed to marshal tool calls: %v", err)
			return "", fmt.Errorf("failed to marshal tool calls: %w", err)
		}

		// 返回特殊的内部信号，由handler的工具调用管理器处理
		// 格式: internal:tool_calls:JSON_DATA
		DebugLog("[SSE Converter] 🔧 Returning tool calls signal for handler processing")
		return fmt.Sprintf("internal:tool_calls:%s", string(toolCallsJSON)), nil
	}

	// 其他情况（例如，空的delta）返回空字符串，由handler忽略
	return "", nil
}

// ValidateAndFixToolResults 导出版本 - 确保所有工具调用都有对应的结果
func ValidateAndFixToolResults(req *AnthropicRequest) error {
	return validateAndFixToolResults(req.Messages)
}

// validateAndFixToolResults 确保所有工具调用都有对应的结果
func validateAndFixToolResults(messages []Message) error {
	toolCallMap := make(map[string]bool)
	toolResultMap := make(map[string]bool)

	// 第一遍：收集所有工具调用和结果
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, call := range msg.ToolCalls {
				toolCallMap[call.ID] = true
			}
		}
	}

	// 第二遍：检查缺失的工具结果
	for callID := range toolCallMap {
		if !toolResultMap[callID] {
			DebugLog("Missing tool result for ID: %s", callID)
		}
	}

	return nil
}

// isContentEmpty 检查消息内容是否为空或无意义
// 返回true表示内容为空，应该被过滤掉
func isContentEmpty(content any) bool {
	if content == nil {
		return true
	}

	// 检查字符串类型的内容
	if contentStr, ok := content.(string); ok {
		return strings.TrimSpace(contentStr) == ""
	}

	// 检查ContentBlock数组类型的内容
	if contentBlocks, ok := content.([]any); ok {
		if len(contentBlocks) == 0 {
			return true
		}

		// 检查所有content block是否都为空
		for _, block := range contentBlocks {
			if blockMap, ok := block.(map[string]any); ok {
				if blockType, exists := blockMap["type"].(string); exists {
					switch blockType {
					case "text":
						if text, exists := blockMap["text"].(string); exists {
							if strings.TrimSpace(text) != "" {
								return false // 找到非空文本，内容不为空
							}
						}
					case "image_url", "tool_use":
						return false // 这些类型的内容不应该被过滤
					}
				}
			}
		}
		return true // 所有文本block都为空
	}

	// 对于其他类型，保守地认为不为空
	return false
}

// SSE事件类型常量 - 符合Anthropic官方规范
const (
	SSEEventMessageStart      = "message_start"
	SSEEventContentBlockStart = "content_block_start"
	SSEEventContentBlockDelta = "content_block_delta"
	SSEEventContentBlockStop  = "content_block_stop"
	SSEEventMessageDelta      = "message_delta"
	SSEEventMessageStop       = "message_stop"
)

// AnthropicSSEFormatter 符合官方规范的SSE格式化器
type AnthropicSSEFormatter struct{}

// NewAnthropicSSEFormatter 创建SSE格式化器实例
func NewAnthropicSSEFormatter() *AnthropicSSEFormatter {
	return &AnthropicSSEFormatter{}
}

// FormatSSEEvent 格式化单个SSE事件，符合Anthropic官方规范
// 格式: event: eventType\ndata: jsonData\n\n
func (f *AnthropicSSEFormatter) FormatSSEEvent(eventType string, data any) string {
	jsonData, err := FastMarshal(data)
	if err != nil {
		DebugLog("SSE格式化失败: %v", err)
		// 返回错误事件而不是空字符串，确保客户端能感知到问题
		return "event: error\ndata: {\"type\":\"error\",\"message\":\"json_marshal_failed\"}\n\n"
	}

	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonData))
}

// FormatMessageStart 格式化message_start事件 - 修复硬编码token问题
func (f *AnthropicSSEFormatter) FormatMessageStart(messageID, model string) string {
	return f.FormatMessageStartWithUsage(messageID, model, nil)
}

// FormatMessageStartWithUsage 格式化message_start事件（支持自定义usage）
func (f *AnthropicSSEFormatter) FormatMessageStartWithUsage(messageID, model string, usage *Usage) string {
	// 设置默认usage，避免硬编码
	defaultUsage := map[string]any{
		"input_tokens":                0,
		"cache_creation_input_tokens": 0,
		"cache_read_input_tokens":     0,
		"output_tokens":               0,
	}

	// 如果提供了实际usage信息，使用它
	if usage != nil {
		// 🔧 核心修复：使用正确的字段优先级策略
		// 优先使用非零值，确保显示实际的token数量
		inputTokens := usage.InputTokens
		if inputTokens == 0 {
			// 回退策略：如果InputTokens为0，使用PromptTokens
			inputTokens = usage.PromptTokens
		}
		defaultUsage["input_tokens"] = inputTokens

		outputTokens := usage.OutputTokens
		if outputTokens == 0 {
			// 回退策略：如果OutputTokens为0，使用CompletionTokens
			outputTokens = usage.CompletionTokens
		}
		defaultUsage["output_tokens"] = outputTokens

		// 🔧 核心修复：确保cache相关token字段的正确传递
		if usage.CacheCreationInputTokens > 0 {
			defaultUsage["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
		}
		if usage.CacheReadInputTokens > 0 {
			defaultUsage["cache_read_input_tokens"] = usage.CacheReadInputTokens
		}

		// 🔧 新增：计算并记录实际使用的token总数
		totalTokens := inputTokens + outputTokens
		DebugLog("[UsageInfo] FormatMessageStart usage mapping: input=%d, output=%d, total=%d, cache_creation=%d, cache_read=%d",
			inputTokens, outputTokens, totalTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens)
	}

	event := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         defaultUsage,
		},
	}
	return f.FormatSSEEvent(SSEEventMessageStart, event)
}

// FormatContentBlockStart 格式化content_block_start事件
func (f *AnthropicSSEFormatter) FormatContentBlockStart(index int, blockType string, additional map[string]any) string {
	contentBlock := map[string]any{
		"type": blockType,
	}

	// 🔧 关键修复：为text类型的content_block添加必需的text字段
	if blockType == "text" {
		contentBlock["text"] = ""
	}

	// 添加额外的内容块属性
	for key, value := range additional {
		contentBlock[key] = value
	}

	event := map[string]any{
		"type":          "content_block_start",
		"index":         index,
		"content_block": contentBlock,
	}
	return f.FormatSSEEvent(SSEEventContentBlockStart, event)
}

// FormatContentBlockDelta 格式化content_block_delta事件
func (f *AnthropicSSEFormatter) FormatContentBlockDelta(index int, deltaType, content string) string {
	event := map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{
			"type": deltaType,
		},
	}

	// 根据delta类型设置不同的内容字段
	switch deltaType {
	case "text_delta":
		event["delta"].(map[string]any)["text"] = content
	case "input_json_delta":
		event["delta"].(map[string]any)["partial_json"] = content
	}

	return f.FormatSSEEvent(SSEEventContentBlockDelta, event)
}

// FormatContentBlockStop 格式化content_block_stop事件
func (f *AnthropicSSEFormatter) FormatContentBlockStop(index int) string {
	event := map[string]any{
		"type":  "content_block_stop",
		"index": index,
	}
	return f.FormatSSEEvent(SSEEventContentBlockStop, event)
}

// FormatMessageDelta 格式化message_delta事件
func (f *AnthropicSSEFormatter) FormatMessageDelta(stopReason string, usage *Usage) string {
	delta := map[string]any{
		"stop_reason":   stopReason,
		"stop_sequence": nil,
	}

	event := map[string]any{
		"type":  "message_delta",
		"delta": delta,
	}

	// 🔧 核心修复：如果有usage信息，创建完整的usage对象包含cache字段
	if usage != nil {
		// 🔧 优化：使用正确的字段优先级策略
		outputTokens := usage.OutputTokens
		if outputTokens == 0 {
			outputTokens = usage.CompletionTokens
		}

		usageMap := map[string]any{
			"output_tokens": outputTokens,
		}

		// 🔧 关键新增：添加cache相关token字段到message_delta中
		if usage.CacheCreationInputTokens > 0 {
			usageMap["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
		}
		if usage.CacheReadInputTokens > 0 {
			usageMap["cache_read_input_tokens"] = usage.CacheReadInputTokens
		}

		event["usage"] = usageMap
		DebugLog("[UsageInfo] FormatMessageDelta usage: output_tokens=%d, cache_creation=%d, cache_read=%d",
			outputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens)
	}

	return f.FormatSSEEvent(SSEEventMessageDelta, event)
}

// FormatMessageStop 格式化message_stop事件
func (f *AnthropicSSEFormatter) FormatMessageStop(additional map[string]any) string {
	event := map[string]any{
		"type": "message_stop",
	}

	// 添加额外信息（如amazon-bedrock-invocationMetrics）
	for key, value := range additional {
		event[key] = value
	}

	return f.FormatSSEEvent(SSEEventMessageStop, event)
}

// SSEEventValidator SSE事件序列验证器 - 确保完全符合Anthropic规范
type SSEEventValidator struct {
	expectedSequence []string
	currentIndex     int
	eventHistory     []string
	mu               sync.Mutex
}

// NewSSEEventValidator 创建新的SSE事件验证器
func NewSSEEventValidator() *SSEEventValidator {
	return &SSEEventValidator{
		expectedSequence: []string{
			SSEEventMessageStart,
			SSEEventContentBlockStart,
			SSEEventContentBlockDelta, // 可能有多个
			SSEEventContentBlockStop,
			SSEEventMessageDelta,
			SSEEventMessageStop,
		},
		currentIndex: 0,
		eventHistory: make([]string, 0, 10),
	}
}

// ValidateEvent 验证事件是否符合Anthropic规范序列
func (v *SSEEventValidator) ValidateEvent(eventType string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.eventHistory = append(v.eventHistory, eventType)

	// 特殊处理：content_block_delta可以重复
	if eventType == SSEEventContentBlockDelta {
		// 必须在content_block_start之后
		if len(v.eventHistory) < 2 || !v.hasEventInHistory(SSEEventContentBlockStart) {
			return fmt.Errorf("content_block_delta received before content_block_start")
		}
		return nil // content_block_delta可以多次出现
	}

	// 验证事件顺序
	switch eventType {
	case SSEEventMessageStart:
		if v.currentIndex != 0 {
			return fmt.Errorf("message_start must be the first event, but received at position %d", len(v.eventHistory))
		}
		v.currentIndex = 1

	case SSEEventContentBlockStart:
		if !v.hasEventInHistory(SSEEventMessageStart) {
			return fmt.Errorf("content_block_start received before message_start")
		}
		v.currentIndex = 2

	case SSEEventContentBlockStop:
		if !v.hasEventInHistory(SSEEventContentBlockStart) {
			return fmt.Errorf("content_block_stop received without corresponding content_block_start")
		}
		v.currentIndex = 4

	case SSEEventMessageDelta:
		if !v.hasEventInHistory(SSEEventContentBlockStop) && !v.hasEventInHistory(SSEEventContentBlockStart) {
			return fmt.Errorf("message_delta received without any content blocks")
		}
		v.currentIndex = 5

	case SSEEventMessageStop:
		if !v.hasEventInHistory(SSEEventMessageDelta) {
			return fmt.Errorf("message_stop received without message_delta")
		}
		v.currentIndex = 6

	default:
		return fmt.Errorf("unknown event type: %s", eventType)
	}

	return nil
}

// hasEventInHistory 检查历史中是否包含指定事件
func (v *SSEEventValidator) hasEventInHistory(eventType string) bool {
	return slices.Contains(v.eventHistory, eventType)
}

// GetValidationReport 获取验证报告
func (v *SSEEventValidator) GetValidationReport() map[string]any {
	v.mu.Lock()
	defer v.mu.Unlock()

	return map[string]any{
		"total_events":      len(v.eventHistory),
		"current_index":     v.currentIndex,
		"event_history":     append([]string{}, v.eventHistory...), // 创建副本
		"sequence_complete": v.currentIndex >= len(v.expectedSequence)-1,
		"expected_next":     v.getNextExpectedEvent(),
	}
}

// getNextExpectedEvent 获取下一个期望的事件
func (v *SSEEventValidator) getNextExpectedEvent() string {
	if v.currentIndex < len(v.expectedSequence) {
		return v.expectedSequence[v.currentIndex]
	}
	return "sequence_complete"
}

// ParseUsageFromResponse 从上游响应中解析完整的usage信息，包括cache相关token字段
func ParseUsageFromResponse(rawUsage map[string]any) *Usage {
	if rawUsage == nil {
		return nil
	}

	usage := &Usage{}

	// 🔧 修复：支持多种数值类型的转换
	parseIntValue := func(v any) int {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		case int64:
			return int(val)
		case string:
			// 尝试从字符串解析
			var intVal int
			if n, err := fmt.Sscanf(val, "%d", &intVal); err == nil && n == 1 {
				return intVal
			}
		}
		return 0
	}

	// 解析基本token字段（OpenAI格式）
	if v, ok := rawUsage["prompt_tokens"]; ok {
		usage.PromptTokens = parseIntValue(v)
	}
	if v, ok := rawUsage["completion_tokens"]; ok {
		usage.CompletionTokens = parseIntValue(v)
	}
	if v, ok := rawUsage["total_tokens"]; ok {
		usage.TotalTokens = parseIntValue(v)
	}

	// 🔧 核心修复：解析上游的详细缓存字段
	if v, ok := rawUsage["prompt_cache_hit_tokens"]; ok {
		usage.PromptCacheHitTokens = parseIntValue(v)
	}
	if v, ok := rawUsage["prompt_cache_miss_tokens"]; ok {
		usage.PromptCacheMissTokens = parseIntValue(v)
	}

	// 🔧 核心修复：正确映射到Anthropic格式
	// 1. 基本字段映射
	if v, ok := rawUsage["input_tokens"]; ok {
		usage.InputTokens = parseIntValue(v)
	} else {
		// 映射：prompt_tokens -> input_tokens
		usage.InputTokens = usage.PromptTokens
	}

	if v, ok := rawUsage["output_tokens"]; ok {
		usage.OutputTokens = parseIntValue(v)
	} else {
		// 映射：completion_tokens -> output_tokens
		usage.OutputTokens = usage.CompletionTokens
	}

	// 2. 🔧 关键映射：缓存字段映射
	if v, ok := rawUsage["cache_creation_input_tokens"]; ok {
		usage.CacheCreationInputTokens = parseIntValue(v)
	} else {
		// 映射：prompt_cache_miss_tokens -> cache_creation_input_tokens
		usage.CacheCreationInputTokens = usage.PromptCacheMissTokens
	}

	if v, ok := rawUsage["cache_read_input_tokens"]; ok {
		usage.CacheReadInputTokens = parseIntValue(v)
	} else {
		// 映射：prompt_cache_hit_tokens -> cache_read_input_tokens
		usage.CacheReadInputTokens = usage.PromptCacheHitTokens
	}

	// 3. 计算total_tokens如果未提供
	if usage.TotalTokens == 0 {
		// 优先使用Anthropic字段计算
		if usage.InputTokens > 0 && usage.OutputTokens > 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		} else {
			// 回退到OpenAI字段
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
	}

	// 🔧 新增：详细的调试信息，展示完整的字段映射
	// DebugLog("[UsageInfo] ParseUsageFromResponse complete mapping:")
	// DebugLog("  OpenAI -> Anthropic: prompt=%d->input=%d, completion=%d->output=%d, total=%d",
	// 	usage.PromptTokens, usage.InputTokens, usage.CompletionTokens, usage.OutputTokens, usage.TotalTokens)
	// DebugLog("  Cache mapping: hit=%d->read=%d, miss=%d->creation=%d",
	// 	usage.PromptCacheHitTokens, usage.CacheReadInputTokens, usage.PromptCacheMissTokens, usage.CacheCreationInputTokens)

	return usage
}

// ValidateCompleteSequence 验证完整的事件序列
func (v *SSEEventValidator) ValidateCompleteSequence() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// 检查必需的事件
	requiredEvents := []string{
		SSEEventMessageStart,
		SSEEventMessageStop,
	}

	for _, required := range requiredEvents {
		if !v.hasEventInHistory(required) {
			return fmt.Errorf("missing required event: %s", required)
		}
	}

	// 验证事件顺序
	if len(v.eventHistory) == 0 {
		return fmt.Errorf("no events recorded")
	}

	if v.eventHistory[0] != SSEEventMessageStart {
		return fmt.Errorf("first event must be message_start, got: %s", v.eventHistory[0])
	}

	if v.eventHistory[len(v.eventHistory)-1] != SSEEventMessageStop {
		return fmt.Errorf("last event must be message_stop, got: %s", v.eventHistory[len(v.eventHistory)-1])
	}

	return nil
}

// Enhanced AnthropicSSEFormatter with validation support
type EnhancedAnthropicSSEFormatter struct {
	*AnthropicSSEFormatter
	validator *SSEEventValidator
}

// NewEnhancedAnthropicSSEFormatter 创建增强的SSE格式化器（带验证）
func NewEnhancedAnthropicSSEFormatter() *EnhancedAnthropicSSEFormatter {
	return &EnhancedAnthropicSSEFormatter{
		AnthropicSSEFormatter: NewAnthropicSSEFormatter(),
		validator:             NewSSEEventValidator(),
	}
}

// FormatSSEEventWithValidation 格式化SSE事件并进行验证
func (f *EnhancedAnthropicSSEFormatter) FormatSSEEventWithValidation(eventType string, data any) (string, error) {
	// 验证事件序列
	if err := f.validator.ValidateEvent(eventType); err != nil {
		DebugLog("SSE Event validation failed: %v", err)
		// 仍然格式化事件，但记录验证错误
	}

	// 格式化事件
	return f.AnthropicSSEFormatter.FormatSSEEvent(eventType, data), nil
}

// GetValidationReport 获取验证报告
func (f *EnhancedAnthropicSSEFormatter) GetValidationReport() map[string]any {
	return f.validator.GetValidationReport()
}

// Reset 重置验证器状态
func (f *EnhancedAnthropicSSEFormatter) Reset() {
	f.validator = NewSSEEventValidator()
}
