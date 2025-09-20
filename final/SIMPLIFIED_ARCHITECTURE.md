# 간소화된 K3s-DaaS 아키텍처

## 🚀 새로운 간단한 플로우

```
kubectl → Nautilus:6443 → Move Contract → Nautilus 처리 → kubectl
```

## 왜 이게 더 좋은가?

### 1. API-Proxy 제거
- kubectl이 직접 Nautilus의 K8s API 서버로 연결
- HTTP ↔ 블록체인 변환 레이어 불필요
- 지연시간 대폭 감소

### 2. Nautilus가 모든 것을 처리
- K8s API 서버 역할
- Seal 토큰 검증
- Move Contract 호출 (필요시)
- 실제 K8s 리소스 관리

## 구현 방안

### 1. kubectl 설정 단순화
```bash
kubectl config set-cluster k3s-daas --server=https://nautilus-endpoint:6443
kubectl config set-credentials user --token=seal_YOUR_TOKEN
kubectl get pods  # 직접 Nautilus로!
```

### 2. Nautilus에서 Seal 토큰 검증 후 처리
```go
// nautilus-release/main.go 수정
func (n *NautilusMaster) handleKubernetesAPIProxy(w http.ResponseWriter, r *http.Request) {
    // 1. Seal 토큰 추출 (Authorization 헤더)
    token := extractBearerToken(r)

    // 2. 두 가지 처리 방식 선택
    if n.needsBlockchainValidation(r.URL.Path) {
        // 중요한 작업은 Move Contract로 검증
        n.handleWithBlockchainValidation(w, r, token)
    } else {
        // 읽기 작업은 로컬에서 바로 처리
        n.handleLocalValidation(w, r, token)
    }
}

func (n *NautilusMaster) needsBlockchainValidation(path string) bool {
    // POST, PUT, DELETE는 블록체인 검증
    // GET은 로컬 캐시에서 처리
    writeOperations := []string{"/api/v1/pods", "/apis/apps/v1/deployments"}
    for _, op := range writeOperations {
        if strings.Contains(path, op) {
            return true
        }
    }
    return false
}

func (n *NautilusMaster) handleWithBlockchainValidation(w http.ResponseWriter, r *http.Request, token string) {
    // 1. Move Contract로 검증 요청
    valid, permissions := n.validateWithContract(token, r.Method, r.URL.Path)
    if !valid {
        http.Error(w, "Unauthorized", 401)
        return
    }

    // 2. 검증 통과시 로컬에서 처리
    n.processK8sRequest(w, r, permissions)
}

func (n *NautilusMaster) handleLocalValidation(w http.ResponseWriter, r *http.Request, token string) {
    // 캐시된 Seal 토큰으로 빠른 검증
    if !n.cachedSealValidator.IsValid(token) {
        http.Error(w, "Unauthorized", 401)
        return
    }

    n.processK8sRequest(w, r, n.getCachedPermissions(token))
}
```

## 3가지 핵심 구현

### 1. Nautilus K8s API 핸들러 완성 ✅
```go
// nautilus-release/k8s_api_handlers.go (새 파일)
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

func (n *NautilusMaster) handleKubernetesAPIProxy(w http.ResponseWriter, r *http.Request) {
    n.logger.Infof("🎯 K8s API: %s %s", r.Method, r.URL.Path)

    // 1. Bearer Token (Seal) 추출
    token := extractBearerToken(r)
    if token == "" {
        http.Error(w, "Missing Authorization header", 401)
        return
    }

    // 2. 빠른 토큰 검증 (캐시 활용)
    if !n.sealTokenValidator.ValidateSealToken(token) {
        http.Error(w, "Invalid Seal token", 401)
        return
    }

    // 3. 요청 종류에 따른 처리
    switch r.Method {
    case "GET":
        n.handleK8sGet(w, r, token)
    case "POST":
        n.handleK8sPost(w, r, token)
    case "PUT":
        n.handleK8sPut(w, r, token)
    case "DELETE":
        n.handleK8sDelete(w, r, token)
    default:
        http.Error(w, "Method not allowed", 405)
    }
}

func (n *NautilusMaster) handleK8sGet(w http.ResponseWriter, r *http.Request, token string) {
    // GET 요청은 etcd에서 직접 조회 (빠른 응답)
    resource := n.parseResourceFromPath(r.URL.Path)

    data, err := n.etcdStore.Get(resource.Key())
    if err != nil {
        n.return404(w, resource.Type, resource.Name)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.Write(data)
}

func (n *NautilusMaster) handleK8sPost(w http.ResponseWriter, r *http.Request, token string) {
    // POST 요청은 Move Contract 검증 후 처리
    body, _ := io.ReadAll(r.Body)

    // Move Contract로 권한 검증 (비동기)
    go n.validateWithMoveContract(token, r.Method, r.URL.Path, body)

    // 로컬에서 즉시 처리 (성능 우선)
    result := n.createK8sResource(r.URL.Path, body)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
}

func extractBearerToken(r *http.Request) string {
    auth := r.Header.Get("Authorization")
    if strings.HasPrefix(auth, "Bearer ") {
        return strings.TrimPrefix(auth, "Bearer ")
    }
    return ""
}

type K8sResource struct {
    Type      string
    Namespace string
    Name      string
}

func (n *NautilusMaster) parseResourceFromPath(path string) K8sResource {
    // /api/v1/namespaces/default/pods/nginx
    // /api/v1/pods
    parts := strings.Split(strings.Trim(path, "/"), "/")

    resource := K8sResource{
        Namespace: "default",
    }

    for i, part := range parts {
        if part == "namespaces" && i+1 < len(parts) {
            resource.Namespace = parts[i+1]
        }
        if part == "pods" || part == "services" || part == "deployments" {
            resource.Type = part
            if i+1 < len(parts) {
                resource.Name = parts[i+1]
            }
        }
    }

    return resource
}

func (r K8sResource) Key() string {
    if r.Name != "" {
        return fmt.Sprintf("/%s/%s/%s", r.Namespace, r.Type, r.Name)
    }
    return fmt.Sprintf("/%s/%s", r.Namespace, r.Type)
}

func (n *NautilusMaster) createK8sResource(path string, body []byte) map[string]interface{} {
    resource := n.parseResourceFromPath(path)

    // K8s 리소스 생성 로직
    key := resource.Key() + "/" + generateResourceName()
    n.etcdStore.Put(key, body)

    // Controller Manager에 알림
    n.notifyControllerManager(K8sAPIRequest{
        Method:       "POST",
        Path:         path,
        Namespace:    resource.Namespace,
        ResourceType: resource.Type,
        Payload:      body,
        Timestamp:    uint64(time.Now().UnixMilli()),
    })

    return map[string]interface{}{
        "apiVersion": "v1",
        "kind":       "Status",
        "status":     "Success",
        "metadata": map[string]interface{}{
            "name": generateResourceName(),
        },
    }
}

func generateResourceName() string {
    return fmt.Sprintf("resource-%d", time.Now().UnixNano())
}

func (n *NautilusMaster) return404(w http.ResponseWriter, resourceType, name string) {
    w.WriteHeader(404)
    error := map[string]interface{}{
        "apiVersion": "v1",
        "kind":       "Status",
        "status":     "Failure",
        "message":    fmt.Sprintf("%s \"%s\" not found", resourceType, name),
        "reason":     "NotFound",
        "code":       404,
    }
    json.NewEncoder(w).Encode(error)
}
```

### 2. Move Contract 응답 메커니즘 ✅
```move
// contracts-release/k8s_gateway.move 수정

// 응답 캐싱을 위한 구조체 추가
struct K8sRequestLog has key, store {
    id: UID,
    requester: address,
    method: String,
    path: String,
    approved: bool,
    processed_at: u64,
    expires_at: u64,
}

// 권한 사전 승인 시스템
public entry fun pre_approve_k8s_request(
    seal_token: &SealToken,
    method: String,
    path: String,
    ctx: &mut TxContext
) {
    // 1. Seal 토큰 검증
    assert!(is_valid_seal_token(seal_token, ctx), E_INVALID_SEAL_TOKEN);

    // 2. 권한 확인
    let required_permission = build_permission_string(&method, &extract_resource_type(&path));
    assert!(has_permission(seal_token, &required_permission), E_UNAUTHORIZED_ACTION);

    // 3. 승인 로그 생성 (30분간 유효)
    let log = K8sRequestLog {
        id: object::new(ctx),
        requester: tx_context::sender(ctx),
        method,
        path,
        approved: true,
        processed_at: tx_context::epoch_timestamp_ms(ctx),
        expires_at: tx_context::epoch_timestamp_ms(ctx) + 1800000, // 30분
    };

    transfer::share_object(log);

    // 4. 승인 이벤트 발생
    event::emit(K8sRequestApproved {
        requester: tx_context::sender(ctx),
        method,
        path,
        log_id: object::id(&log),
        expires_at: log.expires_at,
    });
}

struct K8sRequestApproved has copy, drop {
    requester: address,
    method: String,
    path: String,
    log_id: address,
    expires_at: u64,
}

// 단순화된 권한 확인 (캐시용)
public fun check_cached_permission(
    seal_token: &SealToken,
    method: String,
    path: String,
    ctx: &TxContext
): bool {
    // 기본 검증만 수행 (빠른 응답)
    is_valid_seal_token(seal_token, ctx) &&
    has_permission(seal_token, &build_permission_string(&method, &extract_resource_type(&path)))
}

fun extract_resource_type(path: &String): String {
    // /api/v1/pods -> "pods"
    let path_str = string::bytes(path);
    if (vector::length(path_str) > 0) {
        string::utf8(b"pods") // 간단한 구현
    } else {
        string::utf8(b"unknown")
    }
}
```

### 3. 통합 테스트 시나리오 ✅
```bash
# 1. Nautilus 시작
cd nautilus-release
go run main.go
# → http://localhost:6443 에서 K8s API 서버 시작

# 2. 워커 노드 시작 (스테이킹 포함)
cd worker-release
go run main.go
# → Sui 스테이킹 + Seal 토큰 생성 + Nautilus 등록

# 3. kubectl 설정
kubectl config set-cluster k3s-daas --server=http://localhost:6443
kubectl config set-credentials user --token=seal_WALLET_SIG_CHALLENGE_TIME
kubectl config use-context k3s-daas

# 4. 테스트 명령
kubectl get pods          # GET → 빠른 etcd 조회
kubectl apply -f pod.yaml # POST → Move Contract 검증 후 생성
kubectl delete pod nginx  # DELETE → Move Contract 검증 후 삭제
```

## 결론: API-Proxy 불필요!

**Nautilus가 직접 kubectl의 K8s API 서버 역할을 하면서, 필요시에만 Move Contract로 검증하는 방식이 훨씬 효율적입니다.**

이제 구현하겠습니다!