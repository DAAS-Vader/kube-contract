// K8s-DaaS Worker Registry - 실제 워커 풀과 스테이킹 관리
module k8s_daas::worker_registry {
    use sui::coin::{Self, Coin};
    use sui::sui::SUI;
    use sui::table::{Self, Table};
    use sui::tx_context::{Self, TxContext};
    use sui::object::{Self, UID};
    use sui::transfer;
    use sui::event;
    use std::string::{Self, String};
    use std::vector;

    // ==================== Error Constants ====================

    const EInsufficientStake: u64 = 1;
    const EWorkerAlreadyExists: u64 = 2;
    const EWorkerNotFound: u64 = 3;
    const EInvalidSealToken: u64 = 4;
    const EWorkerNotActive: u64 = 5;
    const EUnauthorized: u64 = 6;
    const EInvalidOperation: u64 = 7;

    // ==================== Constants ====================

    const MIN_STAKE_AMOUNT: u64 = 1000000; // 1 SUI minimum stake
    const MAX_WORKERS_PER_ADDRESS: u64 = 10;
    const HEARTBEAT_TIMEOUT_MS: u64 = 300000; // 5 minutes

    // ==================== Structs ====================

    /// 워커 노드 정보
    public struct WorkerNode has store, drop {
        node_id: String,
        owner: address,
        stake_amount: u64,
        status: String,           // "pending", "active", "busy", "offline", "slashed"
        seal_token: String,
        registered_at: u64,
        last_heartbeat: u64,
        total_pods_served: u64,
        reputation_score: u64,
    }

    /// 워커 레지스트리 - 전체 워커 풀 관리
    public struct WorkerRegistry has key {
        id: UID,
        workers: Table<String, WorkerNode>,     // node_id -> WorkerNode
        owner_workers: Table<address, vector<String>>, // owner -> [node_ids]
        active_workers: vector<String>,         // 활성 워커 목록
        total_stake: u64,
        total_workers: u64,
        admin: address,
    }

    /// 스테이킹 증명서
    public struct StakeProof has key, store {
        id: UID,
        node_id: String,
        stake_amount: u64,
        staked_at: u64,
        owner: address,
    }

    /// 워커 등록 이벤트
    public struct WorkerRegisteredEvent has copy, drop {
        node_id: String,
        owner: address,
        stake_amount: u64,
        seal_token: String,
        timestamp: u64,
    }

    /// 워커 상태 변경 이벤트
    public struct WorkerStatusChangedEvent has copy, drop {
        node_id: String,
        old_status: String,
        new_status: String,
        timestamp: u64,
    }

    /// 스테이킹 이벤트
    public struct StakeDepositedEvent has copy, drop {
        node_id: String,
        owner: address,
        amount: u64,
        timestamp: u64,
    }

    // ==================== Public Functions ====================

    /// 워커 레지스트리 초기화 (한 번만 실행)
    fun init(ctx: &mut TxContext) {
        let registry = WorkerRegistry {
            id: object::new(ctx),
            workers: table::new(ctx),
            owner_workers: table::new(ctx),
            active_workers: vector::empty(),
            total_stake: 0,
            total_workers: 0,
            admin: tx_context::sender(ctx),
        };

        transfer::share_object(registry);
    }

    /// 워커 노드 등록 및 스테이킹
    public fun stake_and_register_worker(
        registry: &mut WorkerRegistry,
        payment: Coin<SUI>,
        node_id: String,
        seal_token: String,
        ctx: &mut TxContext
    ) {
        let sender = tx_context::sender(ctx);
        let stake_amount = coin::value(&payment);

        // 최소 스테이킹 요구사항 확인
        assert!(stake_amount >= MIN_STAKE_AMOUNT, EInsufficientStake);

        // 워커 ID 중복 확인
        assert!(!table::contains(&registry.workers, node_id), EWorkerAlreadyExists);

        // Seal Token 유효성 검사 (길이 확인)
        assert!(string::length(&seal_token) >= 32, EInvalidSealToken);

        // 기존 워커 수 확인
        if (table::contains(&registry.owner_workers, sender)) {
            let owner_worker_list = table::borrow(&registry.owner_workers, sender);
            assert!(vector::length(owner_worker_list) < MAX_WORKERS_PER_ADDRESS, EInvalidOperation);
        };

        let timestamp = tx_context::epoch_timestamp_ms(ctx);

        // 워커 노드 생성
        let worker = WorkerNode {
            node_id,
            owner: sender,
            stake_amount,
            status: string::utf8(b"pending"),
            seal_token,
            registered_at: timestamp,
            last_heartbeat: timestamp,
            total_pods_served: 0,
            reputation_score: 100, // 기본 점수
        };

        // 레지스트리에 워커 추가
        table::add(&mut registry.workers, node_id, worker);

        // 소유자별 워커 목록 업데이트
        if (!table::contains(&registry.owner_workers, sender)) {
            table::add(&mut registry.owner_workers, sender, vector::empty());
        };
        let owner_list = table::borrow_mut(&mut registry.owner_workers, sender);
        vector::push_back(owner_list, node_id);

        // 통계 업데이트
        registry.total_stake = registry.total_stake + stake_amount;
        registry.total_workers = registry.total_workers + 1;

        // 스테이킹 자금을 컨트랙트에 보관 (실제로는 Treasury 등으로)
        transfer::public_transfer(payment, @k8s_daas);

        // 스테이킹 증명서 발행
        let stake_proof = StakeProof {
            id: object::new(ctx),
            node_id,
            stake_amount,
            staked_at: timestamp,
            owner: sender,
        };

        // 이벤트 발생
        event::emit(WorkerRegisteredEvent {
            node_id,
            owner: sender,
            stake_amount,
            seal_token,
            timestamp,
        });

        event::emit(StakeDepositedEvent {
            node_id,
            owner: sender,
            amount: stake_amount,
            timestamp,
        });

        // StakeProof를 sender에게 전송
        transfer::transfer(stake_proof, sender);

        // 성공적으로 등록되었음을 return (empty for now)
        ()
    }

    /// 워커 활성화 (관리자 또는 자동화 시스템)
    public fun activate_worker(
        registry: &mut WorkerRegistry,
        node_id: String,
        ctx: &mut TxContext
    ) {
        // 관리자 권한 확인 (실제로는 더 세밀한 권한 관리)
        let sender = tx_context::sender(ctx);

        assert!(table::contains(&registry.workers, node_id), EWorkerNotFound);

        let worker = table::borrow_mut(&mut registry.workers, node_id);
        let old_status = worker.status;

        // pending 상태인 워커만 활성화 가능
        assert!(old_status == string::utf8(b"pending"), EInvalidOperation);

        worker.status = string::utf8(b"active");
        worker.last_heartbeat = tx_context::epoch_timestamp_ms(ctx);

        // 활성 워커 목록에 추가
        vector::push_back(&mut registry.active_workers, node_id);

        // 이벤트 발생
        event::emit(WorkerStatusChangedEvent {
            node_id,
            old_status,
            new_status: worker.status,
            timestamp: tx_context::epoch_timestamp_ms(ctx),
        });
    }

    /// 워커 하트비트 업데이트
    public fun update_heartbeat(
        registry: &mut WorkerRegistry,
        node_id: String,
        ctx: &mut TxContext
    ) {
        let sender = tx_context::sender(ctx);

        assert!(table::contains(&registry.workers, node_id), EWorkerNotFound);

        let worker = table::borrow_mut(&mut registry.workers, node_id);

        // 워커 소유자만 하트비트 업데이트 가능
        assert!(worker.owner == sender, EUnauthorized);

        worker.last_heartbeat = tx_context::epoch_timestamp_ms(ctx);
    }

    /// 워커 상태 변경
    public fun change_worker_status(
        registry: &mut WorkerRegistry,
        node_id: String,
        new_status: String,
        ctx: &mut TxContext
    ) {
        assert!(table::contains(&registry.workers, node_id), EWorkerNotFound);

        let worker = table::borrow_mut(&mut registry.workers, node_id);
        let old_status = worker.status;

        worker.status = new_status;

        // 활성 워커 목록 관리
        if (new_status == string::utf8(b"active")) {
            if (!vector::contains(&registry.active_workers, &node_id)) {
                vector::push_back(&mut registry.active_workers, node_id);
            };
        } else {
            let (contains, index) = vector::index_of(&registry.active_workers, &node_id);
            if (contains) {
                vector::remove(&mut registry.active_workers, index);
            };
        };

        // 이벤트 발생
        event::emit(WorkerStatusChangedEvent {
            node_id,
            old_status,
            new_status,
            timestamp: tx_context::epoch_timestamp_ms(ctx),
        });
    }

    /// Pod 서비스 완료 시 워커 통계 업데이트
    public fun record_pod_service(
        registry: &mut WorkerRegistry,
        node_id: String,
        success: bool,
        ctx: &mut TxContext
    ) {
        assert!(table::contains(&registry.workers, node_id), EWorkerNotFound);

        let worker = table::borrow_mut(&mut registry.workers, node_id);
        worker.total_pods_served = worker.total_pods_served + 1;

        // 성공/실패에 따른 평판 점수 조정
        if (success) {
            worker.reputation_score = worker.reputation_score + 1;
        } else if (worker.reputation_score > 0) {
            worker.reputation_score = worker.reputation_score - 1;
        };

        worker.last_heartbeat = tx_context::epoch_timestamp_ms(ctx);
    }

    // ==================== View Functions ====================

    /// 워커 정보 조회
    public fun get_worker(registry: &WorkerRegistry, node_id: String): &WorkerNode {
        table::borrow(&registry.workers, node_id)
    }

    /// 활성 워커 목록 조회
    public fun get_active_workers(registry: &WorkerRegistry): &vector<String> {
        &registry.active_workers
    }

    /// 워커 풀 통계 조회
    public fun get_pool_stats(registry: &WorkerRegistry): (u64, u64, u64) {
        (registry.total_workers, vector::length(&registry.active_workers), registry.total_stake)
    }

    /// 특정 주소의 워커 목록 조회
    public fun get_owner_workers(registry: &WorkerRegistry, owner: address): &vector<String> {
        table::borrow(&registry.owner_workers, owner)
    }

    /// 워커가 활성 상태인지 확인
    public fun is_worker_active(registry: &WorkerRegistry, node_id: String): bool {
        if (!table::contains(&registry.workers, node_id)) {
            return false
        };

        let worker = table::borrow(&registry.workers, node_id);
        worker.status == string::utf8(b"active")
    }

    /// 워커의 스테이킹 양 조회
    public fun get_worker_stake(registry: &WorkerRegistry, node_id: String): u64 {
        let worker = table::borrow(&registry.workers, node_id);
        worker.stake_amount
    }

    /// 워커 소유자 확인
    public fun is_worker_owner(registry: &WorkerRegistry, node_id: String, owner: address): bool {
        if (!table::contains(&registry.workers, node_id)) {
            return false
        };

        let worker = table::borrow(&registry.workers, node_id);
        worker.owner == owner
    }

    /// 워커 소유자 주소 조회
    public fun get_worker_owner(registry: &WorkerRegistry, node_id: String): address {
        let worker = table::borrow(&registry.workers, node_id);
        worker.owner
    }
}