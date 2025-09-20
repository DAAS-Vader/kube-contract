# K3s-DaaS (Kubernetes Distributed-as-a-Service)

🚀 **혁신적인 Kubernetes 배포판** - Nautilus TEE, Sui 블록체인, Walrus 스토리지를 통합한 안전하고 분산된 컨테이너 오케스트레이션

## 🏗️ 아키텍처

```
kubectl → Nautilus TEE Master → K3s-DaaS
    ↓           ↓                    ↓
🔥 Hot Tier  🌡️ Warm Tier       🧊 Cold Tier
TEE Memory   Sui Blockchain    Walrus Storage
<50ms        1-3s             5-30s
```

## 🚀 빠른 시작

```bash
# 데모 환경 시작
./start-demo.sh

# 기능 테스트
docker-compose -f docker-compose.demo.yml exec kubectl-demo /scripts/demo-test.sh

# 성능 테스트
docker-compose -f docker-compose.demo.yml exec kubectl-demo /scripts/performance-test.sh

# 환경 종료
docker-compose -f docker-compose.demo.yml down
```

## 📁 프로젝트 구조

```
k3s-daas/
├── k3s-daas/pkg/              # 핵심 DaaS 구현
│   ├── nautilus/client.go     # Nautilus TEE 통합
│   ├── storage/router.go      # 3-tier 스토리지 라우팅
│   ├── walrus/storage.go      # Walrus 분산 스토리지
│   ├── sui/client.go          # Sui 블록체인 클라이언트
│   └── security/              # DaaS 보안 설정
├── architecture/              # 아키텍처 문서
├── demo-scripts/              # 데모 테스트 스크립트
├── docker-compose.demo.yml    # 데모 환경 설정
├── Dockerfile.k3s-daas       # K3s-DaaS 컨테이너
├── start-demo.sh             # 데모 시작 스크립트
├── DEMO-README.md            # 상세 데모 가이드
└── README.md                 # 이 파일
```

## 🎯 핵심 기능

- ✅ **Nautilus TEE**: etcd를 Intel SGX/TDX 보안 메모리로 대체
- ✅ **Sui 블록체인**: 스테이커 인증과 거버넌스
- ✅ **Walrus 스토리지**: 분산 파일 저장소
- ✅ **3-tier 아키텍처**: <50ms 응답 시간 달성
- ✅ **완전한 데모 환경**: Docker Compose로 쉬운 테스트

## 📊 성능 목표

| 계층 | 스토리지 백엔드 | 목표 응답 시간 | 용도 |
|------|----------------|----------------|------|
| Hot | Nautilus TEE | <50ms | 활성 클러스터 작업 |
| Warm | Sui 블록체인 | 1-3s | 메타데이터와 설정 |
| Cold | Walrus 스토리지 | 5-30s | 아카이브와 대용량 파일 |

상세한 사용법은 [DEMO-README.md](DEMO-README.md)를 참조하세요.