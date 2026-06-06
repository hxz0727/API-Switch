# ============================================
# API-Switch Docker
# 构建: docker build -t api-switch .
# 运行: docker run -d -p 8080:8080 \
#          -v ~/.api-switch.yaml:/root/.api-switch.yaml \
#          -v ~/.claude:/root/.claude \
#          api-switch
# ============================================

# ---- Build stage ----
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

# 国内用户可设置构建参数: docker build --build-arg GOPROXY=https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct

WORKDIR /build
COPY go.mod go.sum ./
RUN GOPROXY=${GOPROXY} go mod download

COPY . .
RUN CGO_ENABLED=0 GOPROXY=${GOPROXY} go build -ldflags="-s -w" -o api-switch ./cmd/api-switch/

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /build/api-switch /usr/local/bin/api-switch

# 默认端口
EXPOSE 8080

# 配置文件（挂载时用目录，不要挂载文件）
VOLUME ["/root"]

ENTRYPOINT ["api-switch"]
CMD ["serve"]
