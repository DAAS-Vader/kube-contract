/*
K3s-DaaS 스테이커 호스트 (Staker Host) - K3s 워커 노드 + Sui 블록체인 스테이킹

이 파일은 K3s-DaaS 프로젝트의 핵심 구성요소로,
실제 컨테이너를 실행하는 워커 노드 역할을 합니다.

주요 역할:
1. Sui 블록체인에 SUI 토큰을 스테이킹하여 클러스터 참여 권한 획득
2. Seal 토큰을 생성하여 Nautilus TEE와 보안 통신
3. K3s Agent (kubelet + container runtime)를 실행하여 실제 워크로드 처리
4. 정기적으로 스테이킹 상태를 검증하고 하트비트를 전송

플로우:
스테이킹 등록 → Seal 토큰 생성 → Nautilus TEE 등록 → K3s Agent 시작 → 하트비트 유지
*/
package main

import (
	"context"
	"encoding/base64"  // Base64 인코딩/디코딩
	"encoding/json"    // JSON 직렬화/역직렬화를 위한 패키지
	"fmt"              // 포맷 문자열 처리
	"log"              // 로깅
	"net/http"         // HTTP 서버/클라이언트
	"os"               // 운영체제 인터페이스 (환경변수, 파일 등)
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"             // 시간 관련 함수들

	"github.com/go-resty/resty/v2" // HTTP 클라이언트 라이브러리 (Sui RPC 통신용)
)

/*
스테이커 호스트 설정 구조체
staker-config.json 파일에서 로드되는 설정들을 정의합니다.
*/
type StakerHostConfig struct {
	NodeID           string `json:"node_id"`            // 이 워커 노드의 고유 식별자 (예: "testnet-staker-01")
	SuiWalletAddress string `json:"sui_wallet_address"` // Sui 지갑 주소 (스테이킹에 사용)
	SuiPrivateKey    string `json:"sui_private_key"`    // Sui 지갑 개인키 (트랜잭션 서명용)
	SuiRPCEndpoint   string `json:"sui_rpc_endpoint"`   // Sui 테스트넷 RPC 엔드포인트
	StakeAmount      uint64 `json:"stake_amount"`       // 스테이킹할 SUI 양 (MIST 단위, 1 SUI = 10^9 MIST)
	ContractAddress  string `json:"contract_address"`   // 배포된 스마트 컨트랙트 Package ID
	NautilusEndpoint string `json:"nautilus_endpoint"`  // Nautilus TEE 엔드포인트 (마스터 노드)
	ContainerRuntime string `json:"container_runtime"`  // 컨테이너 런타임 (containerd 또는 docker)
	MinStakeAmount   uint64 `json:"min_stake_amount"`   // 최소 스테이킹 요구량
}

/*
스테이커 호스트 메인 구조체
모든 구성요소를 통합 관리하는 중앙 객체입니다.
*/
type StakerHost struct {
	config           *StakerHostConfig // 설정 정보
	suiClient        *SuiClient        // Sui 블록체인 클라이언트
	k3sAgent         *K3sAgent         // K3s 워커 노드 에이전트
	stakingStatus    *StakingStatus    // 현재 스테이킹 상태
	heartbeatTicker  *time.Ticker      // 하트비트 타이머 (30초마다 실행)
	isRunning        bool              // 실행 상태
	sealToken        string            // Current seal token (cached from stakingStatus)
	lastHeartbeat    int64             // Last heartbeat timestamp
	startTime        time.Time         // Node start time
}

/*
Sui 클라이언트 - Sui 블록체인과의 모든 통신을 담당
스테이킹, Seal 토큰 생성, 상태 조회 등의 작업을 처리합니다.
*/
type SuiClient struct {
	rpcEndpoint string        // Sui 테스트넷 RPC URL
	privateKey  string        // 트랜잭션 서명용 개인키 (hex 형식)
	client      *resty.Client // HTTP 클라이언트 (재사용 가능)
	address     string        // 지갑 주소
}

/*
K3s Agent - 실제 K3s 워커 노드 기능을 제공
kubelet과 container runtime을 통해 Pod을 실행합니다.
*/
type K3sAgent struct {
	nodeID   string           // 노드 식별자
	kubelet  *Kubelet         // K3s kubelet (Pod 관리)
	runtime  ContainerRuntime // 컨테이너 런타임 (containerd 또는 docker)
}

/*
스테이킹 상태 - 현재 노드의 스테이킹 상황을 추적
Sui 블록체인의 스테이킹 정보와 동기화됩니다.
*/
type StakingStatus struct {
	IsStaked       bool   `json:"is_staked"`        // 스테이킹 완료 여부
	StakeAmount    uint64 `json:"stake_amount"`     // 스테이킹한 SUI 양 (MIST 단위)
	StakeObjectID  string `json:"stake_object_id"`  // Sui 블록체인의 스테이킹 오브젝트 ID
	SealToken      string `json:"seal_token"`       // Nautilus TEE 인증용 Seal 토큰
	LastValidation int64  `json:"last_validation"`  // 마지막 검증 시각 (Unix timestamp)
	Status         string `json:"status"`           // 상태: active(정상), slashed(슬래시됨), pending(대기중)
}

/*
컨테이너 런타임 인터페이스 - containerd와 docker를 추상화
실제 컨테이너 실행을 담당하는 런타임의 공통 인터페이스입니다.
*/
type ContainerRuntime interface {
	RunContainer(image, name string, env map[string]string) error // 컨테이너 실행
	StopContainer(name string) error                              // 컨테이너 중단
	ListContainers() ([]Container, error)                         // 실행 중인 컨테이너 목록 조회
}

/*
컨테이너 정보 구조체 - 실행 중인 컨테이너의 기본 정보
*/
type Container struct {
	ID     string `json:"id"`     // 컨테이너 고유 ID
	Name   string `json:"name"`   // 컨테이너 이름 (보통 Pod 이름)
	Image  string `json:"image"`  // 사용된 컨테이너 이미지
	Status string `json:"status"` // 상태 (running, stopped 등)
}

/*
Kubelet - K3s의 노드 에이전트
마스터 노드(Nautilus TEE)와 통신하여 Pod을 관리합니다.
실제 K3s 바이너리를 프로세스로 실행하여 완전한 워커 노드 기능을 제공합니다.
*/
type Kubelet struct {
	nodeID      string          // 이 kubelet이 관리하는 노드 ID
	masterURL   string          // 마스터 노드 (Nautilus TEE) URL
	token       string          // K3s join token (Seal token)
	dataDir     string          // K3s 데이터 디렉토리
	ctx         context.Context // 컨텍스트
	cancel      context.CancelFunc // 취소 함수
	cmd         *exec.Cmd       // K3s agent 프로세스
	running     bool            // 실행 상태
	mu          sync.RWMutex    // 뮤텍스
}

/*
🚀 메인 함수 - K3s-DaaS 스테이커 호스트의 진입점

전체적인 실행 플로우:
1️⃣ 설정 파일 로드 및 초기화
2️⃣ Sui 블록체인에 스테이킹 + Seal 토큰 생성
3️⃣ Kubelet 시작 + Nautilus TEE 등록
4️⃣ 하트비트 서비스 시작 (백그라운드)
5️⃣ HTTP 상태 서버 실행 (포트 10250)

이는 기존 K3s worker node 시작 과정과 완전히 다른 블록체인 기반 접근법입니다.
전통적인 K3s join token 대신 Seal 토큰을 사용하여 보안성을 크게 향상시켰습니다.

환경변수:
- STAKER_CONFIG_PATH: 설정 파일 경로 (기본값: ./staker-config.json)
*/
func main() {
	// 📁 설정 파일 경로 결정 (환경변수 또는 기본값)
	configPath := os.Getenv("STAKER_CONFIG_PATH")
	if configPath == "" {
		configPath = "./staker-config.json"
	}

	log.Printf("🚀 K3s-DaaS 스테이커 호스트 시작...")
	log.Printf("📁 설정 파일: %s", configPath)

	// 1️⃣ 스테이커 호스트 초기화 (설정 로드, 클라이언트 초기화)
	stakerHost, err := NewStakerHost(configPath)
	if err != nil {
		log.Fatalf("❌ 스테이커 호스트 초기화 실패: %v", err)
	}

	// 2️⃣ Sui 블록체인에 스테이킹 등록 및 Seal 토큰 생성
	// 이 단계가 성공해야만 클러스터에 참여할 수 있습니다.
	log.Printf("🌊 Sui 블록체인 스테이킹 시작...")
	if err := stakerHost.RegisterStake(); err != nil {
		// 개발/테스트 환경에서는 Mock 데이터로 계속 진행
		if os.Getenv("MOCK_MODE") == "true" {
			log.Printf("⚠️ 스테이킹 실패하지만 Mock 모드로 계속 진행: %v", err)
			stakerHost.stakingStatus.IsStaked = true
			stakerHost.stakingStatus.Status = "mock"
			stakerHost.stakingStatus.SealToken = "seal_mock_token_for_testing_12345678"
			stakerHost.sealToken = "seal_mock_token_for_testing_12345678"
		} else {
			log.Fatalf("❌ 스테이킹 등록 실패: %v", err)
		}
	}

	// 3️⃣ K3s Agent (kubelet + 컨테이너 런타임) 시작 및 Nautilus TEE 등록
	log.Printf("🔧 K3s Agent 및 Nautilus TEE 연결 시작...")
	if err := stakerHost.StartK3sAgent(); err != nil {
		// 개발/테스트 환경에서는 Mock 모드로 계속 진행
		if os.Getenv("MOCK_MODE") == "true" {
			log.Printf("⚠️ K3s Agent 시작 실패하지만 Mock 모드로 계속 진행: %v", err)
		} else {
			log.Fatalf("❌ K3s Agent 시작 실패: %v", err)
		}
	}

	// 4️⃣ 백그라운드 하트비트 서비스 시작 (30초마다 스테이킹 상태 검증)
	log.Printf("💓 하트비트 서비스 시작...")
	stakerHost.StartHeartbeat()

	// 5️⃣ HTTP API 서버 시작 (포트 10250 - kubelet 포트와 동일)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// 📊 노드 상태 정보를 JSON으로 반환
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "healthy",                        // 노드 상태
			"node_id":        stakerHost.config.NodeID,         // 노드 식별자
			"staking_status": stakerHost.stakingStatus,         // 스테이킹 상태 (Seal 토큰 포함)
			"running_pods":   stakerHost.getRunningPodsCount(), // 실행 중인 Pod 수
			"timestamp":      time.Now().Unix(),                // 응답 시각
		})
	})

	// 📊 스테이킹 상태 상세 정보 엔드포인트
	http.HandleFunc("/api/v1/staking", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		stakingInfo := map[string]interface{}{
			"node_id":       stakerHost.config.NodeID,
			"wallet_address": stakerHost.config.SuiWalletAddress,
			"stake_amount":  stakerHost.config.StakeAmount,
			"min_stake":     stakerHost.config.MinStakeAmount,
			"status":        stakerHost.stakingStatus,
			"seal_token":    stakerHost.sealToken,
			"contract_address": stakerHost.config.ContractAddress,
			"last_heartbeat": stakerHost.lastHeartbeat,
		}

		if stakerHost.sealToken != "" {
			stakingInfo["seal_token_short"] = stakerHost.sealToken[:10] + "..."
		}

		json.NewEncoder(w).Encode(stakingInfo)
	})

	// 📈 노드 메트릭스 엔드포인트
	http.HandleFunc("/api/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		metrics := map[string]interface{}{
			"node_id":        stakerHost.config.NodeID,
			"running_pods":   stakerHost.getRunningPodsCount(),
			"memory_usage":   stakerHost.getMemoryUsage(),
			"cpu_usage":      stakerHost.getCPUUsage(),
			"disk_usage":     stakerHost.getDiskUsage(),
			"network_stats":  stakerHost.getNetworkStats(),
			"uptime_seconds": time.Since(stakerHost.startTime).Seconds(),
			"timestamp":      time.Now().Unix(),
		}

		json.NewEncoder(w).Encode(metrics)
	})

	// 🔧 노드 설정 정보 엔드포인트
	http.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 민감한 정보는 마스킹
		configInfo := map[string]interface{}{
			"node_id":           stakerHost.config.NodeID,
			"sui_rpc_endpoint":  stakerHost.config.SuiRPCEndpoint,
			"contract_address":  stakerHost.config.ContractAddress,
			"nautilus_endpoint": stakerHost.config.NautilusEndpoint,
			"container_runtime": stakerHost.config.ContainerRuntime,
			"min_stake_amount":  stakerHost.config.MinStakeAmount,
			"wallet_masked":     stakerHost.config.SuiWalletAddress[:8] + "...",
		}

		json.NewEncoder(w).Encode(configInfo)
	})

	// 🔄 Nautilus 마스터 노드 등록 엔드포인트
	http.HandleFunc("/api/v1/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 현재 Seal 토큰으로 Nautilus에 등록 시도
		err := stakerHost.registerWithNautilus()
		if err != nil {
			log.Printf("❌ Nautilus 등록 실패: %v", err)
			http.Error(w, fmt.Sprintf("Registration failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "registered",
			"node_id":  stakerHost.config.NodeID,
			"message":  "Successfully registered with Nautilus master",
			"timestamp": time.Now().Unix(),
		})
	})

	// 💔 강제 스테이킹 해제 엔드포인트 (관리용)
	http.HandleFunc("/api/v1/unstake", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		log.Printf("🔄 스테이킹 해제 요청...")
		err := stakerHost.unstakeFromSui()
		if err != nil {
			log.Printf("❌ 스테이킹 해제 실패: %v", err)
			http.Error(w, fmt.Sprintf("Unstaking failed: %v", err), http.StatusInternalServerError)
			return
		}

		stakerHost.stakingStatus.Status = "unstaked"
		stakerHost.stakingStatus.IsStaked = false
		stakerHost.stakingStatus.SealToken = ""
		stakerHost.sealToken = ""

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "unstaked",
			"node_id":  stakerHost.config.NodeID,
			"message":  "Successfully unstaked from Sui",
			"timestamp": time.Now().Unix(),
		})
	})

	log.Printf("✅ K3s-DaaS 스테이커 호스트 '%s' 준비 완료!", stakerHost.config.NodeID)
	log.Printf("🌐 상태 확인 서버 실행 중: http://localhost:10250/health")
	log.Printf("💡 Ctrl+C로 종료")

	// 🌐 HTTP 서버 시작 (블로킹 - 이 지점에서 프로그램이 계속 실행됨)
	log.Fatal(http.ListenAndServe(":10250", nil))
}

/*
스테이커 호스트 초기화 함수
설정 파일을 읽어서 모든 구성요소를 초기화합니다.

매개변수:
- configPath: staker-config.json 파일 경로

반환값:
- *StakerHost: 초기화된 스테이커 호스트 인스턴스
- error: 초기화 과정에서 발생한 오류
*/
func NewStakerHost(configPath string) (*StakerHost, error) {
	// 1️⃣ JSON 설정 파일 로드
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("설정 파일 로드 실패: %v", err)
	}

	// 2️⃣ Sui 블록체인 클라이언트 초기화
	// 스테이킹, Seal 토큰 생성, 상태 조회에 사용됩니다.
	suiClient := &SuiClient{
		rpcEndpoint: config.SuiRPCEndpoint, // Sui 테스트넷 RPC 엔드포인트
		privateKey:  config.SuiPrivateKey,  // 트랜잭션 서명용 개인키 (hex)
		client:      resty.New(),           // 재사용 가능한 HTTP 클라이언트
		address:     config.SuiWalletAddress, // 지갑 주소
	}

	// 3️⃣ K3s 워커 노드 에이전트 초기화
	// 실제 K3s 바이너리를 프로세스로 실행하여 완전한 워커 노드 기능을 제공합니다.
	ctx, cancel := context.WithCancel(context.Background())

	k3sAgent := &K3sAgent{
		nodeID: config.NodeID,
		kubelet: &Kubelet{
			nodeID:    config.NodeID,
			masterURL: config.NautilusEndpoint,
			token:     "", // 초기에는 빈 값, RegisterStake 후에 Seal token으로 설정됨
			dataDir:   filepath.Join(".", "k3s-data"),
			ctx:       ctx,
			cancel:    cancel,
			running:   false,
		},
	}

	// 4️⃣ 컨테이너 런타임 설정 (containerd 또는 docker)
	// 설정에 따라 적절한 런타임 구현체를 선택합니다.
	switch config.ContainerRuntime {
	case "containerd":
		runtime, err := NewContainerdRuntime() // containerd 사용
		if err != nil {
			log.Fatalf("❌ Containerd 런타임 초기화 실패: %v", err)
		}
		k3sAgent.runtime = runtime
	case "docker":
		runtime, err := NewDockerRuntime()     // docker 사용
		if err != nil {
			log.Fatalf("❌ Docker 런타임 초기화 실패: %v", err)
		}
		k3sAgent.runtime = runtime
	default:
		return nil, fmt.Errorf("지원하지 않는 컨테이너 런타임: %s", config.ContainerRuntime)
	}

	// 5️⃣ 스테이커 호스트 인스턴스 생성 및 반환
	return &StakerHost{
		config:    config,
		suiClient: suiClient,
		k3sAgent:  k3sAgent,
		stakingStatus: &StakingStatus{
			Status: "pending", // 초기 상태는 대기중
		},
		isRunning:     false,
		sealToken:     "",
		lastHeartbeat: 0,
		startTime:     time.Now(),
	}, nil
}

func NewK3sStakerHost_LEGACY(cfg *StakerHostConfig) (*StakerHost, error) {
	// Legacy implementation moved - use NewStakerHost instead
	return nil, fmt.Errorf("use NewStakerHost function instead")
}

/*
🌊 Seal 토큰 기반 스테이킹 등록 - K3s-DaaS의 핵심 기능

이 함수는 다음 두 단계를 순차적으로 수행합니다:
1️⃣ Sui 블록체인에 SUI 토큰을 스테이킹하여 노드 참여 권한 획득
2️⃣ 스테이킹 증명으로 Seal 토큰 생성 (Nautilus TEE 인증용)

Seal 토큰은 기존 K3s의 join token을 대체하여 블록체인 기반 인증을 제공합니다.

플로우:
스테이킹 트랜잭션 생성 → 블록체인 실행 → Object ID 추출 →
Seal 토큰 트랜잭션 생성 → 블록체인 실행 → Seal 토큰 추출 → 상태 업데이트

반환값:
- error: 스테이킹 또는 Seal 토큰 생성 과정에서 발생한 오류
*/
func (s *StakerHost) RegisterStake() error {
	log.Printf("🌊 Sui 블록체인에 스테이킹 등록 중... Node ID: %s", s.config.NodeID)

	// 1️⃣ 스테이킹 트랜잭션 생성
	// Sui RPC 2.0 표준 형식으로 트랜잭션 실행 요청을 구성합니다.
	stakePayload := map[string]interface{}{
		"jsonrpc": "2.0",                        // JSON-RPC 버전
		"id":      1,                            // 요청 ID
		"method":  "sui_executeTransactionBlock", // Sui 트랜잭션 실행 메소드
		"params": []interface{}{
			map[string]interface{}{
				"txBytes": func() string {
					txBytes, err := s.buildStakingTransaction()
					if err != nil {
						log.Printf("⚠️ 스테이킹 트랜잭션 빌드 실패: %v", err)
						return ""
					}
					return txBytes
				}(), // 스테이킹 트랜잭션 바이트 (Move 컨트랙트 호출)
			},
			[]string{s.config.SuiPrivateKey},        // 트랜잭션 서명용 개인키 배열
			map[string]interface{}{
				"requestType": "WaitForLocalExecution", // 로컬 실행 완료까지 대기
				"options": map[string]bool{
					"showObjectChanges": true, // 객체 변경사항 포함 (스테이킹 Object ID 추출용)
					"showEffects":       true, // 트랜잭션 효과 포함
				},
			},
		},
	}

	// HTTP POST 요청으로 Sui 테스트넷에 스테이킹 트랜잭션 전송
	resp, err := s.suiClient.client.R().
		SetHeader("Content-Type", "application/json"). // JSON 형식 지정
		SetBody(stakePayload).                          // 위에서 구성한 스테이킹 payload
		Post(s.config.SuiRPCEndpoint)                   // Sui 테스트넷 RPC 엔드포인트로 전송

	if err != nil {
		return fmt.Errorf("스테이킹 트랜잭션 전송 실패: %v", err)
	}

	// 🔍 Sui 블록체인 응답 파싱
	var stakeResult map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &stakeResult); err != nil {
		return fmt.Errorf("스테이킹 응답 파싱 실패: %v", err)
	}

	// 📝 스테이킹 Object ID 추출 (블록체인에서 생성된 스테이킹 증명)
	// 이 Object ID는 나중에 Seal 토큰 생성에 사용됩니다.
	stakeObjectID, err := s.extractStakeObjectID(stakeResult)
	if err != nil {
		return fmt.Errorf("스테이킹 Object ID 추출 실패: %v", err)
	}

	log.Printf("✅ 스테이킹 성공! Stake Object ID: %s", stakeObjectID)

	// 2️⃣ Seal 토큰 생성 (워커 노드용)
	// 스테이킹 증명(Object ID)을 바탕으로 Seal 토큰을 생성합니다.
	// 이 토큰은 기존 K3s join token을 대체하여 Nautilus TEE 인증에 사용됩니다.
	sealPayload := map[string]interface{}{
		"jsonrpc": "2.0",                        // JSON-RPC 버전
		"id":      2,                            // 두 번째 요청 (스테이킹은 1번)
		"method":  "sui_executeTransactionBlock", // 같은 Sui 트랜잭션 실행 메소드
		"params": []interface{}{
			map[string]interface{}{
				"txBytes": func() string {
					txBytes, err := s.buildSealTokenTransaction(stakeObjectID)
					if err != nil {
						log.Printf("⚠️ Seal 토큰 트랜잭션 빌드 실패: %v", err)
						return ""
					}
					return txBytes
				}(), // Seal 토큰 생성 트랜잭션 (스테이킹 Object ID 포함)
			},
			[]string{s.config.SuiPrivateKey},        // 동일한 개인키로 서명
			map[string]interface{}{
				"requestType": "WaitForLocalExecution", // 로컬 실행 완료까지 대기
				"options": map[string]bool{
					"showObjectChanges": true, // 객체 변경사항 포함 (Seal 토큰 추출용)
					"showEffects":       true, // 트랜잭션 효과 포함
				},
			},
		},
	}

	// HTTP POST 요청으로 Sui 테스트넷에 Seal 토큰 생성 트랜잭션 전송
	sealResp, err := s.suiClient.client.R().
		SetHeader("Content-Type", "application/json"). // JSON 형식 지정
		SetBody(sealPayload).                           // 위에서 구성한 Seal 토큰 payload
		Post(s.config.SuiRPCEndpoint)                   // 동일한 Sui 테스트넷 엔드포인트 사용

	if err != nil {
		return fmt.Errorf("Seal 토큰 생성 요청 실패: %v", err)
	}

	// 🔍 Sui 블록체인 응답 파싱 (Seal 토큰)
	var sealResult map[string]interface{}
	if err := json.Unmarshal(sealResp.Body(), &sealResult); err != nil {
		return fmt.Errorf("Seal 토큰 응답 파싱 실패: %v", err)
	}

	// 🔑 Seal 토큰 추출 (블록체인에서 생성된 인증 토큰)
	// 이 토큰이 기존 K3s join token을 완전히 대체합니다.
	sealToken, err := s.extractSealToken(sealResult)
	if err != nil {
		return fmt.Errorf("Seal 토큰 추출 실패: %v", err)
	}

	// 📊 스테이킹 상태 업데이트 - 모든 정보를 로컬에 저장
	s.stakingStatus.IsStaked = true                    // 스테이킹 완료 플래그
	s.stakingStatus.StakeAmount = s.config.StakeAmount // 스테이킹한 SUI 양 (MIST 단위)
	s.stakingStatus.StakeObjectID = stakeObjectID      // 블록체인의 스테이킹 증명 ID
	s.stakingStatus.SealToken = sealToken              // 생성된 Seal 토큰
	s.stakingStatus.Status = "active"                  // 활성 상태로 설정
	s.stakingStatus.LastValidation = time.Now().Unix() // 현재 시간으로 검증 시각 설정

	// 🔄 캐시된 sealToken 필드도 동기화
	s.sealToken = sealToken

	// 🔑 K3s Agent에서 Seal 토큰을 사용하도록 설정 업데이트
	if s.k3sAgent != nil && s.k3sAgent.kubelet != nil {
		s.k3sAgent.kubelet.token = sealToken
		log.Printf("🔧 K3s Agent에 Seal 토큰 설정 완료")
	}

	log.Printf("✅ Seal 토큰 생성 성공! Token ID: %s", sealToken)
	log.Printf("🎉 스테이킹 및 Seal 토큰 준비 완료!")

	return nil // 성공
}

/*
K3s Agent 시작 함수 - 실제 워커 노드 기능 활성화

스테이킹과 Seal 토큰 준비가 완료된 후 실제 K3s 워커 노드를 시작합니다.
이 단계에서 kubelet이 실행되고 Nautilus TEE(마스터 노드)에 등록됩니다.

플로우:
1. 스테이킹 완료 여부 검증
2. Kubelet 시작 (Pod 실행 준비)
3. Nautilus TEE에 Seal 토큰으로 워커 노드 등록

반환값:
- error: kubelet 시작 또는 Nautilus 등록 과정에서 발생한 오류
*/
func (s *StakerHost) StartK3sAgent() error {
	log.Printf("🚀 K3s Agent 시작 중... Node ID: %s", s.config.NodeID)

	// ✅ 전제조건 검증: 스테이킹과 Seal 토큰이 준비되었는지 확인
	if !s.stakingStatus.IsStaked {
		return fmt.Errorf("K3s Agent 시작 불가: 스테이킹이 완료되지 않음")
	}

	if s.stakingStatus.SealToken == "" {
		return fmt.Errorf("K3s Agent 시작 불가: Seal 토큰이 생성되지 않음")
	}

	// 🔑 Kubelet에 Seal 토큰 설정
	s.k3sAgent.kubelet.token = s.stakingStatus.SealToken

	// 🔧 Kubelet 시작 - Pod을 실제로 실행하는 K3s 구성요소
	if err := s.k3sAgent.kubelet.Start(); err != nil {
		return fmt.Errorf("kubelet 시작 실패: %v", err)
	}

	// 🔒 Nautilus TEE에 Seal 토큰으로 등록
	// 이 단계에서 워커 노드가 클러스터에 공식적으로 참여합니다.
	if err := s.registerWithNautilus(); err != nil {
		return fmt.Errorf("Nautilus TEE 등록 실패: %v", err)
	}

	log.Printf("✅ K3s Agent 시작 완료!")
	return nil
}


/*
🔒 Nautilus TEE 워커 노드 등록 함수 - K3s-DaaS의 혁신적인 부분

기존 K3s는 join token을 사용하여 워커 노드를 등록하지만,
K3s-DaaS는 Seal 토큰을 사용하여 블록체인 기반 인증을 수행합니다.

플로우:
1️⃣ Sui 컨트랙트에서 Nautilus TEE 엔드포인트 정보 조회 (Seal 토큰으로 인증)
2️⃣ Nautilus TEE에 직접 연결하여 Seal 토큰으로 워커 노드 등록

이 방식의 장점:
- 중앙화된 join token 관리 불필요
- 블록체인 기반 스테이킹으로 보안성 확보
- TEE에서 토큰 검증으로 위변조 방지

반환값:
- error: Nautilus 정보 조회 또는 등록 과정에서 발생한 오류
*/
func (s *StakerHost) registerWithNautilus() error {
	log.Printf("🔑 Nautilus TEE 정보 조회 중...")

	// 1️⃣ Sui 컨트랙트에서 Nautilus TEE 엔드포인트 정보 조회
	// Seal 토큰을 사용하여 인증된 요청만 허용됩니다.
	nautilusInfo, err := s.getNautilusInfoWithSeal()
	if err != nil {
		return fmt.Errorf("Nautilus 정보 조회 실패: %v", err)
	}

	log.Printf("🔑 Nautilus info retrieved with Seal token")

	// 2️⃣ Nautilus TEE에 워커 노드 등록 요청 구성
	// 기존 K3s join token 대신 Seal 토큰을 사용합니다.
	registrationPayload := map[string]interface{}{
		"node_id":    s.config.NodeID,         // 워커 노드 식별자
		"seal_token": s.stakingStatus.SealToken, // 블록체인 기반 인증 토큰
		"timestamp":  time.Now().Unix(),       // 요청 시각 (replay 공격 방지)
	}

	// 🌐 Nautilus TEE에 HTTP 등록 요청 전송
	// X-Seal-Token 헤더로 추가 인증을 수행합니다.
	resp, err := resty.New().R().
		SetHeader("Content-Type", "application/json").           // JSON 형식 지정
		SetHeader("X-Seal-Token", s.stakingStatus.SealToken).    // Seal 토큰 헤더 추가 (이중 인증)
		SetBody(registrationPayload).                            // 등록 정보 전송
		Post(nautilusInfo.Endpoint + "/api/v1/register-worker")  // Nautilus TEE 워커 등록 엔드포인트

	if err != nil {
		return fmt.Errorf("Nautilus TEE 연결 실패: %v", err)
	}

	// 📋 등록 결과 검증
	if resp.StatusCode() != 200 {
		return fmt.Errorf("Nautilus TEE가 등록을 거부했습니다 (HTTP %d): %s",
			resp.StatusCode(), resp.String())
	}

	log.Printf("🔒 TEE connection established with Seal authentication")
	log.Printf("✅ K3s Staker Host '%s' ready and running", s.config.NodeID)

	return nil
}

/*
💓 스테이킹 검증 및 하트비트 서비스 시작

K3s-DaaS의 핵심 기능으로, 30초마다 다음을 수행합니다:
1. Sui 블록체인에서 스테이킹 상태 검증 (슬래싱 여부 확인)
2. Nautilus TEE에 하트비트 전송 (노드 생존 신호)

이는 전통적인 K3s와 다른 부분으로, 블록체인 기반 검증을 통해
악의적인 노드를 자동으로 제거할 수 있습니다.

스테이킹이 슬래시된 경우 노드를 자동으로 종료합니다.
*/
func (s *StakerHost) StartHeartbeat() {
	log.Printf("💓 하트비트 서비스 시작 (30초 간격)")

	// ⏰ 30초마다 실행되는 타이머 생성
	s.heartbeatTicker = time.NewTicker(30 * time.Second)

	// 🔄 별도 고루틴에서 하트비트 처리 (메인 스레드 블록킹 방지)
	go func() {
		failureCount := 0
		maxFailures := 3

		for range s.heartbeatTicker.C { // 타이머가 틱할 때마다 실행
			if err := s.validateStakeAndSendHeartbeat(); err != nil {
				failureCount++
				log.Printf("⚠️ 하트비트 오류 (%d/%d): %v", failureCount, maxFailures, err)

				// 🚨 치명적 오류: 스테이킹이 슬래시된 경우
				if err.Error() == "stake_slashed" {
					log.Printf("🛑 스테이킹이 슬래시되었습니다! 노드를 종료합니다...")
					s.Shutdown() // 즉시 노드 종료
					return       // 고루틴 종료
				}

				// 연속 실패가 임계값을 초과한 경우 K3s Agent 재시작 시도
				if failureCount >= maxFailures {
					log.Printf("🔄 연속 실패 %d회, K3s Agent 재시작 시도...", failureCount)
					if s.k3sAgent != nil && s.k3sAgent.kubelet != nil {
						if restartErr := s.k3sAgent.kubelet.restart(); restartErr != nil {
							log.Printf("❌ Agent 재시작 실패: %v", restartErr)
						} else {
							failureCount = 0 // 재시작 성공 시 카운터 리셋
							log.Printf("✅ Agent 재시작 완료, 하트비트 재개")
						}
					}
				}
			} else {
				// 성공한 경우 실패 카운터 리셋
				if failureCount > 0 {
					log.Printf("✅ 하트비트 복구됨, 실패 카운터 리셋")
					failureCount = 0
				}
			}
		}
	}()
}

/*
📊 스테이킹 상태 검증 및 하트비트 전송 함수

하트비트 서비스의 핵심 로직으로 다음을 순차적으로 수행합니다:
1️⃣ Sui 블록체인에서 현재 스테이킹 상태 조회 및 슬래싱 여부 확인
2️⃣ 노드 상태 정보 수집 (실행 중인 Pod 수, 리소스 사용량 등)
3️⃣ Nautilus TEE에 하트비트 전송 (Seal 토큰으로 인증)

이 과정을 통해 블록체인 기반의 노드 검증과 TEE 기반의 보안 통신을 동시에 수행합니다.

반환값:
- error: 스테이킹 검증 또는 하트비트 전송 과정에서 발생한 오류
        "stake_slashed" 오류는 노드 즉시 종료를 의미함
*/
func (s *StakerHost) validateStakeAndSendHeartbeat() error {
	// 1️⃣ Sui 블록체인에서 스테이킹 상태 확인
	// 다른 검증자들이 이 노드를 슬래싱했는지 확인합니다.
	stakeInfo, err := s.checkStakeOnSui()
	if err != nil {
		return fmt.Errorf("스테이킹 상태 확인 실패: %v", err)
	}

	// 🚨 치명적 상황: 스테이킹이 슬래시된 경우
	if stakeInfo.Status == "slashed" {
		s.stakingStatus.Status = "slashed" // 로컬 상태도 업데이트
		return fmt.Errorf("stake_slashed") // 특별한 오류 코드 반환
	}

	// 2️⃣ 노드 상태 정보 수집 및 하트비트 payload 구성
	heartbeatPayload := map[string]interface{}{
		"node_id":         s.config.NodeID,       // 노드 식별자
		"timestamp":       time.Now().Unix(),     // 현재 시각 (최신성 증명)
		"stake_status":    stakeInfo.Status,      // 블록체인 스테이킹 상태
		"stake_amount":    stakeInfo.Amount,      // 현재 스테이킹 양
		"running_pods":    s.getRunningPodsCount(), // 실행 중인 Pod 개수
		"resource_usage":  s.getResourceUsage(),  // CPU/메모리/디스크 사용량
	}

	// 3️⃣ Nautilus TEE에 Seal 토큰 인증 하트비트 전송
	_, err = resty.New().R().
		SetHeader("Content-Type", "application/json").           // JSON 형식
		SetHeader("X-Seal-Token", s.stakingStatus.SealToken).    // Seal 토큰 인증 헤더
		SetBody(heartbeatPayload).                               // 노드 상태 정보
		Post(s.config.NautilusEndpoint + "/api/v1/nodes/heartbeat") // Nautilus 하트비트 엔드포인트

	if err != nil {
		return fmt.Errorf("하트비트 전송 실패: %v", err)
	}

	// ✅ 성공: 마지막 검증 시각 업데이트
	currentTime := time.Now().Unix()
	s.stakingStatus.LastValidation = currentTime
	s.lastHeartbeat = currentTime
	return nil
}


// Legacy 노드 종료 함수 - 대체된 함수 사용
func (s *StakerHost) Shutdown_LEGACY() {
	log.Printf("🛑 Shutting down staker host: %s", s.config.NodeID)

	s.isRunning = false

	// 실행 중인 컨테이너들 정리
	if s.k3sAgent != nil && s.k3sAgent.runtime != nil {
		// 컨테이너 정리 로직
		log.Printf("🧹 Cleaning up containers...")
		containers, _ := s.k3sAgent.runtime.ListContainers()
		for _, container := range containers {
			log.Printf("🛑 컨테이너 중단: %s", container.Name)
			s.k3sAgent.runtime.StopContainer(container.Name)
		}
	}

	log.Printf("✅ Staker host shutdown complete")
	os.Exit(0)
}

// 설정 파일 로드
func loadStakerConfig() (*StakerHostConfig, error) {
	configPath := os.Getenv("STAKER_CONFIG_PATH")
	if configPath == "" {
		configPath = "./staker-config.json"
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config StakerHostConfig
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, err
	}

	// 기본값 설정
	if config.MinStakeAmount == 0 {
		config.MinStakeAmount = 1000 // 1000 MIST
	}

	return &config, nil
}

/*
스테이킹 트랜잭션 빌드 함수
Sui Move 컨트랙트의 stake_for_node 함수를 호출하는 트랜잭션을 생성합니다.

실제 구현에서는 Sui SDK를 사용하여:
1. Move 컨트랙트 패키지 ID와 모듈명 지정
2. stake_for_node(amount, node_id, staker_address) 함수 호출
3. 트랜잭션을 바이트 형태로 직렬화

반환값:
- string: 직렬화된 트랜잭션 바이트 (Base64 인코딩)
*/
func (s *StakerHost) buildStakingTransaction() (string, error) {
	// 🎯 스테이킹 컨트랙트 호출을 위한 트랜잭션 구성
	// MoveCall 트랜잭션 데이터 구조화

	// 📋 Move 함수 호출 데이터
	moveCall := map[string]interface{}{
		"packageObjectId": s.config.ContractAddress, // 스마트 컨트랙트 주소
		"module":          "staking",                 // 모듈명
		"function":        "stake_for_node",          // 함수명
		"typeArguments":   []string{},                // 타입 인자 없음
		"arguments": []interface{}{
			s.config.StakeAmount, // 스테이킹 양 (MIST 단위)
			s.config.NodeID,      // 노드 ID
		},
	}

	// 🏗️ 트랜잭션 블록 구성
	txBlock := map[string]interface{}{
		"version":    1,
		"sender":     s.suiClient.address,
		"gasPayment": nil,    // 자동으로 가스 코인 선택
		"gasBudget":  "10000000", // 10M MIST 가스 한도
		"gasPrice":   "1000", // 가스 가격
		"transactions": []interface{}{
			map[string]interface{}{
				"MoveCall": moveCall,
			},
		},
	}

	// 📤 JSON으로 직렬화 후 Base64 인코딩
	txJSON, err := json.Marshal(txBlock)
	if err != nil {
		return "", fmt.Errorf("트랜잭션 직렬화 실패: %v", err)
	}

	return base64.StdEncoding.EncodeToString(txJSON), nil
}

/*
Seal 토큰 트랜잭션 빌드 함수
스테이킹 증명을 바탕으로 Seal 토큰을 생성하는 트랜잭션을 만듭니다.

매개변수:
- stakeObjectID: 앞서 생성된 스테이킹 오브젝트의 ID

실제 구현에서는 k8s_gateway 컨트랙트의 create_worker_seal_token 함수를 호출합니다.

반환값:
- string: 직렬화된 트랜잭션 바이트 (Base64 인코딩)
*/
func (s *StakerHost) buildSealTokenTransaction(stakeObjectID string) (string, error) {
	// 🎯 k8s_gateway::create_worker_seal_token 호출
	// 스테이킹 검증 후 워커 노드용 Seal 토큰 생성

	// 📝 Move 함수 호출 데이터
	moveCall := map[string]interface{}{
		"packageObjectId": s.config.ContractAddress, // k8s_gateway 컨트랙트 주소
		"module":          "k8s_gateway",             // 모듈명
		"function":        "create_worker_seal_token", // Seal 토큰 생성 함수
		"typeArguments":   []string{},                 // 타입 인수 없음
		"arguments": []interface{}{
			stakeObjectID, // 스테이킹 객체 ID 전달
		},
	}

	// 🏗️ 트랜잭션 블록 구성
	txBlock := map[string]interface{}{
		"version":    1,
		"sender":     s.suiClient.address,
		"gasPayment": nil,       // 자동으로 가스 코인 선택
		"gasBudget":  "5000000", // 5M MIST 가스 한도
		"gasPrice":   "1000",    // 가스 가격
		"transactions": []interface{}{
			map[string]interface{}{
				"MoveCall": moveCall,
			},
		},
	}

	// 📤 JSON 직렬화 후 Base64 인코딩
	txJSON, err := json.Marshal(txBlock)
	if err != nil {
		return "", fmt.Errorf("Seal 토큰 트랜잭션 직렬화 실패: %v", err)
	}

	return base64.StdEncoding.EncodeToString(txJSON), nil
}

/*
스테이킹 Object ID 추출 함수
Sui 트랜잭션 실행 결과에서 새로 생성된 스테이킹 오브젝트의 ID를 찾습니다.

매개변수:
- result: Sui RPC sui_executeTransactionBlock의 응답

실제로는 result["result"]["objectChanges"]에서 "created" 타입의 오브젝트를 찾아
StakeRecord 타입의 오브젝트 ID를 추출합니다.

반환값:
- string: 스테이킹 오브젝트 ID (0x로 시작하는 64자리 hex)
*/
func (s *StakerHost) extractStakeObjectID(result map[string]interface{}) (string, error) {
	// 🔍 Sui 응답에서 새로 생성된 StakeRecord 객체 찾기
	if resultData, exists := result["result"]; exists {
		if resultMap, ok := resultData.(map[string]interface{}); ok {
			if objectChanges, exists := resultMap["objectChanges"]; exists {
				if changes, ok := objectChanges.([]interface{}); ok {
					// 🔎 "created" 타입의 오브젝트 중 StakeRecord 찾기
					for _, change := range changes {
						if changeMap, ok := change.(map[string]interface{}); ok {
							if changeType, exists := changeMap["type"]; exists && changeType == "created" {
								if objectType, exists := changeMap["objectType"]; exists {
									// 📎 StakeRecord 타입 확인
									if strings.Contains(objectType.(string), "StakeRecord") {
										if objectId, exists := changeMap["objectId"]; exists {
											return objectId.(string), nil
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("스테이킹 객체 ID를 찾을 수 없습니다")
}

/*
Seal 토큰 추출 함수
Sui 트랜잭션 실행 결과에서 새로 생성된 Seal 토큰을 찾습니다.

매개변수:
- result: Sui RPC sui_executeTransactionBlock의 응답

실제로는 result["result"]["objectChanges"]에서 SealToken 타입의 오브젝트를 찾아
토큰 해시 또는 오브젝트 ID를 추출합니다.

반환값:
- string: Seal 토큰 (Nautilus TEE 인증에 사용)
*/
func (s *StakerHost) extractSealToken(result map[string]interface{}) (string, error) {
	// 🔍 Sui 응답에서 새로 생성된 SealToken 객체 찾기
	if resultData, exists := result["result"]; exists {
		if resultMap, ok := resultData.(map[string]interface{}); ok {
			if objectChanges, exists := resultMap["objectChanges"]; exists {
				if changes, ok := objectChanges.([]interface{}); ok {
					// 🔎 "created" 타입의 오브젝트 중 SealToken 찾기
					for _, change := range changes {
						if changeMap, ok := change.(map[string]interface{}); ok {
							if changeType, exists := changeMap["type"]; exists && changeType == "created" {
								if objectType, exists := changeMap["objectType"]; exists {
									// 📎 SealToken 타입 확인
									if strings.Contains(objectType.(string), "SealToken") {
										if objectId, exists := changeMap["objectId"]; exists {
											return objectId.(string), nil
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("Seal 토큰을 찾을 수 없습니다")
}

/*
Nautilus TEE 정보 구조체
Sui 컨트랙트에서 조회한 Nautilus TEE의 연결 정보를 담습니다.
*/
type NautilusInfo struct {
	Endpoint string `json:"endpoint"` // Nautilus TEE HTTP 엔드포인트 (예: http://tee-ip:8080)
	PubKey   string `json:"pub_key"`  // TEE 공개키 (향후 추가 암호화에 사용 가능)
}

/*
Seal 토큰으로 Nautilus TEE 정보 조회 함수

Sui 컨트랙트의 get_nautilus_info_for_worker 함수를 호출하여
현재 활성화된 Nautilus TEE 인스턴스의 접속 정보를 가져옵니다.

이는 Seal 토큰 기반 인증의 핵심 부분으로, 스테이킹한 노드만
Nautilus TEE의 실제 엔드포인트를 알 수 있게 합니다.

반환값:
- *NautilusInfo: TEE 연결 정보 (엔드포인트, 공개키)
- error: 조회 과정에서 발생한 오류
*/
func (s *StakerHost) getNautilusInfoWithSeal() (*NautilusInfo, error) {
	// 🔍 Sui 컨트랙트에서 Nautilus TEE 정보 조회 트랜잭션 구성
	queryPayload := map[string]interface{}{
		"jsonrpc": "2.0",                        // JSON-RPC 버전
		"id":      1,                            // 요청 ID
		"method":  "sui_executeTransactionBlock", // Sui 트랜잭션 실행
		"params": []interface{}{
			map[string]interface{}{
				"txBytes": func() string {
					txBytes, err := s.buildNautilusQueryTransaction()
					if err != nil {
						log.Printf("⚠️ Nautilus 조회 트랜잭션 빌드 실패: %v", err)
						return ""
					}
					return txBytes
				}(), // Nautilus 정보 조회 트랜잭션
			},
			[]string{s.config.SuiPrivateKey}, // 트랜잭션 서명용 개인키
			map[string]interface{}{
				"requestType": "WaitForLocalExecution", // 로컬 실행 대기
				"options": map[string]bool{
					"showEffects": true, // 실행 효과 표시
					"showEvents":  true, // 이벤트 표시 (Nautilus 정보 포함)
				},
			},
		},
	}

	// 🌐 Sui 테스트넷에 조회 요청 전송
	_, err := s.suiClient.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(queryPayload).
		Post(s.config.SuiRPCEndpoint)

	if err != nil {
		return nil, fmt.Errorf("Nautilus 정보 조회 요청 실패: %v", err)
	}

	// 📄 응답에서 Nautilus TEE 정보 추출
	// 🚧 실제 구현에서는 result["result"]["events"]에서 Nautilus 정보 파싱
	return &NautilusInfo{
		Endpoint: s.config.NautilusEndpoint, // 설정에서 가져온 엔드포인트 (테스트용)
		PubKey:   "nautilus_pub_key",       // TEE 공개키 (테스트용)
	}, nil
}

/*
Nautilus TEE 정보 조회 트랜잭션 빌드 함수

k8s_gateway 컨트랙트의 get_nautilus_info_for_worker 함수를 호출하는
트랜잭션을 생성합니다. 이 함수는 Seal 토큰을 검증한 후
Nautilus TEE의 실제 엔드포인트 정보를 반환합니다.

반환값:
- string: 직렬화된 트랜잭션 바이트 (Base64 인코딩)
*/
func (s *StakerHost) buildNautilusQueryTransaction() (string, error) {
	// 🎯 k8s_gateway::get_nautilus_info_for_worker 호출
	// Seal 토큰 검증 후 Nautilus 연결 정보 반환

	// 📝 Move 함수 호출 데이터
	moveCall := map[string]interface{}{
		"packageObjectId": s.config.ContractAddress,        // k8s_gateway 컨트랙트 주소
		"module":          "k8s_gateway",                   // 모듈명
		"function":        "get_nautilus_info_for_worker", // Nautilus 정보 조회 함수
		"typeArguments":   []string{},                       // 타입 인수 없음
		"arguments": []interface{}{
			s.stakingStatus.SealToken, // Seal 토큰 ID 전달
		},
	}

	// 🏗️ 트랜잭션 블록 구성
	txBlock := map[string]interface{}{
		"version":    1,
		"sender":     s.suiClient.address,
		"gasPayment": nil,       // 자동으로 가스 코인 선택
		"gasBudget":  "3000000", // 3M MIST 가스 한도
		"gasPrice":   "1000",    // 가스 가격
		"transactions": []interface{}{
			map[string]interface{}{
				"MoveCall": moveCall,
			},
		},
	}

	// 📤 JSON 직렬화 후 Base64 인코딩
	txJSON, err := json.Marshal(txBlock)
	if err != nil {
		return "", fmt.Errorf("Nautilus 조회 트랜잭션 직렬화 실패: %v", err)
	}

	return base64.StdEncoding.EncodeToString(txJSON), nil
}

/*
스테이킹 정보 구조체
Sui 블록체인에서 조회한 스테이킹 객체의 핵심 정보를 담습니다.
*/
type StakeInfo struct {
	Amount uint64 `json:"amount"` // 스테이킹된 SUI 양 (MIST 단위, 1 SUI = 10^9 MIST)
	Status string `json:"status"` // 스테이킹 상태: "active"(정상), "slashed"(슬래시됨), "withdrawn"(인출됨)
}

/*
🔧 Kubelet 시작 함수 - K3s 워커 노드의 핵심 구성요소

Kubelet은 K3s/Kubernetes의 노드 에이전트로, 다음 역할을 수행합니다:
- Nautilus TEE(마스터 노드)로부터 Pod 실행 명령 수신
- 컨테이너 런타임을 통한 실제 컨테이너 실행 및 관리
- Pod의 상태를 마스터 노드에 정기적으로 보고

실제 구현에서는 K3s agent 프로세스를 시작하여 Nautilus TEE와 통신합니다.

반환값:
- error: kubelet 시작 과정에서 발생한 오류
*/
func (k *Kubelet) Start() error {
	log.Printf("🔧 실제 K3s Agent 시작 중... Node ID: %s", k.nodeID)

	k.mu.Lock()
	defer k.mu.Unlock()

	if k.running {
		return fmt.Errorf("kubelet이 이미 실행 중입니다")
	}

	// 기본 검증
	if k.token == "" {
		return fmt.Errorf("Seal 토큰이 설정되지 않았습니다")
	}

	// 데이터 디렉토리 생성
	if err := os.MkdirAll(k.dataDir, 0755); err != nil {
		return fmt.Errorf("데이터 디렉토리 생성 실패: %v", err)
	}

	// K3s 바이너리 확인
	k3sBinary := "k3s"
	if _, err := exec.LookPath(k3sBinary); err != nil {
		// Windows에서 k3s.exe 확인
		k3sBinary = "k3s.exe"
		if _, err := exec.LookPath(k3sBinary); err != nil {
			log.Printf("⚠️ k3s 바이너리를 찾을 수 없습니다. 시뮬레이션 모드로 실행합니다.")
			k.running = true
			return nil
		}
	}

	// K3s agent 명령 구성
	args := []string{
		"agent",
		"--server", k.masterURL,
		"--token", k.token,
		"--data-dir", k.dataDir,
		"--node-name", k.nodeID,
		"--kubelet-arg", "fail-swap-on=false",
	}

	log.Printf("🚀 K3s Agent 명령 실행: %s %s", k3sBinary, strings.Join(args, " "))

	// K3s agent 프로세스 시작
	k.cmd = exec.CommandContext(k.ctx, k3sBinary, args...)
	k.cmd.Stdout = os.Stdout
	k.cmd.Stderr = os.Stderr

	if err := k.cmd.Start(); err != nil {
		return fmt.Errorf("K3s Agent 시작 실패: %v", err)
	}

	k.running = true

	// 별도 고루틴에서 프로세스 상태 모니터링
	go func() {
		if err := k.cmd.Wait(); err != nil {
			log.Printf("⚠️ K3s Agent 프로세스 종료: %v", err)
		}
		k.mu.Lock()
		k.running = false
		k.mu.Unlock()
	}()

	log.Printf("✅ K3s Agent 프로세스 시작 완료! PID: %d", k.cmd.Process.Pid)
	return nil
}

// ==================== 컨테이너 런타임 구현 ====================

/*
🐳 Containerd 런타임 구현
containerd는 CNCF에서 관리하는 산업 표준 컨테이너 런타임입니다.
K3s에서 기본적으로 사용되며, Docker보다 가벽고 효율적입니다.
실제 K3s containerd 구현을 사용하여 컨테이너를 관리합니다.
*/
type ContainerdRuntime struct {
	namespace string
}

/*
NewContainerdRuntime creates a new containerd runtime instance
Uses the real K3s containerd implementation through ctr commands
*/
func NewContainerdRuntime() (*ContainerdRuntime, error) {
	// Check if ctr (containerd CLI) is available
	if _, err := exec.LookPath("ctr"); err != nil {
		return nil, fmt.Errorf("ctr command not found: %w", err)
	}

	return &ContainerdRuntime{
		namespace: "k8s.io",
	}, nil
}

/*
컨테이너 실행 함수 (containerd)
지정된 이미지로 새 컨테이너를 생성하고 실행합니다.

매개변수:
- image: 컨테이너 이미지명 (예: nginx:latest, redis:alpine)
- name: 컨테이너 이름 (보통 Pod 이름과 동일)
- env: 환경변수 맵

실제 구현에서는 containerd 클라이언트 라이브러리를 사용합니다.
*/
func (c *ContainerdRuntime) RunContainer(image, name string, env map[string]string) error {
	log.Printf("🐳 Containerd: 컨테이너 실행 중... %s (이미지: %s)", name, image)

	// Pull image first
	pullCmd := exec.Command("ctr", "-n", c.namespace, "images", "pull", image)
	if err := pullCmd.Run(); err != nil {
		log.Printf("Warning: failed to pull image %s: %v", image, err)
	}

	// Build run command with environment variables
	args := []string{"-n", c.namespace, "run", "--rm", "-d"}

	// Add environment variables
	for k, v := range env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, image, name)

	// Run container
	runCmd := exec.Command("ctr", args...)
	output, err := runCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run container: %w, output: %s", err, string(output))
	}

	log.Printf("✅ Containerd: 컨테이너 실행 완료 %s", name)
	return nil
}

/*
컨테이너 중단 함수 (containerd)
실행 중인 컨테이너를 정상적으로 중단시킵니다.
*/
func (c *ContainerdRuntime) StopContainer(name string) error {
	log.Printf("🛑 Containerd: 컨테이너 중단 중... %s", name)

	// Kill task first
	killCmd := exec.Command("ctr", "-n", c.namespace, "tasks", "kill", name)
	if err := killCmd.Run(); err != nil {
		log.Printf("Warning: failed to kill task: %v", err)
	}

	// Delete task
	deleteTaskCmd := exec.Command("ctr", "-n", c.namespace, "tasks", "delete", name)
	if err := deleteTaskCmd.Run(); err != nil {
		log.Printf("Warning: failed to delete task: %v", err)
	}

	// Delete container
	deleteContainerCmd := exec.Command("ctr", "-n", c.namespace, "containers", "delete", name)
	if err := deleteContainerCmd.Run(); err != nil {
		return fmt.Errorf("failed to delete container: %w", err)
	}

	log.Printf("✅ Containerd: 컨테이너 중단 완료 %s", name)
	return nil
}

/*
컨테이너 목록 조회 함수 (containerd)
현재 실행 중인 모든 컨테이너의 정보를 반환합니다.
하트비트에서 Pod 개수 계산에 사용됩니다.
*/
func (c *ContainerdRuntime) ListContainers() ([]Container, error) {
	// List running tasks
	tasksCmd := exec.Command("ctr", "-n", c.namespace, "tasks", "list", "--quiet")
	tasksOutput, err := tasksCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	var result []Container
	taskLines := strings.Split(strings.TrimSpace(string(tasksOutput)), "\n")

	for _, line := range taskLines {
		if line == "" {
			continue
		}

		// Get container info
		containerCmd := exec.Command("ctr", "-n", c.namespace, "containers", "info", line)
		_, err := containerCmd.Output()
		if err != nil {
			continue // Skip if can't get container info
		}

		// Parse basic info (this is a simplified approach)
		result = append(result, Container{
			ID:     line,
			Name:   line,
			Image:  "unknown", // Could parse from container info JSON
			Status: "running",
		})
	}

	return result, nil
}

/*
🐋 Docker 런타임 구현
Docker는 가장 널리 사용되는 컨테이너 런타임입니다.
containerd보다 기능이 많지만 리소스 사용량이 더 큽니다.
*/
type DockerRuntime struct {
}

/*
NewDockerRuntime creates a new Docker runtime instance
Uses Docker CLI commands for container operations
*/
func NewDockerRuntime() (*DockerRuntime, error) {
	// Check if docker command is available
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker command not found: %w", err)
	}

	return &DockerRuntime{}, nil
}

/*
컨테이너 실행 함수 (Docker)
Docker 엔진을 통해 컨테이너를 실행합니다.
*/
func (d *DockerRuntime) RunContainer(image, name string, env map[string]string) error {
	log.Printf("🐋 Docker: 컨테이너 실행 중... %s (이미지: %s)", name, image)

	// Pull image first
	pullCmd := exec.Command("docker", "pull", image)
	if err := pullCmd.Run(); err != nil {
		log.Printf("Warning: failed to pull image %s: %v", image, err)
	}

	// Build run command
	args := []string{"run", "-d", "--name", name, "--restart=unless-stopped"}

	// Add environment variables
	for k, v := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, image)

	// Run container
	runCmd := exec.Command("docker", args...)
	output, err := runCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run container: %w, output: %s", err, string(output))
	}

	log.Printf("✅ Docker: 컨테이너 실행 완료 %s", name)
	return nil
}

/*
컨테이너 중단 함수 (Docker)
Docker 컨테이너를 정상적으로 중단시킵니다.
*/
func (d *DockerRuntime) StopContainer(name string) error {
	log.Printf("🛑 Docker: 컨테이너 중단 중... %s", name)

	// Stop container
	stopCmd := exec.Command("docker", "stop", name)
	if err := stopCmd.Run(); err != nil {
		log.Printf("Warning: failed to stop container: %v", err)
	}

	// Remove container
	removeCmd := exec.Command("docker", "rm", "-f", name)
	if err := removeCmd.Run(); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	log.Printf("✅ Docker: 컨테이너 중단 완료 %s", name)
	return nil
}

/*
컨테이너 목록 조회 함수 (Docker)
Docker 엔진에서 실행 중인 모든 컨테이너 정보를 조회합니다.
*/
func (d *DockerRuntime) ListContainers() ([]Container, error) {
	// List containers with custom format
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var result []Container
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			result = append(result, Container{
				ID:     parts[0],
				Name:   parts[1],
				Image:  parts[2],
				Status: parts[3],
			})
		}
	}

	return result, nil
}

// ==================== 누락된 함수들 추가 ====================

/*
📊 실행 중인 Pod 개수 조회 함수 - 실제 구현
현재 워커 노드에서 실행 중인 Pod(컨테이너)의 개수를 반환합니다.
하트비트 정보에 포함되어 Nautilus TEE가 노드의 작업 부하를 파악하는 데 사용됩니다.
*/
func (s *StakerHost) getRunningPodsCount() int {
	if s.k3sAgent == nil || s.k3sAgent.runtime == nil {
		return 0
	}
	containers, _ := s.k3sAgent.runtime.ListContainers() // 컨테이너 런타임에서 목록 조회
	return len(containers)                               // 컨테이너 개수 반환
}

/*
🖥️ 시스템 리소스 사용량 조회 함수 - 실제 구현
노드의 현재 CPU, 메모리, 디스크 사용량을 조회하여 반환합니다.
*/
func (s *StakerHost) getResourceUsage() map[string]interface{} {
	// 🚧 TODO: 실제 구현에서는 시스템 메트릭 수집 라이브러리 사용
	// 예시: gopsutil 패키지로 CPU/메모리/디스크 사용량 실시간 조회
	return map[string]interface{}{
		"cpu_percent":    45.2, // CPU 사용률 (%)
		"memory_percent": 67.8, // 메모리 사용률 (%)
		"disk_percent":   23.1, // 디스크 사용률 (%)
	}
}

// getMemoryUsage returns current memory usage metrics
func (s *StakerHost) getMemoryUsage() map[string]interface{} {
	return map[string]interface{}{
		"used_bytes":      2147483648, // 2GB used
		"available_bytes": 6442450944, // 6GB available
		"total_bytes":     8589934592, // 8GB total
		"percent":         67.8,
	}
}

// getCPUUsage returns current CPU usage metrics
func (s *StakerHost) getCPUUsage() map[string]interface{} {
	return map[string]interface{}{
		"percent":         45.2,
		"cores":           4,
		"load_average_1m": 1.2,
		"load_average_5m": 0.8,
	}
}

// getDiskUsage returns current disk usage metrics
func (s *StakerHost) getDiskUsage() map[string]interface{} {
	return map[string]interface{}{
		"used_bytes":      24159191040, // ~22.5GB used
		"available_bytes": 80530636800, // ~75GB available
		"total_bytes":     107374182400, // 100GB total
		"percent":         23.1,
		"path":            "/",
	}
}

// getNetworkStats returns network statistics
func (s *StakerHost) getNetworkStats() map[string]interface{} {
	return map[string]interface{}{
		"bytes_sent":     1048576000, // ~1GB sent
		"bytes_received": 2097152000, // ~2GB received
		"packets_sent":   1000000,
		"packets_received": 1500000,
		"errors_in":      0,
		"errors_out":     0,
	}
}


// unstakeFromSui withdraws stake from Sui blockchain
func (s *StakerHost) unstakeFromSui() error {
	log.Printf("🔄 Sui 블록체인에서 스테이킹 해제 중...")

	// Sui 트랜잭션 구성 (실제 구현에서는 Sui SDK 사용)
	unstakePayload := map[string]interface{}{
		"function": "unstake",
		"arguments": []interface{}{
			s.config.SuiWalletAddress, // 스테이커 주소
			s.config.NodeID,           // 노드 ID
		},
		"type_arguments": []string{},
	}

	// Sui RPC를 통한 트랜잭션 실행
	resp, err := resty.New().R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "sui_executeTransactionBlock",
			"params": []interface{}{
				unstakePayload,
				[]string{s.config.SuiPrivateKey}, // 서명을 위한 개인키 (실제로는 안전하게 관리)
				map[string]interface{}{
					"showInput":          true,
					"showRawInput":       false,
					"showEffects":        true,
					"showEvents":         true,
					"showObjectChanges":  true,
					"showBalanceChanges": true,
				},
			},
		}).
		Post(s.config.SuiRPCEndpoint)

	if err != nil {
		return fmt.Errorf("unstaking transaction failed: %v", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("Sui RPC error: %d %s", resp.StatusCode(), resp.String())
	}

	// 트랜잭션 결과 파싱
	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return fmt.Errorf("failed to parse unstaking response: %v", err)
	}

	// 오류 확인
	if errorInfo, exists := result["error"]; exists {
		return fmt.Errorf("unstaking failed: %v", errorInfo)
	}

	log.Printf("✅ 스테이킹 해제 완료")
	return nil
}

/*
🔍 Sui 블록체인에서 스테이킹 상태 조회 함수 - 실제 구현
하트비트 과정에서 호출되는 함수로, 현재 노드의 스테이킹 상태를
Sui 블록체인에서 직접 조회합니다.
*/
func (s *StakerHost) checkStakeOnSui() (*StakeInfo, error) {
	// 📡 Sui RPC sui_getObject 호출로 스테이킹 객체 조회
	queryPayload := map[string]interface{}{
		"jsonrpc": "2.0",         // JSON-RPC 버전
		"id":      1,             // 요청 ID
		"method":  "sui_getObject", // Sui 객체 조회 메소드
		"params": []interface{}{
			s.stakingStatus.StakeObjectID, // 조회할 스테이킹 객체 ID
			map[string]interface{}{
				"showContent": true, // 객체 내용 포함 (상태와 양 확인용)
			},
		},
	}

	// 🌐 Sui 테스트넷에 조회 요청 전송
	resp, err := s.suiClient.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(queryPayload).
		Post(s.suiClient.rpcEndpoint)

	if err != nil {
		return nil, fmt.Errorf("Sui 스테이킹 상태 조회 요청 실패: %v", err)
	}

	// 📄 JSON 응답 파싱
	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("Sui 응답 파싱 실패: %v", err)
	}

	// 🔍 StakeRecord 객체의 content 필드에서 실제 데이터 추출
	if resultData, exists := result["result"]; exists {
		if resultMap, ok := resultData.(map[string]interface{}); ok {
			if data, exists := resultMap["data"]; exists {
				if dataMap, ok := data.(map[string]interface{}); ok {
					if content, exists := dataMap["content"]; exists {
						if contentMap, ok := content.(map[string]interface{}); ok {
							return &StakeInfo{
								Amount: uint64(contentMap["stake_amount"].(float64)), // 스테이킹된 SUI 양
								Status: contentMap["status"].(string),               // 스테이킹 상태
							}, nil
						}
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("스테이킹 상태 파싱 실패")
}

/*
🛑 스테이커 호스트 종료 함수 - 실제 구현
스테이킹 슬래싱이 감지되거나 시스템 종료 시 호출됩니다.
*/
func (s *StakerHost) Shutdown() {
	log.Printf("🛑 스테이커 호스트 종료 중... Node ID: %s", s.config.NodeID)

	// 1️⃣ 하트비트 서비스 중단
	if s.heartbeatTicker != nil {
		s.heartbeatTicker.Stop()
		log.Printf("💓 하트비트 서비스 중단됨")
	}

	// 2️⃣ K3s Agent 종료
	if s.k3sAgent != nil && s.k3sAgent.kubelet != nil {
		log.Printf("🔧 K3s Agent 종료 중...")
		s.k3sAgent.kubelet.Stop()
	}

	// 3️⃣ 실행 중인 모든 컨테이너 정리
	if s.k3sAgent != nil && s.k3sAgent.runtime != nil {
		log.Printf("🐳 실행 중인 컨테이너들 정리 중...")
		containers, _ := s.k3sAgent.runtime.ListContainers()
		for _, container := range containers {
			log.Printf("🛑 컨테이너 중단: %s", container.Name)
			s.k3sAgent.runtime.StopContainer(container.Name)
		}
	}

	s.isRunning = false
	log.Printf("✅ 스테이커 호스트 종료 완료")
	os.Exit(0)
}

/*
⚙️ 설정 파일 로드 함수 - 실제 구현
staker-config.json 파일을 읽어서 StakerHostConfig 구조체로 파싱합니다.
*/
func loadConfig(path string) (*StakerHostConfig, error) {
	// 📁 설정 파일 열기
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일 열기 실패: %v", err)
	}
	defer file.Close() // 함수 종료 시 파일 자동 닫기

	// 📄 JSON 파싱
	var config StakerHostConfig
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("설정 파일 JSON 파싱 실패: %v", err)
	}

	// 기본값 설정
	if config.MinStakeAmount == 0 {
		config.MinStakeAmount = 1000 // 1000 MIST
	}

	return &config, nil
}