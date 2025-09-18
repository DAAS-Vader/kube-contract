#!/bin/bash

# K3s-DaaS Worker Node Launcher

echo "🔧 K3s-DaaS Worker Node"
echo "======================="

# 설정 파일 확인
if [ ! -f "staker-config.json" ]; then
    echo "❌ staker-config.json 파일이 없습니다."
    echo "예제 설정 파일 생성 중..."

    cat > staker-config.json << EOF
{
  "node_id": "sui-hackathon-worker-1",
  "sui_wallet_address": "0x1234567890abcdef1234567890abcdef12345678",
  "sui_private_key": "demo-private-key-for-hackathon",
  "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
  "stake_amount": 1000000000,
  "contract_address": "0x...your-deployed-contract-address",
  "nautilus_endpoint": "http://localhost:8080",
  "container_runtime": "containerd",
  "min_stake_amount": 100000000,
  "heartbeat_interval": 30,
  "mock_mode": true
}
EOF
    echo "✅ 예제 설정 파일이 생성되었습니다."
    echo "필요시 staker-config.json을 수정하세요."
fi

# 바이너리 확인 및 빌드
if [ ! -f "k3s-daas" ] && [ ! -f "k3s-daas.exe" ]; then
    echo "⚠️  바이너리가 없습니다. 빌드 중..."
    go build -o k3s-daas . || go build -o k3s-daas.exe .
fi

# 마스터 노드 연결 확인
echo "🔍 마스터 노드 연결 확인 중..."
if curl -s http://localhost:8080/health > /dev/null; then
    echo "✅ 마스터 노드 연결됨"
else
    echo "❌ 마스터 노드에 연결할 수 없습니다."
    echo "먼저 Nautilus TEE 마스터 노드를 시작하세요."
    exit 1
fi

# 워커 노드 실행
echo "🚀 워커 노드 시작..."
if [ -f "k3s-daas.exe" ]; then
    ./k3s-daas.exe
else
    ./k3s-daas
fi