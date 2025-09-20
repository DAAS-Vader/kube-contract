# Event-Driven K3s-DaaS 아키텍처 설계

## 🎯 올바른 플로우

```
kubectl → API Gateway → Move Contract → Nautilus (Event Listener) → K8s → Contract
```

## 🔧 5단계 구현 계획

### 1단계: API Gateway (Contract Bridge)
**역할**: kubectl ↔ Move Contract 브릿지
```
Input:  HTTP kubectl 요청
Output: Sui RPC executeTransactionBlock 호출
```

### 2단계: Move Contract (Decision Engine)
**역할**: 모든 검증과 결정
```
Input:  execute_kubectl_command()
Process: Seal 검증 + 스테이킹 확인 + 권한 체크
Output: K8sAPIRequest 이벤트 발생
```

### 3단계: Nautilus Event Listener
**역할**: Contract 이벤트 수신 및 실행
```
Input:  Sui Event Stream
Process: K8s API 실행
Output: store_k8s_response() 호출
```

### 4단계: Response Flow
**역할**: 비동기 응답 처리
```
Contract → Response 저장 → API Gateway 폴링 → kubectl
```

### 5단계: Integration Test
**역할**: E2E 테스트 및 검증

## 📊 상세 설계

### API Gateway 설계
```go
// kubectl → Contract 변환기
type ContractAPIGateway struct {
    suiRPCURL     string
    contractAddr  string
    privateKey    string  // 트랜잭션 서명용
}

func (g *ContractAPIGateway) handleKubectlRequest(w http.ResponseWriter, r *http.Request) {
    // 1. kubectl 요청 파싱
    kubectlReq := parseKubectlRequest(r)

    // 2. Move Contract 호출
    txResult := g.callMoveContract("execute_kubectl_command", kubectlReq)

    // 3. 요청 ID 추출
    requestID := extractRequestID(txResult)

    // 4. 응답 대기 (폴링 또는 웹소켓)
    response := g.waitForResponse(requestID)

    // 5. kubectl에 응답
    writeKubectlResponse(w, response)
}
```

### Move Contract 이벤트 설계
```move
// Contract가 발생시키는 이벤트들
struct K8sAPIRequest has copy, drop {
    request_id: String,
    method: String,      // GET, POST, PUT, DELETE
    path: String,        // /api/v1/pods
    namespace: String,   // default
    resource_type: String, // pods
    payload: vector<u8>, // YAML/JSON
    requester: address,
    seal_token_id: address,
    timestamp: u64,
    priority: u8,        // 1=low, 2=normal, 3=high
}

struct K8sValidationResult has copy, drop {
    request_id: String,
    approved: bool,
    permissions: vector<String>,
    stake_amount: u64,
    reason: String,
}
```

### Nautilus Event Listener 설계
```go
// Sui 이벤트 실시간 수신
type SuiEventSubscriber struct {
    wsConn       *websocket.Conn
    nautilus     *NautilusMaster
    lastCursor   string
}

func (s *SuiEventSubscriber) subscribeToContractEvents() {
    filter := EventFilter{
        Package: contractAddress,
        Module:  "k8s_gateway",
        EventType: "K8sAPIRequest",
    }

    for event := range s.receiveEvents(filter) {
        go s.processK8sRequest(event)
    }
}

func (s *SuiEventSubscriber) processK8sRequest(event K8sAPIRequest) {
    // 1. 이벤트 검증
    if !s.validateEvent(event) return

    // 2. K8s API 실행
    result := s.nautilus.executeK8sCommand(event)

    // 3. 결과를 Contract에 저장
    s.storeResponseToContract(event.RequestID, result)
}
```

## 🔄 완전한 플로우 예시

### kubectl get pods 시나리오

```
1. kubectl get pods
   ↓ HTTP GET /api/v1/pods

2. API Gateway
   ↓ parseKubectlRequest()
   ↓ Sui RPC: executeTransactionBlock
   ↓ execute_kubectl_command(seal_token, "GET", "/api/v1/pods", ...)

3. Move Contract
   ↓ is_valid_seal_token() ✅
   ↓ has_permission(seal_token, "pods:read") ✅
   ↓ event::emit(K8sAPIRequest {...})

4. Nautilus Event Listener
   ↓ receiveEvent(K8sAPIRequest)
   ↓ etcd.Get("/default/pods/*")
   ↓ generatePodList()
   ↓ store_k8s_response(request_id, 200, podList)

5. API Gateway
   ↓ waitForResponse(request_id)
   ↓ get_k8s_response(request_id) from Contract
   ↓ HTTP 200 + JSON response

6. kubectl
   ↓ Pod list displayed
```

### kubectl apply -f pod.yaml 시나리오

```
1. kubectl apply -f pod.yaml
   ↓ HTTP POST /api/v1/pods + YAML

2. API Gateway
   ↓ parseKubectlRequest() + YAML payload
   ↓ execute_kubectl_command(seal_token, "POST", "/api/v1/pods", yaml_bytes)

3. Move Contract
   ↓ is_valid_seal_token() ✅
   ↓ has_permission(seal_token, "pods:write") ✅
   ↓ check_stake_amount() >= MIN_STAKE ✅
   ↓ event::emit(K8sAPIRequest {...})

4. Nautilus Event Listener
   ↓ receiveEvent(K8sAPIRequest)
   ↓ parseYAML(event.payload)
   ↓ validatePodSpec()
   ↓ etcd.Put("/default/pods/nginx", podData)
   ↓ notifyControllerManager()
   ↓ schedulePodToWorker()
   ↓ store_k8s_response(request_id, 201, createdPod)

5. API Gateway
   ↓ waitForResponse(request_id)
   ↓ get_k8s_response(request_id)
   ↓ HTTP 201 + Created Pod JSON

6. kubectl
   ↓ "pod/nginx created"
```

## 🔒 보안 모델

### 1. Zero Trust to Nautilus
- Nautilus는 단순 실행자
- 모든 검증은 Contract에서
- Nautilus 손상되어도 무단 작업 불가

### 2. Blockchain Audit Trail
- 모든 kubectl 명령이 블록체인에 기록
- 변조 불가능한 감사 로그
- 실시간 모니터링 가능

### 3. Economic Incentives
- 잘못된 실행시 Nautilus 스테이킹 슬래시
- 올바른 실행시 보상 지급
- 경제적 인센티브로 정직한 행동 유도

## ⚡ 성능 최적화

### 1. 이벤트 필터링
```move
// 특정 Nautilus만 처리할 이벤트
struct K8sAPIRequest has copy, drop {
    nautilus_endpoint: address,  // 특정 Nautilus 지정
    priority: u8,               // 우선순위 기반 처리
}
```

### 2. 배치 처리
```move
// 여러 요청을 한 번에 처리
public entry fun execute_kubectl_batch(
    requests: vector<K8sRequest>,
    ctx: &mut TxContext
) {
    // 배치로 검증 후 한 번에 이벤트 발생
}
```

### 3. 캐싱 레이어
```go
// API Gateway에서 읽기 요청 캐싱
type ResponseCache struct {
    cache map[string]CachedResponse
    ttl   time.Duration
}
```

## 📈 확장성 설계

### 1. 다중 Nautilus
- 여러 Nautilus가 같은 Contract 구독
- 로드 밸런싱 및 고가용성
- 지역별 Nautilus 배포

### 2. 샤딩
- 네임스페이스별 Contract 분리
- 리소스 타입별 처리 분산

### 3. Layer 2 최적화
- 빈번한 읽기 요청은 L2에서 처리
- 중요한 쓰기만 L1에서 검증

이제 이 설계대로 구현해보겠습니다!