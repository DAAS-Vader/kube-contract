# K3s-DaaS Complete Deployment Guide

## 시스템 아키텍처 개요

K3s-DaaS는 Sui 블록체인과 K3s를 통합한 탈중앙화 Kubernetes-as-a-Service 플랫폼입니다.

### 컴포넌트 구조
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  contracts-     │    │  api-proxy/     │    │  nautilus-      │
│  release/       │◄───┤                 ├───►│  release/       │
│                 │    │                 │    │                 │
│ • staking.move  │    │ • gateway.go    │    │ • main.go       │
│ • k8s_gateway   │    │ • listener.go   │    │ • k3s_control   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         ▲                       ▲                       ▲
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│ Sui Blockchain  │    │ kubectl client  │    │ EC2 Instance    │
│ (Testnet)       │    │ requests        │    │ (Master Node)   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                ▲
                                │
                                ▼
                       ┌─────────────────┐
                       │ worker-release/ │
                       │                 │
                       │ • main.go       │
                       │ • staker_host   │
                       │ (Worker Nodes)  │
                       └─────────────────┘
```

## 1. 전제 조건

### 1.1 필수 소프트웨어
- Go 1.21+
- Docker 또는 Containerd
- Sui CLI
- kubectl
- AWS CLI (EC2 배포시)

### 1.2 필수 계정
- Sui Testnet 지갑 (SUI 토큰 보유)
- AWS 계정 (EC2 인스턴스용)

## 2. 스마트 컨트랙트 배포 (contracts-release/)

### 2.1 Sui 개발환경 설정
```bash
# Sui CLI 설치
curl -fsSL https://sui-releases.s3.us-east-1.amazonaws.com/install.sh | bash

# 지갑 설정
sui client new-address ed25519
sui client switch --address YOUR_ADDRESS

# 테스트넷 설정
sui client switch --env testnet
```

### 2.2 컨트랙트 배포
```bash
cd contracts-release/

# 컨트랙트 빌드
sui move build

# 컨트랙트 배포
sui client publish --gas-budget 50000000

# 배포 결과에서 Package ID 저장
export PACKAGE_ID="0x..."
export STAKING_POOL_ID="0x..."
```

### 2.3 설정 파일 업데이트
```bash
# Move.toml 업데이트
sed -i "s/k3s_daas = \"0x0\"/k3s_daas = \"$PACKAGE_ID\"/" Move.toml
```

## 3. Nautilus Master Node 배포 (nautilus-release/)

### 3.1 EC2 인스턴스 생성
```bash
# EC2 인스턴스 시작 (t3.medium 권장)
aws ec2 run-instances \
  --image-id ami-0c02fb55956c7d316 \
  --count 1 \
  --instance-type t3.medium \
  --key-name your-key-pair \
  --security-group-ids sg-12345678 \
  --subnet-id subnet-12345678 \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=k3s-daas-master}]'
```

### 3.2 Master Node 설정
```bash
# EC2 인스턴스에 SSH 접속
ssh -i your-key.pem ubuntu@EC2_PUBLIC_IP

# Go 설치
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# 프로젝트 복사
git clone https://github.com/your-repo/k3s-daas.git
cd k3s-daas/nautilus-release/
```

### 3.3 설정 파일 생성
```bash
# config.json 생성
cat > config.json << EOF
{
  "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
  "contract_address": "$PACKAGE_ID",
  "staking_pool_id": "$STAKING_POOL_ID",
  "private_key": "YOUR_PRIVATE_KEY",
  "k3s_data_dir": "/var/lib/k3s-daas",
  "listen_port": 8080,
  "ec2_instance_id": "i-1234567890abcdef0",
  "region": "us-east-1"
}
EOF
```

### 3.4 Nautilus Master 시작
```bash
# 의존성 설치
go mod tidy

# 백그라운드 실행
nohup go run . > nautilus.log 2>&1 &

# 상태 확인
tail -f nautilus.log
```

## 4. API Proxy 배포 (api-proxy/)

### 4.1 프록시 서버 설정
```bash
cd api-proxy/

# 의존성 설치
go mod tidy

# 환경변수 설정
export SUI_RPC_URL="https://fullnode.testnet.sui.io:443"
export CONTRACT_ADDRESS="$PACKAGE_ID"
export NAUTILUS_ENDPOINT="http://EC2_PUBLIC_IP:8080"
```

### 4.2 Contract API Gateway 시작
```bash
# 게이트웨이 시작 (포트 8080)
go run contract_api_gateway.go &
```

### 4.3 Event Listener 시작
```bash
# 이벤트 리스너 시작 (포트 10250)
go run nautilus_event_listener.go &
```

## 5. Worker Node 배포 (worker-release/)

### 5.1 Worker Node 준비
```bash
# 새로운 서버/VM에서
cd worker-release/

# Go 의존성 설치
go mod tidy
```

### 5.2 스테이킹 설정
```bash
# 워커 노드 설정 파일
cat > staker-config.json << EOF
{
  "node_id": "worker-01",
  "sui_wallet_address": "YOUR_WORKER_WALLET",
  "sui_private_key": "YOUR_WORKER_PRIVATE_KEY",
  "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
  "stake_amount": 1000000000,
  "contract_address": "$PACKAGE_ID",
  "nautilus_endpoint": "http://EC2_PUBLIC_IP:8080",
  "container_runtime": "containerd",
  "min_stake_amount": 1000000000
}
EOF
```

### 5.3 SUI 토큰 스테이킹
```bash
# 스테이킹 트랜잭션 실행
sui client call \
  --package $PACKAGE_ID \
  --module staking \
  --function stake_for_node \
  --args $STAKING_POOL_ID YOUR_SUI_COIN worker-01 \
  --gas-budget 10000000
```

### 5.4 Worker Node 시작
```bash
# 스테이커 호스트 시작
go run . --config=staker-config.json
```

## 6. kubectl 클라이언트 설정

### 6.1 kubectl 설정
```bash
# 클러스터 설정
kubectl config set-cluster k3s-daas \
  --server=http://localhost:8080 \
  --insecure-skip-tls-verify=true

# 사용자 설정 (Seal Token 필요)
kubectl config set-credentials k3s-daas-user \
  --token=seal_YOUR_WALLET_SIGNATURE_CHALLENGE_TIMESTAMP

# 컨텍스트 설정
kubectl config set-context k3s-daas \
  --cluster=k3s-daas \
  --user=k3s-daas-user

# 컨텍스트 사용
kubectl config use-context k3s-daas
```

### 6.2 Seal Token 생성
```bash
# Sui 지갑으로 서명 생성
CHALLENGE="kubectl_access_$(date +%s)"
SIGNATURE=$(sui keytool sign --data $CHALLENGE --keystore-path ~/.sui/sui_config/sui.keystore)

# Seal Token 구성
SEAL_TOKEN="seal_${YOUR_WALLET_ADDRESS}_${SIGNATURE}_${CHALLENGE}_$(date +%s)"

# kubectl 자격증명 업데이트
kubectl config set-credentials k3s-daas-user --token=$SEAL_TOKEN
```

## 7. 시스템 검증

### 7.1 클러스터 상태 확인
```bash
# 노드 확인
kubectl get nodes

# 네임스페이스 확인
kubectl get namespaces

# Pod 생성 테스트
kubectl run test-pod --image=nginx --port=80

# Pod 상태 확인
kubectl get pods
```

### 7.2 로그 모니터링
```bash
# Nautilus Master 로그
tail -f nautilus-release/nautilus.log

# API Gateway 로그
tail -f api-proxy/gateway.log

# Worker Node 로그
tail -f worker-release/staker.log
```

## 8. 보안 고려사항

### 8.1 네트워크 보안
- EC2 Security Groups로 필요한 포트만 개방
- VPC 내부 통신 암호화
- TLS 인증서 적용 (프로덕션)

### 8.2 키 관리
- Private Key는 AWS Secrets Manager 사용
- Environment Variables 대신 KMS 활용
- 정기적인 키 로테이션

### 8.3 스테이킹 보안
- 최소 스테이킹 금액 준수
- 슬래싱 조건 모니터링
- 스테이킹 상태 정기 검증

## 9. 운영 및 유지보수

### 9.1 모니터링
```bash
# Prometheus/Grafana 설정 (선택사항)
helm install prometheus prometheus-community/kube-prometheus-stack

# 메트릭 수집
kubectl port-forward svc/prometheus-operated 9090:9090
```

### 9.2 백업
```bash
# etcd 백업
kubectl get all --all-namespaces -o yaml > cluster-backup.yaml

# 컨트랙트 상태 백업
sui client object $STAKING_POOL_ID
```

### 9.3 업그레이드
```bash
# 컨트랙트 업그레이드
sui client upgrade --package-path contracts-release/

# 바이너리 업그레이드
# 각 컴포넌트별로 순차적 재시작
```

## 10. 문제 해결

### 10.1 일반적인 문제들

**컨트랙트 호출 실패**
```bash
# RPC 연결 확인
curl -X POST https://fullnode.testnet.sui.io:443 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"sui_getLatestCheckpoint"}'

# 가스 부족시 토큰 요청
curl -X POST https://faucet.testnet.sui.io/gas \
  -H "Content-Type: application/json" \
  -d '{"FixedAmountRequest":{"recipient":"YOUR_ADDRESS"}}'
```

**Worker Node 연결 실패**
```bash
# 네트워크 연결 확인
telnet EC2_PUBLIC_IP 8080

# 스테이킹 상태 확인
sui client object YOUR_STAKE_OBJECT_ID
```

**kubectl 인증 실패**
```bash
# Seal Token 재생성
# 위의 6.2 섹션 참조

# API Gateway 로그 확인
grep "Unauthorized" api-proxy/gateway.log
```

### 10.2 성능 최적화

**처리량 향상**
- Worker Node 수 증가
- EC2 인스턴스 타입 업그레이드
- 네트워크 대역폭 확장

**응답 시간 개선**
- Redis 캐싱 추가
- Connection Pool 최적화
- 로드 밸런서 도입

## 11. 프로덕션 배포 체크리스트

- [ ] Sui Mainnet 컨트랙트 배포
- [ ] TLS/SSL 인증서 설정
- [ ] 도메인명 및 DNS 설정
- [ ] 로드 밸런서 구성
- [ ] 모니터링 시스템 구축
- [ ] 백업 시스템 구축
- [ ] 보안 감사 완료
- [ ] 스테이킹 경제학 검증
- [ ] 재해 복구 계획 수립
- [ ] 사용자 문서 작성

## 12. 지원 및 커뮤니티

- GitHub Issues: [프로젝트 저장소 링크]
- Discord: [커뮤니티 링크]
- 문서: [상세 문서 링크]
- 이메일: support@k3s-daas.io

---

**주의**: 이 가이드는 테스트넷 기준으로 작성되었습니다. 메인넷 배포시에는 추가적인 보안 검토와 테스트가 필요합니다.