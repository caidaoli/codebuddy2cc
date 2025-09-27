# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

codebuddy2cc是一个简洁高效的Go API代理程序，用于将Anthropic Messages API格式请求转发到CodeBuddy上游服务。核心功能包括格式转换（Anthropic ↔ OpenAI兼容）、认证管理、流式支持和工具调用。

## 构建和运行命令

```bash
# 构建项目
go build -o codebuddy2cc

# 运行服务
./codebuddy2cc

# 验证和整理依赖
go mod tidy

# 格式化代码（必须执行）
go fmt ./...

# 静态分析检查（必须执行）
go vet ./...

# 运行测试（使用Go标准测试）
go test ./...

# 运行特定包测试
go test ./handlers
go test ./utils
go test ./middleware

# 带详细输出的测试
go test -v ./...
```

## 环境配置

复制环境变量模板并配置：
```bash
cp .env.example .env
```

必需的环境变量：
- `CODEBUDDY2CC_AUTH`: 客户端认证token
- `CODEBUDDY2CC_KEY`: 单一上游API密钥
- `PORT`: 服务端口（可选，默认8080）

可选的调试环境变量：
- `DEBUG`: 调试模式开关（true/1/on启用，默认false）
- `DEBUG_FILE`: 调试日志文件路径（可选，默认仅控制台输出）
- `CODEBUDDY2CC_UPSTREAM_URL`: 上游API地址（可选，测试用）

## 核心架构

### 模块化设计遵循SOLID原则
- **middleware/auth.go**: 单一职责的认证中间件，支持Bearer token和X-API-Key两种认证方式
- **handlers/messages.go**: 专门处理/v1/messages端点，采用统一处理架构：
  - `processUnifiedResponse()`: 核心处理器，统一解析上游SSE响应
  - `writeStreamResponse()`: SSE流式输出适配器
  - `writeNonStreamResponse()`: JSON响应输出适配器
- **utils/converter.go**: 格式转换工具，实现Anthropic与OpenAI格式互转，支持工具调用转换
- **utils/debug.go**: 调试工具，支持文件和控制台日志输出
- **utils/json.go**: 高性能JSON处理，使用bytedance/sonic优化序列化性能
- **utils/model.go**: 模型映射管理，支持通过model.json配置文件进行模型映射
- **main.go**: 程序入口，环境验证、信号处理和路由配置

### 请求处理流程
1. 客户端发送Anthropic格式请求到 `/v1/messages`
2. 认证中间件验证Bearer token或X-API-Key
3. 转换器将Anthropic请求转换为OpenAI兼容格式
4. 转发到上游API `https://www.codebuddy.ai/v2/chat/completions`
5. `processUnifiedResponse()` 统一解析上游SSE流响应
6. 根据客户端需求调用相应输出适配器：
   - 流式：`writeStreamResponse()` 输出SSE格式
   - 非流式：`writeNonStreamResponse()` 输出JSON格式

### 统一响应处理架构（重构后）
项目采用统一处理架构消除代码重复：
- **数据层**: `ResponseData` 结构统一表示处理结果
- **处理层**: `processUnifiedResponse()` 单一处理器解析所有上游响应
- **输出层**: 输出适配器根据客户端需求选择格式
- **优势**: 90%代码重复消除，符合DRY、SRP、OCP原则

### 关键技术特性
- **统一响应架构**: 单一处理管道`processUnifiedResponse()`处理所有上游响应，输出适配器分离格式化逻辑
- **流式响应**: 完整的Server-Sent Events支持，使用自定义SSEStreamParser实现真正的流式解析
- **工具调用**: 支持function calling，映射Anthropic tools到OpenAI functions，支持工具调用结果处理
- **会话级状态管理**: 每个请求独立的`ToolCallsSession`，避免并发冲突
- **智能消息合并**: 自动合并连续的assistant消息，处理复杂的对话流
- **高性能JSON**: 使用bytedance/sonic替代标准库，显著提升序列化性能
- **错误处理**: 完善的错误传播和日志记录机制，支持上游错误透传
- **性能优化**: 基于Gin框架，独立context管理，高并发支持，优化的内存使用
- **调试支持**: 可配置的调试模式，支持文件输出和详细的请求/响应日志
- **模型映射**: 灵活的模型映射机制，支持运行时配置

## API端点

- `POST /v1/messages` - Anthropic Messages API兼容端点
- `GET /health` - 健康检查端点

## 开发原则

项目严格遵循以下开发原则：
- **KISS**: 保持简单，专注核心功能，无不必要的复杂性
- **SRP**: 每个模块单一职责，认证、转换、转发完全分离
- **高性能**: 最小延迟转发，优化的内存使用
- **可靠性**: 完善的错误处理，上游错误透传


## 重要实现细节

### 流式解析器架构
项目实现了自定义的`SSEStreamParser`，解决了标准库在处理SSE流时的性能问题：
- **真正流式**: 不等待完整行，立即处理可用数据块
- **智能边界检测**: 支持标准双换行符(\n\n)和兼容模式单换行符(\n)
- **UTF-8安全**: 防止在多字节字符中间切割，确保中文等字符正确处理

### 工具调用转换逻辑
复杂的工具调用处理机制，支持：
- **OpenAI -> Anthropic**: 聚合分片的tool_calls数据，转换为完整的tool_use格式
- **Anthropic -> OpenAI**: 将tool_use块转换为tool_calls格式
- **会话级状态管理**: 每个请求独立的工具调用状态，避免并发冲突
- **智能参数修复**: 自动修复常见的JSON格式问题

### 消息合并策略
智能处理连续的assistant消息：
- 识别第一个消息有content、第二个消息有tool_calls的场景
- 自动合并为单个消息，避免上游API校验失败
- 保持消息语义的完整性

## 调试和开发

### 调试模式配置
设置环境变量启用详细日志：
```bash
DEBUG=true
DEBUG_FILE=./debug.log
```

调试模式下会记录：
- 完整的请求/响应转换过程
- SSE流解析的每个步骤
- 工具调用的聚合过程
- 模型映射的执行情况

### 常见开发任务
1. **添加新的格式转换**: 修改`utils/converter.go`中的转换函数
2. **扩展认证方式**: 在`middleware/auth.go`中添加新的认证逻辑
3. **调整流式解析**: 优化`handlers/messages.go`中的`SSEStreamParser`
4. **配置模型映射**: 编辑`model.json`文件
5. **新增输出格式**: 在`handlers/messages.go`中添加新的`write*Response()`适配器
6. **修改核心处理逻辑**: 统一在`processUnifiedResponse()`函数中处理

## 代码规范

### Go 语言现代化要求

**类型声明现代化**:
- ✅ **使用 `any` 替代 `interface{}`**: 遵循 Go 1.18+ 社区最佳实践
- ✅ **泛型优先**: 在合适场景使用 Go 1.18+ 泛型语法
- ✅ **类型推导**: 充分利用现代Go的类型推导能力

**代码质量标准**:
- **KISS原则**: 优先选择更简洁、可读性更强的现代语法
- **一致性要求**: 全项目统一使用现代Go语法规范
- **向前兼容**: 充分利用Go语言版本特性，保持技术栈先进性

**具体规范**:
```go
// ✅ 推荐：使用现代语法
func processData(data map[string]any) any {
    return data["result"]
}

// ❌ 避免：过时语法  
func processData(data map[string]interface{}) interface{} {
    return data["result"]
}
```

**工具链要求**:
- **go fmt**: 强制代码格式化
- **go vet**: 静态分析检查
- **现代化检查**: 定期审查并升级代码语法到最新标准