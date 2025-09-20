#!/bin/bash
# Fixed deployment script for K3s-DaaS contracts

set -e

echo "🌊 Deploying K3s-DaaS Contracts to Sui Testnet (Fixed Version)"

# Check Sui CLI
if ! command -v sui &> /dev/null; then
    echo "❌ Sui CLI not found. Please install first"
    exit 1
fi

# Switch to testnet
echo "🔄 Switching to Sui testnet..."
sui client switch --env testnet

# Check balance
echo "💰 Checking SUI balance..."
BALANCE=$(sui client gas 2>/dev/null | grep -E "^\│.*SUI.*\│$" | head -2 | tail -1 | awk -F'│' '{print $4}' | tr -d ' ')
if [ -z "$BALANCE" ] || [ "$BALANCE" = "0.00" ] || [ "$BALANCE" = "0" ]; then
    echo "⚠️ Low SUI balance detected, but proceeding with deployment"
    echo "Current balance: $BALANCE SUI"
else
    echo "✅ Current balance: $BALANCE SUI"
fi

# Step 1: Create separate Move.toml for staking contract first
echo "📝 Creating staking-only Move.toml..."
mkdir -p temp_staking
cp staking.move temp_staking/

cat > temp_staking/Move.toml << EOF
[package]
name = "k8s_interface"
version = "1.0.0"
edition = "2024.beta"

[dependencies]
Sui = { git = "https://github.com/MystenLabs/sui.git", subdir = "crates/sui-framework/packages/sui-framework", rev = "framework/testnet" }

[addresses]
k8s_interface = "0x0"
EOF

# Deploy staking contract first
echo "📦 Publishing staking contract..."
cd temp_staking
STAKING_RESULT=$(sui client publish --gas-budget 100000000 . 2>&1)
STAKING_PACKAGE_ID=$(echo "$STAKING_RESULT" | grep -o "Package published after execution" -A1 | grep -o "0x[a-fA-F0-9]\{64\}" | head -1)

if [ -z "$STAKING_PACKAGE_ID" ]; then
    # Try alternative extraction
    STAKING_PACKAGE_ID=$(echo "$STAKING_RESULT" | grep -o "0x[a-fA-F0-9]\{64\}" | head -1)
fi

if [ -z "$STAKING_PACKAGE_ID" ]; then
    echo "❌ Failed to deploy staking contract"
    echo "$STAKING_RESULT"
    exit 1
fi

echo "✅ Staking contract deployed: $STAKING_PACKAGE_ID"
cd ..

# Step 2: Update Move.toml with the actual staking package ID for gateway
echo "📝 Creating gateway Move.toml with staking dependency..."
cat > Move.toml << EOF
[package]
name = "k3s_daas_contracts"
version = "1.0.0"
edition = "2024.beta"

[dependencies]
Sui = { git = "https://github.com/MystenLabs/sui.git", subdir = "crates/sui-framework/packages/sui-framework", rev = "framework/testnet" }
k8s_interface = { local = "./temp_staking" }

[addresses]
k3s_daas = "0x0"
k8s_interface = "$STAKING_PACKAGE_ID"
EOF

# Step 3: Deploy gateway contract
echo "📦 Publishing k8s_gateway contract..."
GATEWAY_RESULT=$(sui client publish --gas-budget 100000000 . 2>&1)
GATEWAY_PACKAGE_ID=$(echo "$GATEWAY_RESULT" | grep -o "Package published after execution" -A1 | grep -o "0x[a-fA-F0-9]\{64\}" | head -1)

if [ -z "$GATEWAY_PACKAGE_ID" ]; then
    # Try alternative extraction
    GATEWAY_PACKAGE_ID=$(echo "$GATEWAY_RESULT" | grep -o "0x[a-fA-F0-9]\{64\}" | head -1)
fi

if [ -z "$GATEWAY_PACKAGE_ID" ]; then
    echo "❌ Failed to deploy gateway contract"
    echo "$GATEWAY_RESULT"
    exit 1
fi

echo "✅ Gateway contract deployed: $GATEWAY_PACKAGE_ID"

# Step 4: Initialize staking pool
echo "🏊 Initializing staking pool..."
INIT_RESULT=$(sui client call \
    --package "$STAKING_PACKAGE_ID" \
    --module staking \
    --function init_for_testing \
    --gas-budget 10000000 2>&1)

POOL_ID=$(echo "$INIT_RESULT" | grep -o "0x[a-fA-F0-9]\{64\}" | tail -1)

if [ -z "$POOL_ID" ]; then
    echo "⚠️ Failed to initialize staking pool, but contracts are deployed"
    echo "$INIT_RESULT"
fi

# Create deployment info
cat > deployment-info.json << EOF
{
    "network": "testnet",
    "deployed_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "deployer": "$(sui client active-address)",
    "contracts": {
        "staking_package_id": "$STAKING_PACKAGE_ID",
        "gateway_package_id": "$GATEWAY_PACKAGE_ID",
        "staking_pool_id": "$POOL_ID"
    },
    "endpoints": {
        "sui_rpc": "https://fullnode.testnet.sui.io:443"
    }
}
EOF

echo "📄 Deployment info saved to deployment-info.json"

# Clean up
rm -rf temp_staking

echo ""
echo "🎉 Deployment Complete!"
echo "📋 Contract Information:"
echo "   Staking Package ID: $STAKING_PACKAGE_ID"
echo "   Gateway Package ID: $GATEWAY_PACKAGE_ID"
echo "   Pool ID: $POOL_ID"
echo "   Network: Sui Testnet"
echo ""
echo "🔄 Next Steps:"
echo "1. Update your config files with these package IDs"
echo "2. Test the contracts"
echo "3. Start your K3s-DaaS nodes"