# Nautilus Control - K3s Master Node
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Go 모듈 복사 및 의존성 설치
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사
COPY . .

# Nautilus Control 빌드
RUN CGO_ENABLED=0 GOOS=linux go build -o nautilus-control .

# 런타임 이미지
FROM alpine:latest

RUN apk --no-cache add ca-certificates curl bash

# 디렉토리 생성
RUN mkdir -p /var/lib/rancher/k3s /etc/rancher/k3s /usr/local/bin

# K3s 바이너리 직접 다운로드 (Alpine에서는 install script 사용 불가)
RUN curl -L https://github.com/k3s-io/k3s/releases/download/v1.28.2+k3s1/k3s -o /usr/local/bin/k3s && \
    chmod +x /usr/local/bin/k3s

# kubectl 바이너리 다운로드 (K8s API 실행용)
RUN curl -L "https://dl.k8s.io/release/v1.28.2/bin/linux/amd64/kubectl" -o /usr/local/bin/kubectl && \
    chmod +x /usr/local/bin/kubectl

WORKDIR /root/

# 빌드된 바이너리 복사
COPY --from=builder /app/nautilus-control .

# Skip config files for now - will be generated at runtime

# 포트 노출 (K3s API Server + HTTP API)
EXPOSE 6443 8080

# 헬스체크
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

# 실행 명령
CMD ["./nautilus-control"]