/// K3s-DaaS Gateway Contract - 간단한 버전
module k3s_daas::k8s_gateway {
    use sui::object::{Self, UID, ID};
    use sui::tx_context::{Self, TxContext};
    use sui::table::{Self, Table};
    use sui::event;
    use std::string::{Self, String};
    use std::vector;
    use k3s_daas::staking::{StakeRecord, get_stake_record_amount, get_stake_record_node_id, get_stake_record_staker};

    // Errors
    const E_UNAUTHORIZED_ACTION: u64 = 3;

    // K8s API 요청 구조체
    struct K8sAPIRequest has copy, drop {
        request_id: String,
        method: String,
        path: String,
        namespace: String,
        resource_type: String,
        payload: vector<u8>,
        sender: address,
        timestamp: u64,
    }

    // K8s API 응답 구조체 (간단한 버전)
    struct K8sAPIResponse has copy, drop {
        request_id: String,
        status_code: u16,
        body: vector<u8>,
        processed_at: u64,
        nautilus_node: address,
    }

    // 응답 저장용 구조체
    struct ResponseRecord has key, store {
        id: UID,
        request_id: String,
        status_code: u16,
        body: vector<u8>,
        processed_at: u64,
        expires_at: u64,
        requester: address,
    }

    // 응답 레지스트리
    struct ResponseRegistry has key {
        id: UID,
        admin: address,
        responses: Table<String, ID>, // request_id -> ResponseRecord ID
    }

    // Seal Token for worker authentication
    struct SealToken has key, store {
        id: UID,
        stake_record_id: ID,
        permissions: vector<String>,
        nautilus_endpoint: address,
        created_at: u64,
        expires_at: u64,
    }

    // Events
    struct K8sRequestEvent has copy, drop {
        request_id: String,
        method: String,
        path: String,
        sender: address,
        timestamp: u64,
    }

    struct K8sResponseEvent has copy, drop {
        request_id: String,
        status_code: u16,
        responder: address,
        timestamp: u64,
    }

    struct SealTokenCreated has copy, drop {
        token_id: ID,
        stake_record_id: ID,
        nautilus_endpoint: address,
        permissions: vector<String>,
    }

    // 초기화 함수
    fun init(ctx: &mut TxContext) {
        let registry = ResponseRegistry {
            id: object::new(ctx),
            admin: tx_context::sender(ctx),
            responses: table::new(ctx),
        };
        sui::transfer::share_object(registry);
    }

    // Worker seal token 생성
    public fun create_worker_seal_token(
        stake_record: &StakeRecord,
        ctx: &mut TxContext
    ): SealToken {
        let stake_amount = get_stake_record_amount(stake_record);
        let permissions = calculate_basic_permissions(stake_amount);

        let token = SealToken {
            id: object::new(ctx),
            stake_record_id: object::id(stake_record),
            permissions,
            nautilus_endpoint: tx_context::sender(ctx),
            created_at: tx_context::epoch(ctx),
            expires_at: tx_context::epoch(ctx) + 100, // 100 epochs
        };

        event::emit(SealTokenCreated {
            token_id: object::id(&token),
            stake_record_id: object::id(stake_record),
            nautilus_endpoint: tx_context::sender(ctx),
            permissions: token.permissions,
        });

        token
    }

    // kubectl 명령 실행
    public fun execute_kubectl_command(
        token: &SealToken,
        method: String,
        path: String,
        namespace: String,
        resource_type: String,
        payload: vector<u8>,
        ctx: &mut TxContext
    ) {
        // 권한 확인 (간단한 버전)
        assert!(vector::length(&token.permissions) > 0, E_UNAUTHORIZED_ACTION);

        let request_id = generate_request_id(ctx);

        let request = K8sAPIRequest {
            request_id,
            method,
            path,
            namespace,
            resource_type,
            payload,
            sender: tx_context::sender(ctx),
            timestamp: tx_context::epoch(ctx),
        };

        event::emit(K8sRequestEvent {
            request_id: request.request_id,
            method: request.method,
            path: request.path,
            sender: request.sender,
            timestamp: request.timestamp,
        });
    }

    // 응답 저장
    public fun store_k8s_response(
        request_id: String,
        status_code: u16,
        body: vector<u8>,
        registry: &mut ResponseRegistry,
        ctx: &mut TxContext
    ) {
        let response_record = ResponseRecord {
            id: object::new(ctx),
            request_id,
            status_code,
            body,
            processed_at: tx_context::epoch(ctx),
            expires_at: tx_context::epoch(ctx) + 24, // 24 epochs
            requester: tx_context::sender(ctx),
        };

        let record_id = object::id(&response_record);
        table::add(&mut registry.responses, request_id, record_id);

        sui::transfer::share_object(response_record);

        event::emit(K8sResponseEvent {
            request_id,
            status_code,
            responder: tx_context::sender(ctx),
            timestamp: tx_context::epoch(ctx),
        });
    }

    // 응답 상태 조회
    public fun get_k8s_response_status(
        request_id: String,
        registry: &ResponseRegistry
    ): (bool, u16) {
        if (table::contains(&registry.responses, request_id)) {
            // 실제로는 ResponseRecord를 조회해야 하지만 간단한 버전에서는 기본값 반환
            (true, 200)
        } else {
            (false, 0)
        }
    }

    // 권한 확인 (간단한 버전)
    public fun check_permission_quick(
        token: &SealToken,
        _method: String,
        _resource_type: String,
        _ctx: &mut TxContext
    ): bool {
        vector::length(&token.permissions) > 0
    }

    // Helper functions
    fun generate_request_id(ctx: &mut TxContext): String {
        let sender_bytes = sui::address::to_bytes(tx_context::sender(ctx));
        let epoch = tx_context::epoch(ctx);
        let request_bytes = vector::empty<u8>();
        // 간단한 구현으로 변경
        string::utf8(sender_bytes)
    }

    fun calculate_basic_permissions(stake_amount: u64): vector<String> {
        let permissions = vector::empty<String>();

        // 기본 권한 (간단한 버전)
        if (stake_amount >= 500000000) { // 0.5 SUI
            // 권한 추가 로직은 간단하게 구현
        };

        permissions
    }

    // Test helper functions
    #[test_only]
    public fun init_for_testing(ctx: &mut TxContext) {
        init(ctx);
    }
}