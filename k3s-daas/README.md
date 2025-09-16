# K3s-DaaS Staker Host (Worker Node)

**순수 K3s 워커 노드 + Sui 스테이킹 통합**

이 코드베이스는 **워커 노드 (스테이커 호스트) 전용**입니다.
마스터 노드 기능은 **Nautilus TEE**에서 실행됩니다.

## 🏗️ 아키텍처

```
Smart Contract (접근제어) → Nautilus TEE (마스터) → Staker Host (워커)
```

## 🚀 빠른 시작

### 1. 스테이커 호스트 설정

```bash
# 설정 파일 편집
cp staker-config.json.example staker-config.json
vim staker-config.json
```

```json
{
  "node_id": "your-worker-node-id",
  "sui_wallet_address": "0x...",
  "sui_private_key": "...",
  "sui_rpc_endpoint": "https://fullnode.mainnet.sui.io",
  "stake_amount": 1000,
  "contract_address": "0x...",
  "min_stake_amount": 1000
}
```

### 2. 스테이커 호스트 실행

```bash
# 스테이킹 & 워커 노드 시작
export STAKER_CONFIG_PATH=./staker-config.json
go run main.go
```

### 3. 상태 확인

```bash
# 노드 상태 확인
curl http://localhost:10250/health

# 스테이킹 상태 확인
curl http://localhost:10250/stake
```

## 🔧 핵심 기능

### ✅ Sui 스테이킹 통합
- 자동 스테이킹 등록
- 실시간 스테이킹 상태 모니터링
- 슬래싱 감지 및 자동 종료

### ✅ K3s 워커 노드
- 순수 K3s Agent (마스터 기능 없음)
- Containerd/Docker 런타임 지원
- Pod 실행 및 관리

### ✅ 스마트 컨트랙트 연동
- 마스터 노드 정보 자동 조회
- 권한 기반 클러스터 참여

## 📁 프로젝트 구조

```
k3s-daas/                    # 워커 노드 전용
├── main.go                  # 스테이커 호스트 메인
├── staker-config.json       # 워커 노드 설정
├── pkg/
│   ├── agent/              # K3s Agent (워커 노드)
│   ├── containerd/         # 컨테이너 런타임
│   ├── sui/               # Sui 블록체인 클라이언트
│   └── security/          # 인증 및 보안
└── README.md

nautilus-tee/               # 마스터 노드 (별도)
├── main.go                 # Nautilus TEE 마스터
└── ...

contracts/                  # 스마트 컨트랙트 (별도)
├── k8s_gateway.move       # 접근제어
├── staking.move           # 스테이킹 로직
└── ...
```

## 🌊 Sui 통합

### 스테이킹 요구사항
- **워커 노드**: 최소 1,000 MIST (0.000001 SUI)
- **관리자**: 최소 10,000 MIST (0.00001 SUI)

### 스마트 컨트랙트 연동
```go
// 스테이킹 등록
result, err := suiClient.ExecuteTransaction(&sui.TransactionParams{
    PackageID: contractAddress,
    Module:    "staking",
    Function:  "stake_node",
    Arguments: []interface{}{stakeAmount, nodeID},
})

// 마스터 정보 조회
masterInfo, err := suiClient.CallFunction(&sui.FunctionCall{
    PackageID: contractAddress,
    Module:    "k8s_gateway",
    Function:  "get_nautilus_endpoint",
    Arguments: []interface{}{stakeObjectID},
})
```

## 🔒 보안

- **하드웨어 격리**: 마스터 노드는 Nautilus TEE에서 실행
- **스테이킹 기반 참여**: 경제적 인센티브로 악의적 행동 방지
- **실시간 모니터링**: 스테이킹 상태 지속 검증
- **자동 종료**: 슬래싱 감지 시 즉시 워커 노드 종료

## ⚡ 성능

- **컨테이너 실행**: <5초
- **스테이킹 검증**: <30초 간격
- **클러스터 참여**: <1분

## 🐛 문제 해결

### 스테이킹 실패
```bash
# 지갑 잔액 확인
sui client gas

# 스테이킹 상태 확인
sui client object <stake_object_id>
```

### 워커 노드 연결 실패
```bash
# 컨트랙트 상태 확인
sui client call --package <contract_id> --module k8s_gateway --function get_nautilus_endpoint

# 네트워크 연결 확인
curl https://nautilus-tee-endpoint/health
```

## 📝 로그

```bash
# 실시간 로그 확인
tail -f /var/log/k3s-daas.log

# 스테이킹 이벤트만 필터링
grep "💰\|💀\|✅" /var/log/k3s-daas.log
```

---

**워커 노드만 담당합니다. 마스터 기능은 Nautilus TEE에서 실행됩니다!** 🚀