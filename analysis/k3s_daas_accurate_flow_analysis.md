# K3s-DaaS 정확한 플로우 분석 및 정리

## 🚨 현재 문제점 분석

### 1. 중복된 이벤트 처리 경로
- ❌ HTTP 엔드포인트 (`/api/v1/sui-events`)
- ❌ 실시간 폴링 (`subscribeToMoveContractEvents`)
- **문제**: 둘 다 같은 일을 하는 불필요한 중복

### 2. API Proxy 미구현
- Move 컨트랙트에 API Proxy 기능이 없음
- kubectl이 직접 어디로 요청을 보내는지 불분명

### 3. 불명확한 아키텍처
- kubectl → ? → Move Contract → Nautilus TEE
- 중간 연결점이 애매함

## 🎯 올바른 K3s-DaaS 플로우 정의

### 최종 목표 아키텍처:
```
[사용자 kubectl]
        ↓ (Seal Token 포함)
[API Proxy Server]
        ↓ (Move Contract 호출)
[Sui 블록체인]
        ↓ (이벤트 발생)
[Nautilus TEE]
        ↓ (K8s API 처리)
[실제 K8s 클러스터]
```

## 🔧 필요한 구현사항

### 1. API Proxy Server (신규 필요)
```go
// api-proxy/main.go (새로 만들어야 함)
package main

type APIProxy struct {
    suiRPCURL       string
    contractAddress string
}

// kubectl 요청을 받아서 Move Contract 호출
func (p *APIProxy) HandleKubectl(w http.ResponseWriter, r *http.Request) {
    // 1. Seal Token 검증
    sealToken := extractSealToken(r)

    // 2. Move Contract 함수 호출
    err := p.callMoveContract(sealToken, r.Method, r.URL.Path, getPayload(r))

    // 3. 결과 반환 (또는 비동기 처리)
}
```

### 2. Move Contract에 Proxy 함수 추가
```move
// k8s_gateway.move에 추가 필요
public entry fun handle_kubectl_request(
    seal_token: &SealToken,
    method: String,
    path: String,
    namespace: String,
    resource_type: String,
    payload: vector<u8>,
    ctx: &mut TxContext
) {
    // 검증 후 이벤트 발생
    event::emit(K8sAPIRequest { ... });
}
```

### 3. Nautilus TEE는 이벤트만 수신
```go
// nautilus-release/main.go (단순화)
func (s *SuiEventListener) SubscribeToK8sEvents() error {
    // HTTP 엔드포인트 제거, 폴링만 유지
    go s.subscribeToMoveContractEvents()
    return nil
}
```

## 🚀 권장 단순화 방안

### Option 1: HTTP Direct 방식 (가장 단순)
```
[kubectl] → [API Proxy] → [Nautilus TEE] (Move Contract 없이)
```
- kubectl이 API Proxy로 직접 요청
- API Proxy가 Seal Token 검증 후 Nautilus TEE 호출
- Move Contract는 스테이킹만 관리

### Option 2: 현재 방식 완성 (복잡하지만 완전한 블록체인)
```
[kubectl] → [API Proxy] → [Move Contract] → [Nautilus TEE]
```
- 모든 요청이 블록체인에 기록
- 완전한 탈중앙화 아키텍처

## 💡 즉시 정리 권장사항

### 1. 불필요한 코드 제거
```go
// 제거 권장: HTTP 핸들러 (중복)
// http.HandleFunc("/api/v1/sui-events", s.handleSuiEvent)

// 제거 권장: handleSuiEvent 함수 전체
func (s *SuiEventListener) handleSuiEvent(w http.ResponseWriter, r *http.Request) {
    // 이 함수 전체 삭제
}
```

### 2. 단순화된 구현
```go
func (s *SuiEventListener) SubscribeToK8sEvents() error {
    log.Println("TEE: Starting Sui event subscription...")

    // 폴링만 사용 (HTTP 제거)
    go s.subscribeToMoveContractEvents()
    return nil
}
```

### 3. API Proxy 별도 구현 필요
- `api-proxy/main.go` 새 파일 생성
- kubectl → API Proxy → Move Contract 연결
- 또는 kubectl → API Proxy → Nautilus TEE 직접 연결

## 🎯 해커톤 시연을 위한 최소 구현

### 가장 단순한 시연 방법:
1. **kubectl 설정**:
   ```bash
   kubectl config set-cluster k3s-daas --server=http://localhost:8080
   kubectl config set-credentials user --token=seal_abc123
   ```

2. **API Proxy 구현** (30분):
   ```go
   // 8080 포트에서 kubectl 요청 수신
   // Seal Token 검증
   // Nautilus TEE (9443 포트)로 전달
   ```

3. **Nautilus TEE**:
   ```go
   // Move Contract 폴링 제거
   // HTTP 직접 수신으로 단순화
   ```

## 📋 정리 작업 체크리스트

### 즉시 수정 필요:
- [ ] nautilus-release/main.go에서 불필요한 HTTP 핸들러 제거
- [ ] API Proxy 서버 구현 방향 결정
- [ ] Move Contract에 kubectl 핸들러 함수 추가 또는 제거
- [ ] 사용하지 않는 구조체/함수 정리

### 선택 사항:
- [ ] Option 1: 직접 연결 방식으로 단순화
- [ ] Option 2: 완전한 블록체인 방식으로 완성

## 🤔 권장 질문

1. **시연 목표**: 블록체인 연동을 보여주는 것이 중요한가, 아니면 TEE 보안이 중요한가?
2. **복잡성**: 완전한 탈중앙화 vs. 단순한 시연용?
3. **시간**: 해커톤까지 얼마나 시간이 있는가?

## 🎯 결론 및 권장사항

**현재 상태**: 과도한 복잡성으로 인한 혼란
**권장 해결책**:
1. 불필요한 중복 코드 즉시 제거
2. API Proxy 구현 방향 결정
3. 단순하지만 완동하는 시연 버전 우선 구현

어떤 방향으로 진행하시겠습니까?