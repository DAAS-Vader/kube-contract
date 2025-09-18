module k8s_interface::gateway {
    use std::string::{Self, String};
    use sui::object::{Self, ID, UID};
    use sui::tx_context::{Self, TxContext};
    use sui::transfer;
    use sui::event;
    use sui::table::{Self, Table};

    // Error constants
    const E_NOT_AUTHORIZED: u64 = 1;
    const E_CLUSTER_NOT_FOUND: u64 = 2;
    const E_CLUSTER_OFFLINE: u64 = 3;
    const E_INVALID_COMMAND: u64 = 4;

    // Cluster status constants
    const STATUS_ACTIVE: u8 = 1;
    const STATUS_MAINTENANCE: u8 = 2;
    const STATUS_OFFLINE: u8 = 3;

    /// Represents a Kubernetes cluster with TEE endpoint
    struct Cluster has key {
        id: UID,
        nautilus_endpoint: String,
        owner: address,
        status: u8,
        authorized_users: vector<address>,
        created_at: u64,
    }

    /// User permission record with staking requirement
    struct UserPermission has key {
        id: UID,
        user: address,
        cluster_id: ID,
        permissions: vector<String>, // e.g., ["get", "list", "create", "delete"]
        granted_by: address,
        granted_at: u64,
        stake_amount: u64, // Required stake to maintain access
        stake_locked_until: u64, // Epoch when stake can be withdrawn
    }

    /// Stake pool for cluster access
    struct ClusterStakePool has key {
        id: UID,
        cluster_id: ID,
        total_staked: u64,
        min_stake_per_user: u64,
        user_stakes: Table<address, u64>,
        admin: address,
    }

    /// Audit log event
    struct AuditEvent has copy, drop {
        cluster_id: ID,
        user: address,
        command: String,
        args: vector<String>,
        endpoint: String,
        timestamp: u64,
        success: bool,
    }

    /// kubectl request response
    struct KubectlResponse has copy, drop {
        success: bool,
        endpoint: String,
        error_message: String,
    }

    /// Registry for cluster access control
    struct ClusterRegistry has key {
        id: UID,
        clusters: Table<ID, ClusterInfo>,
        admin: address,
    }

    /// Public cluster information (lightweight)
    struct ClusterInfo has store, copy, drop {
        endpoint: String,
        owner: address,
        status: u8,
        created_at: u64,
    }

    /// Initialize a new cluster (shared object for multi-user access)
    public fun create_cluster(
        nautilus_endpoint: String,
        ctx: &mut TxContext
    ): ID {
        let cluster = Cluster {
            id: object::new(ctx),
            nautilus_endpoint,
            owner: tx_context::sender(ctx),
            status: STATUS_ACTIVE,
            authorized_users: vector::empty(),
            created_at: tx_context::epoch(ctx),
        };

        let cluster_id = object::id(&cluster);

        // MUST be shared object so multiple users can access it
        transfer::share_object(cluster);

        cluster_id
    }

    /// Alternative: Create cluster with registry pattern
    public fun create_cluster_with_registry(
        registry: &mut ClusterRegistry,
        nautilus_endpoint: String,
        ctx: &mut TxContext
    ): ID {
        // Only registry admin can create clusters
        assert!(registry.admin == tx_context::sender(ctx), E_NOT_AUTHORIZED);

        let cluster_info = ClusterInfo {
            endpoint: nautilus_endpoint,
            owner: tx_context::sender(ctx),
            status: STATUS_ACTIVE,
            created_at: tx_context::epoch(ctx),
        };

        let cluster_id = object::id_from_address(@0x1); // Generate proper ID
        table::add(&mut registry.clusters, cluster_id, cluster_info);

        cluster_id
    }

    /// Add authorized user to cluster
    public fun authorize_user(
        cluster: &mut Cluster,
        user: address,
        permissions: vector<String>,
        ctx: &mut TxContext
    ) {
        // Only cluster owner can authorize users
        assert!(cluster.owner == tx_context::sender(ctx), E_NOT_AUTHORIZED);

        vector::push_back(&mut cluster.authorized_users, user);

        let permission = UserPermission {
            id: object::new(ctx),
            user,
            cluster_id: object::id(cluster),
            permissions,
            granted_by: tx_context::sender(ctx),
            granted_at: tx_context::epoch(ctx),
        };

        transfer::share_object(permission);
    }

    /// Main kubectl request handler - returns TEE endpoint and logs audit
    public fun kubectl_request(
        cluster: &Cluster,
        user_permission: &UserPermission,
        command: String,
        args: vector<String>,
        ctx: &mut TxContext
    ): KubectlResponse {
        let user = tx_context::sender(ctx);
        let cluster_id = object::id(cluster);
        let timestamp = tx_context::epoch(ctx);

        // Check if cluster is active
        if (cluster.status != STATUS_ACTIVE) {
            let error_response = KubectlResponse {
                success: false,
                endpoint: string::utf8(b""),
                error_message: string::utf8(b"Cluster is not active"),
            };

            // Log failed attempt
            event::emit(AuditEvent {
                cluster_id,
                user,
                command,
                args,
                endpoint: string::utf8(b""),
                timestamp,
                success: false,
            });

            return error_response
        };

        // Verify user permission object belongs to this user and cluster
        assert!(user_permission.user == user, E_NOT_AUTHORIZED);
        assert!(user_permission.cluster_id == cluster_id, E_NOT_AUTHORIZED);

        // Check if user has permission for this command
        let has_permission = check_command_permission(&command, &user_permission.permissions);

        if (!has_permission) {
            let error_response = KubectlResponse {
                success: false,
                endpoint: string::utf8(b""),
                error_message: string::utf8(b"Insufficient permissions for command"),
            };

            // Log unauthorized attempt
            event::emit(AuditEvent {
                cluster_id,
                user,
                command,
                args,
                endpoint: string::utf8(b""),
                timestamp,
                success: false,
            });

            return error_response
        };

        // Return TEE endpoint for authorized request
        let success_response = KubectlResponse {
            success: true,
            endpoint: cluster.nautilus_endpoint,
            error_message: string::utf8(b""),
        };

        // Log successful request
        event::emit(AuditEvent {
            cluster_id,
            user,
            command,
            args,
            endpoint: cluster.nautilus_endpoint,
            timestamp,
            success: true,
        });

        success_response
    }

    /// Check if user has permission for specific command
    fun check_command_permission(command: &String, permissions: &vector<String>): bool {
        let i = 0;
        let len = vector::length(permissions);

        while (i < len) {
            let permission = vector::borrow(permissions, i);

            // Check for exact match or wildcard permissions
            if (permission == command || permission == &string::utf8(b"*")) {
                return true
            };

            // Check for verb-based permissions (e.g., "get" covers "get pods")
            if (string::length(command) > string::length(permission)) {
                let command_bytes = string::bytes(command);
                let permission_bytes = string::bytes(permission);

                if (starts_with(command_bytes, permission_bytes)) {
                    return true
                };
            };

            i = i + 1;
        };

        false
    }

    /// Helper function to check if bytes start with prefix
    fun starts_with(bytes: &vector<u8>, prefix: &vector<u8>): bool {
        let prefix_len = vector::length(prefix);
        let bytes_len = vector::length(bytes);

        if (prefix_len > bytes_len) {
            return false
        };

        let i = 0;
        while (i < prefix_len) {
            if (vector::borrow(bytes, i) != vector::borrow(prefix, i)) {
                return false
            };
            i = i + 1;
        };

        true
    }

    /// Update cluster status (owner only)
    public fun update_cluster_status(
        cluster: &mut Cluster,
        new_status: u8,
        ctx: &mut TxContext
    ) {
        assert!(cluster.owner == tx_context::sender(ctx), E_NOT_AUTHORIZED);
        cluster.status = new_status;
    }

    /// Update cluster endpoint (owner only)
    public fun update_cluster_endpoint(
        cluster: &mut Cluster,
        new_endpoint: String,
        ctx: &mut TxContext
    ) {
        assert!(cluster.owner == tx_context::sender(ctx), E_NOT_AUTHORIZED);
        cluster.nautilus_endpoint = new_endpoint;
    }

    /// Remove user authorization (owner only)
    public fun revoke_user_authorization(
        cluster: &mut Cluster,
        user: address,
        ctx: &mut TxContext
    ) {
        assert!(cluster.owner == tx_context::sender(ctx), E_NOT_AUTHORIZED);

        let (found, index) = vector::index_of(&cluster.authorized_users, &user);
        if (found) {
            vector::remove(&mut cluster.authorized_users, index);
        };
    }

    // === View Functions ===

    /// Get cluster endpoint (view only)
    public fun get_cluster_endpoint(cluster: &Cluster): String {
        cluster.nautilus_endpoint
    }

    /// Get cluster status (view only)
    public fun get_cluster_status(cluster: &Cluster): u8 {
        cluster.status
    }

    /// Get cluster owner (view only)
    public fun get_cluster_owner(cluster: &Cluster): address {
        cluster.owner
    }

    /// Check if user is authorized for cluster (view only)
    public fun is_user_authorized(cluster: &Cluster, user: address): bool {
        vector::contains(&cluster.authorized_users, &user)
    }

    /// Get user permissions (view only)
    public fun get_user_permissions(permission: &UserPermission): &vector<String> {
        &permission.permissions
    }

    // === Test Functions (for testing only) ===

    #[test_only]
    public fun test_create_cluster_and_authorize(): (ID, address) {
        use sui::test_scenario;

        let admin = @0xA;
        let user = @0xB;

        let scenario_val = test_scenario::begin(admin);
        let scenario = &mut scenario_val;

        // Admin creates cluster
        test_scenario::next_tx(scenario, admin);
        {
            let ctx = test_scenario::ctx(scenario);
            let cluster_id = create_cluster(string::utf8(b"https://nautilus-tee.example.com"), ctx);
            test_scenario::return_to_sender(scenario, cluster_id);
        };

        // Admin authorizes user
        test_scenario::next_tx(scenario, admin);
        {
            let cluster = test_scenario::take_shared<Cluster>(scenario);
            let ctx = test_scenario::ctx(scenario);

            let permissions = vector::empty<String>();
            vector::push_back(&mut permissions, string::utf8(b"get"));
            vector::push_back(&mut permissions, string::utf8(b"list"));

            authorize_user(&mut cluster, user, permissions, ctx);
            test_scenario::return_shared(cluster);
        };

        test_scenario::end(scenario_val);
        (object::id_from_address(@0x0), user)
    }

    #[test_only]
    public fun test_kubectl_request_success(): bool {
        use sui::test_scenario;

        let admin = @0xA;
        let user = @0xB;

        let scenario_val = test_scenario::begin(admin);
        let scenario = &mut scenario_val;

        // Create cluster and authorize user (setup)
        test_scenario::next_tx(scenario, admin);
        let cluster_id = {
            let ctx = test_scenario::ctx(scenario);
            create_cluster(string::utf8(b"https://nautilus-tee.example.com"), ctx)
        };

        test_scenario::next_tx(scenario, admin);
        {
            let cluster = test_scenario::take_shared<Cluster>(scenario);
            let ctx = test_scenario::ctx(scenario);

            let permissions = vector::empty<String>();
            vector::push_back(&mut permissions, string::utf8(b"get"));

            authorize_user(&mut cluster, user, permissions, ctx);
            test_scenario::return_shared(cluster);
        };

        // User makes kubectl request
        test_scenario::next_tx(scenario, user);
        {
            let cluster = test_scenario::take_shared<Cluster>(scenario);
            let permission = test_scenario::take_shared<UserPermission>(scenario);
            let ctx = test_scenario::ctx(scenario);

            let args = vector::empty<String>();
            vector::push_back(&mut args, string::utf8(b"pods"));

            let response = kubectl_request(
                &cluster,
                &permission,
                string::utf8(b"get"),
                args,
                ctx
            );

            test_scenario::return_shared(cluster);
            test_scenario::return_shared(permission);

            test_scenario::end(scenario_val);
            return response.success
        };

        test_scenario::end(scenario_val);
        false
    }
}