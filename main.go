package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"codebuddy2cc/handlers"
	"codebuddy2cc/middleware"
	"codebuddy2cc/utils"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found")
	}

	authToken := os.Getenv("CODEBUDDY2CC_AUTH")

	if authToken == "" {
		log.Fatal("CODEBUDDY2CC_AUTH environment variable is required")
	}
	// 初始化debug模式
	utils.InitDebugMode()

	// 初始化模型映射
	if err := utils.LoadModelMapping(); err != nil {
		log.Printf("Warning: Failed to load model mapping: %v", err)
	}

	// 验证上游API密钥
	upstreamKey := os.Getenv("CODEBUDDY2CC_KEY")
	if upstreamKey == "" {
		log.Fatal("CODEBUDDY2CC_KEY environment variable is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	v1 := router.Group("/v1")
	v1.Use(middleware.AuthMiddleware())
	{
		v1.POST("/messages", handlers.MessagesHandler)
		v1.GET("/models", handlers.ModelsHandler)
	}

	router.GET("/health", func(c *gin.Context) {
		healthData := gin.H{
			"status":    "ok",
			"service":   "codebuddy2cc",
			"version":   "1.0.0",
			"timestamp": utils.GetCurrentTimestamp(),
		}

		// 简化的密钥验证
		if os.Getenv("CODEBUDDY2CC_KEY") != "" {
			healthData["upstream_key"] = "configured"
		} else {
			healthData["upstream_key"] = "missing"
		}

		c.JSON(200, healthData)
	})

	// 服务信息端点（用于macOS服务监控）
	router.GET("/service/info", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"service_name": "com.codebuddy2cc.service",
			"binary_name":  "codebuddy2cc",
			"status":       "running",
			"port":         port,
			"debug_mode":   utils.IsDebugEnabled(),
			"timestamp":    utils.GetCurrentTimestamp(),
		})
	})

	// 优雅的信号处理，支持macOS LaunchAgent服务模式
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v, initiating graceful shutdown...", sig)

		// 清理资源
		utils.CloseDebugFile()

		// 根据信号类型处理
		switch sig {
		case syscall.SIGHUP:
			log.Printf("Received SIGHUP, reloading configuration...")
			// 重新加载配置（可以扩展为重新加载.env和模型映射）
			if err := utils.LoadModelMapping(); err != nil {
				log.Printf("Warning: Failed to reload model mapping: %v", err)
			}
			utils.InitDebugMode() // 重新初始化debug模式
			log.Printf("Configuration reloaded successfully")
			return // 不退出，继续运行
		case syscall.SIGINT, syscall.SIGTERM:
			log.Printf("Graceful shutdown completed")
			os.Exit(0)
		}
	}()

	log.Printf("codebuddy2cc server starting on port %s", port)
	log.Fatal(router.Run(":" + port))
}
