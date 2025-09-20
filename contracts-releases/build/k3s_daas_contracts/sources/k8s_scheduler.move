// K8s-DaaS Scheduler - 실제 워커 풀 기반 스케줄링
module k8s_daas::k8s_scheduler {
    use sui::tx_context::{Self, TxContext};
    use sui::object::{Self, UID};
    use sui::table::{Self, Table};
    use sui::transfer;
    use sui::event;
    use std::string::{Self, String};
    use std::vector;
    use k8s_daas::worker_registry::{Self, WorkerRegistry};

    // ==================== Error Constants ====================

    const ENoAvailableWorkers: u64 = 1;
    const EInvalidRequest: u64 = 2;
    const EWorkerNotActive: u64 = 3;
    const EUnauthorizedRequest: u64 = 4;
    const EInvalidSealToken: u64 = 5;

    // ==================== Structs ====================

    /// K8s API 요청
    public struct K8sAPIRequest has store, drop {
        request_id: String,
        method: String,
        resource: String,
        namespace: String,
        name: String,
        payload: String,
        seal_token: String,
        requester: address,
        priority: u8,
        assigned_worker: String,
        status: String,          // "pending", "assigned", "executing", "completed", "failed"
        created_at: u64,
        assigned_at: u64,
        completed_at: u64,
    }

    /// 스케줄러 상태
    public struct K8sScheduler has key {
        id: UID,
        pending_requests: Table<String, K8sAPIRequest>, // request_id -> request
        active_requests: Table<String, K8sAPIRequest>,  // request_id -> request
        completed_requests: Table<String, K8sAPIRequest>, // request_id -> request
        worker_workloads: Table<String, u64>,           // worker_id -> active_request_count
        admin: address,
    }

    /// K8s API 요청 이벤트 (실제 워커 할당 포함)
    public struct K8sAPIRequestScheduledEvent has copy, drop {
        request_id: String,
        method: String,
        resource: String,
        namespace: String,
        name: String,
        payload: String,
        seal_token: String,
        requester: address,
        priority: u8,
        assigned_worker: String,
        timestamp: u64,
    }

    /// K8s API 실행 결과 이벤트
    public struct K8sAPIResultEvent has copy, drop {
        request_id: String,
        assigned_worker: String,
        success: bool,
        output: String,
        error: String,
        execution_time_ms: u64,
        timestamp: u64,
    }

    /// 워커 할당 이벤트
    public struct WorkerAssignedEvent has copy, drop {
        request_id: String,
        worker_id: String,
        workload_count: u64,
        timestamp: u64,
    }

    // ==================== Public Functions ====================

    /// 스케줄러 초기화
    fun init(ctx: &mut TxContext) {
        let scheduler = K8sScheduler {
            id: object::new(ctx),
            pending_requests: table::new(ctx),
            active_requests: table::new(ctx),
            completed_requests: table::new(ctx),
            worker_workloads: table::new(ctx),
            admin: tx_context::sender(ctx),
        };

        transfer::share_object(scheduler);
    }

    /// K8s API 요청 제출 및 워커 할당
    public fun submit_k8s_request(
        scheduler: &mut K8sScheduler,
        registry: &WorkerRegistry,
        request_id: String,
        method: String,
        resource: String,
        namespace: String,
        name: String,
        payload: String,
        seal_token: String,
        priority: u8,
        ctx: &mut TxContext
    ) {
        let sender = tx_context::sender(ctx);
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        // Seal Token 유효성 검사
        assert!(string::length(&seal_token) >= 32, EInvalidSealToken);

        // 요청 유효성 검사
        assert!(is_valid_method(&method), EInvalidRequest);
        assert!(is_valid_resource(&resource), EInvalidRequest);
        assert!(priority >= 1 && priority <= 10, EInvalidRequest);

        // 요청자가 소유한 활성 워커 선택
        let assigned_worker = select_owner_worker(scheduler, registry, sender, priority);
        assert!(assigned_worker != string::utf8(b""), ENoAvailableWorkers);

        // 워커가 실제로 활성 상태이고 요청자 소유인지 확인
        assert!(worker_registry::is_worker_active(registry, assigned_worker), EWorkerNotActive);
        assert!(worker_registry::is_worker_owner(registry, assigned_worker, sender), EUnauthorizedRequest);

        // 요청 객체 생성
        let request = K8sAPIRequest {
            request_id,
            method,
            resource,
            namespace,
            name,
            payload,
            seal_token,
            requester: sender,
            priority,
            assigned_worker,
            status: string::utf8(b"assigned"),
            created_at: timestamp,
            assigned_at: timestamp,
            completed_at: 0,
        };

        // 활성 요청 목록에 추가
        table::add(&mut scheduler.active_requests, request_id, request);

        // 워커 워크로드 업데이트
        if (!table::contains(&scheduler.worker_workloads, assigned_worker)) {
            table::add(&mut scheduler.worker_workloads, assigned_worker, 0);
        };
        let workload = table::borrow_mut(&mut scheduler.worker_workloads, assigned_worker);
        *workload = *workload + 1;

        // 이벤트 발생 - 마스터 노드가 이를 감지하여 실행
        event::emit(K8sAPIRequestScheduledEvent {
            request_id,
            method,
            resource,
            namespace,
            name,
            payload,
            seal_token,
            requester: sender,
            priority,
            assigned_worker,
            timestamp,
        });

        event::emit(WorkerAssignedEvent {
            request_id,
            worker_id: assigned_worker,
            workload_count: *workload,
            timestamp,
        });
    }

    /// API 실행 결과 기록 (마스터 노드에서 호출)
    public fun record_api_result(
        scheduler: &mut K8sScheduler,
        registry: &mut WorkerRegistry,
        request_id: String,
        success: bool,
        output: String,
        error: String,
        execution_time_ms: u64,
        ctx: &mut TxContext
    ) {
        assert!(table::contains(&scheduler.active_requests, request_id), EInvalidRequest);

        let request = table::remove(&mut scheduler.active_requests, request_id);
        let worker_id = request.assigned_worker;
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        // 요청 상태 업데이트
        let mut completed_request = request;
        completed_request.status = if (success) {
            string::utf8(b"completed")
        } else {
            string::utf8(b"failed")
        };
        completed_request.completed_at = timestamp;

        // 완료된 요청 목록으로 이동
        table::add(&mut scheduler.completed_requests, request_id, completed_request);

        // 워커 워크로드 감소
        if (table::contains(&scheduler.worker_workloads, worker_id)) {
            let workload = table::borrow_mut(&mut scheduler.worker_workloads, worker_id);
            if (*workload > 0) {
                *workload = *workload - 1;
            };
        };

        // 워커 통계 업데이트 (워커 레지스트리에)
        worker_registry::record_pod_service(registry, worker_id, success, ctx);

        // 결과 이벤트 발생
        event::emit(K8sAPIResultEvent {
            request_id,
            assigned_worker: worker_id,
            success,
            output,
            error,
            execution_time_ms,
            timestamp,
        });
    }

    // ==================== Internal Functions ====================

    /// 요청자 소유 워커 중 최적 워커 선택 (보안 강화)
    fun select_owner_worker(
        scheduler: &K8sScheduler,
        registry: &WorkerRegistry,
        owner: address,
        priority: u8
    ): String {
        // 해당 주소가 소유한 워커 목록 조회 - registry를 통해 안전하게 접근
        let owner_workers = worker_registry::get_owner_workers(registry, owner);
        if (vector::is_empty(owner_workers)) {
            return string::utf8(b"")
        };

        let mut best_worker = string::utf8(b"");
        let mut best_score = 0u64;
        let mut i = 0;

        while (i < vector::length(owner_workers)) {
            let worker_id = *vector::borrow(owner_workers, i);

            // 워커가 활성 상태인지 확인
            if (!worker_registry::is_worker_active(registry, worker_id)) {
                i = i + 1;
                continue
            };

            // 워커 현재 워크로드
            let workload = if (table::contains(&scheduler.worker_workloads, worker_id)) {
                *table::borrow(&scheduler.worker_workloads, worker_id)
            } else {
                0
            };

            // 워커 평판 점수 (간단화)
            let reputation = 100; // 실제로는 worker_registry에서 가져와야 함

            // 스코어 계산 (낮은 워크로드 + 높은 평판 = 높은 스코어)
            let score = if (workload == 0) {
                reputation * 10 // 유휴 워커 우선
            } else {
                reputation / (workload + 1)
            };

            // 우선순위 높은 요청은 더 좋은 워커 할당
            let adjusted_score = if (priority >= 8) {
                score * 2
            } else {
                score
            };

            if (best_worker == string::utf8(b"") || adjusted_score > best_score) {
                best_worker = worker_id;
                best_score = adjusted_score;
            };

            i = i + 1;
        };

        best_worker
    }

    /// 유효한 메소드인지 확인
    fun is_valid_method(method: &String): bool {
        method == &string::utf8(b"GET") ||
        method == &string::utf8(b"POST") ||
        method == &string::utf8(b"PUT") ||
        method == &string::utf8(b"DELETE") ||
        method == &string::utf8(b"PATCH")
    }

    /// 유효한 리소스인지 확인
    fun is_valid_resource(resource: &String): bool {
        resource == &string::utf8(b"pods") ||
        resource == &string::utf8(b"services") ||
        resource == &string::utf8(b"deployments") ||
        resource == &string::utf8(b"configmaps") ||
        resource == &string::utf8(b"secrets") ||
        resource == &string::utf8(b"namespaces") ||
        resource == &string::utf8(b"nodes")
    }

    // ==================== View Functions ====================

    /// 활성 요청 수 조회
    public fun get_active_request_count(scheduler: &K8sScheduler): u64 {
        table::length(&scheduler.active_requests)
    }

    /// 워커별 워크로드 조회
    public fun get_worker_workload(scheduler: &K8sScheduler, worker_id: String): u64 {
        if (table::contains(&scheduler.worker_workloads, worker_id)) {
            *table::borrow(&scheduler.worker_workloads, worker_id)
        } else {
            0
        }
    }

    /// 요청 상태 조회
    public fun get_request_status(scheduler: &K8sScheduler, request_id: String): String {
        if (table::contains(&scheduler.active_requests, request_id)) {
            let request = table::borrow(&scheduler.active_requests, request_id);
            request.status
        } else if (table::contains(&scheduler.completed_requests, request_id)) {
            let request = table::borrow(&scheduler.completed_requests, request_id);
            request.status
        } else {
            string::utf8(b"not_found")
        }
    }
}