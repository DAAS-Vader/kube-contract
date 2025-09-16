#!/bin/bash

# Seal Token Authentication Demo for kubectl
# Shows how kubectl uses Seal tokens to authenticate with K3s-DaaS

set -e

echo "🔐 K3s-DaaS Seal Token Authentication Demo"
echo "=========================================="

WALLET_ADDRESS="0x1234567890abcdef1234567890abcdef12345678"
CHALLENGE="k3s-daas-demo-challenge-$(date +%s)"
TIMESTAMP=$(date +%s)

# Generate a demo Seal token
echo "📝 Generating Seal token for demo..."
echo "   Wallet Address: $WALLET_ADDRESS"
echo "   Challenge: $CHALLENGE"
echo "   Timestamp: $TIMESTAMP"

# Simulate signature generation (in real implementation, this would be done by Sui wallet)
MESSAGE="$CHALLENGE:$TIMESTAMP:$WALLET_ADDRESS"
SIGNATURE=$(echo -n "$MESSAGE" | sha256sum | cut -d' ' -f1)

# Create Seal token in the expected format
SEAL_TOKEN="SEAL<$WALLET_ADDRESS>::$SIGNATURE::$CHALLENGE"

echo "   Generated Seal Token: $SEAL_TOKEN"
echo ""

# Create kubeconfig with Seal token
echo "⚙️  Creating kubeconfig with Seal token authentication..."
cat > /tmp/kubeconfig-seal << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://k3s-daas-master:6443
    insecure-skip-tls-verify: true
  name: k3s-daas
contexts:
- context:
    cluster: k3s-daas
    user: seal-user
  name: k3s-daas
current-context: k3s-daas
users:
- name: seal-user
  user:
    token: $SEAL_TOKEN
EOF

echo "✅ Kubeconfig created at /tmp/kubeconfig-seal"
echo ""

# Test authentication flow
echo "🧪 Testing Seal token authentication flow..."
echo ""

echo "1. 🏗️  Testing cluster access with Seal token..."
export KUBECONFIG=/tmp/kubeconfig-seal

# Test 1: Version check (should work)
echo "   Testing kubectl version..."
if kubectl version --client > /dev/null 2>&1; then
    echo "   ✅ kubectl client version: OK"
else
    echo "   ❌ kubectl client version: FAILED"
fi

# Test 2: Cluster info (requires authentication)
echo "   Testing cluster access..."
kubectl cluster-info 2>/dev/null && echo "   ✅ Cluster access: OK" || echo "   ⚠️  Cluster access: Authentication required"

# Test 3: Node list (requires authentication + stake verification)
echo "   Testing node listing with Seal token..."
kubectl get nodes 2>/dev/null && echo "   ✅ Node listing: OK" || echo "   ⚠️  Node listing: Requires stake verification"

echo ""
echo "2. 🔍 Demonstrating authentication headers..."

# Show what headers kubectl would send
cat << 'EOF'
   When kubectl makes requests with Seal token, it sends:

   Authorization: Bearer SEAL<wallet>::signature::challenge

   K3s-DaaS extracts this and:
   1. Validates the Seal token format
   2. Verifies the signature
   3. Checks stake on Sui blockchain
   4. Assigns RBAC groups based on stake amount:
      - 10M+ SUI: daas:admin, daas:cluster-admin
      - 5M+ SUI:  daas:operator, daas:namespace-admin
      - 1M+ SUI:  daas:user, daas:developer
EOF

echo ""
echo "3. 🎯 Simulating stake-based authorization..."

# Simulate different stake levels
declare -A stake_levels=(
    ["1000000"]="daas:user,daas:developer"
    ["5000000"]="daas:operator,daas:namespace-admin"
    ["10000000"]="daas:admin,daas:cluster-admin"
)

for stake in "${!stake_levels[@]}"; do
    groups="${stake_levels[$stake]}"
    echo "   Stake: ${stake} SUI → Groups: ${groups}"
done

echo ""
echo "4. 📊 Authentication Flow Summary"
echo "   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐"
echo "   │   kubectl   │───▶│ K3s-DaaS    │───▶│ Sui Chain   │───▶│ Nautilus    │"
echo "   │ (Seal Token)│    │ API Server  │    │ (Validate   │    │ TEE         │"
echo "   │             │    │             │    │  Stake)     │    │ (Store)     │"
echo "   └─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘"
echo ""
echo "   Flow:"
echo "   1. kubectl sends request with Seal token in Authorization header"
echo "   2. K3s-DaaS validates token format and signature"
echo "   3. K3s-DaaS queries Sui blockchain to verify stake amount"
echo "   4. If stake >= minimum (1M SUI), assigns RBAC groups"
echo "   5. Request proceeds to Nautilus TEE for processing"
echo "   6. Response returned through the same chain"

echo ""
echo "5. 🔐 Worker Node Registration with Seal"
echo "   Worker nodes also use Seal tokens for registration:"
echo "   - Each worker generates a Seal token with its wallet"
echo "   - Stake is verified during node join process"
echo "   - Node identity tied to wallet address"
echo "   - Ongoing operations require maintaining stake"

echo ""
echo "📋 Demo Environment Status:"
echo "   • Nautilus TEE: http://nautilus-tee:8080 (simulated)"
echo "   • Sui Blockchain: http://sui-blockchain:9000 (simulated)"
echo "   • Walrus Storage: http://walrus-storage:9002 (simulated)"
echo "   • K3s-DaaS API: https://k3s-daas-master:6443 (Seal auth enabled)"

echo ""
echo "🎉 Seal Token Authentication Demo Complete!"
echo ""
echo "📝 Next Steps:"
echo "   1. In production, use real Sui wallet to generate signatures"
echo "   2. Deploy actual DaaS smart contracts on Sui"
echo "   3. Configure real stake requirements"
echo "   4. Integrate with actual Nautilus TEE hardware"

# Cleanup
rm -f /tmp/kubeconfig-seal