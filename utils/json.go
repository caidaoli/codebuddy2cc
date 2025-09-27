package utils

import (
	"github.com/bytedance/sonic"
)

// JSONCodec 提供统一的JSON编解码接口
// 遵循SOLID原则中的依赖倒置原则，上层模块依赖抽象而非具体实现
type JSONCodec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
	MarshalIndent(v any, prefix, indent string) ([]byte, error)
}

// SonicCodec sonic高性能实现
// 提供2-10倍于标准库的JSON处理性能
type SonicCodec struct{}

func (s SonicCodec) Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

func (s SonicCodec) Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}

func (s SonicCodec) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	// sonic v1.14.1+ 支持 MarshalIndent
	return sonic.MarshalIndent(v, prefix, indent)
}

// JSON 全局JSON编解码器实例
var JSON JSONCodec

// init 初始化JSON编解码器
func init() {
	DebugLog("Using bytedance/sonic for JSON operations")
	JSON = SonicCodec{}
}

// FastMarshal 快速序列化接口，专门用于高频场景
// 针对流式处理和工具调用等性能敏感场景优化
func FastMarshal(v any) ([]byte, error) {
	return JSON.Marshal(v)
}

// FastUnmarshal 快速反序列化接口
func FastUnmarshal(data []byte, v any) error {
	return JSON.Unmarshal(data, v)
}

// PrettyMarshal 格式化序列化，用于调试和日志输出
func PrettyMarshal(v any) ([]byte, error) {
	return JSON.MarshalIndent(v, "", "  ")
}
