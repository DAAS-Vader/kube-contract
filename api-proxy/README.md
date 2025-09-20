# K3s-DaaS API Proxy

API Proxy layer for K3s-DaaS system that handles kubectl requests through blockchain-secured authentication.

## Components

### contract_api_gateway.go
- kubectl 명령을 Sui Contract로 라우팅하는 HTTP 서버
- Seal Token 기반 인증
- 비동기 응답 처리

### nautilus_event_listener.go
- Sui Contract 이벤트를 수신하여 실제 K8s API 호출
- WebSocket 기반 이벤트 구독
- K8s 클러스터와의 직접 통신

## Architecture Flow

```
kubectl → contract_api_gateway → Sui Contract → nautilus_event_listener → K8s API Server
```

## Usage

1. Start contract API gateway:
   ```bash
   go run contract_api_gateway.go
   ```

2. Start event listener:
   ```bash
   go run nautilus_event_listener.go
   ```

3. Configure kubectl to use the proxy:
   ```bash
   kubectl config set-cluster k3s-daas --server=http://localhost:8080
   kubectl config set-credentials user --token=seal_YOUR_TOKEN
   ```