#!/bin/bash

echo "🚀 K3s-DaaS EC2 워커 노드 설정 스크립트"
echo "=========================================="

# 1. 시스템 업데이트
echo "📦 시스템 업데이트 중..."
sudo apt update -y
sudo apt upgrade -y

# 2. Docker 설치 (container runtime으로 docker 사용 시)
echo "🐋 Docker 설치 중..."
sudo apt install -y docker.io
sudo systemctl enable docker
sudo systemctl start docker
sudo usermod -aG docker ubuntu

# 3. Containerd 설치 (container runtime으로 containerd 사용 시)
echo "🐳 Containerd 설치 중..."
sudo apt install -y containerd
sudo systemctl enable containerd
sudo systemctl start containerd

# 4. 필요한 도구 설치
echo "🔧 추가 도구 설치 중..."
sudo apt install -y curl wget jq

# 5. 방화벽 설정 (필요한 포트 오픈)
echo "🔥 방화벽 설정 중..."
sudo ufw allow 6443/tcp  # K3s API server
sudo ufw allow 10250/tcp # Kubelet API
sudo ufw allow 8472/udp  # Flannel VXLAN
sudo ufw allow 51820/udp # Flannel Wireguard

# 6. 실행 권한 설정
echo "🔑 실행 권한 설정 중..."
chmod +x k3s-daas

echo "✅ EC2 설정 완료!"
echo ""
echo "다음 단계:"
echo "1. staker-config.json 파일을 편집하세요"
echo "2. ./k3s-daas 실행하세요"
echo ""
echo "설정 파일 예시:"
echo "  - sui_wallet_address: 실제 SUI 지갑 주소"
echo "  - sui_private_key: 지갑 프라이빗 키"
echo "  - contract_address: 배포된 컨트랙트 주소"
echo "  - nautilus_endpoint: Nautilus TEE 엔드포인트"