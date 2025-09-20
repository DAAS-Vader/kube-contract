# K8s-DaaS 워커 노드 등록 상세 플로우

## 📋 개요

이 문서는 K8s-DaaS 시스템에서 워커 노드가 마스터 노드에 등록되는 전체 플로우를 단계별로 상세히 설명합니다. 실제 SUI 블록체인을 사용한 스테이킹 기반 워커 등록부터 K3s 클러스터 참여까지의 완전한 과정을 다룹니다.

## 🏗️ 시스템 아키텍처

```
[사용자] → [SUI 블록체인] → [스마트 컨트랙트] → [이벤트] → [마스터 노드] → [K3s 클러스터]
    ↓           ↓              ↓           ↓         ↓           ↓
 지갑 서명   트랜잭션 실행    워커 등록    실시간 감지   워커 풀 관리   클러스터 통합
```

## 📝 전체 플로우 단계

### 1단계: 사용자 워커 등록 요청 🚀

#### 1.1 사전 준비사항
- **SUI 지갑**: 최소 1 SUI 보유 (스테이킹용)
- **워커 노드 ID**: 고유한 워커 식별자
- **Seal Token**: 32자 이상의 보안 토큰

#### 1.2 스테이킹 및 등록 트랜잭션 실행

**실행 명령:**
```bash
sui client call \
  --package 0xafe077eecacec8519dd738b73640501b40f35ba5885220c5dfa240885695ab38 \
  --module worker_registry \
  --function stake_and_register_worker \
  --args \
    0x430457c27683bfbaced45a3ce22a9f44519536d9070868954d5a01a1ae0a20a8 \  # WorkerRegistry ID
    0xbe6b62be400dddde9957b8b9b41ea5ccea777aa391029d115b85cc7326e57ce9 \  # SUI Coin Object
    "my-worker-001" \                                                        # 워커 노드 ID
    "seal_token_my_secure_worker_12345678901234567890" \                    # Seal Token (32자+)
  --gas-budget 10000000
```

#### 1.3 스마트 컨트랙트 처리 과정

**worker_registry.move 내부 로직:**

```move
public fun stake_and_register_worker(
    registry: &mut WorkerRegistry,
    payment: Coin<SUI>,           // 스테이킹할 SUI 코인
    node_id: String,              // 워커 노드 ID
    seal_token: String,           // 보안 토큰
    ctx: &mut TxContext
) {
    let sender = tx_context::sender(ctx);
    let stake_amount = coin::value(&payment);

    // 1. 최소 스테이킹 검증 (1 SUI = 1,000,000,000 MIST)
    assert!(stake_amount >= MIN_STAKE_AMOUNT, EInsufficientStake);

    // 2. 워커 ID 중복 검사
    assert!(!table::contains(&registry.workers, node_id), EWorkerAlreadyExists);

    // 3. Seal Token 유효성 검사
    assert!(string::length(&seal_token) >= 32, EInvalidSealToken);

    // 4. 워커 노드 객체 생성
    let worker = WorkerNode {
        node_id,
        owner: sender,                    // 블록체인 지갑 주소
        stake_amount,
        status: string::utf8(b"pending"), // 초기 상태: 대기
        seal_token,
        registered_at: timestamp,
        last_heartbeat: timestamp,
        total_pods_served: 0,
        reputation_score: 100             // 기본 평판 점수
    };

    // 5. 레지스트리에 워커 등록
    table::add(&mut registry.workers, node_id, worker);

    // 6. 소유자별 워커 목록 업데이트
    if (!table::contains(&registry.owner_workers, sender)) {
        table::add(&registry.owner_workers, sender, vector::empty());
    };
    let owner_list = table::borrow_mut(&mut registry.owner_workers, sender);
    vector::push_back(owner_list, node_id);

    // 7. 스테이킹 자금 관리
    transfer::public_transfer(payment, @k8s_daas);

    // 8. StakeProof NFT 발급
    let stake_proof = StakeProof {
        id: object::new(ctx),
        node_id,
        stake_amount,
        staked_at: timestamp,
        owner: sender
    };
    transfer::transfer(stake_proof, sender);

    // 9. 이벤트 발생
    event::emit(WorkerRegisteredEvent {
        node_id,
        owner: sender,
        stake_amount,
        seal_token,
        timestamp
    });
}
```

### 2단계: 블록체인 이벤트 발생 📡

#### 2.1 발생하는 이벤트들

**WorkerRegisteredEvent:**
```json
{
  "node_id": "my-worker-001",
  "owner": "0x2c3dc44f39452ab44db72ffdf4acee24c7a9feeefd0de7ef058ff847f27834e4",
  "stake_amount": 1004607436,
  "seal_token": "seal_token_my_secure_worker_12345678901234567890",
  "timestamp": 1758316932655
}
```

**StakeDepositedEvent:**
```json
{
  "node_id": "my-worker-001",
  "owner": "0x2c3dc44f39452ab44db72ffdf4acee24c7a9feeefd0de7ef058ff847f27834e4",
  "amount": 1004607436,
  "timestamp": 1758316932655
}
```

#### 2.2 트랜잭션 결과물

1. **WorkerNode 객체**: 레지스트리에 저장
2. **StakeProof NFT**: 사용자 지갑으로 전송
3. **SUI 코인**: 컨트랙트 주소로 이동 (스테이킹)
4. **블록체인 이벤트**: 실시간 전파

### 3단계: 마스터 노드 이벤트 감지 🔍

#### 3.1 이벤트 모니터링 시스템

**nautilus-release/sui_integration.go:**
```go
func (s *SuiIntegration) pollEvents() {
    for {
        // SUI RPC로 이벤트 조회
        events, err := s.fetchLatestEvents()
        if err != nil {
            s.logger.Errorf("Failed to fetch events: %v", err)
            continue
        }

        // 이벤트 필터링 및 처리
        for _, event := range events {
            if s.isRelevantEvent(event) {
                s.processEvent(event)
            }
        }

        time.Sleep(5 * time.Second) // 5초마다 폴링
    }
}
```

#### 3.2 이벤트 필터링

**관련 이벤트만 선별:**
```go
func (s *SuiIntegration) isRelevantEvent(event *SuiContractEvent) bool {
    // 우리 컨트랙트 패키지에서 발생한 이벤트인지 확인
    if event.PackageID != s.contractPackageID {
        return false
    }

    // 처리할 이벤트 타입인지 확인
    relevantTypes := []string{
        "WorkerRegisteredEvent",
        "K8sAPIRequestScheduledEvent",
        "WorkerStatusChangedEvent"
    }

    for _, eventType := range relevantTypes {
        if strings.Contains(event.Type, eventType) {
            return true
        }
    }
    return false
}
```

### 4단계: 워커 등록 이벤트 처리 ⚙️

#### 4.1 WorkerRegisteredEvent 처리

**로그 출력:**
```
time="2025-09-20T18:23:47Z" level=info msg="✅ Parsed event: WorkerRegisteredEvent"
time="2025-09-20T18:23:47Z" level=info msg="👥 Processing worker registration event from contract"
```

**처리 로직:**
```go
func (s *SuiIntegration) handleWorkerRegisteredEvent(event *SuiContractEvent) {
    // 이벤트 데이터 파싱
    nodeID := event.ParsedJSON["node_id"].(string)
    sealToken := event.ParsedJSON["seal_token"].(string)
    owner := event.ParsedJSON["owner"].(string)
    stakeAmount := event.ParsedJSON["stake_amount"].(float64)

    s.logger.Infof("👥 Processing worker registration event from contract")

    // 워커 노드 객체 생성
    worker := &WorkerNode{
        NodeID:        nodeID,
        SealToken:     sealToken,
        Status:        "pending",        // 초기 상태
        StakeAmount:   uint64(stakeAmount),
        WorkerAddress: owner,
        RegisteredAt:  time.Now(),
        LastHeartbeat: time.Now(),
    }

    // 워커 풀에 추가
    if err := s.workerPool.AddWorker(worker); err != nil {
        if strings.Contains(err.Error(), "already exists") {
            s.logger.Warnf("⚠️ Worker %s already exists in pool", nodeID)
        } else {
            s.logger.Errorf("❌ Failed to add worker %s: %v", nodeID, err)
            return
        }
    }

    // K3s 조인 토큰 생성 및 할당
    s.assignJoinToken(nodeID)

    // 워커 상태를 활성화로 변경
    s.activateWorker(nodeID)
}
```

#### 4.2 워커 풀 관리

**worker_pool.go:**
```go
func (wp *WorkerPool) AddWorker(worker *WorkerNode) error {
    wp.mutex.Lock()
    defer wp.mutex.Unlock()

    // 중복 체크
    if _, exists := wp.workers[worker.NodeID]; exists {
        return fmt.Errorf("worker %s already exists", worker.NodeID)
    }

    // 워커 추가
    worker.RegisteredAt = time.Now()
    worker.LastHeartbeat = time.Now()
    wp.workers[worker.NodeID] = worker

    wp.logger.Infof("👥 Worker added to pool: %s (stake: %d)",
                    worker.NodeID, worker.StakeAmount)
    return nil
}
```

### 5단계: K3s 조인 토큰 생성 🔑

#### 5.1 조인 토큰 생성

**k3s_control_plane.go:**
```go
func (k3s *K3sManager) GetJoinToken() (string, error) {
    // K3s 서버에서 노드 토큰 읽기
    tokenBytes, err := ioutil.ReadFile("/var/lib/rancher/k3s/server/node-token")
    if err != nil {
        return "", fmt.Errorf("failed to read join token: %v", err)
    }

    token := strings.TrimSpace(string(tokenBytes))
    k3s.logger.Infof("🔑 Generated join token: %s...", token[:20])

    return token, nil
}
```

#### 5.2 워커에 토큰 할당

**로그 출력:**
```
time="2025-09-20T18:23:47Z" level=info msg="🔑 Join token set for worker my-worker-001: K10555fd72ba8ea470df..."
time="2025-09-20T18:23:47Z" level=info msg="🎟️ Join token assigned to worker my-worker-001"
```

**처리 로직:**
```go
func (s *SuiIntegration) assignJoinToken(nodeID string) {
    // K3s 마스터에서 조인 토큰 가져오기
    joinToken, err := s.k3sMgr.GetJoinToken()
    if err != nil {
        s.logger.Errorf("❌ Failed to get join token: %v", err)
        return
    }

    // 워커에 조인 토큰 설정
    if err := s.workerPool.SetWorkerJoinToken(nodeID, joinToken); err != nil {
        s.logger.Errorf("❌ Failed to set join token for %s: %v", nodeID, err)
        return
    }

    s.logger.Infof("🔑 Join token set for worker %s: %s...", nodeID, joinToken[:20])
    s.logger.Infof("🎟️ Join token assigned to worker %s", nodeID)
}
```

### 6단계: 워커 활성화 ✅

#### 6.1 워커 상태 변경

```go
func (wp *WorkerPool) UpdateWorkerStatus(nodeID, status string) error {
    wp.mutex.Lock()
    defer wp.mutex.Unlock()

    worker, exists := wp.workers[nodeID]
    if !exists {
        return fmt.Errorf("worker %s not found", nodeID)
    }

    oldStatus := worker.Status
    worker.Status = status
    worker.LastHeartbeat = time.Now()

    wp.logger.Infof("🔄 Worker %s status: %s → %s", nodeID, oldStatus, status)
    return nil
}
```

#### 6.2 최종 워커 상태

**워커 객체 최종 상태:**
```json
{
  "NodeID": "my-worker-001",
  "SealToken": "seal_token_my_secure_worker_12345678901234567890",
  "Status": "active",
  "StakeAmount": 1004607436,
  "JoinToken": "K10555fd72ba8ea470df8c1db5e88c28c4bc9e8c5a2f42423::server:abc123...",
  "WorkerAddress": "0x2c3dc44f39452ab44db72ffdf4acee24c7a9feeefd0de7ef058ff847f27834e4",
  "RegisteredAt": "2025-09-20T18:23:47Z",
  "LastHeartbeat": "2025-09-20T18:23:47Z"
}
```

## 🔐 인증 및 보안 시스템

### 인증 방식 분석

**현재 시스템의 인증:**
1. **블록체인 서명**: `tx_context::sender(ctx)`로 트랜잭션 서명자 확인
2. **워커 소유권 검증**: `worker_registry::is_worker_owner()`로 소유권 확인
3. **Seal Token**: 추가 보안 레이어
4. **StakeProof NFT**: 스테이킹 증명 (선택적 사용)

**중요: StakeProof NFT는 현재 K8s 요청 시 필수가 아님**

### K8s API 요청 시 인증 플로우

```move
public fun submit_k8s_request(...) {
    let sender = tx_context::sender(ctx);  // 🔑 블록체인 서명으로 신원 확인

    // 요청자가 소유한 워커만 선택
    let assigned_worker = select_owner_worker(scheduler, registry, sender, priority);

    // 이중 확인: 워커 소유권 재검증
    assert!(worker_registry::is_worker_owner(registry, assigned_worker, sender), EUnauthorizedRequest);
}
```

## 📊 워커 풀 관리 시스템

### 워커 상태 관리

**가능한 워커 상태:**
- `pending`: 등록됨, 아직 활성화 안됨
- `active`: 활성화됨, 요청 처리 가능
- `busy`: 요청 처리 중
- `offline`: 오프라인 (하트비트 없음)
- `slashed`: 패널티 상태

### 워커 선택 알고리즘

```move
fun select_owner_worker(
    scheduler: &K8sScheduler,
    registry: &WorkerRegistry,
    owner: address,
    priority: u8
): String {
    // 1. 해당 주소가 소유한 워커 목록 조회
    let owner_workers = worker_registry::get_owner_workers(registry, owner);

    // 2. 최적 워커 선택 로직
    let mut best_worker = string::utf8(b"");
    let mut best_score = 0u64;

    while (i < vector::length(owner_workers)) {
        let worker_id = *vector::borrow(owner_workers, i);

        // 활성 워커만 고려
        if (!worker_registry::is_worker_active(registry, worker_id)) {
            continue
        };

        // 워크로드 기반 점수 계산
        let workload = get_worker_workload(scheduler, worker_id);
        let reputation = 100; // 평판 점수

        let score = if (workload == 0) {
            reputation * 10  // 유휴 워커 우선
        } else {
            reputation / (workload + 1)
        };

        // 우선순위 높은 요청은 더 좋은 워커 할당
        let adjusted_score = if (priority >= 8) {
            score * 2
        } else {
            score
        };

        if (adjusted_score > best_score) {
            best_worker = worker_id;
            best_score = adjusted_score;
        }
    }

    best_worker
}
```

## 🚨 오류 처리 및 예외 상황

### 일반적인 오류 상황

1. **EInsufficientStake**: 스테이킹 금액 부족 (< 1 SUI)
2. **EWorkerAlreadyExists**: 워커 ID 중복
3. **EInvalidSealToken**: Seal Token 길이 부족 (< 32자)
4. **ENoAvailableWorkers**: 사용 가능한 워커 없음
5. **EUnauthorizedRequest**: 인증되지 않은 요청

### 복구 메커니즘

```go
// 워커 하트비트 모니터링
func (wp *WorkerPool) CheckHeartbeats() {
    timeout := 5 * time.Minute
    now := time.Now()

    for nodeID, worker := range wp.workers {
        if now.Sub(worker.LastHeartbeat) > timeout && worker.Status != "offline" {
            worker.Status = "offline"
            wp.logger.Warnf("💀 Worker %s marked offline (no heartbeat)", nodeID)
        }
    }
}
```

## 📈 모니터링 및 메트릭

### 주요 메트릭

1. **워커 풀 통계**: 총 워커 수, 활성 워커 수, 오프라인 워커 수
2. **스테이킹 통계**: 총 스테이킹 금액, 평균 스테이킹 금액
3. **요청 처리 통계**: 성공/실패 비율, 평균 응답 시간
4. **평판 시스템**: 워커별 평판 점수, 서비스 완료 횟수

### 로그 예시

```
2025-09-20T18:23:47Z INFO ✅ Parsed event: WorkerRegisteredEvent
2025-09-20T18:23:47Z INFO 👥 Processing worker registration event from contract
2025-09-20T18:23:47Z INFO 👥 Worker added to pool: my-worker-001 (stake: 1004607436)
2025-09-20T18:23:47Z INFO 🔑 Join token set for worker my-worker-001: K10555fd72ba8ea470df...
2025-09-20T18:23:47Z INFO 🎟️ Join token assigned to worker my-worker-001
2025-09-20T18:23:47Z INFO 🔄 Worker my-worker-001 status: pending → active
```

## 🎯 실제 사용 예시

### 워커 등록 완전한 예시

```bash
# 1. 워커 등록 (스테이킹 포함)
sui client call \
  --package 0xafe077eecacec8519dd738b73640501b40f35ba5885220c5dfa240885695ab38 \
  --module worker_registry \
  --function stake_and_register_worker \
  --args 0x430457c27683bfbaced45a3ce22a9f44519536d9070868954d5a01a1ae0a20a8 0xbe6b62be400dddde9957b8b9b41ea5ccea777aa391029d115b85cc7326e57ce9 "production-worker-01" "seal_token_production_worker_super_secure_123456789012" \
  --gas-budget 10000000

# 2. K8s API 요청 (워커 등록 후)
sui client call \
  --package 0xafe077eecacec8519dd738b73640501b40f35ba5885220c5dfa240885695ab38 \
  --module k8s_scheduler \
  --function submit_k8s_request \
  --args 0x78abc95c4ced8ac1be420786d0d4be2b319acf13a4eb26797500d7d4111bed06 0x430457c27683bfbaced45a3ce22a9f44519536d9070868954d5a01a1ae0a20a8 "get-pods-prod-001" "GET" "pods" "production" "" "" "seal_token_production_worker_super_secure_123456789012" 8 \
  --gas-budget 10000000
```

## 🔮 향후 개선 사항

### 제안되는 개선점

1. **StakeProof NFT 활용**: K8s 요청 시 StakeProof 소유 확인 추가
2. **동적 스테이킹**: 워커 성능에 따른 스테이킹 요구량 조정
3. **슬래싱 메커니즘**: 악성 행동 시 스테이킹 몰수
4. **워커 평판 시스템**: 성과 기반 평판 점수 관리
5. **자동 워커 스케일링**: 수요에 따른 워커 자동 추가/제거

---

이 문서는 K8s-DaaS 시스템의 워커 등록 플로우를 완전히 이해할 수 있도록 상세한 설명과 코드 예시를 제공합니다. 실제 프로덕션 환경에서 이 플로우를 따라 워커를 등록하고 관리할 수 있습니다.