# codebuddy2cc Makefile - macOS Service Management
# 基于ccLoad架构实现的macOS LaunchAgent服务管理

# 变量定义
SERVICE_NAME = com.codebuddy2cc.service
PLIST_TEMPLATE = $(SERVICE_NAME).plist.template
PLIST_FILE = $(SERVICE_NAME).plist
LAUNCH_AGENTS_DIR = $(HOME)/Library/LaunchAgents
TARGET_PLIST = $(LAUNCH_AGENTS_DIR)/$(PLIST_FILE)
BINARY_NAME = codebuddy2cc
LOG_DIR = logs
PROJECT_DIR = $(shell pwd)
GOTAGS ?= go_json

# 端口配置 - 与Go程序(godotenv)保持一致的优先级
# 优先级: 系统环境变量(最高) > .env文件 > 默认值8080

# 仅在环境变量不存在时才加载.env文件
ifeq ($(origin PORT),undefined)
    -include .env
endif
# 最终默认值
PORT ?= 8080

.PHONY: help build generate-plist install-service uninstall-service remove-service remove-service-force start stop restart status logs error-logs clean dev info test lint vet mod-tidy check-files

# 默认目标
help:
	@echo "codebuddy2cc macOS服务管理 Makefile"
	@echo ""
	@echo "可用命令:"
	@echo "  build               - 构建二进制文件"
	@echo "  generate-plist      - 从模板生成 plist 文件"
	@echo "  install-service     - 安装 LaunchAgent 服务"
	@echo "  uninstall-service   - 卸载 LaunchAgent 服务"
	@echo "  remove-service      - 完全删除服务（包括所有相关文件）"
	@echo "  remove-service-force- 强制删除服务（无确认提示）"
	@echo "  start              - 启动服务"
	@echo "  stop               - 停止服务"
	@echo "  restart            - 重启服务"
	@echo "  status             - 查看服务状态"
	@echo "  logs               - 查看服务日志"
	@echo "  error-logs         - 查看错误日志"
	@echo "  check-files        - 检查服务相关文件状态"
	@echo "  clean              - 清理构建文件和日志"
	@echo "  dev                - 开发模式运行"
	@echo "  info               - 查看完整服务信息"
	@echo "  test               - 运行测试"
	@echo "  lint               - 代码检查"
	@echo "  vet                - 静态分析"
	@echo "  mod-tidy           - 整理依赖"

# 构建二进制文件
build:
	@echo "构建 $(BINARY_NAME)..."
	@go build -tags "$(GOTAGS)" -o $(BINARY_NAME) .
	@echo "构建完成: $(BINARY_NAME)"

# 生成 plist 文件（从模板动态替换路径和环境变量）
generate-plist:
	@echo "从模板生成 plist 文件..."
	@if [ ! -f "$(PLIST_TEMPLATE)" ]; then \
		echo "错误: 模板文件 $(PLIST_TEMPLATE) 不存在"; \
		echo "请先运行: make create-plist-template"; \
		exit 1; \
	fi
	@echo "处理环境变量配置..."
	@ENV_VARS=""; \
	if [ -f ".env" ]; then \
		echo "发现 .env 文件，读取环境变量..."; \
		while IFS='=' read -r key value || [ -n "$$key" ]; do \
			if [ -n "$$key" ] && [ "$${key#\#}" = "$$key" ] && [ -n "$$value" ]; then \
				case "$$key" in \
					PORT|DEBUG|DEBUG_FILE|CODEBUDDY2CC_*) \
						if [ -n "$$ENV_VARS" ]; then \
							ENV_VARS="$$ENV_VARS\n\t\t<key>$$key</key>\n\t\t<string>$$value</string>"; \
						else \
							ENV_VARS="\t\t<key>$$key</key>\n\t\t<string>$$value</string>"; \
						fi; \
						;; \
				esac; \
			fi; \
		done < .env; \
	else \
		echo "未发现 .env 文件，跳过环境变量注入"; \
	fi; \
	sed 's|{{PROJECT_DIR}}|$(PROJECT_DIR)|g' $(PLIST_TEMPLATE) | \
	sed "s|{{ENV_VARIABLES}}|$$ENV_VARS|g" > $(PLIST_FILE)
	@echo "plist 文件已生成: $(PLIST_FILE)"
	@if [ -f ".env" ]; then \
		echo "已从 .env 文件注入环境变量到 LaunchAgent 配置"; \
	fi

# 创建 plist 模板文件
create-plist-template:
	@echo "创建 plist 模板文件..."
	@printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0">' \
		'<dict>' \
		'	<key>Label</key>' \
		'	<string>com.codebuddy2cc.service</string>' \
		'	<key>ProgramArguments</key>' \
		'	<array>' \
		'		<string>{{PROJECT_DIR}}/codebuddy2cc</string>' \
		'	</array>' \
		'	<key>WorkingDirectory</key>' \
		'	<string>{{PROJECT_DIR}}</string>' \
		'	<key>StandardOutPath</key>' \
		'	<string>{{PROJECT_DIR}}/logs/codebuddy2cc.log</string>' \
		'	<key>StandardErrorPath</key>' \
		'	<string>{{PROJECT_DIR}}/logs/codebuddy2cc.log</string>' \
		'	<key>EnvironmentVariables</key>' \
		'	<dict>' \
		'		{{ENV_VARIABLES}}' \
		'	</dict>' \
		'	<key>RunAtLoad</key>' \
		'	<true/>' \
		'	<key>KeepAlive</key>' \
		'	<dict>' \
		'		<key>SuccessfulExit</key>' \
		'		<false/>' \
		'		<key>NetworkState</key>' \
		'		<true/>' \
		'	</dict>' \
		'	<key>ProcessType</key>' \
		'	<string>Background</string>' \
		'	<key>Nice</key>' \
		'	<integer>1</integer>' \
		'</dict>' \
		'</plist>' \
		> $(PLIST_TEMPLATE)
	@echo "plist 模板文件已创建: $(PLIST_TEMPLATE)"

# 安装服务
install-service: build create-plist-template generate-plist
	@echo "安装 LaunchAgent 服务..."
	@mkdir -p $(LOG_DIR)
	@mkdir -p $(LAUNCH_AGENTS_DIR)
	@if [ -f "$(TARGET_PLIST)" ]; then \
		echo "服务已存在，先卸载旧服务..."; \
		$(MAKE) uninstall-service; \
	fi
	@if [ ! -f ".env" ]; then \
		echo "警告: .env 文件不存在，请确保环境变量已正确配置"; \
	fi
	@cp $(PLIST_FILE) $(TARGET_PLIST)
	@launchctl load $(TARGET_PLIST)
	@echo "服务安装完成并已启动"
	@sleep 2
	@$(MAKE) status

# 卸载服务
uninstall-service:
	@echo "卸载 LaunchAgent 服务..."
	@if [ -f "$(TARGET_PLIST)" ]; then \
		launchctl unload $(TARGET_PLIST) 2>/dev/null || true; \
		rm -f $(TARGET_PLIST); \
		echo "服务已卸载"; \
	else \
		echo "服务未安装"; \
	fi

# 完全删除服务（包括所有相关文件和数据）
remove-service:
	@echo "=========================================="
	@echo "警告: 即将完全删除服务及所有相关文件"
	@echo "这将删除:"
	@echo "  - LaunchAgent 服务配置"
	@echo "  - 所有日志文件"
	@echo "  - plist 配置文件"
	@echo "  - plist 模板文件"
	@echo "  - 二进制文件"
	@echo "=========================================="
	@echo ""
	@read -p "确认删除所有服务文件? [y/N] " confirm; \
	if [ "$$confirm" = "y" ] || [ "$$confirm" = "Y" ]; then \
		echo "开始删除服务..."; \
		$(MAKE) uninstall-service; \
		echo "删除本地文件..."; \
		rm -f $(BINARY_NAME); \
		rm -f $(PLIST_FILE); \
		rm -f $(PLIST_TEMPLATE); \
		rm -rf $(LOG_DIR); \
		echo ""; \
		echo "✅ 服务已完全删除"; \
		echo "以下文件已被删除:"; \
		echo "  - $(BINARY_NAME) (二进制文件)"; \
		echo "  - $(PLIST_FILE) (plist配置)"; \
		echo "  - $(PLIST_TEMPLATE) (plist模板)"; \
		echo "  - $(LOG_DIR)/ (日志目录)"; \
		echo "  - $(TARGET_PLIST) (系统服务配置)"; \
	else \
		echo "删除操作已取消"; \
	fi

# 强制删除服务（无确认，用于脚本）
remove-service-force: uninstall-service
	@echo "强制删除服务及所有相关文件..."
	@rm -f $(BINARY_NAME)
	@rm -f $(PLIST_FILE)
	@rm -f $(PLIST_TEMPLATE)
	@rm -rf $(LOG_DIR)
	@echo "✅ 服务已强制删除完成"

# 启动服务
start:
	@echo "启动服务..."
	@launchctl start $(SERVICE_NAME)
	@sleep 2
	@$(MAKE) status

# 停止服务
stop:
	@echo "停止服务..."
	@launchctl stop $(SERVICE_NAME)
	@sleep 2
	@$(MAKE) status

# 重启服务
restart: stop start

# 查看服务状态
status:
	@echo "=== 服务状态 ==="
	@launchctl list | grep $(SERVICE_NAME) || echo "服务未运行"

# 查看服务日志
logs:
	@echo "=== 标准输出日志 ==="
	@if [ -f "$(LOG_DIR)/codebuddy2cc.log" ]; then \
		tail -f $(LOG_DIR)/codebuddy2cc.log; \
	else \
		echo "日志文件不存在: $(LOG_DIR)/codebuddy2cc.log"; \
		echo "请检查服务是否正在运行"; \
	fi

# 查看错误日志
error-logs:
	@echo "=== 错误日志 ==="
	@if [ -f "$(LOG_DIR)/codebuddy2cc.error.log" ]; then \
		tail -f $(LOG_DIR)/codebuddy2cc.error.log; \
	else \
		echo "错误日志文件不存在: $(LOG_DIR)/codebuddy2cc.error.log"; \
	fi

# 查看最近日志（非跟踪模式）
logs-recent:
	@echo "=== 最近20行日志 ==="
	@if [ -f "$(LOG_DIR)/codebuddy2cc.log" ]; then \
		tail -20 $(LOG_DIR)/codebuddy2cc.log; \
	else \
		echo "日志文件不存在"; \
	fi

# 清理文件
clean:
	@echo "清理构建文件和日志..."
	@rm -f $(BINARY_NAME)
	@rm -f $(PLIST_FILE)
	@rm -rf $(LOG_DIR)
	@echo "清理完成"

# 深度清理（包括模板文件）
clean-all: clean
	@echo "深度清理（包括模板文件）..."
	@rm -f $(PLIST_TEMPLATE)
	@echo "深度清理完成"

# 开发模式运行（不作为服务）
dev:
	@echo "开发模式运行..."
	@go run -tags "$(GOTAGS)" .

# 查看完整服务信息
info:
	@echo "=== codebuddy2cc 服务信息 ==="
	@echo "服务名称: $(SERVICE_NAME)"
	@echo "配置模板: $(PLIST_TEMPLATE)"
	@echo "配置文件: $(PLIST_FILE)"
	@echo "安装路径: $(TARGET_PLIST)"
	@echo "二进制文件: $(BINARY_NAME)"
	@echo "项目目录: $(PROJECT_DIR)"
	@echo "日志目录: $(LOG_DIR)"
	@echo ""
	@$(MAKE) status

# 健康检查
health:
	@echo "=== 健康检查 ==="
	@curl -s http://localhost:$(PORT)/health || echo "健康检查失败 - 服务可能未运行"

# 运行测试
test:
	@echo "运行 Go 测试..."
	@go test -v ./...

# 代码格式化和检查
lint:
	@echo "运行代码格式化..."
	@go fmt ./...

# 静态分析
vet:
	@echo "运行静态分析..."
	@go vet ./...

# 整理依赖
mod-tidy:
	@echo "整理 Go 模块依赖..."
	@go mod tidy

# 完整的代码质量检查
quality: mod-tidy lint vet test
	@echo "代码质量检查完成"

# 快速部署（构建、安装、启动）
deploy: quality install-service
	@echo "部署完成"
	@$(MAKE) health

# 一键重新部署
redeploy: uninstall-service deploy
	@echo "重新部署完成"

# 检查服务相关文件状态
check-files:
	@echo "=== 服务文件状态检查 ==="
	@echo "项目目录: $(PROJECT_DIR)"
	@echo ""
	@echo "本地文件:"
	@if [ -f "$(BINARY_NAME)" ]; then \
		echo "  ✅ $(BINARY_NAME) (二进制文件)"; \
	else \
		echo "  ❌ $(BINARY_NAME) (二进制文件) - 不存在"; \
	fi
	@if [ -f "$(PLIST_TEMPLATE)" ]; then \
		echo "  ✅ $(PLIST_TEMPLATE) (plist模板)"; \
	else \
		echo "  ❌ $(PLIST_TEMPLATE) (plist模板) - 不存在"; \
	fi
	@if [ -f "$(PLIST_FILE)" ]; then \
		echo "  ✅ $(PLIST_FILE) (plist配置)"; \
	else \
		echo "  ❌ $(PLIST_FILE) (plist配置) - 不存在"; \
	fi
	@if [ -d "$(LOG_DIR)" ]; then \
		echo "  ✅ $(LOG_DIR)/ (日志目录)"; \
		if [ -f "$(LOG_DIR)/codebuddy2cc.log" ]; then \
			echo "    ✅ 标准输出日志"; \
		else \
			echo "    ❌ 标准输出日志 - 不存在"; \
		fi; \
		if [ -f "$(LOG_DIR)/codebuddy2cc.error.log" ]; then \
			echo "    ✅ 错误日志"; \
		else \
			echo "    ❌ 错误日志 - 不存在"; \
		fi; \
	else \
		echo "  ❌ $(LOG_DIR)/ (日志目录) - 不存在"; \
	fi
	@echo ""
	@echo "系统文件:"
	@if [ -f "$(TARGET_PLIST)" ]; then \
		echo "  ✅ $(TARGET_PLIST) (系统服务配置)"; \
	else \
		echo "  ❌ $(TARGET_PLIST) (系统服务配置) - 不存在"; \
	fi
	@echo ""
	@echo "服务状态:"
	@$(MAKE) status