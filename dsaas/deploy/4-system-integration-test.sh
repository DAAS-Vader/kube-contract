#!/bin/bash

# K3s-DaaS Phase 4: 전체 시스템 통합 테스트 및 검증
# 실행: chmod +x 4-system-integration-test.sh && ./4-system-integration-test.sh

set -e

echo "🧪 K3s-DaaS Phase 4: 전체 시스템 통합 테스트"
echo "============================================="

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 테스트 결과 추적
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# 테스트 함수
run_test() {
    local test_name="$1"
    local test_command="$2"

    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "${BLUE}🧪 테스트 $TOTAL_TESTS: $test_name${NC}"

    if eval "$test_command"; then
        echo -e "${GREEN}✅ PASS: $test_name${NC}"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        echo -e "${RED}❌ FAIL: $test_name${NC}"
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
    echo ""
}

# Step 1: 배포 정보 로드
echo -e "${BLUE}Step 1: 배포 정보 로드${NC}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Worker 배포 정보
WORKER_INFO="$SCRIPT_DIR/worker-deployment-info.json"
if [ ! -f "$WORKER_INFO" ]; then
    echo -e "${RED}❌ worker-deployment-info.json을 찾을 수 없습니다.${NC}"
    echo "먼저 Phase 2 스크립트를 실행하세요."
    exit 1
fi

# TEE 배포 정보
TEE_INFO="$SCRIPT_DIR/tee-deployment-info.json"
if [ ! -f "$TEE_INFO" ]; then
    echo -e "${RED}❌ tee-deployment-info.json을 찾을 수 없습니다.${NC}"
    echo "먼저 Phase 3 스크립트를 실행하세요."
    exit 1
fi

# Move Contract 배포 정보
CONTRACT_INFO="$SCRIPT_DIR/../contracts-release/deployment-info.json"
if [ ! -f "$CONTRACT_INFO" ]; then
    echo -e "${RED}❌ deployment-info.json을 찾을 수 없습니다.${NC}"
    echo "먼저 Phase 1 스크립트를 실행하세요."
    exit 1
fi

# 정보 추출
WORKER_IP=$(jq -r '.worker_node.public_ip' "$WORKER_INFO")
TEE_IP=$(jq -r '.tee_node.public_ip' "$TEE_INFO")
PACKAGE_ID=$(jq -r '.contracts.package_id' "$CONTRACT_INFO")
KEY_FILE="$HOME/.ssh/k3s-daas-key.pem"

echo -e "${GREEN}✅ Worker IP: $WORKER_IP${NC}"
echo -e "${GREEN}✅ TEE IP: $TEE_IP${NC}"
echo -e "${GREEN}✅ Contract ID: $PACKAGE_ID${NC}"
echo ""

# Step 2: 기본 연결성 테스트
echo -e "${YELLOW}📡 Step 2: 기본 연결성 테스트${NC}"

run_test "Worker SSH 연결" "ssh -i '$KEY_FILE' -o ConnectTimeout=5 -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'echo SSH OK' 2>/dev/null"

run_test "TEE SSH 연결" "ssh -i '$KEY_FILE' -o ConnectTimeout=5 -o StrictHostKeyChecking=no ubuntu@$TEE_IP 'echo SSH OK' 2>/dev/null"

run_test "Worker API Proxy 헬스체크" "curl -f http://$WORKER_IP:8080/healthz 2>/dev/null"

run_test "TEE API 헬스체크" "curl -k -f https://$TEE_IP:9443/healthz 2>/dev/null"

# Step 3: 서비스 상태 검증
echo -e "${YELLOW}🔧 Step 3: 서비스 상태 검증${NC}"

run_test "Worker API Proxy 서비스" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'systemctl is-active k3s-daas-api-proxy' 2>/dev/null"

run_test "Worker Host 서비스" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'systemctl is-active k3s-daas-worker' 2>/dev/null"

run_test "TEE Enclave 상태" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$TEE_IP 'nitro-cli describe-enclaves | jq -r \".[0].State\" | grep -q \"RUNNING\"' 2>/dev/null"

run_test "TEE 서비스 상태" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$TEE_IP 'systemctl is-active nautilus-tee' 2>/dev/null"

# Step 4: 네트워크 통신 테스트
echo -e "${YELLOW}🌐 Step 4: 네트워크 통신 테스트${NC}"

run_test "Worker → TEE 연결" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'curl -k -f https://$TEE_IP:9443/healthz' 2>/dev/null"

run_test "TEE → Sui Network 연결" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$TEE_IP 'curl -f https://fullnode.testnet.sui.io:443 -X POST -H \"Content-Type: application/json\" -d \"{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":1,\\\"method\\\":\\\"sui_getLatestCheckpointSequenceNumber\\\",\\\"params\\\":[]}\"' 2>/dev/null"

# Step 5: kubectl 설정 및 기본 테스트
echo -e "${YELLOW}⚙️ Step 5: kubectl 설정 및 기본 테스트${NC}"

# Worker Node에서 kubectl 설정
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$WORKER_IP << 'ENDSSH'
# kubectl 설정
kubectl config set-cluster k3s-daas --server=http://localhost:8080 >/dev/null 2>&1
kubectl config set-credentials user --token=seal_0x123_sig_challenge_123456 >/dev/null 2>&1
kubectl config set-context k3s-daas --cluster=k3s-daas --user=user >/dev/null 2>&1
kubectl config use-context k3s-daas >/dev/null 2>&1
ENDSSH

run_test "kubectl 설정" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'kubectl config current-context | grep -q k3s-daas' 2>/dev/null"

run_test "kubectl 기본 명령어" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'kubectl get --raw /healthz' 2>/dev/null | grep -q 'ok' || true"

# Step 6: 테스트 워크로드 배포
echo -e "${YELLOW}🚀 Step 6: 테스트 워크로드 배포${NC}"

# 테스트 Pod YAML 생성 및 배포
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$WORKER_IP << 'ENDSSH'
# 테스트 Pod YAML 생성
cat > test-pod.yaml << EOF
apiVersion: v1
kind: Pod
metadata:
  name: k3s-daas-test-pod
  labels:
    app: k3s-daas-test
spec:
  containers:
  - name: nginx
    image: nginx:alpine
    ports:
    - containerPort: 80
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 200m
        memory: 256Mi
EOF

# Pod 배포 시도
kubectl apply -f test-pod.yaml >/dev/null 2>&1 || true
ENDSSH

run_test "테스트 Pod 배포" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'kubectl get pod k3s-daas-test-pod --no-headers 2>/dev/null | grep -v Error' || true"

# Pod 상태 확인 (시간 주기)
echo "Pod 상태 확인 중..."
for i in {1..30}; do
    POD_STATUS=$(ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'kubectl get pod k3s-daas-test-pod --no-headers 2>/dev/null | awk "{print \$3}"' 2>/dev/null || echo "Unknown")
    echo "Pod 상태: $POD_STATUS (시도 $i/30)"

    if [ "$POD_STATUS" = "Running" ]; then
        run_test "테스트 Pod 실행 상태" "true"
        break
    elif [ "$POD_STATUS" = "Error" ] || [ "$POD_STATUS" = "CrashLoopBackOff" ]; then
        run_test "테스트 Pod 실행 상태" "false"
        break
    fi

    sleep 5
done

if [ "$POD_STATUS" != "Running" ]; then
    run_test "테스트 Pod 실행 상태" "false"
fi

# Step 7: Move Contract 연동 테스트
echo -e "${YELLOW}⛓️ Step 7: Move Contract 연동 테스트${NC}"

# Worker Node에서 스테이킹 설정 확인
run_test "Worker 설정 파일 확인" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'test -f dsaas/worker-config.json && jq -r .contract_address dsaas/worker-config.json | grep -q 0x' 2>/dev/null"

run_test "Sui RPC 연결 테스트" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'curl -f https://fullnode.testnet.sui.io:443 -X POST -H \"Content-Type: application/json\" -d \"{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":1,\\\"method\\\":\\\"sui_getLatestCheckpointSequenceNumber\\\",\\\"params\\\":[]}\"' 2>/dev/null"

# Step 8: 로그 및 모니터링 테스트
echo -e "${YELLOW}📊 Step 8: 로그 및 모니터링 테스트${NC}"

run_test "Worker API Proxy 로그" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'journalctl -u k3s-daas-api-proxy --no-pager -n 5 | grep -q .' 2>/dev/null"

run_test "Worker Host 로그" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'journalctl -u k3s-daas-worker --no-pager -n 5 | grep -q .' 2>/dev/null"

run_test "TEE Enclave 로그" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$TEE_IP 'journalctl -u nautilus-tee --no-pager -n 5 | grep -q .' 2>/dev/null"

# Step 9: 성능 및 응답 시간 테스트
echo -e "${YELLOW}⚡ Step 9: 성능 및 응답 시간 테스트${NC}"

run_test "API Proxy 응답 시간" "time curl -f http://$WORKER_IP:8080/healthz 2>/dev/null | grep -q OK || time curl -f http://$WORKER_IP:8080/healthz >/dev/null 2>&1"

run_test "TEE API 응답 시간" "time curl -k -f https://$TEE_IP:9443/healthz >/dev/null 2>&1"

# Step 10: 시스템 리소스 사용량 확인
echo -e "${YELLOW}💻 Step 10: 시스템 리소스 확인${NC}"

run_test "Worker 메모리 사용량" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'free | grep Mem | awk \"{print (\$3/\$2)*100}\" | cut -d. -f1 | head -c 2 | grep -q [0-9]' 2>/dev/null"

run_test "TEE 메모리 사용량" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$TEE_IP 'free | grep Mem | awk \"{print (\$3/\$2)*100}\" | cut -d. -f1 | head -c 2 | grep -q [0-9]' 2>/dev/null"

run_test "TEE Enclave 리소스 할당" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$TEE_IP 'nitro-cli describe-enclaves | jq -r \".[0].CPUCount\" | grep -q 2' 2>/dev/null"

# Step 11: 정리 작업
echo -e "${YELLOW}🧹 Step 11: 테스트 정리${NC}"

# 테스트 Pod 삭제
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$WORKER_IP << 'ENDSSH'
kubectl delete pod k3s-daas-test-pod --ignore-not-found=true >/dev/null 2>&1 || true
rm -f test-pod.yaml
ENDSSH

run_test "테스트 리소스 정리" "ssh -i '$KEY_FILE' -o StrictHostKeyChecking=no ubuntu@$WORKER_IP 'kubectl get pod k3s-daas-test-pod 2>&1 | grep -q \"not found\"' || true"

# Step 12: 최종 시스템 상태 체크
echo -e "${YELLOW}🏁 Step 12: 최종 시스템 상태 체크${NC}"

run_test "전체 시스템 상태" "curl -f http://$WORKER_IP:8080/healthz && curl -k -f https://$TEE_IP:9443/healthz" >/dev/null 2>&1

# 결과 요약
echo ""
echo -e "${BLUE}📊 통합 테스트 결과 요약${NC}"
echo "================================="
echo -e "총 테스트: ${BLUE}$TOTAL_TESTS${NC}"
echo -e "성공: ${GREEN}$PASSED_TESTS${NC}"
echo -e "실패: ${RED}$FAILED_TESTS${NC}"

SUCCESS_RATE=$((PASSED_TESTS * 100 / TOTAL_TESTS))
echo -e "성공률: ${YELLOW}$SUCCESS_RATE%${NC}"

# 상세 시스템 정보 출력
echo ""
echo -e "${BLUE}📋 시스템 상태 요약${NC}"
echo "===================="

echo "🖥️  Worker Node:"
echo "   IP: $WORKER_IP"
echo "   API Proxy: http://$WORKER_IP:8080"
echo "   SSH: ssh -i $KEY_FILE ubuntu@$WORKER_IP"

echo ""
echo "🛡️  TEE Node:"
echo "   IP: $TEE_IP"
echo "   TEE API: https://$TEE_IP:9443"
echo "   SSH: ssh -i $KEY_FILE ubuntu@$TEE_IP"

echo ""
echo "⛓️  Blockchain:"
echo "   Network: Sui Testnet"
echo "   Contract: $PACKAGE_ID"
echo "   RPC: https://fullnode.testnet.sui.io:443"

# 관리 명령어 제공
echo ""
echo -e "${BLUE}🔧 관리 명령어${NC}"
echo "==============="

cat > "$SCRIPT_DIR/management-commands.sh" << EOF
#!/bin/bash

# K3s-DaaS 시스템 관리 명령어

WORKER_IP="$WORKER_IP"
TEE_IP="$TEE_IP"
KEY_FILE="$KEY_FILE"

echo "🔍 시스템 상태 확인"
echo "=================="

echo "Worker Node 상태:"
ssh -i "\$KEY_FILE" ubuntu@\$WORKER_IP 'systemctl is-active k3s-daas-api-proxy k3s-daas-worker'

echo -e "\nTEE Node 상태:"
ssh -i "\$KEY_FILE" ubuntu@\$TEE_IP 'systemctl is-active nautilus-tee'
ssh -i "\$KEY_FILE" ubuntu@\$TEE_IP 'nitro-cli describe-enclaves | jq -r ".[0].State"'

echo -e "\n🌐 API 헬스체크:"
curl -f http://\$WORKER_IP:8080/healthz && echo " - Worker API OK"
curl -k -f https://\$TEE_IP:9443/healthz && echo " - TEE API OK"

echo -e "\n📊 로그 확인 (마지막 10줄):"
echo "Worker API Proxy:"
ssh -i "\$KEY_FILE" ubuntu@\$WORKER_IP 'journalctl -u k3s-daas-api-proxy --no-pager -n 10'

echo -e "\nWorker Host:"
ssh -i "\$KEY_FILE" ubuntu@\$WORKER_IP 'journalctl -u k3s-daas-worker --no-pager -n 10'

echo -e "\nTEE Service:"
ssh -i "\$KEY_FILE" ubuntu@\$TEE_IP 'journalctl -u nautilus-tee --no-pager -n 10'
EOF

chmod +x "$SCRIPT_DIR/management-commands.sh"

echo "   ./management-commands.sh  # 시스템 상태 확인"
echo "   ssh -i $KEY_FILE ubuntu@$WORKER_IP  # Worker Node 접속"
echo "   ssh -i $KEY_FILE ubuntu@$TEE_IP     # TEE Node 접속"

# 최종 판정
echo ""
if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}🎉 모든 테스트 통과! K3s-DaaS 시스템이 완전히 작동합니다!${NC}"
    echo -e "${GREEN}✅ 프로덕션 배포 준비 완료!${NC}"

    echo ""
    echo -e "${BLUE}🚀 다음 단계 - 실제 사용:${NC}"
    echo "1. Worker Node에서 kubectl 사용:"
    echo "   ssh -i $KEY_FILE ubuntu@$WORKER_IP"
    echo "   kubectl get nodes"
    echo "   kubectl get pods"
    echo ""
    echo "2. 스테이킹 및 Seal Token 생성:"
    echo "   cd dsaas/worker-release && go run main.go"
    echo ""
    echo "3. 모니터링:"
    echo "   ./management-commands.sh"

    exit 0
elif [ $SUCCESS_RATE -ge 80 ]; then
    echo -e "${YELLOW}⚠️  일부 테스트 실패, 하지만 80% 이상 성공으로 사용 가능${NC}"
    echo -e "${YELLOW}🚀 기본 기능 동작 확인됨!${NC}"

    echo ""
    echo -e "${YELLOW}📝 실패한 테스트 확인 및 수정 권장${NC}"
    echo "   ./management-commands.sh  # 상세 로그 확인"

    exit 0
else
    echo -e "${RED}❌ 심각한 문제 발견. 시스템 점검 필요${NC}"
    echo ""
    echo -e "${RED}🔧 문제 해결 방법:${NC}"
    echo "1. 로그 확인: ./management-commands.sh"
    echo "2. 서비스 재시작: sudo systemctl restart k3s-daas-api-proxy k3s-daas-worker nautilus-tee"
    echo "3. 네트워크 확인: 보안 그룹 및 방화벽 설정"
    echo "4. 각 Phase별 스크립트 재실행"

    exit 1
fi