# 🌊 K3s-DaaS 프로젝트 구조

## 📁 정리된 폴더 구조

```
dsaas/
├── README.md                    # 프로젝트 개요
├── SUI_HACKATHON_README.md     # 해커톤 제출용 README
├── CLAUDE.md                   # Claude 개발 지침
├── go.mod                      # 메인 Go 모듈
├── go.sum                      # Go 의존성
├──
├── 📁 scripts/                 # 🎯 데모 및 테스트 스크립트
│   ├── complete-hackathon-demo.sh    # 완전한 해커톤 데모 스크립트
│   ├── deploy-move-contract.sh       # Move 계약 배포 스크립트
│   ├── kubectl-setup.sh              # kubectl 설정 스크립트
│   ├── test-move-contract.sh         # Move 계약 테스트 스크립트
│   └── worker-node-test.sh           # 워커 노드 테스트 스크립트
│
├── 📁 nautilus-release/        # 🌊 Nautilus TEE 마스터 노드 배포용
│   ├── start-nautilus.sh       # 간단한 시작 스크립트
│   ├── main.go                 # Nautilus TEE 메인 코드
│   ├── k3s_control_plane.go    # K3s Control Plane 통합
│   ├── k8s_api_proxy.go        # kubectl API 프록시
│   ├── nautilus_attestation.go # Nautilus 인증 통합
│   ├── seal_auth_integration.go # Seal Token 인증
│   └── go.mod                  # Nautilus TEE Go 모듈
│
├── 📁 worker-release/          # 🔧 워커 노드 배포용
│   ├── start-worker.sh         # 간단한 시작 스크립트
│   ├── main.go                 # 워커 노드 메인 코드
│   ├── staker-config.json      # 워커 노드 설정
│   ├── k3s_agent_integration.go # K3s Agent 통합
│   ├── kubelet_functions.go    # kubelet 기능
│   ├── pkg-reference/          # 포크된 K3s 패키지들
│   └── go.mod                  # 워커 노드 Go 모듈
│
├── 📁 contracts-release/       # 📜 Move 스마트 계약 배포용
│   ├── deploy.sh               # 간단한 배포 스크립트
│   └── k8s_nautilus_verification.move # K3s Nautilus 검증 계약
│
├── 📁 nautilus-tee/           # 🔧 개발용 Nautilus TEE 소스
├── 📁 k3s-daas/               # 🔧 개발용 워커 노드 소스
├── 📁 contracts/              # 🔧 개발용 Move 계약 소스
└── 📁 architecture/           # 🏗️ 아키텍처 문서
```

## 🚀 사용 방법

### 1. 완전한 데모 실행
```bash
./scripts/complete-hackathon-demo.sh
```

### 2. 개별 컴포넌트 실행

#### Nautilus TEE 마스터 노드
```bash
cd nautilus-release
./start-nautilus.sh
```

#### 워커 노드
```bash
cd worker-release
./start-worker.sh
```

#### Move 계약 배포
```bash
cd contracts-release
./deploy.sh
```

## 🎯 배포 가능한 폴더들

- **`nautilus-release/`**: EC2 Nitro 인스턴스에 배포
- **`worker-release/`**: 워커 노드로 사용할 서버에 배포
- **`contracts-release/`**: Sui 네트워크에 Move 계약 배포
- **`scripts/`**: 데모 및 테스트용 스크립트들

## 🏆 해커톤 데모 순서

1. `scripts/complete-hackathon-demo.sh` 실행
2. 각 컴포넌트 개별 테스트
3. kubectl 명령어 시연
4. Move 계약 검증 시연