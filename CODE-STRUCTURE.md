# K3s-DaaS 코드 구조 분석 및 정리

## 🔍 **현재 상황**

프로젝트에 **중복된 코드**가 존재합니다:

### 📁 **발견된 파일들**
```
dsaas/
├── k3s-daas/main.go          ❌ 기존 K3s 기반 코드 (중복)
├── staker-host/main.go       ✅ 새로운 Seal 토큰 기반 워커 노드
├── nautilus-tee/main.go      ✅ TEE 마스터 노드
├── contracts/                ✅ Sui Move 스마트 컨트랙트
└── aws-deployment/           ✅ 배포 스크립트
```

## 🎯 **권장 최종 구조**

### **사용할 파일들** ✅
```
dsaas/
├── staker-host/              # Seal 토큰 기반 워커 노드
│   ├── main.go              # 완전 구현됨 (999줄 + 한글 주석)
│   ├── go.mod
│   └── staker-config.json
├── nautilus-tee/             # TEE 마스터 노드
│   ├── main.go              # 단순화된 TEE 구현
│   └── go.mod
├── contracts/                # Sui Move 스마트 컨트랙트
│   ├── staking.move         # 스테이킹 로직
│   ├── k8s_gateway.move     # Seal 토큰 생성
│   ├── Move.toml
│   └── deploy-testnet.sh
├── aws-deployment/           # AWS + Sui 테스트넷 배포
│   ├── terraform/
│   ├── aws-setup.sh
│   └── TESTING-CHECKLIST.md
└── README.md                 # 프로젝트 전체 가이드
```

### **삭제해야 할 파일들** ❌
```
dsaas/
└── k3s-daas/                 # 전체 디렉토리 삭제
    ├── main.go              # 기존 K3s 코드 (사용 안 함)
    ├── pkg/                 # K3s 패키지들 (필요 없음)
    └── go.mod
```

## 📋 **코드 기능 비교**

| 기능 | k3s-daas/main.go | staker-host/main.go |
|------|------------------|---------------------|
| **아키텍처** | 기존 K3s 방식 | Seal 토큰 기반 |
| **인증** | K3s join token | Seal 토큰 |
| **스테이킹** | 부분 구현 | 완전 구현 |
| **Nautilus 연결** | 불완전 | 완전 구현 |
| **한글 주석** | 없음 | 완전 구현 |
| **테스트넷 준비** | 불완전 | 완전 준비 |

## 🚀 **실행 플로우**

### **최종 아키텍처**
```
1. AWS EC2 #1: Nautilus TEE (마스터)
   └── nautilus-tee/main.go 실행

2. AWS EC2 #2: Staker Host (워커)
   └── staker-host/main.go 실행

3. Sui Testnet: 스마트 컨트랙트
   └── contracts/ 배포
```

### **시작 순서**
```bash
# 1. 스마트 컨트랙트 배포
cd contracts
./deploy-testnet.sh

# 2. Nautilus TEE 시작 (AWS EC2 #1)
cd nautilus-tee
go run main.go

# 3. Staker Host 시작 (AWS EC2 #2)
cd staker-host
go run main.go
```

## 🔧 **정리 작업 필요**

### **즉시 삭제할 파일들**
- `k3s-daas/` 전체 디렉토리
- `k3s-daas/main.go` (중복 코드)
- `k3s-daas/pkg/` (사용하지 않는 K3s 패키지들)

### **유지할 파일들**
- `staker-host/main.go` ✅ (완전 구현, 999줄, 한글 주석)
- `nautilus-tee/main.go` ✅ (TEE 마스터)
- `contracts/` ✅ (Move 스마트 컨트랙트)
- `aws-deployment/` ✅ (배포 스크립트)

## 💡 **권장사항**

1. **k3s-daas/ 디렉토리 완전 삭제**
2. **staker-host/main.go 사용** (완전 구현됨)
3. **nautilus-tee/main.go 사용** (TEE 마스터)
4. **contracts/ 유지** (스마트 컨트랙트)
5. **README.md 업데이트** (새로운 구조 반영)

이렇게 정리하면 중복 제거 + 명확한 구조 + 실제 테스트넷 배포 가능합니다.