// K8s-DaaS Event Definitions for nautilus-control Integration
module k8s_daas::events {
    use sui::tx_context::TxContext;
    use sui::event;
    use std::string::String;

    // ==================== Event Structures ====================

    /// K8s API 요청 이벤트 - nautilus-control이 구독하여 kubectl 실행
    public struct K8sAPIRequestEvent has copy, drop {
        request_id: String,        // 고유 요청 ID
        method: String,            // GET, POST, PUT, DELETE, PATCH
        resource: String,          // pods, services, deployments, etc.
        namespace: String,         // default, kube-system, etc.
        name: String,             // 리소스 이름 (optional)
        payload: String,          // YAML/JSON 데이터 (POST/PUT용)
        seal_token: String,       // TEE 인증 토큰
        requester: address,       // 요청자 주소
        priority: u8,             // 1-10 우선순위
        timestamp: u64,           // 타임스탬프
    }

    /// 워커 노드 관리 이벤트 - 워커 노드 등록/해제
    public struct WorkerNodeEvent has copy, drop {
        action: String,           // register, unregister, heartbeat
        node_id: String,          // worker-node-001
        seal_token: String,       // TEE 토큰
        stake_amount: u64,        // 스테이킹 양
        worker_address: address,  // 워커 노드 주소
        timestamp: u64,           // 타임스탬프
    }

    /// K8s API 실행 결과 이벤트 - nautilus-control이 발생
    public struct K8sAPIResultEvent has copy, drop {
        request_id: String,       // 원본 요청 ID
        success: bool,            // 성공 여부
        output: String,           // kubectl 출력
        error: String,            // 에러 메시지
        execution_time_ms: u64,   // 실행 시간
        timestamp: u64,           // 타임스탬프
        executor: address,        // 실행한 노드 주소
    }

    /// 클러스터 상태 변경 이벤트
    public struct ClusterStateEvent has copy, drop {
        cluster_id: String,       // 클러스터 ID
        state: String,            // active, inactive, error
        node_count: u64,          // 활성 노드 수
        resource_usage: String,   // 리소스 사용량 JSON
        timestamp: u64,           // 타임스탬프
    }

    // ==================== Event Emission Functions ====================

    /// K8s API 요청 이벤트 발생
    public fun emit_k8s_api_request(
        request_id: String,
        method: String,
        resource: String,
        namespace: String,
        name: String,
        payload: String,
        seal_token: String,
        requester: address,
        priority: u8,
        ctx: &mut TxContext
    ) {
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        event::emit(K8sAPIRequestEvent {
            request_id,
            method,
            resource,
            namespace,
            name,
            payload,
            seal_token,
            requester,
            priority,
            timestamp,
        });
    }

    /// 워커 노드 이벤트 발생
    public fun emit_worker_node_event(
        action: String,
        node_id: String,
        seal_token: String,
        stake_amount: u64,
        worker_address: address,
        ctx: &mut TxContext
    ) {
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        event::emit(WorkerNodeEvent {
            action,
            node_id,
            seal_token,
            stake_amount,
            worker_address,
            timestamp,
        });
    }

    /// K8s API 실행 결과 이벤트 발생
    public fun emit_k8s_api_result(
        request_id: String,
        success: bool,
        output: String,
        error: String,
        execution_time_ms: u64,
        executor: address,
        ctx: &mut TxContext
    ) {
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        event::emit(K8sAPIResultEvent {
            request_id,
            success,
            output,
            error,
            execution_time_ms,
            timestamp,
            executor,
        });
    }

    /// 클러스터 상태 변경 이벤트 발생
    public fun emit_cluster_state(
        cluster_id: String,
        state: String,
        node_count: u64,
        resource_usage: String,
        ctx: &mut TxContext
    ) {
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        event::emit(ClusterStateEvent {
            cluster_id,
            state,
            node_count,
            resource_usage,
            timestamp,
        });
    }

    // ==================== Helper Functions ====================

    /// 새 요청 ID 생성 (타임스탬프 기반)
    public fun generate_request_id(ctx: &TxContext): String {
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        // 간단한 ID 생성 (타임스탬프 기반)
        let timestamp_str = std::string::utf8(b"req_");
        timestamp_str
    }

    /// 유효한 메소드인지 확인
    public fun is_valid_method(method: &String): bool {
        method == &std::string::utf8(b"GET") ||
        method == &std::string::utf8(b"POST") ||
        method == &std::string::utf8(b"PUT") ||
        method == &std::string::utf8(b"DELETE") ||
        method == &std::string::utf8(b"PATCH")
    }

    /// 유효한 리소스인지 확인
    public fun is_valid_resource(resource: &String): bool {
        resource == &std::string::utf8(b"pods") ||
        resource == &std::string::utf8(b"services") ||
        resource == &std::string::utf8(b"deployments") ||
        resource == &std::string::utf8(b"configmaps") ||
        resource == &std::string::utf8(b"secrets") ||
        resource == &std::string::utf8(b"namespaces") ||
        resource == &std::string::utf8(b"nodes")
    }

    /// 우선순위 유효성 검사
    public fun is_valid_priority(priority: u8): bool {
        priority >= 1 && priority <= 10
    }
}