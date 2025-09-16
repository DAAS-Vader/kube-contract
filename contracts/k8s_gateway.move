/// K3s-DaaS Gateway Contract - 모든 K8s API 접근을 제어
module k3s_daas::k8s_gateway {
    use sui::object::{Self, UID};
    use sui::tx_context::{Self, TxContext};
    use sui::table::{Self, Table};
    use sui::event;
    use std::string::{Self, String};
    use std::vector;

    // Errors
    const E_INSUFFICIENT_STAKE: u64 = 1;
    const E_INVALID_SEAL_TOKEN: u64 = 2;
    const E_UNAUTHORIZED_ACTION: u64 = 3;
    const E_NAUTILUS_UNAVAILABLE: u64 = 4;

    // K8s API 요청 구조체
    struct K8sAPIRequest has copy, drop {
        method: String,          // GET, POST, PUT, DELETE
        path: String,           // /api/v1/pods, /api/v1/services 등
        namespace: String,      // default, kube-system 등
        resource_type: String,  // Pod, Service, Deployment 등
        payload: vector<u8>,    // YAML/JSON payload
        sender: address,
        timestamp: u64,
    }

    // Nautilus TEE 엔드포인트 정보
    struct NautilusEndpoint has key, store {
        id: UID,
        tee_url: String,        // https://nautilus-tee.example.com
        api_key: String,        // TEE 접근 키
        status: u8,             // 1=active, 2=maintenance, 3=offline
        last_heartbeat: u64,
    }

    // Seal 토큰 - kubectl 인증용
    struct SealToken has key, store {
        id: UID,
        token_hash: String,     // SHA256 hash of token
        owner: address,
        stake_amount: u64,
        permissions: vector<String>, // ["pods:read", "services:write"] 등
        expires_at: u64,
        nautilus_endpoint: address, // 할당된 Nautilus TEE
    }

    // K8s 리소스 생성/수정 이벤트
    struct K8sResourceEvent has copy, drop {
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

    // 워커 노드용 Seal 토큰 생성 (스테이킹 완료 후 자동 생성)
    public entry fun create_worker_seal_token(
        stake_record: &StakeRecord,  // from staking.move
        ctx: &mut TxContext
    ) {
        let staker = tx_context::sender(ctx);

        // 스테이킹 레코드 소유자 확인
        assert!(stake_record.staker == staker, E_UNAUTHORIZED_ACTION);

        // 워커 노드 스테이킹 확인
        assert!(stake_record.stake_type == string::utf8(b"node"), E_UNAUTHORIZED_ACTION);

        // 스테이킹 양에 따른 권한 계산 (워커 노드용)
        let permissions = vector::empty<String>();
        vector::push_back(&mut permissions, string::utf8(b"nodes:write"));
        vector::push_back(&mut permissions, string::utf8(b"pods:write"));

        // Nautilus TEE 할당 (스테이킹 양 기반)
        let nautilus_endpoint = assign_nautilus_endpoint(stake_record.amount);

        let seal_token = SealToken {
            id: object::new(ctx),
            token_hash: generate_worker_token_hash(stake_record.node_id, ctx),
            owner: staker,
            stake_amount: stake_record.amount,
            permissions,
            expires_at: tx_context::epoch(ctx) + 100, // 100 에폭 후 만료
            nautilus_endpoint,
        };

        // 토큰을 사용자에게 전송
        sui::transfer::public_transfer(seal_token, staker);

        // Seal 토큰 생성 이벤트
        event::emit(SealTokenCreated {
            token_id: object::id(&seal_token),
            owner: staker,
            node_id: stake_record.node_id,
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

        // Seal 토큰 소유자 확인
        assert!(seal_token.owner == caller, E_UNAUTHORIZED_ACTION);

        // 토큰 만료 확인
        assert!(tx_context::epoch(ctx) < seal_token.expires_at, E_INVALID_SEAL_TOKEN);

        // Nautilus TEE 엔드포인트 정보 반환
        let nautilus_url = get_nautilus_url(seal_token.nautilus_endpoint);
        let worker_token = encode_seal_token_for_nautilus(seal_token);

        (nautilus_url, worker_token)
    }

    // Seal 토큰 생성 이벤트
    struct SealTokenCreated has copy, drop {
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

    // 스테이킹 양에 따른 권한 계산
    fun calculate_permissions(stake_amount: u64, requested: vector<String>): vector<String> {
        let mut permissions = vector::empty<String>();

        // 100 MIST: 기본 읽기 권한
        if (stake_amount >= 100) {
            vector::push_back(&mut permissions, string::utf8(b"pods:read"));
            vector::push_back(&mut permissions, string::utf8(b"services:read"));
        }

        // 1000 MIST: 워커 노드 권한
        if (stake_amount >= 1000) {
            vector::push_back(&mut permissions, string::utf8(b"nodes:write"));
            vector::push_back(&mut permissions, string::utf8(b"pods:write"));
        }

        // 10000 MIST: 관리자 권한
        if (stake_amount >= 10000) {
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

    // Seal 토큰 유효성 검증
    fun is_valid_seal_token(seal_token: &SealToken, ctx: &mut TxContext): bool {
        seal_token.expires_at > tx_context::epoch(ctx) &&
        seal_token.owner == tx_context::sender(ctx)
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

    fun generate_token_hash(ctx: &mut TxContext): String {
        // 실제로는 cryptographic hash 사용
        string::utf8(b"seal_token_hash_placeholder")
    }

    fun extract_resource_name(payload: &vector<u8>): String {
        // YAML/JSON에서 metadata.name 추출
        string::utf8(b"extracted_name") // 플레이스홀더
    }
}