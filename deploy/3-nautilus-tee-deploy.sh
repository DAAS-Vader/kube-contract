#!/bin/bash

# K3s-DaaS Phase 3: Nautilus TEE (AWS Nitro Enclave) 배포 스크립트
# 실행: chmod +x 3-nautilus-tee-deploy.sh && ./3-nautilus-tee-deploy.sh [WORKER_IP]

set -e

echo "🛡️  K3s-DaaS Phase 3: Nautilus TEE (AWS Nitro Enclave) 배포"
echo "======================================================"

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 매개변수 확인
WORKER_IP="$1"
if [ -z "$WORKER_IP" ]; then
    echo -e "${YELLOW}사용법: $0 [WORKER_NODE_IP]${NC}"
    echo "예시: $0 3.234.123.45"

    # worker-deployment-info.json에서 IP 자동 추출 시도
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    WORKER_INFO="$SCRIPT_DIR/worker-deployment-info.json"
    if [ -f "$WORKER_INFO" ]; then
        WORKER_IP=$(jq -r '.worker_node.public_ip' "$WORKER_INFO" 2>/dev/null || echo "")
        if [ -n "$WORKER_IP" ] && [ "$WORKER_IP" != "null" ]; then
            echo -e "${GREEN}Worker IP를 자동으로 감지했습니다: $WORKER_IP${NC}"
        else
            echo -e "${RED}❌ Worker IP를 지정해주세요.${NC}"
            exit 1
        fi
    else
        echo -e "${RED}❌ Worker IP를 지정해주세요.${NC}"
        exit 1
    fi
fi

# 설정 변수들
AWS_REGION="us-east-1"
INSTANCE_TYPE="m5.large"  # Nitro Enclave 지원
TEE_NAME="k3s-daas-nautilus-tee"

# Step 1: 사전 요구사항 확인
echo -e "${BLUE}Step 1: 사전 요구사항 확인${NC}"

# AWS CLI 확인
if ! command -v aws &> /dev/null; then
    echo -e "${RED}❌ AWS CLI가 설치되지 않았습니다.${NC}"
    exit 1
fi

# jq 확인
if ! command -v jq &> /dev/null; then
    echo -e "${RED}❌ jq가 설치되지 않았습니다.${NC}"
    exit 1
fi

# AWS 설정 확인
AWS_ACCOUNT=$(aws sts get-caller-identity --query Account --output text 2>/dev/null || echo "")
if [ -z "$AWS_ACCOUNT" ]; then
    echo -e "${RED}❌ AWS 자격 증명이 설정되지 않았습니다.${NC}"
    exit 1
fi

echo -e "${GREEN}✅ AWS 계정: $AWS_ACCOUNT${NC}"
echo -e "${GREEN}✅ Worker Node IP: $WORKER_IP${NC}"

# deployment-info.json 확인
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENT_INFO="$SCRIPT_DIR/../contracts-release/deployment-info.json"

if [ ! -f "$DEPLOYMENT_INFO" ]; then
    echo -e "${RED}❌ deployment-info.json을 찾을 수 없습니다.${NC}"
    exit 1
fi

PACKAGE_ID=$(jq -r '.contracts.package_id' "$DEPLOYMENT_INFO")
echo -e "${GREEN}✅ Move Contract 패키지 ID: $PACKAGE_ID${NC}"

# Step 2: TEE Security Group 생성
echo -e "${BLUE}Step 2: TEE Security Group 생성${NC}"

TEE_SG_NAME="k3s-daas-tee-sg"
TEE_SG_ID=$(aws ec2 describe-security-groups --filters "Name=group-name,Values=$TEE_SG_NAME" --region $AWS_REGION --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null || echo "None")

if [ "$TEE_SG_ID" = "None" ] || [ -z "$TEE_SG_ID" ]; then
    echo "새 TEE Security Group을 생성합니다..."

    TEE_SG_ID=$(aws ec2 create-security-group \
        --group-name $TEE_SG_NAME \
        --description "K3s-DaaS Nautilus TEE Security Group" \
        --region $AWS_REGION \
        --query 'GroupId' \
        --output text)

    # SSH 접근 허용
    aws ec2 authorize-security-group-ingress \
        --group-id $TEE_SG_ID \
        --protocol tcp \
        --port 22 \
        --cidr 0.0.0.0/0 \
        --region $AWS_REGION

    # TEE API 포트 허용 (Worker Node에서만)
    aws ec2 authorize-security-group-ingress \
        --group-id $TEE_SG_ID \
        --protocol tcp \
        --port 9443 \
        --cidr $WORKER_IP/32 \
        --region $AWS_REGION

    # TEE API 포트 허용 (모든 곳에서 - 테스트용)
    aws ec2 authorize-security-group-ingress \
        --group-id $TEE_SG_ID \
        --protocol tcp \
        --port 9443 \
        --cidr 0.0.0.0/0 \
        --region $AWS_REGION

    echo -e "${GREEN}✅ TEE Security Group 생성 완료: $TEE_SG_ID${NC}"
else
    echo -e "${GREEN}✅ 기존 TEE Security Group 사용: $TEE_SG_ID${NC}"
fi

# Step 3: Nitro Enclave 지원 EC2 인스턴스 생성
echo -e "${BLUE}Step 3: Nitro Enclave 지원 EC2 인스턴스 생성${NC}"

# 기존 인스턴스 확인
EXISTING_TEE=$(aws ec2 describe-instances \
    --filters "Name=tag:Name,Values=$TEE_NAME" "Name=instance-state-name,Values=running,pending" \
    --region $AWS_REGION \
    --query 'Reservations[].Instances[].InstanceId' \
    --output text 2>/dev/null || echo "")

if [ -n "$EXISTING_TEE" ]; then
    echo -e "${GREEN}✅ 기존 TEE 인스턴스 사용: $EXISTING_TEE${NC}"
    TEE_INSTANCE_ID="$EXISTING_TEE"
else
    echo "새 Nitro Enclave 지원 EC2 인스턴스를 생성합니다..."

    # 최신 Ubuntu 22.04 AMI ID 조회
    AMI_ID=$(aws ec2 describe-images \
        --owners 099720109477 \
        --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*" \
        --region $AWS_REGION \
        --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' \
        --output text)

    echo "Ubuntu 22.04 AMI: $AMI_ID"

    # SSH Key Pair 재사용
    KEY_NAME="k3s-daas-key"

    # Nitro Enclave 지원 인스턴스 생성
    TEE_INSTANCE_ID=$(aws ec2 run-instances \
        --image-id $AMI_ID \
        --instance-type $INSTANCE_TYPE \
        --key-name $KEY_NAME \
        --security-group-ids $TEE_SG_ID \
        --region $AWS_REGION \
        --enclave-options Enabled=true \
        --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$TEE_NAME},{Key=Project,Value=K3s-DaaS-TEE}]" \
        --query 'Instances[0].InstanceId' \
        --output text)

    echo -e "${GREEN}✅ Nitro Enclave EC2 인스턴스 생성 완료: $TEE_INSTANCE_ID${NC}"
fi

# 인스턴스 상태 대기
echo "TEE 인스턴스가 실행 상태가 될 때까지 대기 중..."
aws ec2 wait instance-running --instance-ids $TEE_INSTANCE_ID --region $AWS_REGION

# TEE Public IP 조회
TEE_PUBLIC_IP=$(aws ec2 describe-instances \
    --instance-ids $TEE_INSTANCE_ID \
    --region $AWS_REGION \
    --query 'Reservations[].Instances[].PublicIpAddress' \
    --output text)

echo -e "${GREEN}✅ TEE Public IP: $TEE_PUBLIC_IP${NC}"

# Step 4: SSH Key 및 연결 설정
echo -e "${BLUE}Step 4: SSH 연결 설정${NC}"

KEY_FILE="$HOME/.ssh/k3s-daas-key.pem"

if [ ! -f "$KEY_FILE" ]; then
    echo -e "${RED}❌ SSH 키 파일을 찾을 수 없습니다: $KEY_FILE${NC}"
    echo "Phase 2에서 생성된 키 파일이 필요합니다."
    exit 1
fi

# SSH 연결 대기
echo "SSH 연결이 가능할 때까지 대기 중..."
for i in {1..30}; do
    if ssh -i "$KEY_FILE" -o ConnectTimeout=5 -o StrictHostKeyChecking=no ubuntu@$TEE_PUBLIC_IP "echo 'SSH Connected'" 2>/dev/null; then
        echo -e "${GREEN}✅ TEE SSH 연결 성공${NC}"
        break
    fi
    echo "SSH 연결 시도 $i/30..."
    sleep 10
done

# Step 5: Nitro Enclave 환경 설정
echo -e "${BLUE}Step 5: Nitro Enclave 환경 설정${NC}"

ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$TEE_PUBLIC_IP << 'ENDSSH'
set -e

# 시스템 업데이트
sudo apt-get update
sudo apt-get upgrade -y

# 기본 도구 설치
sudo apt-get install -y curl wget git build-essential jq unzip docker.io

# Docker 설정
sudo usermod -aG docker ubuntu
sudo systemctl enable docker
sudo systemctl start docker

# Nitro Enclave CLI 설치
sudo apt-get install -y aws-nitro-enclaves-cli aws-nitro-enclaves-cli-devel

# Go 설치
GO_VERSION="1.21.3"
wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
rm go${GO_VERSION}.linux-amd64.tar.gz

# Go PATH 설정
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
echo 'export PATH=$PATH:$GOPATH/bin' >> ~/.bashrc

# Nitro Enclave 서비스 설정
sudo systemctl enable nitro-enclaves-allocator
sudo systemctl start nitro-enclaves-allocator

# 리소스 할당 설정
echo 'cpu_count = 2' | sudo tee /etc/nitro_enclaves/allocator.yaml
echo 'memory_mib = 1024' | sudo tee -a /etc/nitro_enclaves/allocator.yaml
sudo systemctl restart nitro-enclaves-allocator

echo "Nitro Enclave 환경 설정 완료"
ENDSSH

echo -e "${GREEN}✅ Nitro Enclave 환경 설정 완료${NC}"

# Step 6: K3s-DaaS 소스 코드 배포
echo -e "${BLUE}Step 6: K3s-DaaS 소스 코드 배포${NC}"

# 소스 코드를 tar로 압축하여 전송
TEMP_TAR="/tmp/k3s-daas-tee-source.tar.gz"
cd "$SCRIPT_DIR/.."
tar --exclude='.git' --exclude='node_modules' --exclude='*.log' -czf "$TEMP_TAR" .

echo "소스 코드를 TEE Node로 전송 중..."
scp -i "$KEY_FILE" -o StrictHostKeyChecking=no "$TEMP_TAR" ubuntu@$TEE_PUBLIC_IP:~/

# 원격에서 압축 해제 및 설정
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$TEE_PUBLIC_IP << ENDSSH
set -e

# 소스 코드 압축 해제
tar -xzf k3s-daas-tee-source.tar.gz
rm k3s-daas-tee-source.tar.gz

# Go 모듈 다운로드
cd dsaas/nautilus-release
export PATH=\$PATH:/usr/local/go/bin
go mod tidy

echo "TEE 소스 코드 설정 완료"
ENDSSH

echo -e "${GREEN}✅ TEE 소스 코드 배포 완료${NC}"

# Step 7: Nautilus TEE Enclave 이미지 빌드
echo -e "${BLUE}Step 7: Nautilus TEE Enclave 이미지 빌드${NC}"

ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$TEE_PUBLIC_IP << ENDSSH
set -e
cd dsaas/nautilus-release

# Enclave용 Dockerfile 생성
cat > Dockerfile.enclave << EOF
FROM amazonlinux:2

# 기본 패키지 설치
RUN yum update -y && yum install -y \\
    golang \\
    git \\
    ca-certificates \\
    && yum clean all

# 작업 디렉토리 설정
WORKDIR /app

# Go 모듈 파일들 먼저 복사
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사
COPY . .

# 애플리케이션 빌드
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o nautilus-tee main.go

# 설정 파일 생성
RUN echo '{"sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443", "listen_port": 9443, "worker_ip": "$WORKER_IP"}' > config.json

# 포트 노출
EXPOSE 9443

# 실행
CMD ["./nautilus-tee"]
EOF

# Docker 그룹 권한 적용 (재로그인 효과)
newgrp docker << 'DOCKERCMD'
# Enclave 이미지 빌드
docker build -f Dockerfile.enclave -t nautilus-tee-enclave .

# Enclave 이미지 파일로 변환
nitro-cli build-enclave --docker-uri nautilus-tee-enclave:latest --output-file nautilus-tee.eif
DOCKERCMD

echo "Enclave 이미지 빌드 완료"
ENDSSH

echo -e "${GREEN}✅ Nautilus TEE Enclave 이미지 빌드 완료${NC}"

# Step 8: Enclave 실행 및 서비스 등록
echo -e "${BLUE}Step 8: Enclave 실행 및 서비스 등록${NC}"

ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$TEE_PUBLIC_IP << 'ENDSSH'
set -e

# Enclave 시작 스크립트 생성
sudo tee /usr/local/bin/start-nautilus-enclave.sh > /dev/null << 'EOF'
#!/bin/bash
cd /home/ubuntu/dsaas/nautilus-release

# 기존 Enclave 정리
nitro-cli terminate-enclave --all 2>/dev/null || true
sleep 2

# 새 Enclave 실행
nitro-cli run-enclave \
    --cpu-count 2 \
    --memory 1024 \
    --eif-path nautilus-tee.eif \
    --debug-mode \
    --enclave-cid 16
EOF

sudo chmod +x /usr/local/bin/start-nautilus-enclave.sh

# Enclave 정지 스크립트 생성
sudo tee /usr/local/bin/stop-nautilus-enclave.sh > /dev/null << 'EOF'
#!/bin/bash
nitro-cli terminate-enclave --all
EOF

sudo chmod +x /usr/local/bin/stop-nautilus-enclave.sh

# systemd 서비스 생성
sudo tee /etc/systemd/system/nautilus-tee.service > /dev/null << 'EOF'
[Unit]
Description=Nautilus TEE Enclave
After=nitro-enclaves-allocator.service docker.service
Requires=nitro-enclaves-allocator.service docker.service

[Service]
Type=forking
User=root
ExecStart=/usr/local/bin/start-nautilus-enclave.sh
ExecStop=/usr/local/bin/stop-nautilus-enclave.sh
Restart=always
RestartSec=10
TimeoutStartSec=60
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
EOF

# systemd 데몬 재로드 및 서비스 활성화
sudo systemctl daemon-reload
sudo systemctl enable nautilus-tee

echo "systemd 서비스 생성 완료"
ENDSSH

echo -e "${GREEN}✅ Enclave 서비스 설정 완료${NC}"

# Step 9: Enclave 시작 및 상태 확인
echo -e "${BLUE}Step 9: Enclave 시작 및 상태 확인${NC}"

ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$TEE_PUBLIC_IP << 'ENDSSH'
set -e

# 서비스 시작
sudo systemctl start nautilus-tee

# 잠시 대기
sleep 10

# Enclave 상태 확인
echo "=== Enclave 상태 ==="
nitro-cli describe-enclaves || echo "Enclave 상태 확인 실패"

# 서비스 상태 확인
echo -e "\n=== TEE 서비스 상태 ==="
sudo systemctl is-active nautilus-tee || true
sudo systemctl status nautilus-tee --no-pager -l || true

# TEE API 헬스체크 (내부)
echo -e "\n=== TEE API 헬스체크 ==="
curl -k https://localhost:9443/healthz 2>/dev/null && echo " - TEE API 정상" || echo " - TEE API 확인 필요"
ENDSSH

echo -e "${GREEN}✅ Enclave 시작 완료${NC}"

# Step 10: Worker Node 설정 업데이트
echo -e "${BLUE}Step 10: Worker Node 설정 업데이트${NC}"

echo "Worker Node의 nautilus_endpoint를 업데이트합니다..."

# Worker Node의 설정 파일 업데이트
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$WORKER_IP << ENDSSH
# worker-config.json 업데이트
jq '.nautilus_endpoint = "https://$TEE_PUBLIC_IP:9443"' dsaas/worker-config.json > tmp.json && mv tmp.json dsaas/worker-config.json

# API Proxy 설정도 업데이트 (main.go의 nautilus endpoint)
sed -i 's/localhost:9443/$TEE_PUBLIC_IP:9443/g' dsaas/api-proxy/main.go

# 서비스 재시작
sudo systemctl restart k3s-daas-api-proxy
sudo systemctl restart k3s-daas-worker

echo "Worker Node 설정 업데이트 완료"
ENDSSH

echo -e "${GREEN}✅ Worker Node 설정 업데이트 완료${NC}"

# Step 11: 네트워크 연결 테스트
echo -e "${BLUE}Step 11: 네트워크 연결 테스트${NC}"

echo "Worker Node에서 TEE 연결 테스트..."
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$WORKER_IP << ENDSSH
# TEE 연결 테스트
echo "TEE 헬스체크 테스트:"
curl -k https://$TEE_PUBLIC_IP:9443/healthz 2>/dev/null && echo " - Worker → TEE 연결 성공" || echo " - Worker → TEE 연결 확인 필요"

# API Proxy 헬스체크
echo "API Proxy 헬스체크 테스트:"
curl http://localhost:8080/healthz 2>/dev/null && echo " - API Proxy 정상" || echo " - API Proxy 확인 필요"
ENDSSH

echo -e "${GREEN}✅ 네트워크 연결 테스트 완료${NC}"

# Step 12: 배포 정보 저장
echo -e "${BLUE}Step 12: 배포 정보 저장${NC}"

cat > "$SCRIPT_DIR/tee-deployment-info.json" << EOF
{
    "deployment_date": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "tee_node": {
        "instance_id": "$TEE_INSTANCE_ID",
        "public_ip": "$TEE_PUBLIC_IP",
        "instance_type": "$INSTANCE_TYPE",
        "region": "$AWS_REGION",
        "name": "$TEE_NAME"
    },
    "enclave": {
        "enabled": true,
        "cpu_count": 2,
        "memory_mib": 1024,
        "cid": 16,
        "image_file": "/home/ubuntu/dsaas/nautilus-release/nautilus-tee.eif"
    },
    "services": {
        "tee_api": {
            "port": 9443,
            "endpoint": "https://$TEE_PUBLIC_IP:9443",
            "health_check": "https://$TEE_PUBLIC_IP:9443/healthz"
        }
    },
    "connections": {
        "worker_node_ip": "$WORKER_IP",
        "worker_to_tee": "https://$TEE_PUBLIC_IP:9443"
    },
    "ssh_access": {
        "key_file": "$KEY_FILE",
        "command": "ssh -i $KEY_FILE ubuntu@$TEE_PUBLIC_IP"
    },
    "management_commands": {
        "check_enclave": "nitro-cli describe-enclaves",
        "check_service": "sudo systemctl status nautilus-tee",
        "restart_service": "sudo systemctl restart nautilus-tee",
        "view_logs": "sudo journalctl -u nautilus-tee -f"
    },
    "next_steps": [
        "Phase 4: 전체 시스템 테스트",
        "kubectl 설정 및 테스트",
        "스테이킹 실행 및 검증"
    ]
}
EOF

echo -e "${GREEN}✅ TEE 배포 정보가 tee-deployment-info.json에 저장되었습니다.${NC}"

# 완료 메시지
echo ""
echo -e "${GREEN}🎉 Phase 3: Nautilus TEE (AWS Nitro Enclave) 배포 완료!${NC}"
echo ""
echo "📋 배포 결과:"
echo "   🛡️  TEE 인스턴스 ID: $TEE_INSTANCE_ID"
echo "   🌐 TEE Public IP: $TEE_PUBLIC_IP"
echo "   🔑 SSH 접속: ssh -i $KEY_FILE ubuntu@$TEE_PUBLIC_IP"
echo "   🔒 TEE API: https://$TEE_PUBLIC_IP:9443"
echo "   🔗 Worker 연결: $WORKER_IP → $TEE_PUBLIC_IP:9443"
echo ""
echo "📋 다음 단계:"
echo "1. TEE 헬스체크: curl -k https://$TEE_PUBLIC_IP:9443/healthz"
echo "2. Worker → TEE 연결 테스트: Phase 4 스크립트 실행"
echo "   ./4-system-integration-test.sh"
echo ""
echo "🔧 TEE 관리 명령어:"
echo "   nitro-cli describe-enclaves"
echo "   sudo systemctl status nautilus-tee"
echo "   sudo systemctl restart nautilus-tee"
echo "   sudo journalctl -u nautilus-tee -f"
echo ""
echo "🔍 Enclave 디버깅:"
echo "   nitro-cli console --enclave-id \$(nitro-cli describe-enclaves | jq -r '.[0].EnclaveID')"
echo ""
echo -e "${GREEN}Phase 3 배포 스크립트 완료! ✨${NC}"

# 정리
rm -f "$TEMP_TAR"