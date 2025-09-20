/// Enhanced K3s-DaaS Gateway Contract - 완전한 응답 메커니즘 포함
module k3s_daas::k8s_gateway {
    use sui::object::{Self, UID};
    use sui::tx_context::{Self, TxContext};
    use sui::table::{Self, Table};
    use sui::event;
    use std::string::{Self, String};
    use std::vector;
    use k3s_daas::staking::{StakingPool, StakeRecord, get_stake_record_amount, get_stake_record_node_id, get_stake_record_staker, get_stake_record_type};

    // Errors
    const E_INSUFFICIENT_STAKE: u64 = 1;
    const E_INVALID_SEAL_TOKEN: u64 = 2;
    const E_UNAUTHORIZED_ACTION: u64 = 3;
    const E_NAUTILUS_UNAVAILABLE: u64 = 4;
    const E_REQUEST_EXPIRED: u64 = 5;
    const E_REQUEST_NOT_FOUND: u64 = 6;

    // K8s API 요청 구조체
    struct K8sAPIRequest has copy, drop {
        request_id: String,      // 고유 요청 ID
        method: String,          // GET, POST, PUT, DELETE
        path: String,           // /api/v1/pods, /api/v1/services 등
        namespace: String,      // default, kube-system 등
        resource_type: String,  // Pod, Service, Deployment 등
        payload: vector<u8>,    // YAML/JSON payload
        sender: address,
        timestamp: u64,
        nautilus_endpoint: address,
    }

    // K8s API 응답 구조체
    struct K8sAPIResponse has copy, drop {
        request_id: String,
        status_code: u16,
        body: vector<u8>,
        headers: Table<String, String>,
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

    // 글로벌 응답 레지스트리
    struct ResponseRegistry has key {
        id: UID,
        responses: Table<String, ID>, // request_id -> ResponseRecord ID
        admin: address,
    }

    // Seal 토큰 구조체 (기존과 동일)
    struct SealToken has key, store {
        id: UID,
        wallet_address: String,
        signature: String,
        challenge: String,
        timestamp: u64,
        stake_amount: u64,
        permissions: vector<String>,
        expires_at: u64,
        nautilus_endpoint: address,
    }

    // 권한 사전 승인 로그
    struct PermissionLog has key, store {
        id: UID,
        requester: address,
        method: String,
        resource_type: String,
        approved: bool,
        approved_at: u64,
        expires_at: u64,
    }

    // 이벤트들
    struct K8sRequestProcessed has copy, drop {
        request_id: String,
        requester: address,
        method: String,
        path: String,
        nautilus_endpoint: address,
        timestamp: u64,
    }

    struct K8sResponseStored has copy, drop {
        request_id: String,
        status_code: u16,
        body_length: u64,
        processed_at: u64,
    }

    struct PermissionPreApproved has copy, drop {
        requester: address,
        method: String,
        resource_type: String,
        expires_at: u64,
        log_id: address,
    }

    // 초기화 함수
    fun init(ctx: &mut TxContext) {
        let registry = ResponseRegistry {
            id: object::new(ctx),
            responses: table::new(ctx),
            admin: tx_context::sender(ctx),
        };
        transfer::share_object(registry);
    }

    // 메인 kubectl 명령 실행 함수 (비동기 처리)
    public entry fun execute_kubectl_command(
        seal_token: &SealToken,
        method: String,
        path: String,
        namespace: String,
        resource_type: String,
        payload: vector<u8>,
        ctx: &mut TxContext
    ) {
        // 1. Seal 토큰 검증
        assert!(is_valid_seal_token(seal_token, ctx), E_INVALID_SEAL_TOKEN);

        // 2. 권한 확인
        let required_permission = build_permission_string(&method, &resource_type);
        assert!(has_permission(seal_token, &required_permission), E_UNAUTHORIZED_ACTION);

        // 3. 고유 요청 ID 생성
        let request_id = generate_request_id(ctx);

        // 4. Nautilus TEE로 요청 라우팅 (이벤트 발생)
        event::emit(K8sAPIRequest {
            request_id,
            method,
            path,
            namespace,
            resource_type,
            payload,
            sender: tx_context::sender(ctx),
            timestamp: tx_context::epoch_timestamp_ms(ctx),
            nautilus_endpoint: seal_token.nautilus_endpoint,
        });

        // 5. 처리 완료 이벤트
        event::emit(K8sRequestProcessed {
            request_id,
            requester: tx_context::sender(ctx),
            method,
            path,
            nautilus_endpoint: seal_token.nautilus_endpoint,
            timestamp: tx_context::epoch_timestamp_ms(ctx),
        });
    }

    // Nautilus가 응답을 저장하는 함수
    public entry fun store_k8s_response(
        request_id: String,
        status_code: u16,
        body: vector<u8>,
        registry: &mut ResponseRegistry,
        ctx: &mut TxContext
    ) {
        let current_time = tx_context::epoch_timestamp_ms(ctx);

        // 응답 레코드 생성
        let response = ResponseRecord {
            id: object::new(ctx),
            request_id,
            status_code,
            body,
            processed_at: current_time,
            expires_at: current_time + 300000, // 5분 후 만료
            requester: tx_context::sender(ctx),
        };

        let response_id = object::id(&response);

        // 레지스트리에 등록
        table::add(&mut registry.responses, request_id, response_id);

        // 응답을 공유 객체로 생성
        transfer::share_object(response);

        // 응답 저장 이벤트
        event::emit(K8sResponseStored {
            request_id,
            status_code,
            body_length: vector::length(&body),
            processed_at: current_time,
        });
    }

    // kubectl 클라이언트가 응답을 조회하는 함수
    public fun get_k8s_response_status(
        request_id: String,
        registry: &ResponseRegistry
    ): (bool, u16) {
        if (table::contains(&registry.responses, request_id)) {
            // 응답이 존재함
            (true, 200)
        } else {
            // 응답이 아직 없음
            (false, 0)
        }
    }

    // 권한 사전 승인 (성능 최적화용)
    public entry fun pre_approve_permissions(
        seal_token: &SealToken,
        methods: vector<String>,
        resource_types: vector<String>,
        ctx: &mut TxContext
    ) {
        // Seal 토큰 검증
        assert!(is_valid_seal_token(seal_token, ctx), E_INVALID_SEAL_TOKEN);

        let current_time = tx_context::epoch_timestamp_ms(ctx);
        let requester = tx_context::sender(ctx);

        let i = 0;
        while (i < vector::length(&methods)) {
            let method = *vector::borrow(&methods, i);
            let j = 0;
            while (j < vector::length(&resource_types)) {
                let resource_type = *vector::borrow(&resource_types, j);

                // 권한 확인
                let required_permission = build_permission_string(&method, &resource_type);
                if (has_permission(seal_token, &required_permission)) {
                    // 권한 로그 생성
                    let log = PermissionLog {
                        id: object::new(ctx),
                        requester,
                        method,
                        resource_type,
                        approved: true,
                        approved_at: current_time,
                        expires_at: current_time + 1800000, // 30분 유효
                    };

                    let log_id = object::id(&log);
                    transfer::share_object(log);

                    // 사전 승인 이벤트
                    event::emit(PermissionPreApproved {
                        requester,
                        method,
                        resource_type,
                        expires_at: log.expires_at,
                        log_id,
                    });
                };

                j = j + 1;
            };
            i = i + 1;
        };
    }

    // 빠른 권한 확인 (캐시용)
    public fun check_permission_quick(
        seal_token: &SealToken,
        method: String,
        resource_type: String,
        ctx: &TxContext
    ): bool {
        // 기본 Seal 토큰 검증
        if (!is_valid_seal_token(seal_token, ctx)) {
            return false
        };

        // 권한 확인
        let required_permission = build_permission_string(&method, &resource_type);
        has_permission(seal_token, &required_permission)
    }

    // 워커 노드용 Seal 토큰 생성 (기존과 동일하지만 개선)
    public entry fun create_worker_seal_token(
        stake_record: &StakeRecord,
        ctx: &mut TxContext
    ) {
        let staker = tx_context::sender(ctx);

        // 스테이킹 레코드 검증
        assert!(get_stake_record_staker(stake_record) == staker, E_UNAUTHORIZED_ACTION);
        assert!(get_stake_record_type(stake_record) == string::utf8(b"node"), E_UNAUTHORIZED_ACTION);

        // 워커 노드 권한 설정
        let permissions = vector::empty<String>();
        vector::push_back(&mut permissions, string::utf8(b"nodes:write"));
        vector::push_back(&mut permissions, string::utf8(b"pods:write"));
        vector::push_back(&mut permissions, string::utf8(b"services:read"));

        // Nautilus TEE 할당
        let stake_amount = get_stake_record_amount(stake_record);
        let node_id = get_stake_record_node_id(stake_record);
        let nautilus_endpoint = assign_nautilus_endpoint(stake_amount);

        let seal_token = SealToken {
            id: object::new(ctx),
            wallet_address: string::utf8(b"0x") + node_id,
            signature: generate_worker_signature(node_id, ctx),
            challenge: generate_challenge(ctx),
            timestamp: tx_context::epoch(ctx),
            stake_amount,
            permissions,
            expires_at: tx_context::epoch(ctx) + 100,
            nautilus_endpoint,
        };

        // 토큰을 사용자에게 전송
        transfer::public_transfer(seal_token, staker);
    }

    // 유틸리티 함수들

    // 고유 요청 ID 생성
    fun generate_request_id(ctx: &mut TxContext): String {
        let tx_hash = tx_context::digest(ctx);
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        let mut id_bytes = vector::empty<u8>();
        vector::append(&mut id_bytes, b"req_");

        // 트랜잭션 해시의 처음 8바이트 사용
        let i = 0;
        while (i < 8 && i < vector::length(tx_hash)) {
            let byte = *vector::borrow(tx_hash, i);
            vector::push_back(&mut id_bytes, byte);
            i = i + 1;
        };

        // 타임스탬프 추가
        vector::append(&mut id_bytes, b"_");
        let ts_str = u64_to_string(timestamp % 1000000); // 마지막 6자리
        vector::append(&mut id_bytes, string::bytes(&ts_str));

        string::utf8(id_bytes)
    }

    // 권한 문자열 생성
    fun build_permission_string(method: &String, resource: &String): String {
        let action = if (*method == string::utf8(b"GET")) {
            string::utf8(b"read")
        } else {
            string::utf8(b"write")
        };

        let mut perm = *resource;
        string::append_utf8(&mut perm, b":");
        string::append(&mut perm, action);
        perm
    }

    // 권한 확인
    fun has_permission(seal_token: &SealToken, required: &String): bool {
        vector::contains(&seal_token.permissions, required) ||
        vector::contains(&seal_token.permissions, &string::utf8(b"*:*"))
    }

    // Seal 토큰 유효성 검증
    fun is_valid_seal_token(seal_token: &SealToken, ctx: &TxContext): bool {
        let current_epoch = tx_context::epoch(ctx);

        // 만료 시간 확인
        if (seal_token.expires_at <= current_epoch) {
            return false
        };

        // 타임스탬프 유효성 확인
        if (current_epoch > seal_token.timestamp + 1000) {
            return false
        };

        // 기본 구조 검증
        string::length(&seal_token.wallet_address) > 0 &&
        string::length(&seal_token.signature) > 0 &&
        string::length(&seal_token.challenge) > 0
    }

    // Nautilus TEE 할당
    fun assign_nautilus_endpoint(stake_amount: u64): address {
        if (stake_amount >= 10000000000) { // 10 SUI
            @0x111 // Premium TEE
        } else if (stake_amount >= 1000000000) { // 1 SUI
            @0x222 // Standard TEE
        } else {
            @0x333 // Basic TEE
        }
    }

    // 헬퍼 함수들
    fun generate_worker_signature(node_id: String, ctx: &mut TxContext): String {
        let mut sig = string::utf8(b"sig_");
        string::append(&mut sig, node_id);
        string::append_utf8(&mut sig, b"_");
        string::append(&mut sig, generate_token_hash(ctx));
        sig
    }

    fun generate_challenge(ctx: &mut TxContext): String {
        let mut challenge = string::utf8(b"challenge_");
        string::append(&mut challenge, generate_token_hash(ctx));
        challenge
    }

    fun generate_token_hash(ctx: &mut TxContext): String {
        let tx_hash = tx_context::digest(ctx);
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        let mut hash_bytes = vector::empty<u8>();
        vector::append(&mut hash_bytes, tx_hash);

        // Convert to hex string
        let hex_chars = b"0123456789abcdef";
        let mut result = vector::empty<u8>();
        vector::push_back(&mut result, 0x73); // 's'
        vector::push_back(&mut result, 0x65); // 'e'
        vector::push_back(&mut result, 0x61); // 'a'
        vector::push_back(&mut result, 0x6c); // 'l'
        vector::push_back(&mut result, 0x5f); // '_'

        let mut i = 0;
        while (i < 16 && i < vector::length(&hash_bytes)) {
            let byte = *vector::borrow(&hash_bytes, i);
            vector::push_back(&mut result, *vector::borrow(hex_chars, ((byte >> 4) as u64)));
            vector::push_back(&mut result, *vector::borrow(hex_chars, ((byte & 0x0f) as u64)));
            i = i + 1;
        };

        string::utf8(result)
    }

    // u64를 문자열로 변환
    fun u64_to_string(num: u64): String {
        if (num == 0) {
            return string::utf8(b"0")
        };

        let mut digits = vector::empty<u8>();
        let mut n = num;
        while (n > 0) {
            let digit = (n % 10) as u8;
            vector::push_back(&mut digits, digit + 48); // ASCII '0' = 48
            n = n / 10;
        };

        // 뒤집기
        vector::reverse(&mut digits);
        string::utf8(digits)
    }

    // 스테이킹 양에 따른 권한 계산
    fun calculate_permissions(stake_amount: u64): vector<String> {
        let mut permissions = vector::empty<String>();

        // 500,000,000 MIST (0.5 SUI): 기본 읽기 권한
        if (stake_amount >= 500000000) {
            vector::push_back(&mut permissions, string::utf8(b"pods:read"));
            vector::push_back(&mut permissions, string::utf8(b"services:read"));
            vector::push_back(&mut permissions, string::utf8(b"configmaps:read"));
        }

        // 1,000,000,000 MIST (1 SUI): 사용자/워커 노드 권한
        if (stake_amount >= 1000000000) {
            vector::push_back(&mut permissions, string::utf8(b"nodes:write"));
            vector::push_back(&mut permissions, string::utf8(b"pods:write"));
            vector::push_back(&mut permissions, string::utf8(b"services:write"));
        }

        // 5,000,000,000 MIST (5 SUI): 운영자 권한
        if (stake_amount >= 5000000000) {
            vector::push_back(&mut permissions, string::utf8(b"deployments:write"));
            vector::push_back(&mut permissions, string::utf8(b"secrets:read"));
            vector::push_back(&mut permissions, string::utf8(b"namespaces:write"));
        }

        // 10,000,000,000 MIST (10 SUI): 관리자 권한
        if (stake_amount >= 10000000000) {
            vector::push_back(&mut permissions, string::utf8(b"*:*"));
        }

        permissions
    }

    // 뷰 함수들 (응답 조회용)
    public fun get_response_status(
        request_id: String,
        registry: &ResponseRegistry
    ): bool {
        table::contains(&registry.responses, request_id)
    }

    public fun get_permission_cache_status(
        requester: address,
        method: String,
        resource_type: String
    ): bool {
        // TODO: 권한 캐시 구현
        true
    }
}