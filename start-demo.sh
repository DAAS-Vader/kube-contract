#!/bin/bash

# K3s-DaaS Demo Environment Startup Script

set -e

echo "🚀 Starting K3s-DaaS Demo Environment"
echo "====================================="

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker first."
    exit 1
fi

echo "✅ Docker is running"

# Create necessary directories
echo "📁 Creating demo directories..."
mkdir -p nautilus-data nautilus-config sui-data walrus-data demo-scripts

# Create Nautilus configuration
echo "⚙️  Creating Nautilus TEE configuration..."
cat > nautilus-config/config.json << 'EOF'
{
  "tee_mode": "simulation",
  "api_key": "demo-key-nautilus",
  "enclave_path": "/app/enclave",
  "performance_target": "50ms",
  "memory_limit": "512MB",
  "attestation": {
    "enabled": true,
    "quote_provider": "simulation"
  }
}
EOF

# Create K3s-DaaS configuration
echo "⚙️  Creating K3s-DaaS configuration..."
cat > k3s-daas-config.yaml << 'EOF'
daas:
  enabled: true
  nautilus:
    tee_endpoint: "http://nautilus-tee:8080"
    api_key: "demo-key-nautilus"
    enclave_path: "/app/enclave"
    performance_target: "50ms"
  sui:
    rpc_endpoint: "http://sui-blockchain:9000"
    private_key: "ed25519_private_key_demo_hex_32_bytes_000000000000000000000000000000"
    contract_package: "0xabcdef1234567890"
  walrus:
    api_endpoint: "http://walrus-storage:9002"
    publisher_url: "http://walrus-storage:9002"
    aggregator_url: "http://walrus-storage:9003"
  seal:
    min_stake: 100        # 0.0000001 SUI (100 MIST) - 테스트넷용
    min_node_stake: 1000  # 0.000001 SUI (1000 MIST) - 워커 노드용
    min_user_stake: 100   # 0.0000001 SUI (100 MIST) - 일반 사용자용
    min_admin_stake: 10000 # 0.00001 SUI (10000 MIST) - 관리자용
    token_validity: "24h"
    blockchain_timeout: "10s"
EOF

# Build and start the demo environment
echo "🔨 Building and starting demo environment..."
docker-compose -f docker-compose.demo.yml down --remove-orphans || true
docker-compose -f docker-compose.demo.yml build
docker-compose -f docker-compose.demo.yml up -d

echo "⏳ Waiting for services to start..."
sleep 10

# Check service health
echo "🔍 Checking service health..."

echo "   - Checking Nautilus TEE..."
timeout 60 bash -c 'until curl -f http://localhost:8080/api/v1/health >/dev/null 2>&1; do sleep 2; done' || {
    echo "❌ Nautilus TEE health check failed"
    exit 1
}
echo "   ✅ Nautilus TEE is healthy"

echo "   - Checking Sui blockchain..."
timeout 60 bash -c 'until curl -f http://localhost:9000/health >/dev/null 2>&1; do sleep 2; done' || {
    echo "❌ Sui blockchain health check failed"
    exit 1
}
echo "   ✅ Sui blockchain is healthy"

echo "   - Checking Walrus storage..."
timeout 60 bash -c 'until curl -f http://localhost:9002/health >/dev/null 2>&1; do sleep 2; done' || {
    echo "❌ Walrus storage health check failed"
    exit 1
}
echo "   ✅ Walrus storage is healthy"

echo "   - Checking K3s-DaaS master..."
timeout 120 bash -c 'until curl -k https://localhost:6443/livez >/dev/null 2>&1; do sleep 5; done' || {
    echo "❌ K3s-DaaS master health check failed"
    echo "Checking logs..."
    docker-compose -f docker-compose.demo.yml logs k3s-daas-master
    exit 1
}
echo "   ✅ K3s-DaaS master is healthy"

echo ""
echo "🎉 K3s-DaaS Demo Environment is ready!"
echo "======================================"
echo ""
echo "📊 Service URLs:"
echo "   - Nautilus TEE:     http://localhost:8080"
echo "   - Sui Blockchain:   http://localhost:9000"
echo "   - Walrus Storage:   http://localhost:9002"
echo "   - K3s API Server:   https://localhost:6443"
echo ""
echo "🧪 Run the demo test:"
echo "   docker-compose -f docker-compose.demo.yml exec kubectl-demo /scripts/demo-test.sh"
echo ""
echo "📋 Check logs:"
echo "   docker-compose -f docker-compose.demo.yml logs [service-name]"
echo ""
echo "🛑 Stop the demo:"
echo "   docker-compose -f docker-compose.demo.yml down"
echo ""
echo "Demo environment started successfully! 🚀"