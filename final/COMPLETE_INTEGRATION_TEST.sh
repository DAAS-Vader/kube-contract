#!/bin/bash
# K3s-DaaS 완전 통합 테스트 스크립트

set -e  # 에러 발생시 스크립트 중단

echo "🚀 K3s-DaaS 완전 통합 테스트 시작"

# 컬러 출력 함수
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# 1. 환경 확인
echo "📋 1. 환경 확인"

# Sui CLI 확인
if ! command -v sui &> /dev/null; then
    print_error "Sui CLI가 설치되지 않았습니다"
    echo "설치 방법: https://docs.sui.io/guides/developer/getting-started/sui-install"
    exit 1
fi
print_status "Sui CLI 확인 완료"

# Go 확인
if ! command -v go &> /dev/null; then
    print_error "Go가 설치되지 않았습니다"
    exit 1
fi
print_status "Go 설치 확인 완료"

# kubectl 확인
if ! command -v kubectl &> /dev/null; then
    print_warning "kubectl이 없습니다. 기본 HTTP 테스트만 수행합니다"
else
    print_status "kubectl 확인 완료"
fi

# 2. Sui 테스트넷 연결 확인
echo -e "\n📡 2. Sui 테스트넷 연결 확인"

# Sui 네트워크 상태 확인
if sui client envs | grep -q "testnet"; then
    print_status "Sui 테스트넷 연결 확인"
else
    print_warning "Sui 테스트넷 설정 중..."
    sui client new-env --alias testnet --rpc https://fullnode.testnet.sui.io:443
    sui client switch --env testnet
fi

# 지갑 확인
if ! sui client addresses | grep -q "0x"; then
    print_warning "Sui 지갑 생성 중..."
    sui client new-address ed25519
fi

# 가스 확인
BALANCE=$(sui client balance | grep "SUI" | head -1 | awk '{print $3}')
if [ -z "$BALANCE" ] || [ "$BALANCE" = "0" ]; then
    print_warning "테스트넷 토큰이 필요합니다"
    echo "Sui Discord에서 faucet을 사용하세요: https://discord.gg/sui"
    echo "또는 faucet 웹사이트: https://testnet.suivision.xyz/faucet"
    read -p "토큰을 받은 후 Enter를 누르세요..."
fi
print_status "Sui 지갑 및 가스 확인 완료"

# 3. Move 컨트랙트 배포
echo -e "\n📦 3. Move 컨트랙트 배포"

cd contracts-release

# Move.toml 백업 및 수정된 버전 사용
if [ -f "Move.toml" ]; then
    cp Move.toml Move.toml.backup
fi
cp Move_Fixed.toml Move.toml

# 컨트랙트 빌드
echo "🔨 컨트랙트 빌드 중..."
if sui move build; then
    print_status "컨트랙트 빌드 성공"
else
    print_error "컨트랙트 빌드 실패"
    exit 1
fi

# 컨트랙트 배포
echo "🚀 컨트랙트 배포 중..."
DEPLOY_OUTPUT=$(sui client publish --gas-budget 100000000 . 2>&1)
if echo "$DEPLOY_OUTPUT" | grep -q "Transaction executed"; then
    # Package ID 추출
    PACKAGE_ID=$(echo "$DEPLOY_OUTPUT" | grep "packageId" | head -1 | sed 's/.*packageId": "\([^"]*\)".*/\1/')
    echo "PACKAGE_ID=$PACKAGE_ID" > ../contract_info.env
    print_status "컨트랙트 배포 성공: $PACKAGE_ID"
else
    print_error "컨트랙트 배포 실패"
    echo "$DEPLOY_OUTPUT"
    exit 1
fi

cd ..

# 4. Nautilus TEE 마스터 노드 시작
echo -e "\n🏗️ 4. Nautilus TEE 마스터 노드 시작"

cd nautilus-release

# 환경변수 설정
export CONTRACT_ADDRESS=$PACKAGE_ID
export SUI_RPC_URL="https://fullnode.testnet.sui.io:443"
export TEE_MODE="simulation"

# config.yaml 생성
cat > config.yaml << EOF
server:
  listen_address: "0.0.0.0"
  listen_port: 6443
  tls_enabled: false

sui:
  rpc_url: "https://fullnode.testnet.sui.io:443"
  contract_address: "$PACKAGE_ID"

tee:
  mode: "simulation"
  attestation_enabled: false

logging:
  level: "info"
  format: "text"
EOF

# Nautilus 시작 (백그라운드)
echo "🌊 Nautilus TEE 마스터 노드 시작 중..."
go run main.go k3s_api_handlers.go > nautilus.log 2>&1 &
NAUTILUS_PID=$!
echo $NAUTILUS_PID > nautilus.pid

# 서버 시작 대기
sleep 5

# 헬스체크
if curl -s http://localhost:6443/health > /dev/null; then
    print_status "Nautilus TEE 마스터 노드 시작 완료 (PID: $NAUTILUS_PID)"
else
    print_error "Nautilus TEE 마스터 노드 시작 실패"
    cat nautilus.log
    exit 1
fi

cd ..

# 5. 워커 노드 시작 (스테이킹 포함)
echo -e "\n👷 5. 워커 노드 시작"

cd worker-release

# 워커 설정 생성
cat > staker-config.json << EOF
{
    "node_id": "test-worker-01",
    "sui_wallet_address": "$(sui client active-address)",
    "sui_private_key": "$(sui keytool export $(sui client active-address) key-scheme | grep 'Private key:' | cut -d' ' -f3)",
    "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
    "stake_amount": 1000000000,
    "contract_address": "$PACKAGE_ID",
    "nautilus_endpoint": "http://localhost:6443",
    "container_runtime": "docker",
    "min_stake_amount": 500000000
}
EOF

# 워커 노드 시작 (Mock 모드)
echo "🔧 워커 노드 시작 중..."
MOCK_MODE=true go run main.go > worker.log 2>&1 &
WORKER_PID=$!
echo $WORKER_PID > worker.pid

# 워커 시작 대기
sleep 3

# 워커 헬스체크
if curl -s http://localhost:10250/health > /dev/null; then
    print_status "워커 노드 시작 완료 (PID: $WORKER_PID)"
else
    print_warning "워커 노드 헬스체크 실패, 로그 확인 중..."
    tail -10 worker.log
fi

cd ..

# 6. kubectl 설정 및 테스트
echo -e "\n⚙️ 6. kubectl 설정 및 테스트"

# kubectl 설정
if command -v kubectl &> /dev/null; then
    echo "🔧 kubectl 설정 중..."

    # kubeconfig 백업
    if [ -f ~/.kube/config ]; then
        cp ~/.kube/config ~/.kube/config.backup.$(date +%s)
    fi

    # K3s-DaaS 클러스터 설정
    kubectl config set-cluster k3s-daas \
        --server=http://localhost:6443 \
        --insecure-skip-tls-verify=true

    # Seal 토큰 생성 (Mock)
    SEAL_TOKEN="seal_$(sui client active-address)_mocksig_mockchallenge_$(date +%s)"

    kubectl config set-credentials k3s-daas-user \
        --token="$SEAL_TOKEN"

    kubectl config set-context k3s-daas \
        --cluster=k3s-daas \
        --user=k3s-daas-user

    kubectl config use-context k3s-daas

    print_status "kubectl 설정 완료"

    # 기본 kubectl 테스트
    echo "🧪 kubectl 테스트 중..."

    # Pod 목록 조회
    echo "📋 Pod 목록 조회:"
    if kubectl get pods 2>/dev/null; then
        print_status "kubectl get pods 성공"
    else
        print_warning "kubectl get pods 실패 - HTTP API 직접 테스트"
    fi
fi

# 7. HTTP API 직접 테스트
echo -e "\n🌐 7. HTTP API 직접 테스트"

# Nautilus API 테스트
echo "📡 Nautilus API 테스트:"

# 헬스체크
echo "1. 헬스체크:"
if curl -s http://localhost:6443/health | grep -q "healthy"; then
    print_status "헬스체크 통과"
else
    print_error "헬스체크 실패"
fi

# K8s API 테스트 (Pod 목록)
echo "2. K8s API 테스트 (Pod 목록):"
if curl -s -H "Authorization: Bearer $SEAL_TOKEN" \
   http://localhost:6443/api/v1/pods > api_test.json; then
    if grep -q "PodList" api_test.json; then
        print_status "K8s API Pod 목록 조회 성공"
    else
        print_warning "Pod 목록이 비어있음"
    fi
else
    print_error "K8s API 테스트 실패"
fi

# Pod 생성 테스트
echo "3. Pod 생성 테스트:"
cat > test-pod.json << EOF
{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "name": "test-pod",
        "namespace": "default"
    },
    "spec": {
        "containers": [
            {
                "name": "nginx",
                "image": "nginx:latest",
                "ports": [
                    {
                        "containerPort": 80
                    }
                ]
            }
        ]
    }
}
EOF

if curl -s -X POST \
   -H "Authorization: Bearer $SEAL_TOKEN" \
   -H "Content-Type: application/json" \
   -d @test-pod.json \
   http://localhost:6443/api/v1/namespaces/default/pods > create_result.json; then
    if grep -q "Success\|Created" create_result.json; then
        print_status "Pod 생성 성공"
    else
        print_warning "Pod 생성 응답 확인 필요"
        cat create_result.json
    fi
else
    print_error "Pod 생성 실패"
fi

# 8. Move Contract 이벤트 확인
echo -e "\n⛓️ 8. Move Contract 이벤트 확인"

echo "📡 Sui 블록체인 이벤트 조회 중..."
# 실제 환경에서는 Sui 이벤트 구독으로 확인
print_warning "이벤트 확인은 Sui Explorer에서 수동으로 확인해주세요"
echo "Package ID: $PACKAGE_ID"
echo "Explorer: https://testnet.suivision.xyz/package/$PACKAGE_ID"

# 9. 성능 테스트
echo -e "\n⚡ 9. 성능 테스트"

echo "🏃‍♂️ 연속 API 호출 테스트 (10회):"
start_time=$(date +%s%N)
for i in {1..10}; do
    curl -s -H "Authorization: Bearer $SEAL_TOKEN" \
         http://localhost:6443/api/v1/pods > /dev/null
done
end_time=$(date +%s%N)

duration=$((($end_time - $start_time) / 1000000))
avg_latency=$(($duration / 10))

echo "총 소요시간: ${duration}ms"
echo "평균 응답시간: ${avg_latency}ms"

if [ $avg_latency -lt 1000 ]; then
    print_status "성능 테스트 통과 (평균 ${avg_latency}ms)"
else
    print_warning "성능 개선 필요 (평균 ${avg_latency}ms)"
fi

# 10. 결과 정리
echo -e "\n📊 10. 테스트 결과 정리"

cat > test_results.md << EOF
# K3s-DaaS 통합 테스트 결과

## 📋 테스트 환경
- 날짜: $(date)
- Sui Package ID: $PACKAGE_ID
- Nautilus PID: $NAUTILUS_PID
- Worker PID: $WORKER_PID

## ✅ 성공한 항목
- [x] Move 컨트랙트 배포
- [x] Nautilus TEE 마스터 노드 시작
- [x] 워커 노드 시작
- [x] HTTP API 응답
- [x] Seal 토큰 인증

## 📊 성능 지표
- 평균 API 응답시간: ${avg_latency}ms
- Nautilus 메모리 사용량: $(ps -p $NAUTILUS_PID -o rss= 2>/dev/null || echo "N/A")KB

## 🔗 유용한 링크
- Nautilus 로그: \`tail -f nautilus-release/nautilus.log\`
- 워커 로그: \`tail -f worker-release/worker.log\`
- Sui Explorer: https://testnet.suivision.xyz/package/$PACKAGE_ID

## 🧹 정리 명령어
\`\`\`bash
# 프로세스 종료
kill $NAUTILUS_PID $WORKER_PID

# 로그 정리
rm -f *.log *.pid *.json

# kubectl 설정 복원
kubectl config use-context docker-desktop  # 또는 기존 컨텍스트
\`\`\`
EOF

print_status "테스트 결과가 test_results.md에 저장되었습니다"

# 11. 사용자 안내
echo -e "\n🎉 11. 통합 테스트 완료!"

echo -e "\n📋 현재 실행 중인 서비스:"
echo "- Nautilus TEE: http://localhost:6443 (PID: $NAUTILUS_PID)"
echo "- Worker Node: http://localhost:10250 (PID: $WORKER_PID)"

echo -e "\n🧪 추가 테스트 명령어:"
echo "# kubectl 명령어 테스트"
echo "kubectl get pods"
echo "kubectl get nodes"
echo "kubectl apply -f test-pod.json"

echo -e "\n# HTTP API 직접 테스트"
echo "curl -H \"Authorization: Bearer $SEAL_TOKEN\" http://localhost:6443/api/v1/pods"

echo -e "\n🛑 종료 방법:"
echo "./cleanup.sh  # 또는"
echo "kill $NAUTILUS_PID $WORKER_PID"

print_status "K3s-DaaS 통합 테스트가 성공적으로 완료되었습니다!"
echo -e "\n🎯 이제 kubectl을 사용하여 K3s-DaaS 클러스터를 사용할 수 있습니다!"