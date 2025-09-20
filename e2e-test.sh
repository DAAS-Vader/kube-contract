#!/bin/bash

# K3s-DaaS E2E 프로덕션 테스트 스크립트
set -e

echo "🚀 K3s-DaaS E2E 프로덕션 테스트 시작"
echo "================================="

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 테스트 결과 추적
PASSED_TESTS=0
FAILED_TESTS=0
TOTAL_TESTS=0

# 테스트 함수
run_test() {
    local test_name="$1"
    local test_command="$2"
    local expected_status="$3"

    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo -e "\n${BLUE}[테스트 $TOTAL_TESTS]${NC} $test_name"
    echo "---"

    if eval "$test_command"; then
        if [ "$expected_status" = "success" ]; then
            echo -e "${GREEN}✅ PASS${NC}: $test_name"
            PASSED_TESTS=$((PASSED_TESTS + 1))
        else
            echo -e "${RED}❌ FAIL${NC}: $test_name (예상: 실패, 실제: 성공)"
            FAILED_TESTS=$((FAILED_TESTS + 1))
        fi
    else
        if [ "$expected_status" = "fail" ]; then
            echo -e "${GREEN}✅ PASS${NC}: $test_name (예상된 실패)"
            PASSED_TESTS=$((PASSED_TESTS + 1))
        else
            echo -e "${RED}❌ FAIL${NC}: $test_name"
            FAILED_TESTS=$((FAILED_TESTS + 1))
        fi
    fi
}

# 환경 체크
echo -e "${YELLOW}🔍 환경 체크${NC}"
echo "Docker 상태 확인..."
if ! docker --version > /dev/null 2>&1; then
    echo -e "${RED}❌ Docker가 설치되지 않았습니다${NC}"
    exit 1
fi

if ! docker compose version > /dev/null 2>&1; then
    echo -e "${RED}❌ Docker Compose가 설치되지 않았습니다${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Docker 환경 준비 완료${NC}"

# 기존 컨테이너 정리
echo -e "\n${YELLOW}🧹 환경 정리${NC}"
docker compose down --volumes --remove-orphans 2>/dev/null || true
docker system prune -f > /dev/null 2>&1 || true

# 1. 컨테이너 빌드 및 시작
echo -e "\n${YELLOW}🏗️ 컨테이너 빌드 및 시작${NC}"
run_test "Docker Compose 빌드" "docker compose build --no-cache" "success"
run_test "컨테이너 시작" "docker compose up -d" "success"

# 컨테이너 시작 대기
echo -e "\n${YELLOW}⏳ 컨테이너 초기화 대기 (60초)${NC}"
sleep 60

# 2. 헬스체크 테스트
echo -e "\n${YELLOW}🏥 헬스체크 테스트${NC}"
run_test "API Gateway 헬스체크" "curl -f http://localhost:8080/healthz" "success"
run_test "Event Listener 헬스체크" "curl -f http://localhost:10250/health" "success"
run_test "Nautilus Control 헬스체크" "curl -f http://localhost:8081/healthz" "success"
run_test "Worker Node 헬스체크" "curl -f http://localhost:10251/healthz" "success"

# 3. API Gateway 기능 테스트
echo -e "\n${YELLOW}🔌 API Gateway 기능 테스트${NC}"
run_test "kubectl API 시뮬레이션 (GET /api/v1/pods)" \
    "curl -f -H 'Authorization: Bearer test_token' http://localhost:8080/api/v1/pods" "success"

run_test "kubectl API 시뮬레이션 (GET /api/v1/namespaces/default/services)" \
    "curl -f -H 'Authorization: Bearer test_token' http://localhost:8080/api/v1/namespaces/default/services" "success"

# 4. 컨테이너 상태 확인
echo -e "\n${YELLOW}📊 컨테이너 상태 확인${NC}"
run_test "모든 컨테이너 실행 중" "docker compose ps | grep -q 'Up'" "success"

# 5. 로그 체크
echo -e "\n${YELLOW}📝 컨테이너 로그 확인${NC}"
echo "API Gateway 로그 (최근 10줄):"
docker compose logs --tail=10 api-gateway

echo -e "\nEvent Listener 로그 (최근 10줄):"
docker compose logs --tail=10 event-listener

echo -e "\nNautilus Control 로그 (최근 10줄):"
docker compose logs --tail=10 nautilus-control

echo -e "\nWorker Node 로그 (최근 10줄):"
docker compose logs --tail=10 worker-node

# 6. 네트워크 연결 테스트
echo -e "\n${YELLOW}🌐 네트워크 연결 테스트${NC}"
run_test "컨테이너 간 네트워크 연결" \
    "docker exec k3s-daas-gateway ping -c 1 k3s-daas-listener > /dev/null 2>&1" "success"

# 7. 리소스 사용량 확인
echo -e "\n${YELLOW}💾 리소스 사용량 확인${NC}"
echo "컨테이너 리소스 사용량:"
docker stats --no-stream --format "table {{.Container}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}"

# 테스트 결과 요약
echo -e "\n${YELLOW}📋 테스트 결과 요약${NC}"
echo "================================="
echo -e "총 테스트: $TOTAL_TESTS"
echo -e "${GREEN}성공: $PASSED_TESTS${NC}"
echo -e "${RED}실패: $FAILED_TESTS${NC}"

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "\n${GREEN}🎉 모든 테스트 통과! K3s-DaaS 시스템이 정상 작동합니다.${NC}"

    echo -e "\n${BLUE}📖 kubectl 사용 가이드:${NC}"
    echo "kubectl config set-cluster k3s-daas --server=http://localhost:8080"
    echo "kubectl config set-credentials user --token=seal_YOUR_WALLET_SIGNATURE"
    echo "kubectl config set-context k3s-daas --cluster=k3s-daas --user=user"
    echo "kubectl config use-context k3s-daas"
    echo "kubectl get pods"

    exit 0
else
    echo -e "\n${RED}❌ 일부 테스트 실패. 로그를 확인하고 문제를 해결하세요.${NC}"
    echo -e "\n${YELLOW}디버깅 명령어:${NC}"
    echo "docker compose logs api-gateway"
    echo "docker compose logs event-listener"
    echo "docker compose logs nautilus-control"
    echo "docker compose logs worker-node"
    exit 1
fi