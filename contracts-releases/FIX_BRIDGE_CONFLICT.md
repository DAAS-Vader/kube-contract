# Bridge 모듈 충돌 해결 가이드

## 문제 상황
```
Duplicate module found: 0x000000000000000000000000000000000000000000000000000000000000000b::bridge
```

## 원인
- Sui 프레임워크 업데이트로 인한 Bridge 모듈 중복 포함
- Move.lock 파일의 의존성 충돌

## 해결 방법

### 1. Move.lock 파일 삭제 및 재생성
```bash
cd contracts-releases
rm Move.lock
```

### 2. 최신 Sui 버전으로 빌드
```bash
# 최신 Sui CLI 확인
sui --version

# 의존성 업데이트
sui move build --force

# 또는 특정 버전 지정
sui move build --skip-fetch-latest-git-deps
```

### 3. 대안적 해결책 (권장)

현재 Testnet의 안정 버전 사용:
```bash
# 1. Move.toml에서 명시적 의존성 제거 (현재 상태)
# 2. 클린 빌드
sui move clean
sui move build

# 3. 배포 시 --skip-dependency-verification 플래그 사용
sui client publish --gas-budget 100000000 --skip-dependency-verification
```

### 4. 최종 해결책이 안될 경우

새로운 프로젝트로 재시작:
```bash
sui move new k3s_daas_contracts_v2
cd k3s_daas_contracts_v2

# 기존 .move 파일들 복사
cp ../contracts-releases/*.move sources/

# 빌드 및 배포
sui move build
sui client publish --gas-budget 100000000
```

## 추천 배포 명령어

```bash
# 1차 시도 (의존성 검증 건너뛰기)
sui client publish --gas-budget 100000000 --skip-dependency-verification

# 2차 시도 (강제 최신 의존성 사용 안함)
sui move build --skip-fetch-latest-git-deps
sui client publish --gas-budget 100000000

# 3차 시도 (완전 클린 빌드)
sui move clean
sui move build
sui client publish --gas-budget 100000000
```

## 검증
배포 성공 후:
```bash
# Package ID 확인
export PACKAGE_ID="0x..."

# 함수 호출 테스트
sui client call \
  --package $PACKAGE_ID \
  --module staking \
  --function get_min_node_stake \
  --gas-budget 10000000
```