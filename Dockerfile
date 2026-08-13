# ===== 编译阶段 =====
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /app/yyb-go ./cmd/yyb-go

# ===== 运行阶段 =====
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /app/yyb-go .
COPY --from=builder /app/resource/templates ./resource/templates

EXPOSE 5800
CMD ["./yyb-go"]
