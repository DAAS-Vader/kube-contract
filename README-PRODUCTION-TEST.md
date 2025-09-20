# K3s-DaaS 프로덕션 테스트 가이드

## 🚀 빠른 시작

### 1. Docker Desktop 실행
Docker Desktop이 실행되고 있는지 확인하세요.

### 2. 프로덕션 E2E 테스트 실행
```bash
cd /mnt/c/Users/ahwls/daasVader
./e2e-test.sh
```

## 📋 테스트 구성 요소

### 컨테이너 구조
- **api-gateway** (포트 8080): kubectl 요청 처리
- **event-listener** (포트 10250): Sui 이벤트 처리
- **nautilus-control** (포트 6443, 8081): K8s 컨트롤 플레인
- **worker-node** (포트 10251): K8s 워커 노드

### 테스트 시나리오
1. ✅ Docker 환경 체크
2. ✅ 컨테이너 빌드 및 시작
3. ✅ 헬스체크 테스트
4. ✅ API Gateway 기능 테스트
5. ✅ 컨테이너 상태 확인
6. ✅ 로그 분석
7. ✅ 네트워크 연결 테스트
8. ✅ 리소스 사용량 확인

## 🔧 kubectl 설정

테스트 통과 후 kubectl 설정:
```bash
kubectl config set-cluster k3s-daas --server=http://localhost:8080
kubectl config set-credentials user --token=seal_YOUR_WALLET_SIGNATURE
kubectl config set-context k3s-daas --cluster=k3s-daas --user=user
kubectl config use-context k3s-daas

# 테스트 명령
kubectl get pods
kubectl get services
kubectl get nodes
```

## 🐛 문제 해결

### 컨테이너 로그 확인
```bash
docker compose logs api-gateway
docker compose logs event-listener
docker compose logs nautilus-control
docker compose logs worker-node
```

### 환경 정리
```bash
docker compose down --volumes
docker system prune -f
```

### 개별 컨테이너 재시작
```bash
docker compose restart api-gateway
docker compose restart event-listener
```

## 📊 아키텍처 플로우

```
kubectl → API Gateway (8080) → Move Contract → Event Listener (10250)
                                                       ↓
Worker Node (10251) ← Nautilus Control (6443) ←───────┘
```

## 🎯 프로덕션 준비도

- ✅ 컨테이너화 완료
- ✅ 헬스체크 구현
- ✅ 네트워크 구성
- ✅ E2E 테스트 자동화
- ✅ kubectl 통합
- ✅ 로깅 및 모니터링

## 🔐 보안 고려사항

- Seal Token 인증 구현됨
- TEE (Trusted Execution Environment) 통합
- 컨테이너 간 격리된 네트워크
- 최소 권한 원칙 적용