# 🚀 K3s-DaaS 빠른 시작 가이드

**5분 안에 블록체인 네이티브 Kubernetes 클러스터 구축하기**

---

## 📋 준비사항 체크리스트

### ✅ AWS 계정 및 권한
- [ ] AWS 계정 및 CLI 설정 완료
- [ ] EC2 인스턴스 생성 권한
- [ ] Nitro Enclaves 지원 리전 (us-east-1, us-west-2 등)

### ✅ Sui 블록체인 준비
- [ ] Sui 지갑 생성 및 프라이빗 키 보유
- [ ] 테스트넷에서 최소 1000 MIST 스테이킹
- [ ] Sui CLI 기본 사용법 숙지

### ✅ 로컬 환경
- [ ] SSH 키페어 생성
- [ ] Git 설치
- [ ] kubectl 설치 (선택사항)

---

## ⚡ 1단계: 마스터 노드 배포 (3분)

### 🖥️ EC2 인스턴스 생성

```bash
# 1. Nautilus TEE 마스터용 인스턴스 생성
aws ec2 run-instances \
    --image-id ami-0c02fb55956c7d316 \
    --instance-type m5.2xlarge \
    --key-name your-key-pair \
    --security-groups k3s-daas-master \
    --enclave-options Enabled=true \
    --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=k3s-daas-master}]'

# 2. 퍼블릭 IP 확인
MASTER_IP=$(aws ec2 describe-instances \
    --filters "Name=tag:Name,Values=k3s-daas-master" "Name=instance-state-name,Values=running" \
    --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)

echo "마스터 노드 IP: $MASTER_IP"
```

### 🔧 마스터 노드 설정

```bash
# 1. SSH 접속
ssh -i your-key.pem ec2-user@$MASTER_IP

# 2. 설정 스크립트 다운로드
curl -fsSL https://raw.githubusercontent.com/your-org/k3s-daas/main/deploy/aws-setup-scripts.sh -o setup.sh
chmod +x setup.sh

# 3. 환경변수 설정
export SUI_PRIVATE_KEY="your-sui-private-key-here"
export SUI_NETWORK_URL="https://fullnode.testnet.sui.io:443"

# 4. 마스터 노드 자동 설정 (약 2분 소요)
./setup.sh master

# 5. 서비스 시작
sudo systemctl start nautilus-tee

# 6. 상태 확인
./check-master-status.sh
```

### ✅ 마스터 노드 검증

```bash
# API 서버 응답 확인
curl http://localhost:8080/health
# 예상 출력: {"status":"ok","tee":"active","blockchain":"connected"}

# kubectl 테스트
k3s-kubectl get nodes
# 예상 출력: 마스터 노드가 Ready 상태로 표시

# TEE 인증 확인
curl http://localhost:8080/attestation/report | jq .
```

---

## ⚡ 2단계: 워커 노드 배포 (2분)

### 🖥️ 워커 노드 인스턴스 생성

```bash
# 1. 워커 노드 3개 생성 (로컬에서 실행)
for i in {1..3}; do
  aws ec2 run-instances \
    --image-id ami-0c02fb55956c7d316 \
    --instance-type t3.medium \
    --key-name your-key-pair \
    --security-groups k3s-daas-worker \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=k3s-daas-worker-$i}]" &
done

wait  # 모든 인스턴스 생성 완료 대기

# 2. 워커 노드 IP 목록 확인
aws ec2 describe-instances \
    --filters "Name=tag:Name,Values=k3s-daas-worker-*" "Name=instance-state-name,Values=running" \
    --query 'Reservations[].Instances[].PublicIpAddress' --output text
```

### 🔧 워커 노드 설정 (병렬 실행)

```bash
# 각 워커 노드에 대해 아래 작업을 병렬로 수행

# 워커 노드 1 설정
ssh -i your-key.pem ec2-user@WORKER_IP_1 '
    curl -fsSL https://raw.githubusercontent.com/your-org/k3s-daas/main/deploy/aws-setup-scripts.sh -o setup.sh
    chmod +x setup.sh
    export SUI_PRIVATE_KEY="your-worker-private-key-1"
    export MASTER_IP="'$MASTER_IP'"
    ./setup.sh worker
    sudo systemctl start k3s-daas-worker
' &

# 워커 노드 2 설정
ssh -i your-key.pem ec2-user@WORKER_IP_2 '
    curl -fsSL https://raw.githubusercontent.com/your-org/k3s-daas/main/deploy/aws-setup-scripts.sh -o setup.sh
    chmod +x setup.sh
    export SUI_PRIVATE_KEY="your-worker-private-key-2"
    export MASTER_IP="'$MASTER_IP'"
    ./setup.sh worker
    sudo systemctl start k3s-daas-worker
' &

# 워커 노드 3 설정
ssh -i your-key.pem ec2-user@WORKER_IP_3 '
    curl -fsSL https://raw.githubusercontent.com/your-org/k3s-daas/main/deploy/aws-setup-scripts.sh -o setup.sh
    chmod +x setup.sh
    export SUI_PRIVATE_KEY="your-worker-private-key-3"
    export MASTER_IP="'$MASTER_IP'"
    ./setup.sh worker
    sudo systemctl start k3s-daas-worker
' &

wait  # 모든 워커 노드 설정 완료 대기
```

### ✅ 워커 노드 검증

```bash
# 마스터 노드에서 클러스터 상태 확인
ssh -i your-key.pem ec2-user@$MASTER_IP

# 노드 목록 확인
k3s-kubectl get nodes -o wide
# 예상 출력:
# NAME            STATUS   ROLES    AGE   VERSION        INTERNAL-IP   EXTERNAL-IP
# master-node     Ready    master   5m    v1.28.3+k3s    10.0.1.10
# worker-node-1   Ready    <none>   2m    v1.28.3+k3s    10.0.1.11
# worker-node-2   Ready    <none>   2m    v1.28.3+k3s    10.0.1.12
# worker-node-3   Ready    <none>   2m    v1.28.3+k3s    10.0.1.13

# 시스템 파드 확인
k3s-kubectl get pods --all-namespaces
```

---

## 🧪 3단계: 테스트 애플리케이션 배포

### 🚀 Nginx 테스트 배포

```bash
# 1. 테스트 네임스페이스 생성
k3s-kubectl create namespace demo

# 2. Nginx 배포
k3s-kubectl create deployment nginx-demo --image=nginx:alpine --replicas=3 -n demo

# 3. 서비스 노출
k3s-kubectl expose deployment nginx-demo --port=80 --type=NodePort -n demo

# 4. 배포 확인
k3s-kubectl get pods -n demo -o wide
k3s-kubectl get svc -n demo

# 5. 서비스 접근 테스트
SERVICE_PORT=$(k3s-kubectl get svc nginx-demo -n demo -o jsonpath='{.spec.ports[0].nodePort}')
curl http://$WORKER_IP_1:$SERVICE_PORT
```

### 📊 스트레스 테스트

```bash
# 1. 고부하 테스트 배포
k3s-kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stress-test
  namespace: demo
spec:
  replicas: 10
  selector:
    matchLabels:
      app: stress-test
  template:
    metadata:
      labels:
        app: stress-test
    spec:
      containers:
      - name: stress
        image: polinux/stress
        command: ["stress"]
        args: ["--cpu", "1", "--timeout", "300s"]
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi
EOF

# 2. 파드 분산 확인
k3s-kubectl get pods -n demo -o wide | grep stress-test

# 3. 노드 리소스 사용량 확인
k3s-kubectl top nodes
k3s-kubectl top pods -n demo
```

---

## 🔐 4단계: Seal Token 인증 테스트

### 🎯 인증 시스템 검증

```bash
# 1. 잘못된 토큰으로 접근 (실패해야 함)
curl -H "Authorization: Bearer invalid-token" \
    http://$MASTER_IP:8080/api/v1/nodes

# 2. Seal Token 생성
SEAL_TOKEN=$(curl -s -X POST http://$MASTER_IP:8080/auth/seal-token \
    -H "Content-Type: application/json" \
    -d '{
        "wallet_address": "your-wallet-address",
        "staking_proof": "your-staking-proof"
    }' | jq -r '.token')

# 3. 유효한 토큰으로 API 접근
curl -H "Authorization: Bearer $SEAL_TOKEN" \
    http://$MASTER_IP:8080/api/v1/nodes | jq .

# 4. 스테이킹 기반 권한 확인
curl -H "Authorization: Bearer $SEAL_TOKEN" \
    http://$MASTER_IP:8080/sui/staking/status | jq .
```

---

## 📊 5단계: 모니터링 설정

### 📈 대시보드 접근

```bash
# 1. 시스템 상태 대시보드
curl http://$MASTER_IP:8080/dashboard/status | jq .

# 2. 메트릭 수집 확인
curl http://$MASTER_IP:8080/metrics | head -20

# 3. Sui 블록체인 연결 상태
curl http://$MASTER_IP:8080/sui/health | jq .
```

### 📝 로그 모니터링 설정

```bash
# 마스터 노드에서
sudo journalctl -u nautilus-tee -f &

# 각 워커 노드에서 (새 터미널)
ssh -i your-key.pem ec2-user@$WORKER_IP_1 'sudo journalctl -u k3s-daas-worker -f' &
ssh -i your-key.pem ec2-user@$WORKER_IP_2 'sudo journalctl -u k3s-daas-worker -f' &
ssh -i your-key.pem ec2-user@$WORKER_IP_3 'sudo journalctl -u k3s-daas-worker -f' &
```

---

## 🎉 완료! 클러스터 사용하기

### ⚡ 일반적인 kubectl 명령어

```bash
# 클러스터 정보
k3s-kubectl cluster-info

# 모든 리소스 확인
k3s-kubectl get all --all-namespaces

# 노드 세부 정보
k3s-kubectl describe nodes

# 이벤트 확인
k3s-kubectl get events --sort-by=.metadata.creationTimestamp

# 리소스 사용량
k3s-kubectl top nodes
k3s-kubectl top pods --all-namespaces
```

### 🔧 고급 기능 테스트

```bash
# 1. Persistent Volume 테스트
k3s-kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: test-pvc
  namespace: demo
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
EOF

# 2. ConfigMap 및 Secret 테스트
k3s-kubectl create configmap app-config --from-literal=key1=value1 -n demo
k3s-kubectl create secret generic app-secret --from-literal=password=secret123 -n demo

# 3. Ingress 컨트롤러 테스트 (Traefik 기본 설치됨)
k3s-kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx-ingress
  namespace: demo
spec:
  rules:
  - host: nginx.local
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: nginx-demo
            port:
              number: 80
EOF

# 4. Horizontal Pod Autoscaler 테스트
k3s-kubectl autoscale deployment nginx-demo --cpu-percent=50 --min=1 --max=10 -n demo
```

---

## 🔧 트러블슈팅 빠른 가이드

### ❗ 일반적인 문제들

#### 문제 1: 마스터 노드 시작 실패
```bash
# 해결책:
sudo systemctl status nautilus-tee
sudo journalctl -u nautilus-tee -n 50

# Nitro Enclaves 확인
sudo systemctl status nitro-enclaves-allocator
```

#### 문제 2: 워커 노드 연결 실패
```bash
# 해결책:
# 1. 보안 그룹 확인
aws ec2 describe-security-groups --group-names k3s-daas-master k3s-daas-worker

# 2. 네트워크 연결 테스트
telnet $MASTER_IP 6443
telnet $MASTER_IP 8080

# 3. 스테이킹 상태 확인
sui client balance
```

#### 문제 3: kubectl 명령어 실패
```bash
# 해결책:
curl http://$MASTER_IP:8080/health
k3s-kubectl get nodes --v=9  # 디버그 모드
```

### 🚨 응급 복구 명령어

```bash
# 모든 서비스 재시작
sudo systemctl restart nautilus-tee        # 마스터에서
sudo systemctl restart k3s-daas-worker     # 워커에서

# 클러스터 초기화 (데이터 손실 주의!)
sudo rm -rf /var/lib/k3s-daas/*
sudo systemctl restart nautilus-tee
```

---

## 🎯 성공! 다음 단계

### ✅ 완료된 것들
- ✅ 세계 최초 블록체인 네이티브 Kubernetes 클러스터 구축
- ✅ TEE 기반 보안 Control Plane 실행
- ✅ Sui 블록체인 기반 노드 인증
- ✅ 100% kubectl 호환성 확인
- ✅ 실제 워크로드 배포 및 테스트

### 🚀 추가 탐색 방향
1. **프로덕션 환경 배포**: 고가용성, 로드 밸런싱 설정
2. **CI/CD 파이프라인 구축**: GitOps with ArgoCD
3. **모니터링 강화**: Prometheus + Grafana 연동
4. **백업 및 재해복구**: etcd 백업 자동화
5. **멀티 클러스터 관리**: 여러 리전에 클러스터 배포

### 🏆 축하합니다!

**당신은 방금 혁명적인 기술을 성공적으로 배포했습니다!**

K3s-DaaS는 단순한 Kubernetes가 아닙니다:
- 🔐 **TEE 보안**: 하드웨어 수준 격리
- 🌊 **블록체인 인증**: 탈중앙화된 노드 관리
- ⚡ **완전한 호환성**: 기존 DevOps 도구 그대로 사용
- 🚀 **미래 지향적**: Web3 + 클라우드 네이티브 융합

**Happy Kubernetes-ing on Sui! 🎊**