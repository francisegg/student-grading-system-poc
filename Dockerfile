# Build Stage
FROM golang:1.24-alpine AS builder

# 安裝編譯需要的工具
RUN apk add --no-cache git build-base

WORKDIR /app

# 複製依賴檔並下載
COPY go.mod go.sum ./
RUN go mod download

# 複製原始碼
COPY . .

# 編譯 Go 程式 (產出執行檔名為 server)
RUN go build -o server main.go

# Run Stage (使用輕量級映像檔)
FROM alpine:latest

WORKDIR /root/

# 因為您的 templates 是讀取 HTML 檔，必須複製過去
COPY --from=builder /app/server .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/.env . 
COPY --from=builder /app/static ./static

# 安裝必要的憑證庫 (讓 Go 可以發送 HTTPS 請求給 Google OAuth)
RUN apk add --no-cache ca-certificates tzdata

# 設定時區
ENV TZ=Asia/Taipei

EXPOSE 8080

CMD ["./server"]