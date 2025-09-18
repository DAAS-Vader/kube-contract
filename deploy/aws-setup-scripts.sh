#!/bin/bash
# K3s-DaaS AWS 자동 설정 스크립트
# 사용법: ./aws-setup-scripts.sh master|worker

set -e

# 색상 코드
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 로깅 함수
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# 사용법 출력
usage() {
    echo "K3s-DaaS AWS 자동 설정 스크립트"
    echo "사용법: $0 {master|worker}"
    echo ""
    echo "  master  - Nautilus TEE 마스터 노드 설정"
    echo "  worker  - 워커 노드 설정"
    echo ""
    echo "환경변수 (필수):"
    echo "  SUI_PRIVATE_KEY     - Sui 지갑 프라이빗 키"
    echo "  SUI_NETWORK_URL     - Sui 네트워크 URL (기본값: testnet)"
    echo "  MASTER_IP           - 마스터 노드 IP (워커 노드용)"
    exit 1
}

# 환경변수 검증
check_env_vars() {
    if [[ -z "$SUI_PRIVATE_KEY" ]]; then
        log_error "SUI_PRIVATE_KEY 환경변수가 설정되지 않았습니다"
        exit 1
    fi

    if [[ "$1" == "worker" && -z "$MASTER_IP" ]]; then
        log_error "워커 노드 설정 시 MASTER_IP 환경변수가 필요합니다"
        exit 1
    fi

    # 기본값 설정
    export SUI_NETWORK_URL=${SUI_NETWORK_URL:-"https://fullnode.testnet.sui.io:443"}
    export K3S_DAAS_DATA_DIR=${K3S_DAAS_DATA_DIR:-"/var/lib/k3s-daas"}
}

# 공통 패키지 설치
install_common_packages() {
    log_step "공통 패키지 설치 중..."

    # 시스템 업데이트
    sudo yum update -y

    # 필수 패키지 설치
    sudo yum install -y \
        docker \
        git \
        curl \
        wget \
        jq \
        htop \
        vim

    # Docker 시작 및 활성화
    sudo systemctl start docker
    sudo systemctl enable docker

    # 사용자를 docker 그룹에 추가
    sudo usermod -a -G docker ec2-user

    log_info "공통 패키지 설치 완료"
}

# Go 설치
install_golang() {
    log_step "Go 언어 설치 중..."

    # Go 다운로드 및 설치
    cd /tmp
    wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz

    # PATH 설정
    echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee -a /etc/profile
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/go/bin

    # 설치 확인
    /usr/local/go/bin/go version

    log_info "Go 설치 완료"
}

# Sui CLI 설치
install_sui_cli() {
    log_step "Sui CLI 설치 중..."

    # Sui CLI 다운로드
    cd /tmp
    curl -fLJO https://github.com/MystenLabs/sui/releases/latest/download/sui-testnet-x86_64-unknown-linux-gnu.tgz
    tar -xzf sui-testnet-x86_64-unknown-linux-gnu.tgz
    sudo mv sui /usr/local/bin/
    sudo chmod +x /usr/local/bin/sui

    # 설치 확인
    sui --version

    log_info "Sui CLI 설치 완료"
}

# containerd 설치 (워커 노드용)
install_containerd() {
    log_step "containerd 설치 중..."

    # containerd 설치
    sudo yum install -y containerd

    # containerd 설정
    sudo mkdir -p /etc/containerd
    sudo containerd config default | sudo tee /etc/containerd/config.toml

    # systemd cgroup 드라이버 설정
    sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml

    # containerd 시작 및 활성화
    sudo systemctl start containerd
    sudo systemctl enable containerd

    log_info "containerd 설치 완료"
}

# Nitro Enclaves 설정 (마스터 노드용)
setup_nitro_enclaves() {
    log_step "Nitro Enclaves 설정 중..."

    # Nitro Enclaves CLI 설치
    sudo amazon-linux-extras install aws-nitro-enclaves-cli
    sudo yum install aws-nitro-enclaves-cli-devel -y

    # Nitro Enclaves 설정
    sudo systemctl enable nitro-enclaves-allocator.service
    sudo systemctl start nitro-enclaves-allocator.service

    # 리소스 할당 설정
    sudo mkdir -p /etc/nitro_enclaves
    sudo tee /etc/nitro_enclaves/allocator.yaml > /dev/null <<EOF
# Nitro Enclaves 리소스 할당
memory_mib: 2048
cpu_count: 2
EOF

    # 서비스 재시작
    sudo systemctl restart nitro-enclaves-allocator.service

    # 상태 확인
    sudo systemctl status nitro-enclaves-allocator.service

    log_info "Nitro Enclaves 설정 완료"
}

# K3s-DaaS 소스 다운로드 및 빌드
build_k3s_daas() {
    local node_type=$1
    log_step "K3s-DaaS $node_type 노드 빌드 중..."

    # 소스 코드 다운로드
    cd /home/ec2-user
    git clone https://github.com/your-org/k3s-daas.git
    cd k3s-daas

    # 빌드 디렉토리 선택
    if [[ "$node_type" == "master" ]]; then
        cd nautilus-release
        BUILD_TARGET="nautilus-tee"
    else
        cd worker-release
        BUILD_TARGET="k3s-daas-worker"
    fi

    # Go 모듈 다운로드
    /usr/local/go/bin/go mod tidy

    # 빌드
    /usr/local/go/bin/go build -o $BUILD_TARGET .
    chmod +x $BUILD_TARGET

    log_info "K3s-DaaS $node_type 노드 빌드 완료"
}

# 마스터 노드 설정
setup_master_node() {
    log_step "Nautilus TEE 마스터 노드 설정 중..."

    # 데이터 디렉토리 생성
    sudo mkdir -p $K3S_DAAS_DATA_DIR
    sudo chown ec2-user:ec2-user $K3S_DAAS_DATA_DIR

    # systemd 서비스 파일 생성
    sudo tee /etc/systemd/system/nautilus-tee.service > /dev/null <<EOF
[Unit]
Description=Nautilus TEE K3s Master
After=network.target nitro-enclaves-allocator.service
Requires=nitro-enclaves-allocator.service

[Service]
Type=simple
User=ec2-user
WorkingDirectory=/home/ec2-user/k3s-daas/nautilus-release
Environment=SUI_MASTER_PRIVATE_KEY=$SUI_PRIVATE_KEY
Environment=SUI_NETWORK_URL=$SUI_NETWORK_URL
Environment=K3S_DAAS_TEE_MODE=production
Environment=K3S_DAAS_BIND_ADDRESS=0.0.0.0
Environment=K3S_DAAS_DATA_DIR=$K3S_DAAS_DATA_DIR
Environment=PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=/home/ec2-user/k3s-daas/nautilus-release/nautilus-tee
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

    # 서비스 등록 및 시작
    sudo systemctl daemon-reload
    sudo systemctl enable nautilus-tee

    log_info "마스터 노드 설정 완료"
    log_warn "서비스를 시작하려면 'sudo systemctl start nautilus-tee' 명령어를 실행하세요"
}

# 워커 노드 설정
setup_worker_node() {
    log_step "K3s-DaaS 워커 노드 설정 중..."

    # 노드 ID 생성
    NODE_ID="worker-$(hostname)-$(date +%s)"

    # 데이터 디렉토리 생성
    sudo mkdir -p $K3S_DAAS_DATA_DIR
    sudo chown ec2-user:ec2-user $K3S_DAAS_DATA_DIR

    # systemd 서비스 파일 생성
    sudo tee /etc/systemd/system/k3s-daas-worker.service > /dev/null <<EOF
[Unit]
Description=K3s-DaaS Worker Node
After=network.target docker.service containerd.service
Requires=docker.service containerd.service

[Service]
Type=simple
User=ec2-user
WorkingDirectory=/home/ec2-user/k3s-daas/worker-release
Environment=SUI_WORKER_PRIVATE_KEY=$SUI_PRIVATE_KEY
Environment=SUI_WORKER_NETWORK_URL=$SUI_NETWORK_URL
Environment=K3S_DAAS_SERVER_URL=http://$MASTER_IP:6443
Environment=K3S_DAAS_NAUTILUS_ENDPOINT=http://$MASTER_IP:8080
Environment=K3S_DAAS_WORKER_NODE_ID=$NODE_ID
Environment=K3S_DAAS_WORKER_DATA_DIR=$K3S_DAAS_DATA_DIR
Environment=PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=/home/ec2-user/k3s-daas/worker-release/k3s-daas-worker
Restart=always
RestartSec=15

[Install]
WantedBy=multi-user.target
EOF

    # 서비스 등록 및 시작
    sudo systemctl daemon-reload
    sudo systemctl enable k3s-daas-worker

    log_info "워커 노드 설정 완료"
    log_warn "서비스를 시작하려면 'sudo systemctl start k3s-daas-worker' 명령어를 실행하세요"
}

# kubectl 설정 도우미 생성
create_kubectl_helper() {
    log_step "kubectl 도우미 스크립트 생성 중..."

    # kubectl 별칭 스크립트 생성
    tee /home/ec2-user/k3s-kubectl > /dev/null <<EOF
#!/bin/bash
# K3s-DaaS kubectl 도우미
kubectl --server=http://${MASTER_IP:-localhost}:8080 "\$@"
EOF

    chmod +x /home/ec2-user/k3s-kubectl

    # bashrc에 별칭 추가
    echo 'alias k3s-kubectl="/home/ec2-user/k3s-kubectl"' >> ~/.bashrc

    log_info "kubectl 도우미 생성 완료 (사용법: k3s-kubectl get nodes)"
}

# 상태 확인 스크립트 생성
create_status_check() {
    local node_type=$1
    log_step "상태 확인 스크립트 생성 중..."

    if [[ "$node_type" == "master" ]]; then
        tee /home/ec2-user/check-master-status.sh > /dev/null <<EOF
#!/bin/bash
echo "=== Nautilus TEE 마스터 노드 상태 ==="
echo
echo "1. 서비스 상태:"
sudo systemctl status nautilus-tee --no-pager
echo
echo "2. API 서버 상태:"
curl -s http://localhost:8080/health || echo "API 서버 응답 없음"
echo
echo "3. 최근 로그 (마지막 10줄):"
sudo journalctl -u nautilus-tee -n 10 --no-pager
echo
echo "4. 클러스터 노드 목록:"
/home/ec2-user/k3s-kubectl get nodes 2>/dev/null || echo "클러스터 연결 실패"
EOF
    else
        tee /home/ec2-user/check-worker-status.sh > /dev/null <<EOF
#!/bin/bash
echo "=== K3s-DaaS 워커 노드 상태 ==="
echo
echo "1. 서비스 상태:"
sudo systemctl status k3s-daas-worker --no-pager
echo
echo "2. Docker 상태:"
sudo systemctl status docker --no-pager | head -5
echo
echo "3. containerd 상태:"
sudo systemctl status containerd --no-pager | head -5
echo
echo "4. 최근 로그 (마지막 10줄):"
sudo journalctl -u k3s-daas-worker -n 10 --no-pager
echo
echo "5. 마스터 연결 테스트:"
curl -s http://$MASTER_IP:8080/health || echo "마스터 노드 연결 실패"
EOF
    fi

    chmod +x /home/ec2-user/check-*-status.sh
    log_info "상태 확인 스크립트 생성 완료"
}

# 메인 함수
main() {
    local node_type=$1

    # 입력 검증
    if [[ "$node_type" != "master" && "$node_type" != "worker" ]]; then
        usage
    fi

    log_info "K3s-DaaS $node_type 노드 설정을 시작합니다..."

    # 환경변수 검증
    check_env_vars $node_type

    # 공통 설정
    install_common_packages
    install_golang
    install_sui_cli

    # 노드 타입별 설정
    if [[ "$node_type" == "master" ]]; then
        setup_nitro_enclaves
        build_k3s_daas "master"
        setup_master_node
        create_kubectl_helper
        create_status_check "master"

        log_info "🎉 Nautilus TEE 마스터 노드 설정이 완료되었습니다!"
        echo
        echo "다음 단계:"
        echo "1. 서비스 시작: sudo systemctl start nautilus-tee"
        echo "2. 상태 확인: ./check-master-status.sh"
        echo "3. kubectl 테스트: k3s-kubectl get nodes"

    else
        install_containerd
        build_k3s_daas "worker"
        setup_worker_node
        create_status_check "worker"

        log_info "🎉 K3s-DaaS 워커 노드 설정이 완료되었습니다!"
        echo
        echo "다음 단계:"
        echo "1. 서비스 시작: sudo systemctl start k3s-daas-worker"
        echo "2. 상태 확인: ./check-worker-status.sh"
        echo "3. 마스터에서 노드 확인: k3s-kubectl get nodes"
    fi

    echo
    log_warn "주의: 서비스 시작 전에 Sui 지갑에 충분한 스테이킹이 있는지 확인하세요!"
}

# 스크립트 실행
main "$@"