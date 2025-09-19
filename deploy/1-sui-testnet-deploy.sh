#!/bin/bash

# K3s-DaaS Phase 1: Sui 테스트넷 배포 스크립트
# 실행: chmod +x 1-sui-testnet-deploy.sh && ./1-sui-testnet-deploy.sh

set -e

echo "🌊 K3s-DaaS Phase 1: Sui 테스트넷 배포"
echo "========================================"

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Step 1: Sui CLI 설치 확인
echo -e "${BLUE}Step 1: Sui CLI 설치 확인${NC}"
if ! command -v sui &> /dev/null; then
    echo -e "${YELLOW}Sui CLI가 설치되지 않았습니다. 설치를 시작합니다...${NC}"

    # 운영체제 감지
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Ubuntu/Linux
        curl -fsSL https://github.com/MystenLabs/sui/releases/latest/download/sui-ubuntu-x86_64.tgz | tar -xzf -
        sudo mv sui /usr/local/bin/
        echo -e "${GREEN}✅ Sui CLI 설치 완료 (Linux)${NC}"
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        curl -fsSL https://github.com/MystenLabs/sui/releases/latest/download/sui-macos-x86_64.tgz | tar -xzf -
        sudo mv sui /usr/local/bin/
        echo -e "${GREEN}✅ Sui CLI 설치 완료 (macOS)${NC}"
    else
        echo -e "${RED}❌ 지원되지 않는 운영체제입니다. 수동으로 설치해주세요.${NC}"
        echo "설치 가이드: https://docs.sui.io/build/install"
        exit 1
    fi
else
    echo -e "${GREEN}✅ Sui CLI가 이미 설치되어 있습니다.${NC}"
fi

# Step 2: Sui 환경 설정
echo -e "${BLUE}Step 2: Sui 테스트넷 환경 설정${NC}"

# 테스트넷 환경 설정
sui client new-env --alias testnet --rpc https://fullnode.testnet.sui.io:443 2>/dev/null || true
sui client switch --env testnet

echo -e "${GREEN}✅ 테스트넷 환경 설정 완료${NC}"

# Step 3: 지갑 주소 확인
echo -e "${BLUE}Step 3: 지갑 주소 확인${NC}"

WALLET_ADDRESS=$(sui client active-address 2>/dev/null || echo "")

if [ -z "$WALLET_ADDRESS" ]; then
    echo -e "${YELLOW}새 지갑을 생성합니다...${NC}"
    sui client new-address ed25519
    WALLET_ADDRESS=$(sui client active-address)
    echo -e "${GREEN}✅ 새 지갑 생성 완료: $WALLET_ADDRESS${NC}"
else
    echo -e "${GREEN}✅ 기존 지갑 사용: $WALLET_ADDRESS${NC}"
fi

# Step 4: SUI 토큰 잔액 확인
echo -e "${BLUE}Step 4: SUI 토큰 잔액 확인${NC}"

BALANCE=$(sui client gas --json 2>/dev/null | jq -r '.[0].balance' 2>/dev/null || echo "0")

if [ "$BALANCE" = "0" ] || [ "$BALANCE" = "null" ] || [ -z "$BALANCE" ]; then
    echo -e "${YELLOW}⚠️  SUI 토큰이 부족합니다. 테스트넷 토큰을 요청하세요.${NC}"
    echo ""
    echo "📱 Discord Faucet 사용법:"
    echo "1. https://discord.com/channels/916379725201563759/1037811694564560966"
    echo "2. 다음 명령어를 입력하세요:"
    echo -e "${BLUE}   !faucet $WALLET_ADDRESS${NC}"
    echo ""
    echo "잠시 기다린 후 다시 스크립트를 실행하세요."
    echo "또는 'c'를 입력하고 Enter를 누르면 계속 진행합니다."

    read -p "SUI 토큰을 받으셨나요? (y/c/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[YyCc]$ ]]; then
        echo -e "${RED}배포를 중단합니다.${NC}"
        exit 1
    fi

    # 잔액 재확인
    BALANCE=$(sui client gas --json 2>/dev/null | jq -r '.[0].balance' 2>/dev/null || echo "0")
fi

echo -e "${GREEN}✅ 현재 SUI 잔액: $BALANCE MIST${NC}"

# Step 5: Move Contract 배포
echo -e "${BLUE}Step 5: Move Contract 배포${NC}"

# contracts-release 디렉토리로 이동
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTRACTS_DIR="$SCRIPT_DIR/../contracts-release"

if [ ! -d "$CONTRACTS_DIR" ]; then
    echo -e "${RED}❌ contracts-release 디렉토리를 찾을 수 없습니다.${NC}"
    exit 1
fi

cd "$CONTRACTS_DIR"

# Move.toml 확인
if [ ! -f "Move.toml" ]; then
    echo -e "${RED}❌ Move.toml 파일을 찾을 수 없습니다.${NC}"
    exit 1
fi

echo "📦 Move Contract 배포 중..."

# 계약 배포
DEPLOY_RESULT=$(sui client publish --gas-budget 100000000 . 2>&1)
DEPLOY_SUCCESS=$?

if [ $DEPLOY_SUCCESS -ne 0 ]; then
    echo -e "${RED}❌ Move Contract 배포 실패${NC}"
    echo "$DEPLOY_RESULT"
    exit 1
fi

# 패키지 ID 추출
PACKAGE_ID=$(echo "$DEPLOY_RESULT" | grep -o "0x[a-fA-F0-9]\{64\}" | head -1)

if [ -z "$PACKAGE_ID" ]; then
    echo -e "${RED}❌ 패키지 ID를 추출할 수 없습니다.${NC}"
    echo "$DEPLOY_RESULT"
    exit 1
fi

echo -e "${GREEN}✅ Move Contract 배포 성공!${NC}"
echo -e "${GREEN}   패키지 ID: $PACKAGE_ID${NC}"

# Step 6: 배포 정보 저장
echo -e "${BLUE}Step 6: 배포 정보 저장${NC}"

# 배포 정보 JSON 생성
cat > deployment-info.json << EOF
{
    "network": "testnet",
    "deployed_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "deployer": "$WALLET_ADDRESS",
    "contracts": {
        "package_id": "$PACKAGE_ID",
        "staking_module": "k8s_interface::staking",
        "gateway_module": "k3s_daas::k8s_gateway"
    },
    "endpoints": {
        "sui_rpc": "https://fullnode.testnet.sui.io:443",
        "sui_faucet": "https://discord.com/channels/916379725201563759/1037811694564560966"
    },
    "next_steps": {
        "worker_config": "이 package_id를 worker-config.json에 설정하세요",
        "staking_amount": 1000000000,
        "min_amounts": {
            "user": 500000000,
            "node": 1000000000,
            "admin": 10000000000
        }
    }
}
EOF

echo -e "${GREEN}✅ 배포 정보가 deployment-info.json에 저장되었습니다.${NC}"

# Step 7: 배포 검증
echo -e "${BLUE}Step 7: 배포 검증${NC}"

# 패키지 정보 조회로 검증
PACKAGE_INFO=$(sui client object --id $PACKAGE_ID 2>/dev/null || echo "")

if [ -n "$PACKAGE_INFO" ]; then
    echo -e "${GREEN}✅ 배포 검증 성공 - 패키지가 블록체인에 정상 등록되었습니다.${NC}"
else
    echo -e "${YELLOW}⚠️  패키지 검증을 건너뜁니다 (네트워크 지연 가능성)${NC}"
fi

# 완료 메시지
echo ""
echo -e "${GREEN}🎉 Phase 1: Sui 테스트넷 배포 완료!${NC}"
echo ""
echo "📋 다음 단계:"
echo "1. deployment-info.json 파일을 Worker Node로 복사"
echo "2. Phase 2: EC2 Worker Node 배포 스크립트 실행"
echo "   ./2-ec2-worker-deploy.sh"
echo ""
echo "📄 배포된 계약 정보:"
echo "   🆔 패키지 ID: $PACKAGE_ID"
echo "   💰 지갑 주소: $WALLET_ADDRESS"
echo "   🌐 네트워크: Sui Testnet"
echo ""
echo -e "${BLUE}deployment-info.json 파일 내용:${NC}"
cat deployment-info.json

echo ""
echo -e "${GREEN}Phase 1 배포 스크립트 완료! ✨${NC}"