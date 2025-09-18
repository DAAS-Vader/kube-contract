#!/bin/bash

# K3s-DaaS Nautilus TEE Master Node Launcher

echo "🌊 K3s-DaaS Nautilus TEE Master Node"
echo "==================================="

# 환경변수 설정
export NAUTILUS_ENCLAVE_ID="sui-hackathon-k3s-daas"
export CLUSTER_ID="sui-k3s-daas-hackathon"
export SUI_RPC_URL="https://fullnode.testnet.sui.io:443"

# 바이너리 확인 및 빌드
if [ ! -f "nautilus-tee" ] && [ ! -f "nautilus-tee.exe" ]; then
    echo "⚠️  바이너리가 없습니다. 빌드 중..."
    go build -o nautilus-tee . || go build -o nautilus-tee.exe .
fi

# 실행
echo "🚀 Nautilus TEE 마스터 노드 시작..."
if [ -f "nautilus-tee.exe" ]; then
    ./nautilus-tee.exe
else
    ./nautilus-tee
fi