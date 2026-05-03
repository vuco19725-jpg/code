FROM golang:1.20-alpine AS builder

WORKDIR /app

# 安装依赖
RUN apk add --no-cache git ca-certificates

# 复制源码
COPY . .

# 下载依赖
RUN go mod download

# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o seckill .

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/seckill /app/
COPY --from=builder /app/config/config_prod.yaml /etc/seckill/config.yaml
COPY --from=builder /app/scripts/lua /app/scripts/lua

# 创建日志目录
RUN mkdir -p /app/logs

# 暴露端口
EXPOSE 8080

# 设置时区
ENV TZ=Asia/Shanghai

CMD ["./seckill"]
