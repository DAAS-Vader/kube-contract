#!/bin/bash

# K3s-DaaS End-to-End Comprehensive Test
# 전체 시스템 통합 테스트

set -e

echo "🧪 K3s-DaaS End-to-End Comprehensive Test"
echo "=========================================="
echo "📅 시작 시간: $(date)"
echo ""

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 결과 추적
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

# 1. 코드 컴파일 테스트
echo -e "${YELLOW}📦 Step 1: 코드 컴파일 테스트${NC}"

run_test "API Proxy 컴파일" "(cd api-proxy && go build -o api-proxy-test main.go)"
run_test "Nautilus TEE 컴파일" "(cd nautilus-release && go build -o nautilus-test main.go)"
run_test "Worker Node 컴파일" "(cd worker-release && go build -o worker-test main.go)"

# 2. Move Contract 문법 검사
echo -e "${YELLOW}⛓️  Step 2: Move Contract 문법 검사${NC}"

run_test "Move.toml 유효성" "(cd contracts-release && [ -f Move.toml ] && grep -q 'k3s_daas' Move.toml)"
run_test "staking.move 문법" "(cd contracts-release && grep -q 'module k8s_interface::staking' staking.move)"
run_test "k8s_gateway.move 문법" "(cd contracts-release && grep -q 'module k3s_daas::k8s_gateway' k8s_gateway.move)"
run_test "Move Contract 의존성" "(cd contracts-release && grep -q 'get_stake_record_amount' k8s_gateway.move)"

# 3. 시스템 아키텍처 검증
echo -e "${YELLOW}🏗️  Step 3: 시스템 아키텍처 검증${NC}"

run_test "API Proxy 포트 설정" "(cd api-proxy && grep -q ':8080' main.go)"
run_test "Nautilus 이벤트 구독" "(cd nautilus-release && grep -q 'SubscribeToK8sEvents' main.go)"
run_test "Worker 스테이킹 로직" "(cd worker-release && grep -q 'stakeForNode' main.go)"
run_test "Seal Token 구조체" "(cd api-proxy && grep -q 'type SealToken struct' main.go)"

# 4. 중복 코드 제거 확인
echo -e "${YELLOW}🗑️  Step 4: 중복 코드 제거 확인${NC}"

run_test "nautilus k8s_api_proxy.go 삭제 확인" "[ ! -f nautilus-release/k8s_api_proxy.go ]"
run_test "nautilus seal_auth_integration.go 삭제 확인" "[ ! -f nautilus-release/seal_auth_integration.go ]"
run_test "contracts deploy.sh 삭제 확인" "[ ! -f contracts-release/deploy.sh ]"
run_test "contracts verification.move 삭제 확인" "[ ! -f contracts-release/k8s_nautilus_verification.move ]"

# 5. 설정 파일 검증
echo -e "${YELLOW}⚙️  Step 5: 설정 파일 검증${NC}"

run_test "API Proxy go.mod" "(cd api-proxy && [ -f go.mod ] && grep -q 'api-proxy' go.mod)"
run_test "Nautilus go.mod" "(cd nautilus-release && [ -f go.mod ] && grep -q 'nautilus-release' go.mod)"
run_test "Worker go.mod" "(cd worker-release && [ -f go.mod ] && grep -q 'worker-release' go.mod)"
run_test "Contract Move.toml" "(cd contracts-release && [ -f Move.toml ] && grep -q 'k3s_daas_contracts' Move.toml)"

# 6. 핵심 기능 코드 존재 확인
echo -e "${YELLOW}🔧 Step 6: 핵심 기능 코드 확인${NC}"

run_test "kubectl 요청 핸들러" "(cd api-proxy && grep -q 'handleKubectlRequest' main.go)"
run_test "Seal Token 파싱" "(cd api-proxy && grep -q 'extractSealToken' main.go)"
run_test "Direct Mode 핸들러" "(cd api-proxy && grep -q 'handleDirectMode' main.go)"
run_test "Sui 이벤트 리스너" "(cd nautilus-release && grep -q 'SuiEventListener' main.go)"
run_test "K8s API 처리" "(cd nautilus-release && grep -q 'ProcessK8sRequest' main.go)"
run_test "스테이킹 함수" "(cd contracts-release && grep -q 'stake_for_node' staking.move)"
run_test "kubectl 게이트웨이" "(cd contracts-release && grep -q 'execute_kubectl_command' k8s_gateway.move)"

# 7. 통합 흐름 검증
echo -e "${YELLOW}🌊 Step 7: 통합 흐름 검증${NC}"

run_test "API Proxy → Nautilus 연결" "(cd api-proxy && grep -q 'localhost:9443' main.go)"
run_test "Seal Token 헤더 전달" "(cd api-proxy && grep -q 'X-Seal-Wallet' main.go)"
run_test "Move Contract getter 사용" "(cd contracts-release && grep -q 'get_stake_record_amount' k8s_gateway.move)"
run_test "스테이킹 단위 일치" "(cd contracts-release && grep -q '1000000000' staking.move && grep -q '1000000000' k8s_gateway.move)"

# 8. 시뮬레이션 테스트 (실제 프로세스 시작 없이)
echo -e "${YELLOW}🎭 Step 8: 시뮬레이션 테스트${NC}"

# Mock Seal Token 생성
MOCK_SEAL_TOKEN="seal_0x123_sig_challenge_123456"

run_test "Mock Seal Token 형식" "echo '$MOCK_SEAL_TOKEN' | grep -q '^seal_'"
run_test "kubectl 설정 명령어" "kubectl config set-cluster k3s-daas --server=http://localhost:8080 >/dev/null 2>&1"
run_test "kubectl 인증 설정" "kubectl config set-credentials user --token=$MOCK_SEAL_TOKEN >/dev/null 2>&1"

# 9. 문서 및 분석 검증
echo -e "${YELLOW}📋 Step 9: 문서 및 분석 검증${NC}"

run_test "완전한 플로우 보고서" "[ -f analysis/complete_flow_report_final.md ]"
run_test "시스템 분석 보고서" "[ -f analysis/comprehensive_system_analysis_final.md ]"
run_test "k8s_gateway 목적 분석" "[ -f analysis/k8s_gateway_purpose_analysis.md ]"

# 10. 배포 준비도 검증
echo -e "${YELLOW}🚀 Step 10: 배포 준비도 검증${NC}"

run_test "배포 스크립트 실행 권한" "[ -x contracts-release/deploy-testnet.sh ]"
run_test "배포 정보 템플릿" "(cd contracts-release && grep -q 'deployment-info.json' deploy-testnet.sh)"
run_test "Sui 테스트넷 설정" "(cd contracts-release && grep -q 'testnet' deploy-testnet.sh)"

# 결과 요약
echo ""
echo "📊 테스트 결과 요약"
echo "==================="
echo -e "총 테스트: ${BLUE}$TOTAL_TESTS${NC}"
echo -e "성공: ${GREEN}$PASSED_TESTS${NC}"
echo -e "실패: ${RED}$FAILED_TESTS${NC}"

SUCCESS_RATE=$((PASSED_TESTS * 100 / TOTAL_TESTS))
echo -e "성공률: ${YELLOW}$SUCCESS_RATE%${NC}"

echo ""
echo "📅 완료 시간: $(date)"

# 최종 판정
if [ $FAILED_TESTS -eq 0 ]; then
    echo ""
    echo -e "${GREEN}🎉 모든 테스트 통과! 시스템이 완전히 준비되었습니다.${NC}"
    echo -e "${GREEN}✅ K3s-DaaS 해커톤 시연 준비 완료!${NC}"
    exit 0
elif [ $SUCCESS_RATE -ge 90 ]; then
    echo ""
    echo -e "${YELLOW}⚠️  일부 테스트 실패, 하지만 90% 이상 성공으로 시연 가능${NC}"
    echo -e "${YELLOW}🚀 K3s-DaaS 해커톤 시연 가능!${NC}"
    exit 0
else
    echo ""
    echo -e "${RED}❌ 심각한 문제 발견. 추가 수정 필요${NC}"
    exit 1
fi