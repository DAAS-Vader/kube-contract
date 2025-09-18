#!/bin/bash

# K3s-DaaS Move Contract Deployment

echo "📜 K3s-DaaS Move Contract Deployment"
echo "====================================="

# Sui CLI 확인
if ! command -v sui &> /dev/null; then
    echo "❌ Sui CLI가 설치되지 않았습니다."
    echo "설치: cargo install --locked --git https://github.com/MystenLabs/sui.git --branch testnet sui"
    exit 1
fi

# Move.toml 자동 생성
cat > Move.toml << EOF
[package]
name = "k3s_daas"
version = "1.0.0"
edition = "2024.beta"

[dependencies]
Sui = { git = "https://github.com/MystenLabs/sui.git", subdir = "crates/sui-framework/packages/sui-framework", rev = "testnet" }

[addresses]
k3s_daas = "0x0"
EOF

# 환경 설정
sui client switch --env testnet 2>/dev/null || sui client new-env --alias testnet --rpc https://fullnode.testnet.sui.io:443

# 계약 배포
echo "📦 Move 계약 배포 중..."
sui client publish . --gas-budget 20000000