# Dockerfile for codebuddy2cc
# 基于Alpine Linux的多阶段构建，实现最小化的生产镜像

# 构建阶段
FROM golang:1.25.0-alpine AS builder

# 安装必要的构建工具和证书
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /build

# 复制模块定义文件
COPY go.mod go.sum ./

# 下载依赖（利用Docker层缓存）
RUN go mod download

# 复制源代码
COPY . .

# 构建二进制文件
# - 使用CGO_ENABLED=0确保静态链接
# - 使用ldflags减小二进制文件大小
# - 使用go_json构建标签，利用bytedance/sonic的性能优化
RUN CGO_ENABLED=0 GOOS=linux go build \
    -tags go_json \
    -ldflags="-w -s" \
    -o codebuddy2cc .

# 运行阶段
FROM alpine:latest

# 安装运行时必需的包
RUN apk add --no-cache ca-certificates tzdata curl

# 创建非root用户
RUN adduser -D -s /bin/sh codebuddy

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/codebuddy2cc .

# 复制配置文件（如果存在）
COPY --from=builder /build/model.json .


# 切换到非root用户
USER codebuddy

# 暴露端口（默认8080，可通过环境变量覆盖）
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${PORT:-8080}/health || exit 1

# 启动应用
CMD ["./codebuddy2cc"]