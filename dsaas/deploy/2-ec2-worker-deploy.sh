#!/bin/bash

# K3s-DaaS Phase 2: EC2 Worker Node 배포 스크립트
# 실행: chmod +x 2-ec2-worker-deploy.sh && ./2-ec2-worker-deploy.sh

set -e

echo "🖥️  K3s-DaaS Phase 2: EC2 Worker Node 배포"
echo "=========================================="

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 설정 변수들
AWS_REGION="us-east-1"
INSTANCE_TYPE="t3.medium"
WORKER_NAME="k3s-daas-worker-1"

# Step 1: 사전 요구사항 확인
echo -e "${BLUE}Step 1: 사전 요구사항 확인${NC}"

# AWS CLI 확인
if ! command -v aws &> /dev/null; then
    echo -e "${RED}❌ AWS CLI가 설치되지 않았습니다.${NC}"
    echo "설치 가이드: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"
    exit 1
fi

# jq 설치 확인 및 설치
if ! command -v jq &> /dev/null; then
    echo -e "${YELLOW}jq가 설치되지 않았습니다. 설치를 시도합니다...${NC}"
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        sudo apt-get update && sudo apt-get install -y jq
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        brew install jq
    else
        echo -e "${RED}❌ jq를 수동으로 설치해주세요.${NC}"
        exit 1
    fi
fi

# AWS 설정 확인
AWS_ACCOUNT=$(aws sts get-caller-identity --query Account --output text 2>/dev/null || echo "")
if [ -z "$AWS_ACCOUNT" ]; then
    echo -e "${RED}❌ AWS 자격 증명이 설정되지 않았습니다.${NC}"
    echo "aws configure를 실행하여 설정해주세요."
    exit 1
fi

echo -e "${GREEN}✅ AWS 계정: $AWS_ACCOUNT${NC}"

# deployment-info.json 확인
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENT_INFO="$SCRIPT_DIR/../contracts-release/deployment-info.json"

if [ ! -f "$DEPLOYMENT_INFO" ]; then
    echo -e "${RED}❌ deployment-info.json을 찾을 수 없습니다.${NC}"
    echo "먼저 Phase 1 스크립트를 실행하세요: ./1-sui-testnet-deploy.sh"
    exit 1
fi

PACKAGE_ID=$(jq -r '.contracts.package_id' "$DEPLOYMENT_INFO")
echo -e "${GREEN}✅ Move Contract 패키지 ID: $PACKAGE_ID${NC}"

# Step 2: Key Pair 생성 (존재하지 않는 경우)
echo -e "${BLUE}Step 2: SSH Key Pair 확인/생성${NC}"

KEY_NAME="k3s-daas-key"
KEY_FILE="$HOME/.ssh/$KEY_NAME.pem"

# 기존 키 페어 확인
EXISTING_KEY=$(aws ec2 describe-key-pairs --key-names $KEY_NAME --region $AWS_REGION 2>/dev/null || echo "")

if [ -z "$EXISTING_KEY" ]; then
    echo "새 SSH Key Pair를 생성합니다..."
    aws ec2 create-key-pair \
        --key-name $KEY_NAME \
        --region $AWS_REGION \
        --query 'KeyMaterial' \
        --output text > "$KEY_FILE"

    chmod 600 "$KEY_FILE"
    echo -e "${GREEN}✅ SSH Key Pair 생성 완료: $KEY_FILE${NC}"
else
    echo -e "${GREEN}✅ 기존 SSH Key Pair 사용: $KEY_NAME${NC}"
    if [ ! -f "$KEY_FILE" ]; then
        echo -e "${YELLOW}⚠️  개인키 파일이 없습니다. 기존 파일을 사용하거나 새로 생성하세요.${NC}"
    fi
fi

# Step 3: Security Group 생성
echo -e "${BLUE}Step 3: Security Group 생성${NC}"

SG_NAME="k3s-daas-worker-sg"
SG_ID=$(aws ec2 describe-security-groups --filters "Name=group-name,Values=$SG_NAME" --region $AWS_REGION --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null || echo "None")

if [ "$SG_ID" = "None" ] || [ -z "$SG_ID" ]; then
    echo "새 Security Group을 생성합니다..."

    SG_ID=$(aws ec2 create-security-group \
        --group-name $SG_NAME \
        --description "K3s-DaaS Worker Node Security Group" \
        --region $AWS_REGION \
        --query 'GroupId' \
        --output text)

    # SSH 접근 허용
    aws ec2 authorize-security-group-ingress \
        --group-id $SG_ID \
        --protocol tcp \
        --port 22 \
        --cidr 0.0.0.0/0 \
        --region $AWS_REGION

    # API Proxy 포트 허용
    aws ec2 authorize-security-group-ingress \
        --group-id $SG_ID \
        --protocol tcp \
        --port 8080 \
        --cidr 0.0.0.0/0 \
        --region $AWS_REGION

    echo -e "${GREEN}✅ Security Group 생성 완료: $SG_ID${NC}"
else
    echo -e "${GREEN}✅ 기존 Security Group 사용: $SG_ID${NC}"
fi

# Step 4: EC2 인스턴스 생성
echo -e "${BLUE}Step 4: EC2 Worker Node 인스턴스 생성${NC}"

# 기존 인스턴스 확인
EXISTING_INSTANCE=$(aws ec2 describe-instances \
    --filters "Name=tag:Name,Values=$WORKER_NAME" "Name=instance-state-name,Values=running,pending" \
    --region $AWS_REGION \
    --query 'Reservations[].Instances[].InstanceId' \
    --output text 2>/dev/null || echo "")

if [ -n "$EXISTING_INSTANCE" ]; then
    echo -e "${GREEN}✅ 기존 Worker Node 인스턴스 사용: $EXISTING_INSTANCE${NC}"
    INSTANCE_ID="$EXISTING_INSTANCE"
else
    echo "새 EC2 인스턴스를 생성합니다..."

    # 최신 Ubuntu 22.04 AMI ID 조회
    AMI_ID=$(aws ec2 describe-images \
        --owners 099720109477 \
        --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*" \
        --region $AWS_REGION \
        --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' \
        --output text)

    echo "Ubuntu 22.04 AMI: $AMI_ID"

    # 인스턴스 생성
    INSTANCE_ID=$(aws ec2 run-instances \
        --image-id $AMI_ID \
        --instance-type $INSTANCE_TYPE \
        --key-name $KEY_NAME \
        --security-group-ids $SG_ID \
        --region $AWS_REGION \
        --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$WORKER_NAME},{Key=Project,Value=K3s-DaaS}]" \
        --query 'Instances[0].InstanceId' \
        --output text)

    echo -e "${GREEN}✅ EC2 인스턴스 생성 완료: $INSTANCE_ID${NC}"
fi

# 인스턴스 상태 대기
echo "인스턴스가 실행 상태가 될 때까지 대기 중..."
aws ec2 wait instance-running --instance-ids $INSTANCE_ID --region $AWS_REGION

# Public IP 조회
PUBLIC_IP=$(aws ec2 describe-instances \
    --instance-ids $INSTANCE_ID \
    --region $AWS_REGION \
    --query 'Reservations[].Instances[].PublicIpAddress' \
    --output text)

echo -e "${GREEN}✅ Worker Node Public IP: $PUBLIC_IP${NC}"

# Step 5: SSH 연결 대기 및 기본 설정
echo -e "${BLUE}Step 5: SSH 연결 대기 및 초기 설정${NC}"

echo "SSH 연결이 가능할 때까지 대기 중..."
for i in {1..30}; do
    if ssh -i "$KEY_FILE" -o ConnectTimeout=5 -o StrictHostKeyChecking=no ubuntu@$PUBLIC_IP "echo 'SSH Connected'" 2>/dev/null; then
        echo -e "${GREEN}✅ SSH 연결 성공${NC}"
        break
    fi
    echo "SSH 연결 시도 $i/30..."
    sleep 10
done

# 기본 패키지 업데이트 및 설치
echo "기본 패키지 설치 중..."
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$PUBLIC_IP << 'ENDSSH'
set -e

# 시스템 업데이트
sudo apt-get update
sudo apt-get upgrade -y

# 기본 도구 설치
sudo apt-get install -y curl wget git build-essential jq unzip

# Go 설치
GO_VERSION="1.21.3"
wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
rm go${GO_VERSION}.linux-amd64.tar.gz

# Go PATH 설정
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
echo 'export PATH=$PATH:$GOPATH/bin' >> ~/.bashrc

# kubectl 설치
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
rm kubectl

echo "기본 설치 완료"
ENDSSH

echo -e "${GREEN}✅ 기본 패키지 설치 완료${NC}"

# Step 6: K3s-DaaS 소스 코드 배포
echo -e "${BLUE}Step 6: K3s-DaaS 소스 코드 배포${NC}"

# 소스 코드를 tar로 압축하여 전송
TEMP_TAR="/tmp/k3s-daas-source.tar.gz"
cd "$SCRIPT_DIR/.."
tar --exclude='.git' --exclude='node_modules' --exclude='*.log' -czf "$TEMP_TAR" .

echo "소스 코드를 Worker Node로 전송 중..."
scp -i "$KEY_FILE" -o StrictHostKeyChecking=no "$TEMP_TAR" ubuntu@$PUBLIC_IP:~/

# 원격에서 압축 해제 및 설정
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$PUBLIC_IP << ENDSSH
set -e

# 소스 코드 압축 해제
tar -xzf k3s-daas-source.tar.gz
rm k3s-daas-source.tar.gz

# 로그 디렉토리 생성
mkdir -p dsaas/logs

# Go 모듈 다운로드
cd dsaas/api-proxy
export PATH=\$PATH:/usr/local/go/bin
go mod tidy

cd ../worker-release
go mod tidy

cd ../nautilus-release
go mod tidy

echo "소스 코드 설정 완료"
ENDSSH

echo -e "${GREEN}✅ 소스 코드 배포 완료${NC}"

# Step 7: Worker 설정 파일 생성
echo -e "${BLUE}Step 7: Worker 설정 파일 생성${NC}"

# worker-config.json 생성
ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$PUBLIC_IP << ENDSSH
cat > dsaas/worker-config.json << EOF
{
    "contract_address": "$PACKAGE_ID",
    "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
    "stake_amount": 1000000000,
    "node_id": "worker-node-1",
    "nautilus_endpoint": "http://localhost:9443",
    "api_proxy_port": 8080,
    "log_level": "info"
}
EOF
ENDSSH

echo -e "${GREEN}✅ Worker 설정 파일 생성 완료${NC}"

# Step 8: systemd 서비스 생성
echo -e "${BLUE}Step 8: systemd 서비스 생성${NC}"

ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$PUBLIC_IP << 'ENDSSH'
# API Proxy 서비스
sudo tee /etc/systemd/system/k3s-daas-api-proxy.service > /dev/null << EOF
[Unit]
Description=K3s-DaaS API Proxy
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/dsaas/api-proxy
Environment=PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=/usr/local/go/bin/go run main.go
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# Worker Host 서비스
sudo tee /etc/systemd/system/k3s-daas-worker.service > /dev/null << EOF
[Unit]
Description=K3s-DaaS Worker Host
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/dsaas/worker-release
Environment=PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=/usr/local/go/bin/go run main.go
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# systemd 데몬 재로드
sudo systemctl daemon-reload

# 서비스 활성화
sudo systemctl enable k3s-daas-api-proxy
sudo systemctl enable k3s-daas-worker

echo "systemd 서비스 생성 완료"
ENDSSH

echo -e "${GREEN}✅ systemd 서비스 생성 완료${NC}"

# Step 9: 서비스 시작 및 상태 확인
echo -e "${BLUE}Step 9: 서비스 시작 및 상태 확인${NC}"

ssh -i "$KEY_FILE" -o StrictHostKeyChecking=no ubuntu@$PUBLIC_IP << 'ENDSSH'
# 서비스 시작
sudo systemctl start k3s-daas-api-proxy
sudo systemctl start k3s-daas-worker

# 잠시 대기
sleep 5

# 서비스 상태 확인
echo "=== API Proxy 서비스 상태 ==="
sudo systemctl is-active k3s-daas-api-proxy || true
sudo systemctl status k3s-daas-api-proxy --no-pager -l || true

echo -e "\n=== Worker 서비스 상태 ==="
sudo systemctl is-active k3s-daas-worker || true
sudo systemctl status k3s-daas-worker --no-pager -l || true

# API Proxy 헬스체크
echo -e "\n=== API Proxy 헬스체크 ==="
curl -f http://localhost:8080/healthz 2>/dev/null && echo " - API Proxy 정상" || echo " - API Proxy 확인 필요"
ENDSSH

echo -e "${GREEN}✅ 서비스 시작 완료${NC}"

# Step 10: 배포 정보 저장
echo -e "${BLUE}Step 10: 배포 정보 저장${NC}"

cat > "$SCRIPT_DIR/worker-deployment-info.json" << EOF
{
    "deployment_date": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "worker_node": {
        "instance_id": "$INSTANCE_ID",
        "public_ip": "$PUBLIC_IP",
        "instance_type": "$INSTANCE_TYPE",
        "region": "$AWS_REGION",
        "name": "$WORKER_NAME"
    },
    "services": {
        "api_proxy": {
            "port": 8080,
            "endpoint": "http://$PUBLIC_IP:8080",
            "health_check": "http://$PUBLIC_IP:8080/healthz"
        },
        "worker_host": {
            "service": "k3s-daas-worker",
            "config_file": "/home/ubuntu/dsaas/worker-config.json"
        }
    },
    "ssh_access": {
        "key_file": "$KEY_FILE",
        "command": "ssh -i $KEY_FILE ubuntu@$PUBLIC_IP"
    },
    "next_steps": [
        "Phase 3: Nautilus TEE 배포",
        "kubectl 설정 테스트",
        "스테이킹 실행"
    ]
}
EOF

echo -e "${GREEN}✅ 배포 정보가 worker-deployment-info.json에 저장되었습니다.${NC}"

# 완료 메시지
echo ""
echo -e "${GREEN}🎉 Phase 2: EC2 Worker Node 배포 완료!${NC}"
echo ""
echo "📋 배포 결과:"
echo "   🖥️  인스턴스 ID: $INSTANCE_ID"
echo "   🌐 Public IP: $PUBLIC_IP"
echo "   🔑 SSH 접속: ssh -i $KEY_FILE ubuntu@$PUBLIC_IP"
echo "   🌊 API Proxy: http://$PUBLIC_IP:8080"
echo ""
echo "📋 다음 단계:"
echo "1. API Proxy 헬스체크: curl http://$PUBLIC_IP:8080/healthz"
echo "2. Phase 3: Nautilus TEE 배포 스크립트 실행"
echo "   ./3-nautilus-tee-deploy.sh $PUBLIC_IP"
echo ""
echo "🔧 서비스 관리 명령어:"
echo "   sudo systemctl status k3s-daas-api-proxy"
echo "   sudo systemctl status k3s-daas-worker"
echo "   sudo systemctl restart k3s-daas-api-proxy"
echo "   sudo systemctl restart k3s-daas-worker"
echo ""
echo -e "${GREEN}Phase 2 배포 스크립트 완료! ✨${NC}"

# 정리
rm -f "$TEMP_TAR"