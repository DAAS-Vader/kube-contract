# K3s-DaaS 완전 동작을 위한 필수 수정사항

## 현재 문제점

### 1. API-Proxy 블록체인 모드 미구현
**파일**: `api-proxy/main.go:237-247`
**문제**: Move Contract 호출이 TODO로 남아있고 직접 모드로 폴백

### 2. Nautilus K8s API 핸들러 누락
**파일**: `nautilus-release/main.go:577-578`
**문제**: `handleKubernetesAPIProxy` 함수가 선언되었지만 구현되지 않음

### 3. Move Contract 응답 처리 불완전
**파일**: `contracts-release/k8s_gateway.move:160-196`
**문제**: 이벤트만 발생시키고 응답 메커니즘 없음

## 완전 동작을 위한 수정방안

### 1. API-Proxy 블록체인 모드 완성

```go
// api-proxy/main.go 수정
func (p *APIProxy) handleBlockchainMode(w http.ResponseWriter, req *KubectlRequest) {
    p.logger.Info("⛓️ Blockchain mode: Calling Move Contract...")

    // 1. Move Contract 호출 구성
    contractCall := map[string]interface{}{
        "jsonrpc": "2.0",
        "id":      1,
        "method":  "sui_executeTransactionBlock",
        "params": []interface{}{
            map[string]interface{}{
                "txBytes": p.buildExecuteKubectlTx(req),
            },
            []string{p.privateKey},
            map[string]interface{}{
                "requestType": "WaitForLocalExecution",
                "options": map[string]bool{
                    "showEvents": true,
                },
            },
        },
    }

    // 2. Sui RPC 호출
    resp, err := http.Post(p.suiRPCURL, "application/json",
        bytes.NewBuffer(jsonBytes))

    // 3. 이벤트에서 Nautilus 응답 대기
    nautilusResponse := p.waitForNautilusResponse(resp)

    // 4. kubectl에 응답 반환
    p.forwardResponse(w, nautilusResponse)
}

func (p *APIProxy) buildExecuteKubectlTx(req *KubectlRequest) string {
    // Move Contract execute_kubectl_command 호출 트랜잭션 구성
    moveCall := map[string]interface{}{
        "packageObjectId": p.contractAddress,
        "module":          "k8s_gateway",
        "function":        "execute_kubectl_command",
        "arguments": []interface{}{
            req.SealToken.ObjectID,  // seal_token 참조
            req.Method,
            req.Path,
            p.extractNamespace(req.Path),
            p.extractResourceType(req.Path),
            req.Body,
        },
    }
    // ... 트랜잭션 직렬화
}
```

### 2. Nautilus K8s API 핸들러 구현

```go
// nautilus-release/main.go 추가
func (n *NautilusMaster) handleKubernetesAPIProxy(w http.ResponseWriter, r *http.Request) {
    n.logger.Infof("K8s API: %s %s", r.Method, r.URL.Path)

    // 1. Seal 토큰 검증
    sealToken := r.Header.Get("X-Seal-Token")
    if !n.sealTokenValidator.ValidateSealToken(sealToken) {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    // 2. K8s API 요청 파싱
    body, _ := io.ReadAll(r.Body)
    k8sReq := K8sAPIRequest{
        Method:       r.Method,
        Path:         r.URL.Path,
        Namespace:    n.extractNamespace(r.URL.Path),
        ResourceType: n.extractResourceType(r.URL.Path),
        Payload:      body,
        Sender:       n.getSenderFromSealToken(sealToken),
        Timestamp:    uint64(time.Now().UnixMilli()),
    }

    // 3. 실제 K8s API 처리
    response, err := n.ProcessK8sRequest(k8sReq)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // 4. 응답 반환
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func (n *NautilusMaster) extractNamespace(path string) string {
    // /api/v1/namespaces/default/pods -> "default"
    parts := strings.Split(path, "/")
    for i, part := range parts {
        if part == "namespaces" && i+1 < len(parts) {
            return parts[i+1]
        }
    }
    return "default"
}

func (n *NautilusMaster) extractResourceType(path string) string {
    // /api/v1/pods -> "pods"
    // /apis/apps/v1/deployments -> "deployments"
    parts := strings.Split(path, "/")
    return parts[len(parts)-1]
}
```

### 3. Move Contract 응답 메커니즘 추가

```move
// contracts-release/k8s_gateway.move 수정

// 응답 저장용 구조체 추가
struct K8sResponse has key, store {
    id: UID,
    request_id: String,
    status_code: u16,
    body: vector<u8>,
    headers: Table<String, String>,
    timestamp: u64,
}

// 응답 저장용 레지스트리
struct ResponseRegistry has key {
    id: UID,
    responses: Table<String, ID>, // request_id -> Response ID
}

// kubectl 명령 실행 후 응답 처리
public entry fun execute_kubectl_command(
    seal_token: &SealToken,
    method: String,
    path: String,
    namespace: String,
    resource_type: String,
    payload: vector<u8>,
    ctx: &mut TxContext
) {
    // 기존 검증 로직...

    // 요청 ID 생성
    let request_id = generate_request_id(ctx);

    // Nautilus TEE로 요청 라우팅
    route_to_nautilus(seal_token, method, path, namespace,
                     resource_type, payload, request_id, ctx);
}

// Nautilus가 응답을 저장하는 함수
public entry fun store_k8s_response(
    request_id: String,
    status_code: u16,
    body: vector<u8>,
    registry: &mut ResponseRegistry,
    ctx: &mut TxContext
) {
    let response = K8sResponse {
        id: object::new(ctx),
        request_id,
        status_code,
        body,
        headers: table::new(ctx),
        timestamp: tx_context::epoch_timestamp_ms(ctx),
    };

    let response_id = object::id(&response);
    table::add(&mut registry.responses, request_id, response_id);
    transfer::share_object(response);
}

// API-Proxy가 응답을 조회하는 함수
public fun get_k8s_response(
    request_id: String,
    registry: &ResponseRegistry
): Option<&K8sResponse> {
    if (table::contains(&registry.responses, request_id)) {
        let response_id = table::borrow(&registry.responses, request_id);
        // 실제로는 dynamic field로 응답 객체 조회
        option::some(response_ref)
    } else {
        option::none()
    }
}
```

### 4. 완전한 플로우 구현

```bash
# 1. kubectl 명령 실행
kubectl get pods

# 2. API-Proxy 처리 플로우
API-Proxy → Move Contract → Nautilus TEE → K8s API → 응답 저장 → API-Proxy 조회 → kubectl

# 3. 구체적인 구현 단계
[kubectl] ---> [API-Proxy:8080]
                     ↓ sui_executeTransactionBlock
[Move Contract] <----┘
     ↓ K8sAPIRequest 이벤트
[Nautilus TEE:9443]
     ↓ 실제 K8s API 처리
[etcd + Controller Manager]
     ↓ store_k8s_response
[Move Contract] <----┘
     ↓ 응답 조회
[API-Proxy] --------┘
     ↓ HTTP 응답
[kubectl] <---------┘
```

## 수정 우선순위

### 1순위 (즉시 필요)
- [ ] Nautilus `handleKubernetesAPIProxy` 구현
- [ ] Move Contract `init` 함수 추가
- [ ] Move Contract 모듈 의존성 수정

### 2순위 (핵심 기능)
- [ ] API-Proxy 블록체인 모드 완성
- [ ] Move Contract 응답 메커니즘 구현
- [ ] Sui 이벤트 수신 로직 완성

### 3순위 (최적화)
- [ ] 비동기 응답 처리 최적화
- [ ] 에러 처리 강화
- [ ] 성능 모니터링 추가

## 예상 구현 시간
- **1순위**: 4-6시간
- **2순위**: 8-12시간
- **3순위**: 4-6시간
- **총 예상 시간**: 16-24시간

## 완성 후 예상 성능
- **처리량**: 초당 20-50 kubectl 명령
- **지연시간**: 5-10초 (블록체인 확정 포함)
- **가용성**: 99% (Sui 네트워크 의존)

이 수정사항들을 모두 구현하면 **완전히 동작하는** K3s-DaaS 시스템이 될 것입니다.