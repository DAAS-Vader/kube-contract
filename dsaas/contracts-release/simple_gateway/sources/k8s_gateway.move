/// K3s-DaaS Gateway Contract - 모든 K8s API 접근을 제어
module k3s_daas::k8s_gateway {
    use sui::object::{Self, UID};
    use sui::tx_context::{Self, TxContext};
    use sui::table::{Self, Table};
    use sui::event;
    use std::string::{Self, String};
    use std::vector;
    // Removed staking dependency for standalone deployment

    // Errors
    const E_INSUFFICIENT_STAKE: u64 = 1;
    const E_INVALID_SEAL_TOKEN: u64 = 2;
    const E_UNAUTHORIZED_ACTION: u64 = 3;
    const E_NAUTILUS_UNAVAILABLE: u64 = 4;

    // K8s API 요청 구조체
    public struct K8sAPIRequest has copy, drop {
        method: String,          // GET, POST, PUT, DELETE
        path: String,           // /api/v1/pods, /api/v1/services 등
        namespace: String,      // default, kube-system 등
        resource_type: String,  // Pod, Service, Deployment 등
        payload: vector<u8>,    // YAML/JSON payload
        sender: address,
        timestamp: u64,
    }

    // Nautilus TEE 엔드포인트 정보
    public struct NautilusEndpoint has key, store {
        id: UID,
        tee_url: String,        // https://nautilus-tee.example.com
        api_key: String,        // TEE 접근 키
        status: u8,             // 1=active, 2=maintenance, 3=offline
        last_heartbeat: u64,
    }

    // Seal 토큰 - kubectl 인증용 (Go 시스템과 호환)
    public struct SealToken has key, store {
        id: UID,
        wallet_address: String, // Go의 WalletAddress와 일치
        signature: String,      // Go의 Signature와 일치
        challenge: String,      // Go의 Challenge와 일치
        timestamp: u64,         // Go의 Timestamp와 일치
        stake_amount: u64,      // 추가: 스테이킹 양
        permissions: vector<String>, // 추가: ["pods:read", "services:write"] 등
        expires_at: u64,        // 추가: 만료 시간
        nautilus_endpoint: address, // 추가: 할당된 Nautilus TEE
    }

    // K8s 리소스 생성/수정 이벤트
    public struct K8sResourceEvent has copy, drop {
        resource_type: String,
        namespace: String,
        name: String,
        action: String,         // created, updated, deleted
        executor: address,
        nautilus_node: address,
        timestamp: u64,
    }

    // kubectl API 호출의 메인 진입점
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

        // 3. Nautilus TEE로 요청 라우팅
        route_to_nautilus(seal_token, method, path, namespace, resource_type, payload, ctx);
    }

    // 워커 노드용 Seal 토큰 생성 (simplified for standalone deployment)
    public entry fun create_worker_seal_token(
        node_id: String,
        stake_amount: u64,
        ctx: &mut TxContext
    ) {
        let staker = tx_context::sender(ctx);

        // Basic stake amount check
        assert!(stake_amount >= 1000000000, E_INSUFFICIENT_STAKE); // 1 SUI minimum

        // 스테이킹 양에 따른 권한 계산 (워커 노드용)
        let permissions = vector::empty<String>();
        vector::push_back(&mut permissions, string::utf8(b"nodes:write"));
        vector::push_back(&mut permissions, string::utf8(b"pods:write"));

        // Nautilus TEE 할당 (스테이킹 양 기반)
        let nautilus_endpoint = assign_nautilus_endpoint(stake_amount);

        let seal_token = SealToken {
            id: object::new(ctx),
            wallet_address: string::utf8(b"0x") + node_id, // Convert node_id to wallet format
            signature: generate_worker_signature(node_id, ctx),
            challenge: generate_challenge(ctx),
            timestamp: tx_context::epoch(ctx),
            stake_amount,
            permissions,
            expires_at: tx_context::epoch(ctx) + 100, // 100 에폭 후 만료
            nautilus_endpoint,
        };

        // 토큰을 사용자에게 전송
        sui::transfer::public_transfer(seal_token, staker);

        // Seal 토큰 생성 이벤트 (getter 함수 사용)
        event::emit(SealTokenCreated {
            token_id: object::id(&seal_token),
            owner: staker,
            node_id,
            nautilus_endpoint,
            expires_at: seal_token.expires_at,
        });
    }

    // 워커 노드가 Nautilus 정보를 조회
    public fun get_nautilus_info_for_worker(
        seal_token: &SealToken,
        ctx: &mut TxContext
    ): (String, String) {
        let caller = tx_context::sender(ctx);

        // Seal 토큰 지갑 주소 확인 (owner 개념을 wallet_address로 변경)
        // Note: 실제로는 caller address와 wallet_address 매핑 검증 필요

        // 토큰 만료 확인
        assert!(tx_context::epoch(ctx) < seal_token.expires_at, E_INVALID_SEAL_TOKEN);

        // Nautilus TEE 엔드포인트 정보 반환
        let nautilus_url = get_nautilus_url(seal_token.nautilus_endpoint);
        let worker_token = encode_seal_token_for_nautilus(seal_token);

        (nautilus_url, worker_token)
    }

    // Seal 토큰 생성 이벤트
    public struct SealTokenCreated has copy, drop {
        token_id: address,
        owner: address,
        node_id: String,
        nautilus_endpoint: address,
        expires_at: u64,
    }

    // 실제 Nautilus TEE로 요청 전달
    fun route_to_nautilus(
        seal_token: &SealToken,
        method: String,
        path: String,
        namespace: String,
        resource_type: String,
        payload: vector<u8>,
        ctx: &mut TxContext
    ) {
        // K8s API 요청 이벤트 발생 - Nautilus가 수신
        event::emit(K8sAPIRequest {
            method,
            path,
            namespace,
            resource_type,
            payload,
            sender: tx_context::sender(ctx),
            timestamp: tx_context::epoch_timestamp_ms(ctx),
        });

        // 리소스 생성/수정 시 추가 이벤트
        if (method == string::utf8(b"POST") || method == string::utf8(b"PUT")) {
            event::emit(K8sResourceEvent {
                resource_type,
                namespace,
                name: extract_resource_name(&payload), // payload에서 이름 추출
                action: if (method == string::utf8(b"POST")) {
                    string::utf8(b"created")
                } else {
                    string::utf8(b"updated")
                },
                executor: tx_context::sender(ctx),
                nautilus_node: seal_token.nautilus_endpoint,
                timestamp: tx_context::epoch_timestamp_ms(ctx),
            });
        }
    }

    // 스테이킹 양에 따른 권한 계산 (Go 시스템과 일치하도록 수정)
    fun calculate_permissions(stake_amount: u64, _requested: vector<String>): vector<String> {
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
            vector::push_back(&mut permissions, string::utf8(b"*:*")); // 모든 권한
        }

        permissions
    }

    // Nautilus TEE 할당 (스테이킹 기반 로드 밸런싱)
    fun assign_nautilus_endpoint(stake_amount: u64): address {
        // 높은 스테이킹 = 더 좋은 TEE 할당
        if (stake_amount >= 10000) {
            @0x111 // Premium TEE
        } else if (stake_amount >= 1000) {
            @0x222 // Standard TEE
        } else {
            @0x333 // Basic TEE
        }
    }

    // 권한 확인
    fun has_permission(seal_token: &SealToken, required: &String): bool {
        vector::contains(&seal_token.permissions, required) ||
        vector::contains(&seal_token.permissions, &string::utf8(b"*:*"))
    }

    // Seal 토큰 유효성 검증 (Go 시스템과 호환)
    fun is_valid_seal_token(seal_token: &SealToken, ctx: &mut TxContext): bool {
        // 만료 시간 확인
        if (seal_token.expires_at <= tx_context::epoch(ctx)) {
            return false
        };

        // 타임스탬프 유효성 확인 (너무 오래된 토큰 거부)
        let current_epoch = tx_context::epoch(ctx);
        if (current_epoch > seal_token.timestamp + 1000) { // 1000 에폭 후 무효
            return false
        };

        // 기본 구조 검증
        string::length(&seal_token.wallet_address) > 0 &&
        string::length(&seal_token.signature) > 0 &&
        string::length(&seal_token.challenge) > 0
    }

    // Helper functions
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

    // 누락된 helper 함수들 구현
    fun generate_worker_signature(node_id: String, ctx: &mut TxContext): String {
        // 워커 노드 서명 생성 (실제로는 암호학적 서명)
        let mut sig = string::utf8(b"sig_");
        string::append(&mut sig, node_id);
        string::append_utf8(&mut sig, b"_");
        string::append(&mut sig, generate_token_hash(ctx));
        sig
    }

    fun generate_challenge(ctx: &mut TxContext): String {
        // 챌린지 문자열 생성
        let mut challenge = string::utf8(b"challenge_");
        string::append(&mut challenge, generate_token_hash(ctx));
        challenge
    }

    fun get_nautilus_url(_endpoint: address): String {
        // Nautilus TEE URL 조회 (실제로는 레지스트리에서 조회)
        string::utf8(b"https://nautilus-tee.example.com")
    }

    fun encode_seal_token_for_nautilus(seal_token: &SealToken): String {
        // Nautilus용 토큰 인코딩
        let mut encoded = string::utf8(b"nautilus_token_");
        string::append(&mut encoded, seal_token.wallet_address);
        encoded
    }

    fun generate_token_hash(ctx: &mut TxContext): String {
        // Generate cryptographic hash using transaction context and timestamp
        let tx_hash = tx_context::digest(ctx);
        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        // Combine tx hash and timestamp for unique seal token
        let mut hash_bytes = vector::empty<u8>();
        vector::append(&mut hash_bytes, *tx_hash);

        // Convert timestamp to bytes manually
        let mut timestamp_bytes = vector::empty<u8>();
        let mut ts = timestamp;
        while (ts > 0) {
            vector::push_back(&mut timestamp_bytes, ((ts % 256) as u8));
            ts = ts / 256;
        };
        vector::append(&mut hash_bytes, timestamp_bytes);

        // Convert to hex string for seal token
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
            vector::push_back(&mut result, *vector::borrow(&hex_chars, ((byte >> 4) as u64)));
            vector::push_back(&mut result, *vector::borrow(&hex_chars, ((byte & 0x0f) as u64)));
            i = i + 1;
        };

        string::utf8(result)
    }

    fun extract_resource_name(_payload: &vector<u8>): String {
        // YAML/JSON에서 metadata.name 추출
        string::utf8(b"extracted_name") // 플레이스홀더
    }
}