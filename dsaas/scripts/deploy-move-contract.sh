#!/bin/bash

# Move 계약 배포 스크립트 for K3s-DaaS Sui Hackathon

echo "🌊 Sui K3s-DaaS Move 계약 배포"
echo "============================="

# 1. Sui CLI 설치 확인
echo "1. Sui CLI 설치 확인..."
if ! command -v sui &> /dev/null; then
    echo "   ❌ Sui CLI가 설치되지 않았습니다"
    echo "   설치 방법: https://docs.sui.io/build/install"
    echo "   또는: cargo install --locked --git https://github.com/MystenLabs/sui.git --branch devnet sui"
    exit 1
else
    echo "   ✅ Sui CLI 설치됨"
    sui --version
fi

# 2. Sui 네트워크 설정 확인
echo "2. Sui 네트워크 설정 확인..."
CURRENT_ENV=$(sui client active-env 2>/dev/null || echo "none")
echo "   현재 환경: $CURRENT_ENV"

if [ "$CURRENT_ENV" != "testnet" ]; then
    echo "   🔧 Testnet으로 환경 설정 중..."
    sui client new-env --alias testnet --rpc https://fullnode.testnet.sui.io:443
    sui client switch --env testnet
fi

# 3. Sui 지갑 확인
echo "3. Sui 지갑 확인..."
ACTIVE_ADDRESS=$(sui client active-address 2>/dev/null || echo "none")
if [ "$ACTIVE_ADDRESS" = "none" ]; then
    echo "   ⚠️  활성 지갑이 없습니다. 새 지갑 생성 중..."
    sui client new-address ed25519
    ACTIVE_ADDRESS=$(sui client active-address)
fi
echo "   ✅ 활성 지갑: $ACTIVE_ADDRESS"

# 4. SUI 잔액 확인
echo "4. SUI 잔액 확인..."
BALANCE=$(sui client balance 2>/dev/null | grep "SUI" | head -1 || echo "0 SUI")
echo "   잔액: $BALANCE"

if [[ "$BALANCE" == *"0 SUI"* ]] || [[ "$BALANCE" == "" ]]; then
    echo "   ⚠️  SUI 잔액이 부족합니다"
    echo "   Testnet Faucet에서 SUI를 받으세요:"
    echo "   https://discord.com/channels/916379725201563759/971488439931392130"
    echo "   또는: curl --location --request POST 'https://faucet.testnet.sui.io/gas' --header 'Content-Type: application/json' --data-raw '{\"FixedAmountRequest\":{\"recipient\":\"$ACTIVE_ADDRESS\"}}'"
    read -p "   SUI를 받으신 후 Enter를 누르세요..."
fi

# 5. Move 계약 파일 확인
echo "5. Move 계약 파일 확인..."
if [ ! -f "contracts/k8s_nautilus_verification.move" ]; then
    echo "   ❌ Move 계약 파일이 없습니다: contracts/k8s_nautilus_verification.move"
    exit 1
else
    echo "   ✅ Move 계약 파일 존재"
fi

# 6. Move.toml 파일 생성
echo "6. Move.toml 설정 파일 생성..."
cat > contracts/Move.toml << EOF
[package]
name = "k3s_daas"
version = "1.0.0"
edition = "2024.beta"

[dependencies]
Sui = { git = "https://github.com/MystenLabs/sui.git", subdir = "crates/sui-framework/packages/sui-framework", rev = "testnet" }

[addresses]
k3s_daas = "0x0"
EOF
echo "   ✅ Move.toml 파일 생성됨"

# 7. Move 계약 빌드 테스트
echo "7. Move 계약 빌드 테스트..."
cd contracts
if sui move build --dump-bytecode-as-base64; then
    echo "   ✅ Move 계약 빌드 성공"
else
    echo "   ❌ Move 계약 빌드 실패"
    cd ..
    exit 1
fi
cd ..

# 8. Move 계약 배포
echo "8. Move 계약 배포 중..."
echo "   배포 명령어: sui client publish contracts --gas-budget 20000000"
DEPLOY_RESULT=$(sui client publish contracts --gas-budget 20000000 2>&1)
echo "$DEPLOY_RESULT"

# 9. Package ID 추출
PACKAGE_ID=$(echo "$DEPLOY_RESULT" | grep -o "Created Objects:" -A 10 | grep "PackageID" | grep -o "0x[a-f0-9]\{64\}" | head -1)
if [ -n "$PACKAGE_ID" ]; then
    echo "   ✅ 계약 배포 성공!"
    echo "   📦 Package ID: $PACKAGE_ID"

    # Package ID를 환경변수 파일에 저장
    echo "SUI_PACKAGE_ID=$PACKAGE_ID" > .env
    echo "   💾 Package ID가 .env 파일에 저장됨"

    # Move 계약 호출 테스트
    echo "9. Move 계약 호출 테스트..."
    echo "   테스트 명령어 (수동 실행):"
    echo "   sui client call --package $PACKAGE_ID --module nautilus_verification --function get_verified_clusters_count"

else
    echo "   ❌ Package ID를 찾을 수 없습니다"
    echo "   배포 결과를 확인하세요"
fi

echo ""
echo "🎯 다음 단계:"
echo "   1. .env 파일에서 SUI_PACKAGE_ID 확인"
echo "   2. nautilus-tee 설정에 Package ID 추가"
echo "   3. K3s 클러스터 검증 테스트"
echo ""
echo "🌊 Sui Explorer에서 확인:"
echo "   https://testnet.suivision.xyz/package/$PACKAGE_ID"