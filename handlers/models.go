package handlers

import (
	"codebuddy2cc/utils"
	"time"

	"github.com/gin-gonic/gin"
)

// ModelObject OpenAI模型对象定义
type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse OpenAI /v1/models端点响应格式
type ModelsResponse struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

// ModelsHandler 处理 GET /v1/models 请求
// 符合OpenAI API规范，返回model.json中配置的所有模型
func ModelsHandler(c *gin.Context) {
	// 获取model.json中的所有模型ID（keys）
	modelMappings := utils.GetModelMappings()

	// 构建OpenAI格式的模型列表
	models := make([]ModelObject, 0, len(modelMappings))
	currentTime := time.Now().Unix()

	for modelID := range modelMappings {
		models = append(models, ModelObject{
			ID:      modelID,
			Object:  "model",
			Created: currentTime,
			OwnedBy: "codebuddy2cc",
		})
	}

	// 按照OpenAI规范返回
	response := ModelsResponse{
		Object: "list",
		Data:   models,
	}

	utils.DebugLog("Returning %d models from model.json", len(models))
	c.JSON(200, response)
}
