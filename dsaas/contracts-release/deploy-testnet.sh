#!/bin/bash
# Deploy K3s-DaaS Smart Contracts to Sui Testnet

set -e

echo "🌊 Deploying K3s-DaaS Contracts to Sui Testnet"

# Check Sui CLI
if ! command -v sui &> /dev/null; then
    echo "❌ Sui CLI not found. Please install first:"
    echo "   cargo install --git https://github.com/MystenLabs/sui.git --tag testnet sui"
    exit 1
fi

# Switch to testnet
echo "🔄 Switching to Sui testnet..."
sui client switch --env testnet

# Check balance
echo "💰 Checking SUI balance..."
BALANCE=$(sui client gas | grep "│ SUI" | awk '{print $4}' | head -1)
if [ -z "$BALANCE" ] || [ "$BALANCE" = "0" ]; then
    echo "❌ Insufficient SUI balance for deployment"
    echo "🎯 Get testnet SUI from Discord faucet:"
    echo "   https://discord.com/channels/916379725201563759/1037811694564560966"
    echo "   !faucet $(sui client active-address)"
    exit 1
fi

echo "✅ Current balance: $BALANCE SUI"

# Create Move.toml if not exists
if [ ! -f "Move.toml" ]; then
    echo "📝 Creating Move.toml..."
    cat > Move.toml << EOF
[package]
name = "k3s_daas_contracts"
version = "1.0.0"
edition = "2024.beta"

[dependencies]
Sui = { git = "https://github.com/MystenLabs/sui.git", subdir = "crates/sui-framework/packages/sui-framework", rev = "testnet" }

[addresses]
k3s_interface = "0x0"
k8s_interface = "0x0"
k3s_daas = "0x0"
EOF
fi

# Publish staking contract
echo "📦 Publishing staking contract..."
STAKING_RESULT=$(sui client publish --gas-budget 100000000 . 2>&1)
STAKING_PACKAGE_ID=$(echo "$STAKING_RESULT" | grep -o "0x[a-fA-F0-9]\{64\}" | head -1)

if [ -z "$STAKING_PACKAGE_ID" ]; then
    echo "❌ Failed to deploy staking contract"
    echo "$STAKING_RESULT"
    exit 1
fi

echo "✅ Staking contract deployed: $STAKING_PACKAGE_ID"

# Initialize staking pool
echo "🏊 Initializing staking pool..."
INIT_RESULT=$(sui client call \
    --package "$STAKING_PACKAGE_ID" \
    --module staking \
    --function init_for_testing \
    --gas-budget 10000000 2>&1)

POOL_ID=$(echo "$INIT_RESULT" | grep -o "0x[a-fA-F0-9]\{64\}" | tail -1)

if [ -z "$POOL_ID" ]; then
    echo "❌ Failed to initialize staking pool"
    echo "$INIT_RESULT"
    exit 1
fi

echo "✅ Staking pool initialized: $POOL_ID"

# Create deployment info file
cat > deployment-info.json << EOF
{
    "network": "testnet",
    "deployed_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "deployer": "$(sui client active-address)",
    "contracts": {
        "staking_package_id": "$STAKING_PACKAGE_ID",
        "staking_pool_id": "$POOL_ID"
    },
    "endpoints": {
        "sui_rpc": "https://fullnode.testnet.sui.io:443",
        "sui_faucet": "https://discord.com/channels/916379725201563759/1037811694564560966"
    },
    "testing": {
        "min_stake_amount": 1000,
        "test_node_id": "test-worker-1"
    }
}
EOF

echo "📄 Deployment info saved to deployment-info.json"

# Test staking function
echo "🧪 Testing staking function..."
sui client call \
    --package "$STAKING_PACKAGE_ID" \
    --module staking \
    --function get_min_node_stake \
    --gas-budget 1000000

echo ""
echo "🎉 Deployment Complete!"
echo "📋 Contract Information:"
echo "   Package ID: $STAKING_PACKAGE_ID"
echo "   Pool ID: $POOL_ID"
echo "   Network: Sui Testnet"
echo ""
echo "🔄 Next Steps:"
echo "1. Update staker-config.json with package ID: $STAKING_PACKAGE_ID"
echo "2. Test staking: sui client call --package $STAKING_PACKAGE_ID --module staking --function stake_for_node ..."
echo "3. Start K3s-DaaS worker nodes"
echo ""
echo "💡 Example staker-config.json:"
echo "{"
echo "  \"contract_address\": \"$STAKING_PACKAGE_ID\","
echo "  \"sui_rpc_endpoint\": \"https://fullnode.testnet.sui.io:443\","
echo "  \"stake_amount\": 1000"
echo "}"