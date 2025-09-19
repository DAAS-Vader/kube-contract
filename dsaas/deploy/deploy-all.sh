#!/bin/bash

# K3s-DaaS 전체 자동 배포 마스터 스크립트
# 실행: chmod +x deploy-all.sh && ./deploy-all.sh

set -e

echo "🚀 K3s-DaaS 전체 자동 배포 시작"
echo "=============================="

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 스크립트 디렉토리
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 로그 파일
LOG_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOG_DIR"
DEPLOYMENT_LOG="$LOG_DIR/deployment-$(date +%Y%m%d-%H%M%S).log"

# 로그 함수
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" | tee -a "$DEPLOYMENT_LOG"
}

# 에러 핸들링
error_exit() {
    echo -e "${RED}❌ 오류: $1${NC}" | tee -a "$DEPLOYMENT_LOG"
    echo "배포 로그: $DEPLOYMENT_LOG"
    exit 1
}

# 성공 메시지
success() {
    echo -e "${GREEN}✅ $1${NC}" | tee -a "$DEPLOYMENT_LOG"
}

# 경고 메시지
warning() {
    echo -e "${YELLOW}⚠️  $1${NC}" | tee -a "$DEPLOYMENT_LOG"
}

# 정보 메시지
info() {
    echo -e "${BLUE}ℹ️  $1${NC}" | tee -a "$DEPLOYMENT_LOG"
}

# 배너 출력
cat << 'EOF'
 _  _____ ____        ____             ____
| |/ /____|  _ \      |  _ \  __ _  __ / ___|
| ' /|___ \| |_) |____| | | |/ _` |/ _` \___ \
| . \ ___) |  _ <_____| |_| | (_| | (_| |___) |
|_|\_\____/|_| \_\    |____/ \__,_|\__,_|____/

Kubernetes Decentralized as a Service
Powered by Sui Blockchain + AWS Nitro Enclaves
EOF

echo ""
log "K3s-DaaS 전체 배포 시작"

# Step 0: 사전 검사
echo -e "${BLUE}Step 0: 사전 검사${NC}"
log "사전 요구사항 검사 시작"

# 필수 도구 확인
REQUIRED_TOOLS=("aws" "jq" "curl" "ssh" "git")
for tool in "${REQUIRED_TOOLS[@]}"; do
    if ! command -v "$tool" &> /dev/null; then
        error_exit "$tool이 설치되지 않았습니다."
    fi
done

# AWS 설정 확인
if ! aws sts get-caller-identity &> /dev/null; then
    error_exit "AWS 자격 증명이 설정되지 않았습니다. 'aws configure'를 실행하세요."
fi

# 스크립트 파일 존재 확인
SCRIPTS=(
    "1-sui-testnet-deploy.sh"
    "2-ec2-worker-deploy.sh"
    "3-nautilus-tee-deploy.sh"
    "4-system-integration-test.sh"
)

for script in "${SCRIPTS[@]}"; do
    if [ ! -f "$SCRIPT_DIR/$script" ]; then
        error_exit "스크립트 파일이 없습니다: $script"
    fi
    chmod +x "$SCRIPT_DIR/$script"
done

success "사전 검사 완료"

# 사용자 확인
echo ""
info "다음 작업이 수행됩니다:"
echo "1. Sui 테스트넷에 Move Contract 배포"
echo "2. EC2 Worker Node 생성 및 설정"
echo "3. AWS Nitro Enclave TEE Node 생성 및 설정"
echo "4. 전체 시스템 통합 테스트"
echo ""
warning "이 과정에서 AWS 리소스가 생성되어 비용이 발생할 수 있습니다."
echo ""

read -p "계속 진행하시겠습니까? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    log "사용자가 배포를 취소했습니다."
    exit 0
fi

# Phase 1: Sui 테스트넷 배포
echo ""
echo -e "${YELLOW}🌊 Phase 1: Sui 테스트넷 Move Contract 배포${NC}"
log "Phase 1 시작: Sui 테스트넷 배포"

if [ -f "$SCRIPT_DIR/../contracts-release/deployment-info.json" ]; then
    warning "기존 deployment-info.json이 발견되었습니다."
    read -p "새로 배포하시겠습니까? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -f "$SCRIPT_DIR/../contracts-release/deployment-info.json"
        info "기존 배포 정보를 삭제했습니다."
    fi
fi

if [ ! -f "$SCRIPT_DIR/../contracts-release/deployment-info.json" ]; then
    log "Sui 테스트넷 배포 스크립트 실행"
    if ! "$SCRIPT_DIR/1-sui-testnet-deploy.sh" 2>&1 | tee -a "$DEPLOYMENT_LOG"; then
        error_exit "Phase 1: Sui 테스트넷 배포 실패"
    fi
    success "Phase 1: Sui 테스트넷 배포 완료"
else
    info "기존 Sui 배포를 사용합니다."
fi

# 배포 정보 확인
if [ ! -f "$SCRIPT_DIR/../contracts-release/deployment-info.json" ]; then
    error_exit "deployment-info.json 파일이 생성되지 않았습니다."
fi

PACKAGE_ID=$(jq -r '.contracts.package_id' "$SCRIPT_DIR/../contracts-release/deployment-info.json")
info "Move Contract 패키지 ID: $PACKAGE_ID"

# Phase 2: EC2 Worker Node 배포
echo ""
echo -e "${YELLOW}🖥️  Phase 2: EC2 Worker Node 배포${NC}"
log "Phase 2 시작: EC2 Worker Node 배포"

if [ -f "$SCRIPT_DIR/worker-deployment-info.json" ]; then
    warning "기존 Worker Node 배포가 발견되었습니다."
    EXISTING_WORKER_IP=$(jq -r '.worker_node.public_ip' "$SCRIPT_DIR/worker-deployment-info.json")
    info "기존 Worker IP: $EXISTING_WORKER_IP"

    read -p "기존 Worker Node를 사용하시겠습니까? (Y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        info "기존 Worker Node를 사용합니다."
        WORKER_IP="$EXISTING_WORKER_IP"
    else
        log "새 Worker Node 배포 시작"
        if ! "$SCRIPT_DIR/2-ec2-worker-deploy.sh" 2>&1 | tee -a "$DEPLOYMENT_LOG"; then
            error_exit "Phase 2: EC2 Worker Node 배포 실패"
        fi
    fi
else
    log "Worker Node 배포 스크립트 실행"
    if ! "$SCRIPT_DIR/2-ec2-worker-deploy.sh" 2>&1 | tee -a "$DEPLOYMENT_LOG"; then
        error_exit "Phase 2: EC2 Worker Node 배포 실패"
    fi
fi

# Worker 정보 확인
if [ ! -f "$SCRIPT_DIR/worker-deployment-info.json" ]; then
    error_exit "worker-deployment-info.json 파일이 생성되지 않았습니다."
fi

WORKER_IP=$(jq -r '.worker_node.public_ip' "$SCRIPT_DIR/worker-deployment-info.json")
success "Phase 2: EC2 Worker Node 배포 완료 (IP: $WORKER_IP)"

# Phase 3: Nautilus TEE 배포
echo ""
echo -e "${YELLOW}🛡️  Phase 3: Nautilus TEE (AWS Nitro Enclave) 배포${NC}"
log "Phase 3 시작: Nautilus TEE 배포"

if [ -f "$SCRIPT_DIR/tee-deployment-info.json" ]; then
    warning "기존 TEE Node 배포가 발견되었습니다."
    EXISTING_TEE_IP=$(jq -r '.tee_node.public_ip' "$SCRIPT_DIR/tee-deployment-info.json")
    info "기존 TEE IP: $EXISTING_TEE_IP"

    read -p "기존 TEE Node를 사용하시겠습니까? (Y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        info "기존 TEE Node를 사용합니다."
        TEE_IP="$EXISTING_TEE_IP"
    else
        log "새 TEE Node 배포 시작"
        if ! "$SCRIPT_DIR/3-nautilus-tee-deploy.sh" "$WORKER_IP" 2>&1 | tee -a "$DEPLOYMENT_LOG"; then
            error_exit "Phase 3: Nautilus TEE 배포 실패"
        fi
    fi
else
    log "TEE 배포 스크립트 실행"
    if ! "$SCRIPT_DIR/3-nautilus-tee-deploy.sh" "$WORKER_IP" 2>&1 | tee -a "$DEPLOYMENT_LOG"; then
        error_exit "Phase 3: Nautilus TEE 배포 실패"
    fi
fi

# TEE 정보 확인
if [ ! -f "$SCRIPT_DIR/tee-deployment-info.json" ]; then
    error_exit "tee-deployment-info.json 파일이 생성되지 않았습니다."
fi

TEE_IP=$(jq -r '.tee_node.public_ip' "$SCRIPT_DIR/tee-deployment-info.json")
success "Phase 3: Nautilus TEE 배포 완료 (IP: $TEE_IP)"

# Phase 4: 시스템 통합 테스트
echo ""
echo -e "${YELLOW}🧪 Phase 4: 전체 시스템 통합 테스트${NC}"
log "Phase 4 시작: 시스템 통합 테스트"

read -p "전체 시스템 테스트를 실행하시겠습니까? (Y/n): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Nn]$ ]]; then
    log "시스템 통합 테스트 실행"
    if ! "$SCRIPT_DIR/4-system-integration-test.sh" 2>&1 | tee -a "$DEPLOYMENT_LOG"; then
        warning "시스템 통합 테스트에서 일부 문제가 발견되었습니다."
        warning "로그를 확인하여 문제를 해결하세요: $DEPLOYMENT_LOG"
    else
        success "Phase 4: 시스템 통합 테스트 완료"
    fi
else
    info "시스템 통합 테스트를 건너뜁니다."
fi

# 배포 완료 요약
echo ""
echo -e "${GREEN}🎉 K3s-DaaS 전체 배포 완료!${NC}"
echo "=============================="

# 최종 배포 정보 파일 생성
cat > "$SCRIPT_DIR/final-deployment-summary.json" << EOF
{
    "deployment_date": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "status": "completed",
    "components": {
        "sui_contract": {
            "package_id": "$PACKAGE_ID",
            "network": "testnet",
            "rpc": "https://fullnode.testnet.sui.io:443"
        },
        "worker_node": {
            "public_ip": "$WORKER_IP",
            "api_proxy": "http://$WORKER_IP:8080",
            "ssh": "ssh -i ~/.ssh/k3s-daas-key.pem ubuntu@$WORKER_IP"
        },
        "tee_node": {
            "public_ip": "$TEE_IP",
            "tee_api": "https://$TEE_IP:9443",
            "ssh": "ssh -i ~/.ssh/k3s-daas-key.pem ubuntu@$TEE_IP"
        }
    },
    "quick_start": {
        "kubectl_setup": [
            "ssh -i ~/.ssh/k3s-daas-key.pem ubuntu@$WORKER_IP",
            "kubectl config set-cluster k3s-daas --server=http://localhost:8080",
            "kubectl config set-credentials user --token=seal_0x123_sig_challenge_123456",
            "kubectl config set-context k3s-daas --cluster=k3s-daas --user=user",
            "kubectl config use-context k3s-daas",
            "kubectl get nodes"
        ],
        "staking": [
            "cd dsaas/worker-release",
            "go run main.go"
        ]
    },
    "monitoring": {
        "management_script": "./management-commands.sh",
        "deployment_log": "$DEPLOYMENT_LOG"
    }
}
EOF

log "최종 배포 요약 저장: final-deployment-summary.json"

# 요약 정보 출력
echo ""
info "🌊 Sui Testnet Contract: $PACKAGE_ID"
info "🖥️  Worker Node: $WORKER_IP (http://$WORKER_IP:8080)"
info "🛡️  TEE Node: $TEE_IP (https://$TEE_IP:9443)"
echo ""

# 다음 단계 안내
echo -e "${BLUE}📋 다음 단계 - 시스템 사용법${NC}"
echo "=============================="
echo ""
echo "1. Worker Node에 접속하여 kubectl 사용:"
echo -e "   ${GREEN}ssh -i ~/.ssh/k3s-daas-key.pem ubuntu@$WORKER_IP${NC}"
echo -e "   ${GREEN}kubectl get nodes${NC}"
echo -e "   ${GREEN}kubectl get pods${NC}"
echo ""
echo "2. 스테이킹 및 Seal Token 생성:"
echo -e "   ${GREEN}cd dsaas/worker-release${NC}"
echo -e "   ${GREEN}go run main.go${NC}"
echo ""
echo "3. 시스템 모니터링:"
echo -e "   ${GREEN}./management-commands.sh${NC}"
echo ""
echo "4. 테스트 워크로드 배포:"
echo -e "   ${GREEN}kubectl apply -f https://k8s.io/examples/pods/simple-pod.yaml${NC}"
echo ""

# 중요한 파일들 위치 안내
echo -e "${BLUE}📁 중요한 파일들${NC}"
echo "================="
echo "• 배포 로그: $DEPLOYMENT_LOG"
echo "• 배포 요약: $SCRIPT_DIR/final-deployment-summary.json"
echo "• SSH 키: ~/.ssh/k3s-daas-key.pem"
echo "• 관리 스크립트: $SCRIPT_DIR/management-commands.sh"
echo ""

# 비용 안내
echo -e "${YELLOW}💰 AWS 비용 안내${NC}"
echo "=================="
echo "• Worker Node (t3.medium): ~$0.05/hour"
echo "• TEE Node (m5.large): ~$0.10/hour"
echo "• 총 예상 비용: ~$0.15/hour (~$3.6/day)"
echo ""
warning "사용하지 않을 때는 인스턴스를 중지하여 비용을 절약하세요."

# 성공 메시지
echo ""
success "K3s-DaaS 배포가 성공적으로 완료되었습니다!"
success "이제 블록체인 기반 Kubernetes 클러스터를 사용할 수 있습니다!"

log "전체 배포 완료"

echo ""
echo -e "${GREEN}🚀 Happy Kubernetes + Blockchain! 🎉${NC}"