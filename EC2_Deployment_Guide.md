# K3s-DaaS EC2 배포 가이드

> **완전한 kubectl 호환 블록체인 Kubernetes 시스템**
> Sui 블록체인 + Nautilus TEE + K3s 통합 솔루션

---

## 🎯 배포 개요

K3s-DaaS는 다음 구성으로 EC2에 배포됩니다:

- **마스터 노드**: Nautilus TEE에서 실행되는 K3s Control Plane
- **워커 노드**: EC2 Ubuntu 인스턴스에서 실행되는 K3s Agent
- **인증 시스템**: Sui 블록체인 기반 Seal Token 인증

---

## 🏗️ 인프라 요구사항

### 마스터 노드 (Nautilus TEE)
- **인스턴스 타입**: `m5.large` 이상 (TEE 지원)
- **OS**: Ubuntu 22.04 LTS
- **스토리지**: 20GB GP3 SSD
- **메모리**: 8GB 이상
- **CPU**: 2 vCPU 이상

### 워커 노드
- **인스턴스 타입**: `t3.medium` 이상
- **OS**: Ubuntu 22.04 LTS
- **스토리지**: 20GB GP3 SSD
- **메모리**: 4GB 이상
- **CPU**: 2 vCPU 이상

### 네트워크 설정
```yaml
보안 그룹:
  - SSH (22): 관리자 IP만
  - K3s API (6443): 워커 노드들
  - K3s Proxy (8080): kubectl 클라이언트
  - Sui RPC (443): 아웃바운드
```

---

## 🚀 1단계: 마스터 노드 배포

### 1.1 EC2 인스턴스 생성
```bash
# AWS CLI로 마스터 노드 인스턴스 생성
aws ec2 run-instances \
  --image-id ami-0c7217cdde317cfec \
  --instance-type m5.large \
  --key-name your-key-pair \
  --security-group-ids sg-your-master-sg \
  --subnet-id subnet-your-public-subnet \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=K3s-DaaS-Master}]'
```

### 1.2 마스터 노드 설정
```bash
# SSH 접속
ssh -i your-key.pem ubuntu@master-node-ip

# 시스템 업데이트
sudo apt update && sudo apt upgrade -y

# Docker 설치 (컨테이너 런타임용)
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker ubuntu

# 필수 패키지 설치
sudo apt install -y curl wget unzip jq

# Go 1.21 설치 (필요시 소스 빌드용)
wget https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

### 1.3 Nautilus TEE 바이너리 배포
```bash
# 마스터 노드 바이너리 업로드
scp -i your-key.pem nautilus-tee/nautilus-tee.exe ubuntu@master-ip:~/

# 실행 권한 설정
chmod +x ~/nautilus-tee.exe

# 설정 디렉토리 생성
mkdir -p ~/k3s-daas-config
```

### 1.4 마스터 노드 설정 파일
```bash
# 마스터 설정 파일 생성
cat > ~/k3s-daas-config/master-config.yaml << EOF
tee:
  enabled: true
  type: "simulation"  # 실제 TEE에서는 "sgx" 또는 "sev"

k3s:
  bind_address: "0.0.0.0"
  https_port: 6443
  http_port: 8080
  data_dir: "/var/lib/k3s-daas"

sui:
  endpoint: "https://sui-testnet.nodereal.io"
  gateway_package: "your-gateway-package-id"

seal_auth:
  enabled: true
  cache_size: 1000
  token_ttl: 3600
EOF
```

### 1.5 마스터 노드 시작
```bash
# 서비스로 등록
sudo tee /etc/systemd/system/k3s-daas-master.service << EOF
[Unit]
Description=K3s-DaaS Master Node (Nautilus TEE)
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu
ExecStart=/home/ubuntu/nautilus-tee.exe
Restart=always
RestartSec=5
Environment=HOME=/home/ubuntu

[Install]
WantedBy=multi-user.target
EOF

# 서비스 시작
sudo systemctl daemon-reload
sudo systemctl enable k3s-daas-master
sudo systemctl start k3s-daas-master

# 상태 확인
sudo systemctl status k3s-daas-master
```

---

## 🔧 2단계: 워커 노드 배포

### 2.1 워커 노드 인스턴스 생성
```bash
# 워커 노드 인스턴스 생성 (필요한 만큼 반복)
aws ec2 run-instances \
  --image-id ami-0c7217cdde317cfec \
  --instance-type t3.medium \
  --key-name your-key-pair \
  --security-group-ids sg-your-worker-sg \
  --subnet-id subnet-your-private-subnet \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=K3s-DaaS-Worker-1}]'
```

### 2.2 워커 노드 설정
```bash
# SSH 접속
ssh -i your-key.pem ubuntu@worker-node-ip

# 시스템 업데이트 및 Docker 설치
sudo apt update && sudo apt upgrade -y
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker ubuntu

# 컨테이너 런타임 설정
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json << EOF
{
  "exec-opts": ["native.cgroupdriver=systemd"],
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "100m"
  },
  "storage-driver": "overlay2"
}
EOF

sudo systemctl restart docker
```

### 2.3 워커 바이너리 배포
```bash
# 워커 노드 바이너리 업로드
scp -i your-key.pem k3s-daas/k3s-daas-worker.exe ubuntu@worker-ip:~/

# 실행 권한 설정
chmod +x ~/k3s-daas-worker.exe
```

### 2.4 워커 노드 설정 파일
```bash
# 워커 설정 파일 생성
cat > ~/staker-config.json << EOF
{
  "node_id": "k3s-daas-worker-$(hostname)",
  "sui_endpoint": "https://sui-testnet.nodereal.io",
  "private_key": "your-sui-private-key",
  "k3s_master_endpoint": "https://master-node-ip:6443",
  "proxy_endpoint": "http://master-node-ip:8080",
  "heartbeat_interval": 30,
  "staking_amount": 1000000000,
  "mock_mode": false
}
EOF
```

### 2.5 워커 노드 시작
```bash
# 서비스로 등록
sudo tee /etc/systemd/system/k3s-daas-worker.service << EOF
[Unit]
Description=K3s-DaaS Worker Node
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu
ExecStart=/home/ubuntu/k3s-daas-worker.exe
Restart=always
RestartSec=5
Environment=HOME=/home/ubuntu

[Install]
WantedBy=multi-user.target
EOF

# 서비스 시작
sudo systemctl daemon-reload
sudo systemctl enable k3s-daas-worker
sudo systemctl start k3s-daas-worker

# 상태 확인
sudo systemctl status k3s-daas-worker
```

---

## 🎮 3단계: kubectl 설정 및 테스트

### 3.1 로컬 kubectl 설정
```bash
# kubectl 설치 (로컬 머신에서)
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# kubeconfig 생성
mkdir -p ~/.kube
cat > ~/.kube/config << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: http://master-node-ip:8080
    insecure-skip-tls-verify: true
  name: k3s-daas
contexts:
- context:
    cluster: k3s-daas
    user: k3s-daas-admin
  name: k3s-daas
current-context: k3s-daas
users:
- name: k3s-daas-admin
  user:
    # Seal Token 인증은 자동으로 처리됨
EOF
```

### 3.2 클러스터 연결 테스트
```bash
# 클러스터 정보 확인
kubectl cluster-info

# 노드 상태 확인
kubectl get nodes

# 시스템 파드 확인
kubectl get pods --all-namespaces

# 서비스 확인
kubectl get services
```

---

## 📊 4단계: 모니터링 및 로그

### 4.1 시스템 상태 모니터링
```bash
# 마스터 노드 로그
sudo journalctl -u k3s-daas-master -f

# 워커 노드 로그
sudo journalctl -u k3s-daas-worker -f

# 리소스 사용량
htop
docker stats
```

### 4.2 클러스터 상태 확인
```bash
# 클러스터 전체 상태
kubectl get all --all-namespaces

# 이벤트 모니터링
kubectl get events --sort-by='.metadata.creationTimestamp'

# 노드 상세 정보
kubectl describe nodes
```

---

## 🔧 5단계: 문제 해결

### 5.1 일반적인 문제들

**마스터 노드가 시작되지 않는 경우:**
```bash
# 로그 확인
sudo journalctl -u k3s-daas-master --no-pager

# 포트 충돌 확인
sudo netstat -tulpn | grep -E "(6443|8080)"

# 방화벽 확인
sudo ufw status
```

**워커 노드가 연결되지 않는 경우:**
```bash
# 네트워크 연결 확인
telnet master-node-ip 6443

# Seal Token 인증 상태 확인
grep -i "seal" /var/log/syslog

# Docker 상태 확인
sudo systemctl status docker
```

**kubectl 명령이 작동하지 않는 경우:**
```bash
# API 서버 응답 확인
curl -k http://master-node-ip:8080/api/v1

# kubeconfig 확인
kubectl config view

# 연결 상태 확인
kubectl get --raw /healthz
```

### 5.2 성능 최적화

**마스터 노드 최적화:**
```bash
# etcd 성능 튜닝
echo 'vm.max_map_count=262144' | sudo tee -a /etc/sysctl.conf
sudo sysctl -p

# 파일 디스크립터 제한 증가
echo 'ubuntu soft nofile 65536' | sudo tee -a /etc/security/limits.conf
echo 'ubuntu hard nofile 65536' | sudo tee -a /etc/security/limits.conf
```

**워커 노드 최적화:**
```bash
# 컨테이너 런타임 최적화
sudo mkdir -p /etc/systemd/system/docker.service.d
sudo tee /etc/systemd/system/docker.service.d/override.conf << EOF
[Service]
ExecStart=
ExecStart=/usr/bin/dockerd --max-concurrent-downloads 10 --max-concurrent-uploads 5
EOF

sudo systemctl daemon-reload
sudo systemctl restart docker
```

---

## 🔐 6단계: 보안 설정

### 6.1 네트워크 보안
```bash
# 보안 그룹 최소화
aws ec2 authorize-security-group-ingress \
  --group-id sg-your-master-sg \
  --protocol tcp \
  --port 6443 \
  --source-group sg-your-worker-sg

# 불필요한 포트 차단
sudo ufw enable
sudo ufw allow 22/tcp
sudo ufw allow from worker-subnet 6443/tcp
sudo ufw allow from kubectl-clients 8080/tcp
```

### 6.2 Seal Token 보안
```bash
# 프라이빗 키 보안 설정
chmod 600 ~/staker-config.json
sudo chown root:root ~/staker-config.json

# 환경 변수로 설정 (더 안전)
export SUI_PRIVATE_KEY="your-private-key"
# config에서 private_key 필드 제거
```

---

## 📈 7단계: 스케일링

### 7.1 워커 노드 추가
```bash
# Auto Scaling Group 설정
aws autoscaling create-auto-scaling-group \
  --auto-scaling-group-name k3s-daas-workers \
  --launch-template LaunchTemplateName=k3s-daas-worker \
  --min-size 2 \
  --max-size 10 \
  --desired-capacity 3 \
  --vpc-zone-identifier "subnet-1,subnet-2"
```

### 7.2 로드 밸런서 설정
```bash
# ALB 생성 (kubectl 액세스용)
aws elbv2 create-load-balancer \
  --name k3s-daas-kubectl-lb \
  --subnets subnet-1 subnet-2 \
  --security-groups sg-kubectl-lb
```

---

## ✅ 배포 완료 체크리스트

- [ ] 마스터 노드 EC2 인스턴스 생성 완료
- [ ] Nautilus TEE 바이너리 배포 완료
- [ ] 마스터 노드 서비스 시작 완료
- [ ] 워커 노드(들) EC2 인스턴스 생성 완료
- [ ] 워커 바이너리 배포 완료
- [ ] 워커 노드 서비스 시작 완료
- [ ] kubectl 설정 완료
- [ ] 클러스터 연결 테스트 완료
- [ ] 모니터링 설정 완료
- [ ] 보안 설정 완료

## 🎯 성공 확인 명령어

```bash
# 전체 시스템 상태 확인
kubectl get nodes -o wide
kubectl get pods --all-namespaces
kubectl top nodes
kubectl cluster-info

# 샘플 애플리케이션 배포 테스트
kubectl create deployment nginx --image=nginx
kubectl expose deployment nginx --port=80 --type=NodePort
kubectl get services
```

---

## 📞 지원 및 문의

- **GitHub Issues**: [k3s-daas/issues](https://github.com/k3s-io/k3s-daas/issues)
- **문서**: 이 가이드와 함께 제공되는 기술 분석서 참조
- **Sui 블록체인**: [Sui Documentation](https://docs.sui.io)
- **K3s 공식**: [K3s Documentation](https://docs.k3s.io)

---

> 🚀 **배포 완료!** 이제 완전한 kubectl 호환성을 가진 블록체인 기반 Kubernetes 클러스터가 준비되었습니다.