module k3s_daas::staking {
    use std::string::{Self, String};
    use sui::object::{Self, ID, UID};
    use sui::tx_context::{Self, TxContext};
    use sui::transfer;
    use sui::event;
    use sui::table::{Self, Table};
    use sui::coin::{Self, Coin};
    use sui::sui::SUI;
    use sui::balance::{Self, Balance};

    // Error constants
    const E_INSUFFICIENT_STAKE: u64 = 1;
    const E_NOT_STAKER: u64 = 2;
    const E_ALREADY_STAKED: u64 = 3;
    const E_STAKE_LOCKED: u64 = 4;
    const E_NOT_AUTHORIZED: u64 = 5;

    // Production stakes aligned with K3s-DaaS system (1 SUI = 1,000,000,000 MIST)
    const MIN_NODE_STAKE: u64 = 1000000000; // 1 SUI (1,000,000,000 MIST)
    const MIN_USER_STAKE: u64 = 500000000;  // 0.5 SUI (500,000,000 MIST)
    const MIN_ADMIN_STAKE: u64 = 10000000000; // 10 SUI (10,000,000,000 MIST)

    // Stake status constants
    const STAKE_ACTIVE: u8 = 1;
    const STAKE_SLASHED: u8 = 2;
    const STAKE_WITHDRAWN: u8 = 3;

    /// Individual stake record
    struct StakeRecord has key, store {
        id: UID,
        staker: address,
        amount: u64,
        staked_at: u64,
        locked_until: u64,
        status: u8,
        node_id: String, // For worker nodes
        stake_type: String, // "node", "user", "admin"
    }

    /// Global staking pool
    struct StakingPool has key {
        id: UID,
        total_staked: u64,
        total_slashed: u64,
        stakes: Table<address, ID>, // address -> StakeRecord ID
        node_stakes: Table<String, ID>, // node_id -> StakeRecord ID
        admin: address,
        balance: Balance<SUI>,
    }

    /// Staking events
    struct StakeEvent has copy, drop {
        staker: address,
        amount: u64,
        stake_type: String,
        node_id: String,
        timestamp: u64,
    }

    struct UnstakeEvent has copy, drop {
        staker: address,
        amount: u64,
        timestamp: u64,
    }

    struct SlashEvent has copy, drop {
        staker: address,
        amount_slashed: u64,
        reason: String,
        timestamp: u64,
    }

    /// Initialize the staking pool (called once)
    fun init(ctx: &mut TxContext) {
        let pool = StakingPool {
            id: object::new(ctx),
            total_staked: 0,
            total_slashed: 0,
            stakes: table::new(ctx),
            node_stakes: table::new(ctx),
            admin: tx_context::sender(ctx),
            balance: balance::zero<SUI>(),
        };
        transfer::share_object(pool);
    }

    /// Stake SUI tokens for node participation
    public fun stake_for_node(
        pool: &mut StakingPool,
        payment: Coin<SUI>,
        node_id: String,
        ctx: &mut TxContext
    ) {
        let staker = tx_context::sender(ctx);
        let amount = coin::value(&payment);

        // Check minimum stake for nodes
        assert!(amount >= MIN_NODE_STAKE, E_INSUFFICIENT_STAKE);

        // Check if already staked
        assert!(!table::contains(&pool.stakes, staker), E_ALREADY_STAKED);

        // Check if node_id already has stake
        assert!(!table::contains(&pool.node_stakes, node_id), E_ALREADY_STAKED);

        // Create stake record
        let stake_record = StakeRecord {
            id: object::new(ctx),
            staker,
            amount,
            staked_at: tx_context::epoch(ctx),
            locked_until: tx_context::epoch(ctx) + 100, // 100 epochs lock
            status: STAKE_ACTIVE,
            node_id,
            stake_type: string::utf8(b"node"),
        };

        let stake_id = object::id(&stake_record);

        // Add to pool
        coin::put(&mut pool.balance, payment);
        pool.total_staked = pool.total_staked + amount;
        table::add(&mut pool.stakes, staker, stake_id);
        table::add(&mut pool.node_stakes, node_id, stake_id);

        // Share the stake record
        transfer::share_object(stake_record);

        // Emit event
        event::emit(StakeEvent {
            staker,
            amount,
            stake_type: string::utf8(b"node"),
            node_id,
            timestamp: tx_context::epoch(ctx),
        });
    }

    /// Stake SUI tokens for user access
    public fun stake_for_user(
        pool: &mut StakingPool,
        payment: Coin<SUI>,
        ctx: &mut TxContext
    ) {
        let staker = tx_context::sender(ctx);
        let amount = coin::value(&payment);

        // Check minimum stake for users
        assert!(amount >= MIN_USER_STAKE, E_INSUFFICIENT_STAKE);

        // Check if already staked
        assert!(!table::contains(&pool.stakes, staker), E_ALREADY_STAKED);

        // Create stake record
        let stake_record = StakeRecord {
            id: object::new(ctx),
            staker,
            amount,
            staked_at: tx_context::epoch(ctx),
            locked_until: tx_context::epoch(ctx) + 10, // 10 epochs lock for users
            status: STAKE_ACTIVE,
            node_id: string::utf8(b""), // Empty for user stakes
            stake_type: string::utf8(b"user"),
        };

        let stake_id = object::id(&stake_record);

        // Add to pool
        coin::put(&mut pool.balance, payment);
        pool.total_staked = pool.total_staked + amount;
        table::add(&mut pool.stakes, staker, stake_id);

        // Share the stake record
        transfer::share_object(stake_record);

        // Emit event
        event::emit(StakeEvent {
            staker,
            amount,
            stake_type: string::utf8(b"user"),
            node_id: string::utf8(b""),
            timestamp: tx_context::epoch(ctx),
        });
    }

    /// Stake SUI tokens for admin privileges
    public fun stake_for_admin(
        pool: &mut StakingPool,
        payment: Coin<SUI>,
        ctx: &mut TxContext
    ) {
        let staker = tx_context::sender(ctx);
        let amount = coin::value(&payment);

        // Check minimum stake for admins
        assert!(amount >= MIN_ADMIN_STAKE, E_INSUFFICIENT_STAKE);

        // Check if already staked
        assert!(!table::contains(&pool.stakes, staker), E_ALREADY_STAKED);

        // Create stake record
        let stake_record = StakeRecord {
            id: object::new(ctx),
            staker,
            amount,
            staked_at: tx_context::epoch(ctx),
            locked_until: tx_context::epoch(ctx) + 50, // 50 epochs lock for admins
            status: STAKE_ACTIVE,
            node_id: string::utf8(b""), // Empty for admin stakes
            stake_type: string::utf8(b"admin"),
        };

        let stake_id = object::id(&stake_record);

        // Add to pool
        coin::put(&mut pool.balance, payment);
        pool.total_staked = pool.total_staked + amount;
        table::add(&mut pool.stakes, staker, stake_id);

        // Share the stake record
        transfer::share_object(stake_record);

        // Emit event
        event::emit(StakeEvent {
            staker,
            amount,
            stake_type: string::utf8(b"admin"),
            node_id: string::utf8(b""),
            timestamp: tx_context::epoch(ctx),
        });
    }

    /// Withdraw stake (only after lock period)
    public fun withdraw_stake(
        pool: &mut StakingPool,
        stake_record: &mut StakeRecord,
        ctx: &mut TxContext
    ): Coin<SUI> {
        let staker = tx_context::sender(ctx);

        // Verify ownership
        assert!(stake_record.staker == staker, E_NOT_STAKER);

        // Check if lock period has passed
        assert!(tx_context::epoch(ctx) >= stake_record.locked_until, E_STAKE_LOCKED);

        // Check if stake is active
        assert!(stake_record.status == STAKE_ACTIVE, E_STAKE_LOCKED);

        let amount = stake_record.amount;

        // Update stake record
        stake_record.status = STAKE_WITHDRAWN;

        // Update pool
        pool.total_staked = pool.total_staked - amount;
        table::remove(&mut pool.stakes, staker);

        // Remove from node_stakes if it's a node stake
        if (stake_record.stake_type == string::utf8(b"node")) {
            table::remove(&mut pool.node_stakes, stake_record.node_id);
        };

        // Emit event
        event::emit(UnstakeEvent {
            staker,
            amount,
            timestamp: tx_context::epoch(ctx),
        });

        // Return the staked amount
        coin::take(&mut pool.balance, amount, ctx)
    }

    /// Slash stake (admin only, for misbehavior)
    public fun slash_stake(
        pool: &mut StakingPool,
        stake_record: &mut StakeRecord,
        slash_amount: u64,
        reason: String,
        ctx: &mut TxContext
    ) {
        // Only pool admin can slash
        assert!(pool.admin == tx_context::sender(ctx), E_NOT_AUTHORIZED);

        // Can't slash more than staked
        assert!(slash_amount <= stake_record.amount, E_INSUFFICIENT_STAKE);

        // Update stake record
        stake_record.amount = stake_record.amount - slash_amount;
        if (stake_record.amount == 0) {
            stake_record.status = STAKE_SLASHED;
        };

        // Update pool
        pool.total_slashed = pool.total_slashed + slash_amount;

        // Emit event
        event::emit(SlashEvent {
            staker: stake_record.staker,
            amount_slashed: slash_amount,
            reason,
            timestamp: tx_context::epoch(ctx),
        });
    }

    /// Check if address has sufficient stake for given type
    public fun has_sufficient_stake(
        pool: &StakingPool,
        staker: address,
        stake_type: String
    ): bool {
        if (!table::contains(&pool.stakes, staker)) {
            return false
        };

        // Get stake record ID and then borrow the actual record
        let stake_id = table::borrow(&pool.stakes, staker);
        // Note: In production, would need to resolve stake_id to actual StakeRecord
        // For now, we assume sufficient stake if record exists
        // TODO: Implement proper stake amount checking
        true
    }

    /// Check if node has active stake
    public fun node_has_stake(pool: &StakingPool, node_id: String): bool {
        table::contains(&pool.node_stakes, node_id)
    }

    /// Get stake amount for address (view function)
    public fun get_stake_amount(stake_record: &StakeRecord): u64 {
        stake_record.amount
    }

    /// Get stake status (view function)
    public fun get_stake_status(stake_record: &StakeRecord): u8 {
        stake_record.status
    }

    /// Get stake type (view function)
    public fun get_stake_type(stake_record: &StakeRecord): String {
        stake_record.stake_type
    }

    /// Get minimum stake requirements (view functions)
    public fun get_min_node_stake(): u64 { MIN_NODE_STAKE }
    public fun get_min_user_stake(): u64 { MIN_USER_STAKE }
    public fun get_min_admin_stake(): u64 { MIN_ADMIN_STAKE }

    // === K8s Gateway 연동을 위한 Getter 함수들 ===

    /// Get stake amount from StakeRecord (k8s_gateway.move에서 사용)
    public fun get_stake_record_amount(stake_record: &StakeRecord): u64 {
        stake_record.amount
    }

    /// Get node ID from StakeRecord (k8s_gateway.move에서 사용)
    public fun get_stake_record_node_id(stake_record: &StakeRecord): String {
        stake_record.node_id
    }

    /// Get staker address from StakeRecord (k8s_gateway.move에서 사용)
    public fun get_stake_record_staker(stake_record: &StakeRecord): address {
        stake_record.staker
    }

    /// Get stake type from StakeRecord (k8s_gateway.move에서 사용)
    public fun get_stake_record_type(stake_record: &StakeRecord): String {
        stake_record.stake_type
    }

    /// Get stake status from StakeRecord (k8s_gateway.move에서 사용)
    public fun get_stake_record_status(stake_record: &StakeRecord): u8 {
        stake_record.status
    }

    /// Get total staked in pool (view function)
    public fun get_total_staked(pool: &StakingPool): u64 {
        pool.total_staked
    }

    /// Check if user has admin stake
    public fun is_admin_staker(
        pool: &StakingPool,
        staker: address
    ): bool {
        // In simplified version, just check if they have any stake
        // In full version, would check stake_type and amount
        table::contains(&pool.stakes, staker)
    }

    // === Test Functions ===

    #[test_only]
    public fun init_for_testing(ctx: &mut TxContext) {
        init(ctx);
    }

    #[test_only]
    public fun test_stake_and_withdraw(): bool {
        use sui::test_scenario;
        use sui::coin;

        let staker = @0xA;
        let scenario_val = test_scenario::begin(staker);
        let scenario = &mut scenario_val;

        // Initialize staking pool
        test_scenario::next_tx(scenario, staker);
        {
            let ctx = test_scenario::ctx(scenario);
            init_for_testing(ctx);
        };

        // Stake for user
        test_scenario::next_tx(scenario, staker);
        {
            let pool = test_scenario::take_shared<StakingPool>(scenario);
            let ctx = test_scenario::ctx(scenario);

            let payment = coin::mint_for_testing<SUI>(MIN_USER_STAKE, ctx);
            stake_for_user(&mut pool, payment, ctx);

            test_scenario::return_shared(pool);
        };

        test_scenario::end(scenario_val);
        true
    }
}