/// Minimal K3s-DaaS Gateway Contract
module k3s_daas::gateway {
    use sui::object::{UID};
    use sui::tx_context::{TxContext};
    use sui::transfer;
    use sui::event;
    use std::string::{String};
    use std::vector;

    // Basic structs for the gateway functionality
    public struct K8sAPIRequest has copy, drop {
        method: String,
        path: String,
        namespace: String,
        resource_type: String,
        payload: vector<u8>,
        sender: address,
        timestamp: u64,
    }

    public struct SealToken has key, store {
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

    public struct K8sResourceEvent has copy, drop {
        resource_type: String,
        namespace: String,
        name: String,
        action: String,
        executor: address,
        nautilus_node: address,
        timestamp: u64,
    }

    // Basic initialization function
    fun init(_ctx: &mut TxContext) {
        // Initialize the gateway module
    }

    // Create a basic seal token for testing
    public entry fun create_seal_token(
        node_id: String,
        stake_amount: u64,
        ctx: &mut TxContext
    ) {
        let seal_token = SealToken {
            id: object::new(ctx),
            wallet_address: node_id,
            signature: std::string::utf8(b"test_signature"),
            challenge: std::string::utf8(b"test_challenge"),
            timestamp: 0,
            stake_amount,
            permissions: vector::empty<String>(),
            expires_at: 0,
            nautilus_endpoint: @0x0,
        };

        transfer::transfer(seal_token, tx_context::sender(ctx));
    }

    // Emit an event when K8s resource is created
    public entry fun emit_k8s_event(
        resource_type: String,
        namespace: String,
        name: String,
        action: String,
        ctx: &mut TxContext
    ) {
        let event = K8sResourceEvent {
            resource_type,
            namespace,
            name,
            action,
            executor: tx_context::sender(ctx),
            nautilus_node: @0x0,
            timestamp: 0,
        };
        event::emit(event);
    }
}