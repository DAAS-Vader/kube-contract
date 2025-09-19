# K3s-DaaS Production Deployment Guide

## 🎯 완전한 프로덕션 배포 가이드

이 가이드는 K3s-DaaS를 실제 프로덕션 환경에 배포하는 단계별 방법을 제공합니다.

### 📋 배포 아키텍처

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Sui Testnet   │    │   EC2 Instance   │    │  EC2 Instance   │
│  (Move Contracts)│    │  (Worker Node)   │    │ (Nautilus TEE)  │
│                 │    │                  │    │ AWS Nitro       │
│  - Staking      │◄──►│  - Staker Host   │◄──►│  Enclave        │
│  - k8s Gateway  │    │  - API Proxy     │    │  - TEE Master   │
│  - Events       │    │  - kubectl       │    │  - K8s Control  │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

---

## 🚀 Phase 1: Sui 테스트넷 배포

### Step 1.1: Sui CLI 설치 및 설정

```bash
# Sui CLI 설치
curl -fsSL https://github.com/MystenLabs/sui/releases/latest/download/sui-ubuntu-x86_64.tgz | tar -xzf -
sudo mv sui /usr/local/bin/

# Sui 환경 설정
sui client new-env --alias testnet --rpc https://fullnode.testnet.sui.io:443
sui client switch --env testnet

# 새 지갑 생성 (또는 기존 지갑 복구)
sui client new-address ed25519
```

### Step 1.2: 테스트넷 SUI 토큰 획득

```bash
# Discord에서 SUI 토큰 요청
# https://discord.com/channels/916379725201563759/1037811694564560966
# !faucet $(sui client active-address)

# 잔액 확인
sui client gas
```

### Step 1.3: Move Contract 배포

```bash
# 프로젝트 클론
git clone <your-repo-url>
cd dsaas/contracts-release

# 배포 스크립트 실행
chmod +x deploy-testnet.sh
./deploy-testnet.sh

# 배포 결과 확인
cat deployment-info.json
```

**예상 출력**:
```json
{
    "network": "testnet",
    "deployed_at": "2025-09-19T16:00:00Z",
    "deployer": "0x...",
    "contracts": {
        "staking_package_id": "0x1234...",
        "staking_pool_id": "0x5678...",
        "gateway_package_id": "0x9abc..."
    },
    "endpoints": {
        "sui_rpc": "https://fullnode.testnet.sui.io:443"
    }
}
```

---

## 🖥️ Phase 2: EC2 Worker Node 배포

### Step 2.1: EC2 인스턴스 생성

```bash
# AWS CLI를 통한 EC2 인스턴스 생성
aws ec2 run-instances \
    --image-id ami-0c02fb55956c7d316 \
    --instance-type t3.medium \
    --key-name your-key-pair \
    --security-group-ids sg-12345678 \
    --subnet-id subnet-12345678 \
    --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=k3s-daas-worker-1}]'

# 인스턴스 IP 확인
aws ec2 describe-instances --filters "Name=tag:Name,Values=k3s-daas-worker-1" \
    --query 'Reservations[].Instances[].PublicIpAddress' --output text
```

### Step 2.2: Worker Node 환경 설정

```bash
# EC2 인스턴스에 SSH 접속
ssh -i your-key.pem ubuntu@<WORKER_NODE_IP>

# 기본 패키지 설치
sudo apt update && sudo apt install -y curl wget git build-essential

# Go 설치
wget https://go.dev/dl/go1.21.3.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.3.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# kubectl 설치
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

### Step 2.3: K3s-DaaS Worker 배포

```bash
# 프로젝트 클론
git clone <your-repo-url>
cd dsaas

# Worker Node 설정 파일 생성
cat > worker-config.json << EOF
{
    "contract_address": "0x1234...",
    "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
    "stake_amount": 1000000000,
    "node_id": "worker-node-1",
    "nautilus_endpoint": "https://<NAUTILUS_TEE_IP>:9443"
}
EOF

# API Proxy 시작 (백그라운드)
cd api-proxy
nohup go run main.go > ../logs/api-proxy.log 2>&1 &

# Worker Host 시작
cd ../worker-release
nohup go run main.go > ../logs/worker-host.log 2>&1 &

# 서비스 상태 확인
curl http://localhost:8080/healthz
```

### Step 2.4: Worker Node를 systemd 서비스로 등록

```bash
# API Proxy 서비스 생성
sudo tee /etc/systemd/system/k3s-daas-api-proxy.service > /dev/null << EOF
[Unit]
Description=K3s-DaaS API Proxy
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/dsaas/api-proxy
ExecStart=/usr/local/go/bin/go run main.go
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# Worker Host 서비스 생성
sudo tee /etc/systemd/system/k3s-daas-worker.service > /dev/null << EOF
[Unit]
Description=K3s-DaaS Worker Host
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/dsaas/worker-release
ExecStart=/usr/local/go/bin/go run main.go
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# 서비스 시작
sudo systemctl daemon-reload
sudo systemctl enable k3s-daas-api-proxy k3s-daas-worker
sudo systemctl start k3s-daas-api-proxy k3s-daas-worker

# 서비스 상태 확인
sudo systemctl status k3s-daas-api-proxy
sudo systemctl status k3s-daas-worker
```

---

## 🛡️ Phase 3: AWS Nitro Enclave (Nautilus TEE) 배포

### Step 3.1: Nitro Enclave 지원 EC2 인스턴스 생성

```bash
# Nitro Enclave를 지원하는 인스턴스 타입으로 생성
aws ec2 run-instances \
    --image-id ami-0c02fb55956c7d316 \
    --instance-type m5.large \
    --key-name your-key-pair \
    --security-group-ids sg-12345678 \
    --subnet-id subnet-12345678 \
    --enclave-options Enabled=true \
    --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=k3s-daas-nautilus-tee}]'

# 인스턴스 IP 확인
aws ec2 describe-instances --filters "Name=tag:Name,Values=k3s-daas-nautilus-tee" \
    --query 'Reservations[].Instances[].PublicIpAddress' --output text
```

### Step 3.2: Nitro Enclave 환경 설정

```bash
# TEE 인스턴스에 SSH 접속
ssh -i your-key.pem ubuntu@<NAUTILUS_TEE_IP>

# Nitro Enclave CLI 설치
sudo apt update
sudo apt install -y aws-nitro-enclaves-cli aws-nitro-enclaves-cli-devel

# Docker 설치 (Enclave 이미지 빌드용)
sudo apt install -y docker.io
sudo usermod -aG docker ubuntu
sudo systemctl enable docker
sudo systemctl start docker

# Enclave 서비스 시작
sudo systemctl enable nitro-enclaves-allocator
sudo systemctl start nitro-enclaves-allocator

# 리소스 할당 (2 vCPU, 1GB RAM)
echo 'cpu_count = 2' | sudo tee /etc/nitro_enclaves/allocator.yaml
echo 'memory_mib = 1024' | sudo tee -a /etc/nitro_enclaves/allocator.yaml
sudo systemctl restart nitro-enclaves-allocator
```

### Step 3.3: Nautilus TEE 애플리케이션 빌드

```bash
# 프로젝트 클론
git clone <your-repo-url>
cd dsaas/nautilus-release

# Enclave용 Dockerfile 생성
cat > Dockerfile.enclave << EOF
FROM amazonlinux:2

# 기본 패키지 설치
RUN yum update -y && yum install -y \
    golang \
    git \
    ca-certificates

# 애플리케이션 복사
COPY . /app
WORKDIR /app

# Go 모듈 다운로드 및 빌드
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o nautilus-tee main.go

# 설정 파일 생성
RUN echo '{"sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443", "listen_port": 9443}' > config.json

# 포트 노출
EXPOSE 9443

# 실행
CMD ["./nautilus-tee"]
EOF

# Enclave 이미지 빌드
docker build -f Dockerfile.enclave -t nautilus-tee-enclave .

# Enclave 이미지 파일로 변환
nitro-cli build-enclave --docker-uri nautilus-tee-enclave:latest --output-file nautilus-tee.eif
```

### Step 3.4: Nautilus TEE Enclave 실행

```bash
# Enclave 실행
nitro-cli run-enclave \
    --cpu-count 2 \
    --memory 1024 \
    --eif-path nautilus-tee.eif \
    --debug-mode \
    --enclave-cid 16

# Enclave 상태 확인
nitro-cli describe-enclaves

# Enclave 로그 확인 (디버그 모드에서만)
nitro-cli console --enclave-id $(nitro-cli describe-enclaves | jq -r '.[0].EnclaveID')
```

### Step 3.5: Nautilus TEE 서비스 등록

```bash
# Enclave 시작 스크립트 생성
sudo tee /usr/local/bin/start-nautilus-enclave.sh > /dev/null << 'EOF'
#!/bin/bash
cd /home/ubuntu/dsaas/nautilus-release
nitro-cli run-enclave \
    --cpu-count 2 \
    --memory 1024 \
    --eif-path nautilus-tee.eif \
    --debug-mode \
    --enclave-cid 16
EOF

sudo chmod +x /usr/local/bin/start-nautilus-enclave.sh

# systemd 서비스 생성
sudo tee /etc/systemd/system/nautilus-tee.service > /dev/null << EOF
[Unit]
Description=Nautilus TEE Enclave
After=nitro-enclaves-allocator.service
Requires=nitro-enclaves-allocator.service

[Service]
Type=forking
User=root
ExecStart=/usr/local/bin/start-nautilus-enclave.sh
ExecStop=/usr/bin/nitro-cli terminate-enclave --all
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# 서비스 시작
sudo systemctl daemon-reload
sudo systemctl enable nautilus-tee
sudo systemctl start nautilus-tee

# 서비스 상태 확인
sudo systemctl status nautilus-tee
nitro-cli describe-enclaves
```

---

## 🔧 Phase 4: 시스템 통합 및 테스트

### Step 4.1: 네트워크 연결 확인

```bash
# Worker Node에서 Nautilus TEE 연결 테스트
curl -k https://<NAUTILUS_TEE_IP>:9443/healthz

# API Proxy 설정 업데이트 (Worker Node에서)
sed -i 's/localhost:9443/<NAUTILUS_TEE_IP>:9443/g' ~/dsaas/api-proxy/main.go
sudo systemctl restart k3s-daas-api-proxy
```

### Step 4.2: kubectl 설정 및 테스트

```bash
# Worker Node에서 kubectl 설정
kubectl config set-cluster k3s-daas --server=http://localhost:8080
kubectl config set-credentials user --token=seal_0x123_sig_challenge_123456
kubectl config set-context k3s-daas --cluster=k3s-daas --user=user
kubectl config use-context k3s-daas

# 기본 테스트
kubectl get nodes
kubectl get pods --all-namespaces
```

### Step 4.3: 스테이킹 테스트

```bash
# Worker Node에서 스테이킹 실행
cd ~/dsaas/worker-release

# 대화형 스테이킹
go run main.go
# 입력:
# - 스테이킹 양: 1000000000 (1 SUI)
# - 노드 ID: worker-node-1
# - 계약 주소: <deployment-info.json의 staking_package_id>
```

### Step 4.4: End-to-End 테스트

```bash
# 테스트 Pod 배포
cat > test-pod.yaml << EOF
apiVersion: v1
kind: Pod
metadata:
  name: k3s-daas-test
  labels:
    app: test
spec:
  containers:
  - name: nginx
    image: nginx:alpine
    ports:
    - containerPort: 80
EOF

kubectl apply -f test-pod.yaml
kubectl get pods -w

# 서비스 생성
kubectl expose pod k3s-daas-test --type=NodePort --port=80
kubectl get services

# 정리
kubectl delete pod k3s-daas-test
kubectl delete service k3s-daas-test
```

---

## 📊 Phase 5: 모니터링 및 로그

### Step 5.1: 로그 모니터링 설정

```bash
# Worker Node 로그 디렉토리 생성
mkdir -p ~/dsaas/logs

# 로그 확인 스크립트 생성
cat > ~/dsaas/check-logs.sh << 'EOF'
#!/bin/bash
echo "=== API Proxy Logs ==="
sudo journalctl -u k3s-daas-api-proxy --no-pager -n 20

echo -e "\n=== Worker Host Logs ==="
sudo journalctl -u k3s-daas-worker --no-pager -n 20

echo -e "\n=== System Status ==="
systemctl is-active k3s-daas-api-proxy
systemctl is-active k3s-daas-worker
EOF

chmod +x ~/dsaas/check-logs.sh
```

### Step 5.2: Nautilus TEE 모니터링

```bash
# TEE 인스턴스에서 모니터링 스크립트 생성
cat > ~/dsaas/monitor-tee.sh << 'EOF'
#!/bin/bash
echo "=== Enclave Status ==="
nitro-cli describe-enclaves

echo -e "\n=== TEE Service Status ==="
systemctl is-active nautilus-tee

echo -e "\n=== Enclave Console (last 20 lines) ==="
if [ "$(nitro-cli describe-enclaves | jq -r '.[0].State')" = "RUNNING" ]; then
    timeout 5 nitro-cli console --enclave-id $(nitro-cli describe-enclaves | jq -r '.[0].EnclaveID') | tail -20
fi
EOF

chmod +x ~/dsaas/monitor-tee.sh
```

---

## 🚀 Phase 6: 프로덕션 최적화

### Step 6.1: 보안 설정

```bash
# 방화벽 설정 (Worker Node)
sudo ufw enable
sudo ufw allow ssh
sudo ufw allow 8080/tcp  # API Proxy
sudo ufw allow from <NAUTILUS_TEE_IP> to any port 22  # TEE에서 SSH

# 방화벽 설정 (Nautilus TEE)
sudo ufw enable
sudo ufw allow ssh
sudo ufw allow 9443/tcp  # TEE API
sudo ufw allow from <WORKER_NODE_IP> to any port 9443
```

### Step 6.2: 자동 시작 설정

```bash
# Worker Node 자동 시작 검증
sudo systemctl is-enabled k3s-daas-api-proxy
sudo systemctl is-enabled k3s-daas-worker

# TEE 자동 시작 검증
sudo systemctl is-enabled nautilus-tee
```

### Step 6.3: 백업 및 복구 절차

```bash
# 설정 파일 백업 (Worker Node)
tar -czf k3s-daas-config-backup-$(date +%Y%m%d).tar.gz \
    ~/dsaas/worker-config.json \
    ~/dsaas/api-proxy/main.go \
    /etc/systemd/system/k3s-daas-*

# Enclave 이미지 백업 (TEE Node)
cp ~/dsaas/nautilus-release/nautilus-tee.eif ~/nautilus-tee-backup-$(date +%Y%m%d).eif
```

---

## 📋 배포 체크리스트

### ✅ Sui 테스트넷 배포
- [ ] Sui CLI 설치 및 설정
- [ ] 테스트넷 SUI 토큰 획득
- [ ] Move Contract 배포 성공
- [ ] deployment-info.json 생성 확인

### ✅ Worker Node 배포
- [ ] EC2 인스턴스 생성 및 설정
- [ ] Go, kubectl 설치
- [ ] API Proxy 서비스 실행
- [ ] Worker Host 서비스 실행
- [ ] systemd 서비스 등록

### ✅ Nautilus TEE 배포
- [ ] Nitro Enclave 지원 EC2 생성
- [ ] Nitro Enclave CLI 설치
- [ ] Enclave 이미지 빌드
- [ ] Enclave 실행 및 서비스 등록

### ✅ 시스템 통합
- [ ] 네트워크 연결 확인
- [ ] kubectl 설정 및 테스트
- [ ] 스테이킹 테스트 성공
- [ ] End-to-End 테스트 완료

### ✅ 모니터링 및 최적화
- [ ] 로그 모니터링 설정
- [ ] 보안 설정 완료
- [ ] 자동 시작 설정
- [ ] 백업 절차 수립

---

## 🎯 성공 지표

### 시스템이 정상 작동 중인 경우:

1. **API Proxy**: `curl http://<WORKER_IP>:8080/healthz` → "OK"
2. **Nautilus TEE**: `nitro-cli describe-enclaves` → State: "RUNNING"
3. **kubectl**: `kubectl get nodes` → 노드 목록 정상 출력
4. **스테이킹**: 블록체인에 스테이킹 트랜잭션 성공
5. **Pod 배포**: `kubectl apply -f test-pod.yaml` → Pod 생성 성공

### 트러블슈팅

**일반적인 문제들**:
- 포트 충돌: `netstat -tulpn | grep :8080`
- 서비스 상태: `sudo systemctl status <service-name>`
- 로그 확인: `sudo journalctl -u <service-name> -f`
- Enclave 상태: `nitro-cli describe-enclaves`

---

**배포 완료 후 K3s-DaaS가 완전한 프로덕션 환경에서 실행됩니다!** 🚀

**문의사항**: 배포 중 문제가 발생하면 로그를 확인하고 각 Phase별 체크리스트를 재검토하세요.