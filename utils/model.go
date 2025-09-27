package utils

import (
	"os"
	"path/filepath"
)

type ModelMapping struct {
	Models map[string]string `json:"models"`
}

var modelMapping *ModelMapping

// LoadModelMapping 加载模型映射配置
func LoadModelMapping() error {
	// 获取配置文件路径
	configPath := filepath.Join(".", "model.json")

	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		DebugLog("Model mapping file not found: %s, using original models", configPath)
		modelMapping = &ModelMapping{Models: make(map[string]string)}
		return nil
	}

	// 读取文件内容
	data, err := os.ReadFile(configPath)
	if err != nil {
		DebugLog("Failed to read model mapping file: %v", err)
		modelMapping = &ModelMapping{Models: make(map[string]string)}
		return nil
	}

	// 解析JSON
	var mapping ModelMapping
	if err := FastUnmarshal(data, &mapping); err != nil {
		DebugLog("Failed to parse model mapping file: %v", err)
		modelMapping = &ModelMapping{Models: make(map[string]string)}
		return nil
	}

	modelMapping = &mapping
	DebugLog("Model mapping loaded successfully with %d mappings", len(mapping.Models))
	return nil
}

// MapModel 将输入模型映射为目标模型，如果没有映射则返回原模型
func MapModel(inputModel string) string {
	if modelMapping == nil {
		if err := LoadModelMapping(); err != nil {
			return inputModel
		}
	}

	if targetModel, exists := modelMapping.Models[inputModel]; exists {
		DebugLog("Model mapping: %s -> %s", inputModel, targetModel)
		return targetModel
	}

	DebugLog("No mapping found for model: %s, using original", inputModel)
	return inputModel
}

// GetModelMappings 获取所有模型映射（用于测试和调试）
func GetModelMappings() map[string]string {
	if modelMapping == nil {
		LoadModelMapping()
	}
	return modelMapping.Models
}
