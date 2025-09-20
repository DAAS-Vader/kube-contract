# K3s-DaaS Production Deployment Scripts

## 🎯 개요

이 디렉토리는 K3s-DaaS (Kubernetes Decentralized as a Service)를 실제 프로덕션 환경에 배포하기 위한 스크립트들을 포함합니다.

K3s-DaaS는 Sui 블록체인과 AWS Nitro Enclaves를 활용하여 완전히 탈중앙화된 Kubernetes 클러스터를 제공합니다.

## 🏗️ 아키텍처

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

## 📋 파일 목록

### 🚀 배포 스크립트
- **`deploy-all.sh`** - 전체 자동 배포 마스터 스크립트
- **`1-sui-testnet-deploy.sh`** - Sui 테스트넷 Move Contract 배포
- **`2-ec2-worker-deploy.sh`** - EC2 Worker Node 생성 및 설정
- **`3-nautilus-tee-deploy.sh`** - AWS Nitro Enclave TEE Node 배포
- **`4-system-integration-test.sh`** - 전체 시스템 통합 테스트

### 📚 문서
- **`K3S_DAAS_PRODUCTION_DEPLOYMENT_GUIDE.md`** - 상세 배포 가이드
- **`README.md`** - 이 파일

## 🚀 빠른 시작

### 사전 요구사항

1. **AWS CLI 설치 및 설정**
   ```bash
   aws configure
   ```

2. **필수 도구 설치**
   ```bash
   # Ubuntu/Debian
   sudo apt-get install -y curl wget git jq

   # macOS
   brew install curl wget git jq
   ```

3. **Sui CLI 설치** (자동으로 설치됨)

### 전체 자동 배포

```bash
# 모든 스크립트를 실행 가능하게 만들기
chmod +x *.sh

# 전체 자동 배포 실행
./deploy-all.sh
```

### 단계별 배포

각 단계를 개별적으로 실행할 수도 있습니다:

```bash
# Phase 1: Sui 테스트넷 배포
./1-sui-testnet-deploy.sh

# Phase 2: EC2 Worker Node 배포
./2-ec2-worker-deploy.sh

# Phase 3: Nautilus TEE 배포
./3-nautilus-tee-deploy.sh [WORKER_IP]

# Phase 4: 시스템 통합 테스트
./4-system-integration-test.sh
```

## 📊 배포 과정

### Phase 1: Sui 테스트넷 배포
- Sui CLI 설치 및 환경 설정
- 테스트넷 SUI 토큰 획득 안내
- Move Contract 컴파일 및 배포
- 배포 정보 JSON 파일 생성

### Phase 2: EC2 Worker Node 배포
- EC2 인스턴스 생성 (t3.medium)
- 기본 개발 환경 설정 (Go, kubectl 등)
- K3s-DaaS 소스 코드 배포
- API Proxy 및 Worker Host 서비스 설정
- systemd 서비스 등록 및 시작

### Phase 3: Nautilus TEE 배포
- Nitro Enclave 지원 EC2 인스턴스 생성 (m5.large)
- AWS Nitro Enclave CLI 설치
- Nautilus TEE Docker 이미지 빌드
- Enclave 이미지 파일(.eif) 생성
- Enclave 실행 및 서비스 등록

### Phase 4: 시스템 통합 테스트
- 기본 연결성 테스트
- 서비스 상태 검증
- kubectl 설정 및 테스트
- 테스트 워크로드 배포
- 성능 및 리소스 사용량 확인

## 📁 생성되는 파일들

배포 완료 후 다음 파일들이 생성됩니다:

```
deploy/
├── deployment-info.json              # Sui Contract 배포 정보
├── worker-deployment-info.json       # Worker Node 배포 정보
├── tee-deployment-info.json          # TEE Node 배포 정보
├── final-deployment-summary.json     # 전체 배포 요약
├── management-commands.sh             # 시스템 관리 스크립트
└── logs/
    └── deployment-YYYYMMDD-HHMMSS.log # 배포 로그
```

## 🔧 시스템 관리

### 상태 확인
```bash
# 전체 시스템 상태 확인
./management-commands.sh

# 개별 노드 접속
ssh -i ~/.ssh/k3s-daas-key.pem ubuntu@<WORKER_IP>
ssh -i ~/.ssh/k3s-daas-key.pem ubuntu@<TEE_IP>
```

### 서비스 관리
```bash
# Worker Node 서비스
sudo systemctl status k3s-daas-api-proxy
sudo systemctl status k3s-daas-worker
sudo systemctl restart k3s-daas-api-proxy

# TEE Node 서비스
sudo systemctl status nautilus-tee
sudo systemctl restart nautilus-tee
nitro-cli describe-enclaves
```

### kubectl 사용
```bash
# Worker Node에서 kubectl 설정
kubectl config set-cluster k3s-daas --server=http://localhost:8080
kubectl config set-credentials user --token=seal_0x123_sig_challenge_123456
kubectl config set-context k3s-daas --cluster=k3s-daas --user=user
kubectl config use-context k3s-daas

# 기본 명령어
kubectl get nodes
kubectl get pods --all-namespaces
kubectl apply -f your-app.yaml
```

## 💰 비용 정보

### AWS 리소스 비용 (us-east-1 기준)
- **Worker Node (t3.medium)**: ~$0.05/hour
- **TEE Node (m5.large)**: ~$0.10/hour
- **총 예상 비용**: ~$0.15/hour (~$3.6/day)

### 비용 절약 팁
- 사용하지 않을 때는 인스턴스 중지
- Spot 인스턴스 사용 고려
- 개발/테스트 환경에서는 더 작은 인스턴스 타입 사용

## 🛡️ 보안 고려사항

### 네트워크 보안
- Security Group을 통한 최소 권한 원칙 적용
- SSH 접근은 필요한 IP에서만 허용
- TEE API는 Worker Node에서만 접근 가능

### 인증 및 권한
- Seal Token 기반 블록체인 인증
- 스테이킹 양에 따른 자동 권한 부여
- AWS Nitro Enclave 하드웨어 보안

### 데이터 보호
- 모든 kubectl 명령어 블록체인 감사 로그
- TEE 내부에서 안전한 K8s API 처리
- 변조 불가능한 블록체인 기록

## 🔍 트러블슈팅

### 일반적인 문제들

1. **Sui 배포 실패**
   ```bash
   # 잔액 확인
   sui client gas

   # 테스트넷 토큰 요청
   # Discord: https://discord.com/channels/916379725201563759/1037811694564560966
   # !faucet <YOUR_ADDRESS>
   ```

2. **EC2 인스턴스 접속 실패**
   ```bash
   # SSH 키 권한 확인
   chmod 600 ~/.ssh/k3s-daas-key.pem

   # Security Group 확인
   aws ec2 describe-security-groups --group-ids <SG_ID>
   ```

3. **Enclave 실행 실패**
   ```bash
   # Nitro Enclave 상태 확인
   sudo systemctl status nitro-enclaves-allocator

   # 리소스 할당 확인
   cat /etc/nitro_enclaves/allocator.yaml
   ```

4. **서비스 로그 확인**
   ```bash
   # Worker Node 로그
   sudo journalctl -u k3s-daas-api-proxy -f
   sudo journalctl -u k3s-daas-worker -f

   # TEE Node 로그
   sudo journalctl -u nautilus-tee -f
   ```

### 완전 재시작
```bash
# 모든 서비스 재시작
sudo systemctl restart k3s-daas-api-proxy k3s-daas-worker
sudo systemctl restart nautilus-tee

# Enclave 재시작
sudo /usr/local/bin/stop-nautilus-enclave.sh
sudo /usr/local/bin/start-nautilus-enclave.sh
```

## 📞 지원

### 문서 및 가이드
- [K3s-DaaS 전체 문서](../analysis/)
- [Move Contract 분석](../analysis/k8s_gateway_purpose_analysis.md)
- [시스템 아키텍처](../analysis/complete_flow_report_final.md)

### 로그 위치
- 배포 로그: `./logs/deployment-*.log`
- 서비스 로그: `journalctl -u <service-name>`
- 시스템 상태: `./management-commands.sh`

## 🚀 다음 단계

배포 완료 후:

1. **스테이킹 실행**
   ```bash
   cd dsaas/worker-release
   go run main.go
   ```

2. **실제 워크로드 배포**
   ```bash
   kubectl apply -f your-kubernetes-manifests.yaml
   ```

3. **모니터링 설정**
   - Prometheus/Grafana 설치
   - 로그 집계 시스템 구성
   - 알림 시스템 설정

4. **확장**
   - 추가 Worker Node 배포
   - 멀티 리전 설정
   - 프로덕션 도메인 연결

---

**K3s-DaaS로 블록체인 기반 Kubernetes의 새로운 세상을 경험하세요!** 🚀