// K3s-DaaS Worker Node (Staker Host)
// 순수 워커 노드 - 마스터 기능 완전 제거
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/k3s-io/k3s/pkg/agent/config"
	"github.com/k3s-io/k3s/pkg/daemons/agent"
	"github.com/k3s-io/k3s/pkg/agent/containerd"
	"github.com/k3s-io/k3s/pkg/sui"
	"github.com/k3s-io/k3s/pkg/security"
)

// 스테이커 호스트 설정
type StakerHostConfig struct {
	NodeID           string `json:"node_id"`
	SuiWalletAddress string `json:"sui_wallet_address"`
	SuiPrivateKey    string `json:"sui_private_key"`
	SuiRPCEndpoint   string `json:"sui_rpc_endpoint"`
	StakeAmount      uint64 `json:"stake_amount"`
	ContractAddress  string `json:"contract_address"`
	MinStakeAmount   uint64 `json:"min_stake_amount"`
}

// K3s 워커 노드 + Sui 스테이킹
type K3sStakerHost struct {
	config         *StakerHostConfig
	agentConfig    *config.Agent
	suiClient      *sui.Client
	stakingStatus  *StakingStatus
	containerRuntime containerd.Runtime
	isRunning      bool
}

type StakingStatus struct {
	IsStaked      bool   `json:"is_staked"`
	StakeAmount   uint64 `json:"stake_amount"`
	StakeObjectID string `json:"stake_object_id"`
	SealTokenID   string `json:"seal_token_id"`
	Status        string `json:"status"` // active, slashed, pending
	LastValidated int64  `json:"last_validated"`
}

func main() {
	log.Println("🚀 Starting K3s-DaaS Staker Host (Worker Node Only)")

	// 설정 로드
	config, err := loadStakerConfig()
	if err != nil {
		log.Fatalf("❌ Failed to load staker config: %v", err)
	}

	// 스테이커 호스트 초기화
	stakerHost, err := NewK3sStakerHost(config)
	if err != nil {
		log.Fatalf("❌ Failed to initialize staker host: %v", err)
	}

	// 1. Sui 스테이킹 등록
	if err := stakerHost.RegisterStake(); err != nil {
		log.Fatalf("❌ Failed to register stake: %v", err)
	}

	// 2. K3s Agent 시작 (워커 노드만)
	if err := stakerHost.StartK3sWorker(); err != nil {
		log.Fatalf("❌ Failed to start K3s worker: %v", err)
	}

	// 3. 스테이킹 모니터링 시작
	stakerHost.StartStakeMonitoring()

	// 4. HTTP 서버 시작 (상태 확인용)
	stakerHost.StartStatusServer()

	log.Printf("✅ K3s Staker Host '%s' ready and running", config.NodeID)
	select {} // 무한 대기
}

func NewK3sStakerHost(cfg *StakerHostConfig) (*K3sStakerHost, error) {
	// Sui 클라이언트 초기화
	suiClient, err := sui.NewClient(cfg.SuiRPCEndpoint, cfg.SuiPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Sui client: %v", err)
	}

	// K3s Agent 설정 (워커 노드만)
	agentConfig := &config.Agent{
		NodeName:     cfg.NodeID,
		ClientKubeConfigPath: "", // 마스터에서 받음
		DataDir:      "/var/lib/k3s-daas",

		// 워커 노드 전용 설정
		DisableKubeProxy: false,
		DisableNetworkPolicy: false,

		// 컨테이너 런타임 설정
		ContainerRuntimeEndpoint: "/run/containerd/containerd.sock",

		// 마스터 노드 정보 (스마트 컨트랙트에서 받음)
		ServerURL: "", // 나중에 컨트랙트에서 설정
	}

	return &K3sStakerHost{
		config:      cfg,
		agentConfig: agentConfig,
		suiClient:   suiClient,
		stakingStatus: &StakingStatus{
			Status: "pending",
		},
		isRunning: false,
	}, nil
}

// Sui 블록체인에 스테이킹 등록 및 Seal 토큰 생성
func (s *K3sStakerHost) RegisterStake() error {
	log.Printf("💰 Registering stake for node %s (Amount: %d MIST)",
		s.config.NodeID, s.config.StakeAmount)

	// 1. 스테이킹 트랜잭션 생성
	stakeTxParams := &sui.TransactionParams{
		PackageID: s.config.ContractAddress,
		Module:    "staking",
		Function:  "stake_for_node",
		Arguments: []interface{}{
			s.config.StakeAmount,
			s.config.NodeID,
		},
		GasBudget: 10000000,
	}

	stakeResult, err := s.suiClient.ExecuteTransaction(stakeTxParams)
	if err != nil {
		return fmt.Errorf("failed to submit staking transaction: %v", err)
	}

	log.Printf("✅ Successfully staked! Stake Object ID: %s", stakeResult.ObjectID)

	// 2. Seal 토큰 생성 (워커 노드용)
	sealTxParams := &sui.TransactionParams{
		PackageID: s.config.ContractAddress,
		Module:    "k8s_gateway",
		Function:  "create_worker_seal_token",
		Arguments: []interface{}{
			stakeResult.ObjectID, // StakeRecord 객체 ID
		},
		GasBudget: 5000000,
	}

	sealResult, err := s.suiClient.ExecuteTransaction(sealTxParams)
	if err != nil {
		return fmt.Errorf("failed to create seal token: %v", err)
	}

	// 스테이킹 및 Seal 토큰 정보 저장
	s.stakingStatus.IsStaked = true
	s.stakingStatus.StakeAmount = s.config.StakeAmount
	s.stakingStatus.StakeObjectID = stakeResult.ObjectID
	s.stakingStatus.SealTokenID = sealResult.ObjectID
	s.stakingStatus.Status = "active"
	s.stakingStatus.LastValidated = time.Now().Unix()

	log.Printf("✅ Seal token created! Token ID: %s", sealResult.ObjectID)
	return nil
}

// K3s 워커 노드 시작
func (s *K3sStakerHost) StartK3sWorker() error {
	log.Printf("🔧 Starting K3s worker node: %s", s.config.NodeID)

	// 스테이킹 확인
	if !s.stakingStatus.IsStaked {
		return fmt.Errorf("cannot start worker: node is not staked")
	}

	// Seal 토큰으로 Nautilus TEE 정보 조회
	nautilusInfo, err := s.getNautilusInfoWithSeal()
	if err != nil {
		return fmt.Errorf("failed to get Nautilus info: %v", err)
	}

	s.agentConfig.ServerURL = nautilusInfo.ServerURL
	s.agentConfig.Token = nautilusInfo.SealToken // Seal 토큰을 K3s Agent에 전달

	// K3s Agent 시작 (순수 워커 노드)
	ctx := context.Background()

	// 컨테이너 런타임 초기화
	runtime, err := containerd.NewContainerdRuntime(s.agentConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize containerd: %v", err)
	}
	s.containerRuntime = runtime

	// K3s 에이전트 데몬 시작
	if err := agent.Agent(ctx, s.agentConfig); err != nil {
		return fmt.Errorf("failed to start K3s agent: %v", err)
	}

	s.isRunning = true
	log.Printf("✅ K3s worker node started successfully")
	return nil
}

// Seal 토큰을 사용해서 Nautilus 정보 조회
func (s *K3sStakerHost) getNautilusInfoWithSeal() (*NautilusInfo, error) {
	// Seal 토큰을 사용해서 Nautilus 정보 조회
	result, err := s.suiClient.CallFunction(&sui.FunctionCall{
		PackageID: s.config.ContractAddress,
		Module:    "k8s_gateway",
		Function:  "get_nautilus_info_for_worker",
		Arguments: []interface{}{s.stakingStatus.SealTokenID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get Nautilus info with Seal: %v", err)
	}

	return &NautilusInfo{
		ServerURL: result["nautilus_url"].(string),
		SealToken: result["worker_token"].(string), // 인코딩된 Seal 토큰
	}, nil
}

type NautilusInfo struct {
	ServerURL string `json:"server_url"`
	SealToken string `json:"seal_token"` // Seal 토큰 (Nautilus 인증용)
}

// 스테이킹 상태 모니터링
func (s *K3sStakerHost) StartStakeMonitoring() {
	log.Printf("👀 Starting stake monitoring...")

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if err := s.validateStake(); err != nil {
				log.Printf("⚠️  Stake validation failed: %v", err)

				if err.Error() == "stake_slashed" {
					log.Printf("💀 Stake was slashed! Shutting down...")
					s.Shutdown()
					return
				}
			}
		}
	}()
}

// 스테이킹 상태 검증
func (s *K3sStakerHost) validateStake() error {
	// Sui에서 스테이킹 객체 조회
	stakeInfo, err := s.suiClient.GetObject(s.stakingStatus.StakeObjectID)
	if err != nil {
		return fmt.Errorf("failed to get stake object: %v", err)
	}

	// 슬래싱 확인
	if stakeInfo.Content["status"].(string) == "slashed" {
		s.stakingStatus.Status = "slashed"
		return fmt.Errorf("stake_slashed")
	}

	// 스테이킹 양 확인
	currentStake := uint64(stakeInfo.Content["amount"].(float64))
	if currentStake < s.config.MinStakeAmount {
		return fmt.Errorf("stake amount too low: %d < %d", currentStake, s.config.MinStakeAmount)
	}

	s.stakingStatus.LastValidated = time.Now().Unix()
	return nil
}

// 상태 확인 HTTP 서버
func (s *K3sStakerHost) StartStatusServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]interface{}{
			"node_id":        s.config.NodeID,
			"staking_status": s.stakingStatus,
			"k3s_running":    s.isRunning,
			"timestamp":      time.Now().Unix(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	http.HandleFunc("/stake", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.stakingStatus)
	})

	go func() {
		log.Printf("🌐 Status server listening on :10250")
		log.Fatal(http.ListenAndServe(":10250", nil))
	}()
}

// 노드 종료
func (s *K3sStakerHost) Shutdown() {
	log.Printf("🛑 Shutting down staker host: %s", s.config.NodeID)

	s.isRunning = false

	// 실행 중인 컨테이너들 정리
	if s.containerRuntime != nil {
		// 컨테이너 정리 로직
		log.Printf("🧹 Cleaning up containers...")
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