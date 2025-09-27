package utils

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// 全局debug开关和文件句柄
var (
	debugMode bool
	debugFile *os.File
)

// InitDebugMode 初始化debug模式
func InitDebugMode() {
	debugEnv := strings.ToLower(strings.TrimSpace(os.Getenv("DEBUG")))
	debugMode = debugEnv == "true" || debugEnv == "1" || debugEnv == "on"

	if debugMode {
		log.Printf("Debug mode ENABLED")

		// 检查是否设置了debug文件路径
		debugFilePath := os.Getenv("DEBUG_FILE")
		if debugFilePath != "" {
			var err error
			debugFile, err = os.OpenFile(debugFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				log.Printf("Failed to open debug file %s: %v", debugFilePath, err)
				debugFile = nil
			} else {
				log.Printf("Debug output will be saved to: %s", debugFilePath)
				// 写入分隔符标识新的会话开始
				fmt.Fprintf(debugFile, "\n=== Debug Session Started: %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		log.Printf("Debug mode disabled")
	}
}

// CloseDebugFile 关闭debug文件（程序退出时调用）
func CloseDebugFile() {
	if debugFile != nil {
		fmt.Fprintf(debugFile, "=== Debug Session Ended: %s ===\n\n", time.Now().Format("2006-01-02 15:04:05"))
		debugFile.Close()
		debugFile = nil
	}
}

// IsDebugEnabled 检查是否处于debug模式（服务端点兼容）
func IsDebugEnabled() bool {
	return debugMode
}

// GetCurrentTimestamp 获取当前时间戳
func GetCurrentTimestamp() string {
	return time.Now().Format("2006-01-02T15:04:05Z07:00")
}

// IsDebugMode 检查是否处于debug模式
func IsDebugMode() bool {
	return debugMode
}

// writeToDebugFile 写入内容到debug文件
func writeToDebugFile(content string) {
	if debugFile != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		fmt.Fprintf(debugFile, "[%s] %s\n", timestamp, content)
		debugFile.Sync() // 立即刷新到磁盘
	}
}

// DebugLogJSON 在debug模式下输出JSON格式的调试信息
func DebugLogJSON(prefix string, data interface{}) {
	if !debugMode {
		return
	}

	jsonData, err := PrettyMarshal(data)
	if err != nil {
		message := fmt.Sprintf("[DEBUG] %s: Failed to marshal JSON: %v", prefix, err)
		log.Printf("%s", message)
		writeToDebugFile(message)
		return
	}

	message := fmt.Sprintf("[DEBUG] %s:\n%s", prefix, string(jsonData))
	log.Printf("%s", message)
	writeToDebugFile(message)
}

// DebugLog 在debug模式下输出普通调试信息
func DebugLog(format string, args ...interface{}) {
	if !debugMode {
		return
	}

	message := fmt.Sprintf("[DEBUG] "+format, args...)
	log.Printf("%s", message)
	writeToDebugFile(message)
}

// DebugLogToolCall 专门用于工具调用的调试日志，包含更多上下文信息
func DebugLogToolCall(sessionID, action, toolID string, stats map[string]int, extra ...interface{}) {
	if !debugMode {
		return
	}

	var extraInfo string
	if len(extra) > 0 {
		extraInfo = fmt.Sprintf(" | extra: %+v", extra)
	}

	message := fmt.Sprintf("[DEBUG] [ToolCall] session=%s action=%s toolID=%s stats=%+v%s",
		sessionID, action, toolID, stats, extraInfo)
	log.Printf("%s", message)
	writeToDebugFile(message)
}

// DebugLogError 专门用于错误调试日志
func DebugLogError(context string, err error, details ...interface{}) {
	if !debugMode {
		return
	}

	var detailsStr string
	if len(details) > 0 {
		detailsStr = fmt.Sprintf(" | details: %+v", details)
	}

	message := fmt.Sprintf("[DEBUG] [ERROR] context=%s error=%v%s", context, err, detailsStr)
	log.Printf("%s", message)
	writeToDebugFile(message)
}
