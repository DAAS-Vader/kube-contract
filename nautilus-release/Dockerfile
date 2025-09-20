# Nautilus Release Docker Container
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Go 모듈 복사 및 의존성 설치
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사
COPY . .

# Nautilus 빌드
RUN CGO_ENABLED=0 GOOS=linux go build -o nautilus-release .

# 런타임 이미지
FROM alpine:latest

RUN apk --no-cache add ca-certificates curl
WORKDIR /root/

# 빌드된 바이너리 복사
COPY --from=builder /app/nautilus-release .

# 설정 파일 복사
COPY --from=builder /app/*.yaml ./

# 포트 노출 (Control Plane API)
EXPOSE 6443 8080

# 헬스체크
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

# 실행 명령
CMD ["./nautilus-release"]