# K3s-DaaS AWS + Sui Testnet 테스트 체크리스트

## 🚀 **배포 전 준비사항**

### **AWS 설정**
- [ ] AWS CLI 설치 및 인증 설정
- [ ] EC2 Key Pair 생성
- [ ] Nitro Enclave 지원 인스턴스 타입 확인 (m5.xlarge+)
- [ ] 적절한 리전 선택 (us-west-2 권장)

### **Sui 설정**
- [ ] Sui CLI 설치: `cargo install --git https://github.com/MystenLabs/sui.git --tag testnet sui`
- [ ] 테스트넷 지갑 생성: `sui client new-address ed25519`
- [ ] Discord에서 테스트넷 SUI 받기: `!faucet <your-address>`
- [ ] 잔액 확인: `sui client gas`

## 📦 **1단계: AWS 인프라 배포**

```bash
# 1. Terraform으로 인프라 배포
cd aws-deployment/terraform
terraform init
terraform plan -var="key_pair_name=your-key-pair"
terraform apply -var="key_pair_name=your-key-pair"

# 2. 출력에서 IP 주소 확인
terraform output
```

**확인 사항**:
- [ ] Nautilus TEE 인스턴스 실행 중
- [ ] Staker Host 인스턴스 실행 중
- [ ] 보안 그룹 규칙 올바르게 설정됨
- [ ] SSH 접속 가능

## 🌊 **2단계: Sui 스마트 컨트랙트 배포**

```bash
# 1. 컨트랙트 배포
cd contracts
chmod +x deploy-testnet.sh
./deploy-testnet.sh

# 2. 배포 정보 확인
cat deployment-info.json
```

**확인 사항**:
- [ ] 스테이킹 컨트랙트 배포 성공
- [ ] 게이트웨이 컨트랙트 배포 성공
- [ ] Package ID 정보 저장됨
- [ ] 스테이킹 풀 초기화 완료

## 🔒 **3단계: Nautilus TEE 설정**

```bash
# SSH to Nautilus TEE instance
ssh -i your-key.pem ec2-user@<nautilus-ip>

# 1. Nitro Enclave 상태 확인
sudo systemctl status nitro-enclaves-allocator
nitro-cli describe-enclaves

# 2. Nautilus TEE 이미지 빌드
cd /opt/k3s-daas/nautilus-tee
docker build -f Dockerfile.nitro -t nautilus-tee .
nitro-cli build-enclave --docker-uri nautilus-tee --output-file nautilus-tee.eif

# 3. TEE 시작
nitro-cli run-enclave \
  --eif-path nautilus-tee.eif \
  --memory 1024 \
  --cpu-count 2 \
  --debug-mode
```

**확인 사항**:
- [ ] Nitro Enclave 정상 할당됨
- [ ] TEE 이미지 빌드 성공
- [ ] TEE 인스턴스 실행 중
- [ ] Health check 응답: `curl http://localhost:8080/health`

## 🖥️ **4단계: Staker Host 설정**

```bash
# SSH to Staker Host instance
ssh -i your-key.pem ec2-user@<staker-ip>

# 1. 설정 파일 업데이트
cd /opt/k3s-daas/k3s-daas
cp staker-config.json staker-config.json.backup

# 2. 실제 값으로 설정 업데이트
cat > staker-config.json << EOF
{
  "node_id": "aws-staker-$(hostname)",
  "sui_wallet_address": "$(sui client active-address)",
  "sui_private_key": "$(cat ~/.sui/sui_config/sui.keystore/* | jq -r '.private_key')",
  "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
  "stake_amount": 1000,
  "contract_address": "REPLACE_WITH_ACTUAL_PACKAGE_ID",
  "min_stake_amount": 1000
}
EOF

# 3. Go 의존성 설치
go mod tidy

# 4. 빌드 및 실행
go build -o k3s-daas main.go
./k3s-daas
```

**확인 사항**:
- [ ] Sui 지갑 주소 올바르게 설정됨
- [ ] 컨트랙트 주소 올바르게 설정됨
- [ ] Go 빌드 성공
- [ ] 스테이커 호스트 실행됨

## 🧪 **5단계: 통합 테스트**

### **스테이킹 테스트**
```bash
# Staker Host에서 확인
curl http://localhost:10250/health
curl http://localhost:10250/stake

# 로그 확인
journalctl -f -u k3s-daas
```

### **TEE 연결 테스트**
```bash
# Nautilus TEE에서 확인
curl http://localhost:8080/health
nitro-cli describe-enclaves
```

### **Kubernetes 클러스터 테스트**
```bash
# kubectl 설정 (Staker Host에서)
export KUBECONFIG=/var/lib/k3s-daas/kubeconfig.yaml

# 노드 상태 확인
kubectl get nodes
kubectl get pods --all-namespaces
kubectl cluster-info
```

## ✅ **성공 기준**

모든 항목이 체크되어야 테스트 성공:

### **기본 동작**
- [ ] 스테이킹 트랜잭션 성공
- [ ] Seal 토큰 생성 성공
- [ ] Nautilus TEE 연결 성공
- [ ] 워커 노드 등록 성공

### **로그 확인**
```
✅ Successfully staked! Stake Object ID: 0x...
✅ Seal token created! Token ID: 0x...
🔑 Nautilus info retrieved with Seal token
🔒 TEE connection established with Seal authentication
✅ K3s Staker Host 'aws-staker-...' ready and running
```

### **상태 확인**
- [ ] `kubectl get nodes` 에서 워커 노드 Ready 상태
- [ ] `nitro-cli describe-enclaves` 에서 TEE 실행 중
- [ ] 30초마다 스테이킹 상태 검증 로그

## 🚨 **문제 해결**

### **일반적 오류**
1. **Sui 잔액 부족**: Discord에서 추가 faucet 요청
2. **TEE 메모리 부족**: allocator.yaml에서 memory_mib 증가
3. **네트워킹 오류**: 보안 그룹 포트 확인
4. **빌드 오류**: Go 의존성 `go mod tidy` 재실행

### **디버깅 명령어**
```bash
# AWS 로그
sudo journalctl -f -u k3s-daas
sudo journalctl -f -u nitro-enclaves-allocator

# Sui 상태
sui client gas
sui client objects
sui client active-address

# 네트워크 테스트
telnet <nautilus-ip> 8080
telnet <nautilus-ip> 6443
```

## 🎯 **테스트 시나리오**

### **시나리오 1: 정상 플로우**
1. 스테이커 호스트 시작
2. 스테이킹 성공 확인
3. Seal 토큰 생성 확인
4. TEE 연결 성공 확인
5. Pod 배포 테스트

### **시나리오 2: 장애 상황**
1. 스테이킹 슬래싱 시뮬레이션
2. TEE 연결 끊김 테스트
3. 네트워크 장애 복구 테스트

**테스트 완료 후 정리**:
```bash
# 리소스 정리
terraform destroy -var="key_pair_name=your-key-pair"
```

🎉 **이 체크리스트를 따라하면 실제 AWS + Sui 환경에서 K3s-DaaS가 완벽하게 동작할 것입니다!**