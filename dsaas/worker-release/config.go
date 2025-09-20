// Configuration management for K3s-DaaS Worker Node
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// 워커 노드 전체 설정 구조체
type WorkerConfig struct {
	// 노드 설정
	Node NodeConfig `json:"node"`

	// K3s Agent 설정
	K3s K3sAgentWorkerConfig `json:"k3s"`

	// Sui 스테이킹 설정
	Staking StakingConfig `json:"staking"`

	// 로깅 설정
	Logging WorkerLoggingConfig `json:"logging"`
}

// 노드 설정
type NodeConfig struct {
	NodeID             string `json:"node_id"`
	NautilusEndpoint   string `json:"nautilus_endpoint"`
	ContainerRuntime   string `json:"container_runtime"`
	DataDir            string `json:"data_dir"`
}

// K3s Agent 설정 (이미 있는 것과 중복 방지)
type K3sAgentWorkerConfig struct {
	ServerURL                string   `json:"server_url"`
	Token                    string   `json:"token"`
	DataDir                  string   `json:"data_dir"`
	NodeName                 string   `json:"node_name"`
	NodeIP                   string   `json:"node_ip"`
	ContainerRuntimeEndpoint string   `json:"container_runtime_endpoint"`
	KubeletArgs              []string `json:"kubelet_args"`
	NodeLabels               []string `json:"node_labels"`
	LogLevel                 string   `json:"log_level"`
}

// 스테이킹 설정
type StakingConfig struct {
	MinStakeAmount     uint64 `json:"min_stake_amount"`
	StakeCheckInterval int    `json:"stake_check_interval_seconds"`
	SuiNetworkURL      string `json:"sui_network_url"`
	PrivateKey         string `json:"private_key"`
	StakingPoolID      string `json:"staking_pool_id"`
}

// 로깅 설정 (워커용)
type WorkerLoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
}

// 전역 워커 설정 변수
var WorkerGlobalConfig *WorkerConfig

// 워커 설정 초기화
func InitializeWorkerConfig() error {
	config, err := LoadWorkerConfig()
	if err != nil {
		return fmt.Errorf("워커 설정 로드 실패: %v", err)
	}

	WorkerGlobalConfig = config
	return nil
}

// 워커 설정 로드
func LoadWorkerConfig() (*WorkerConfig, error) {
	// 1. 기본 설정으로 시작
	config := getDefaultWorkerConfig()

	// 2. 설정 파일에서 로드 (있다면)
	if err := loadWorkerFromFile(config); err != nil {
		fmt.Printf("⚠️ 워커 설정 파일을 찾을 수 없어 기본값 사용: %v\n", err)
	}

	// 3. 환경변수로 오버라이드
	loadWorkerFromEnvironment(config)

	return config, nil
}

// 기본 워커 설정 값
func getDefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		Node: NodeConfig{
			NodeID:             "k3s-daas-worker-001",
			NautilusEndpoint:   "http://localhost:8080",
			ContainerRuntime:   "containerd",
			DataDir:            "/var/lib/k3s-daas-agent",
		},
		K3s: K3sAgentWorkerConfig{
			ServerURL:                "http://localhost:8080",
			DataDir:                  "/var/lib/k3s-daas-agent",
			NodeIP:                   "0.0.0.0",
			ContainerRuntimeEndpoint: "unix:///run/containerd/containerd.sock",
			KubeletArgs: []string{
				"--container-runtime=remote",
				"--fail-swap-on=false",
				"--cgroup-driver=systemd",
			},
			LogLevel: "info",
		},
		Staking: StakingConfig{
			MinStakeAmount:     1000000000, // 1000 MIST
			StakeCheckInterval: 60,         // 60초
			SuiNetworkURL:      "https://fullnode.testnet.sui.io:443",
			PrivateKey:         "", // 환경변수에서 설정
			StakingPoolID:      "", // 환경변수에서 설정
		},
		Logging: WorkerLoggingConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
	}
}

// 워커 설정 파일에서 로드
func loadWorkerFromFile(config *WorkerConfig) error {
	configPaths := []string{
		os.Getenv("K3S_DAAS_WORKER_CONFIG"),
		"./worker-config.json",
		"/etc/k3s-daas/worker-config.json",
		filepath.Join(os.Getenv("HOME"), ".k3s-daas", "worker-config.json"),
	}

	for _, path := range configPaths {
		if path == "" {
			continue
		}

		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, config); err != nil {
				return fmt.Errorf("워커 설정 파일 파싱 실패 (%s): %v", path, err)
			}
			fmt.Printf("✅ 워커 설정 파일 로드 완료: %s\n", path)
			return nil
		}
	}

	return fmt.Errorf("워커 설정 파일을 찾을 수 없음")
}

// 환경변수에서 워커 설정 로드
func loadWorkerFromEnvironment(config *WorkerConfig) {
	// 노드 설정
	if val := os.Getenv("K3S_DAAS_WORKER_NODE_ID"); val != "" {
		config.Node.NodeID = val
	}
	if val := os.Getenv("K3S_DAAS_NAUTILUS_ENDPOINT"); val != "" {
		config.Node.NautilusEndpoint = val
	}
	if val := os.Getenv("K3S_DAAS_CONTAINER_RUNTIME"); val != "" {
		config.Node.ContainerRuntime = val
	}
	if val := os.Getenv("K3S_DAAS_WORKER_DATA_DIR"); val != "" {
		config.Node.DataDir = val
		config.K3s.DataDir = val
	}

	// K3s 설정
	if val := os.Getenv("K3S_DAAS_SERVER_URL"); val != "" {
		config.K3s.ServerURL = val
	}
	if val := os.Getenv("K3S_DAAS_NODE_IP"); val != "" {
		config.K3s.NodeIP = val
	}
	if val := os.Getenv("K3S_DAAS_RUNTIME_ENDPOINT"); val != "" {
		config.K3s.ContainerRuntimeEndpoint = val
	}

	// 스테이킹 설정
	if val := os.Getenv("K3S_DAAS_MIN_STAKE"); val != "" {
		if amount, err := strconv.ParseUint(val, 10, 64); err == nil {
			config.Staking.MinStakeAmount = amount
		}
	}
	if val := os.Getenv("SUI_WORKER_PRIVATE_KEY"); val != "" {
		config.Staking.PrivateKey = val
	}
	if val := os.Getenv("SUI_STAKING_POOL_ID"); val != "" {
		config.Staking.StakingPoolID = val
	}
	if val := os.Getenv("SUI_WORKER_NETWORK_URL"); val != "" {
		config.Staking.SuiNetworkURL = val
	}

	// 로깅 설정
	if val := os.Getenv("K3S_DAAS_WORKER_LOG_LEVEL"); val != "" {
		config.Logging.Level = val
	}
}

// 워커 설정 기본값 저장
func SaveDefaultWorkerConfig(path string) error {
	config := getDefaultWorkerConfig()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("워커 설정 디렉토리 생성 실패: %v", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("워커 설정 직렬화 실패: %v", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("워커 설정 파일 저장 실패: %v", err)
	}

	fmt.Printf("✅ 기본 워커 설정 파일 생성: %s\n", path)
	return nil
}

// 워커 설정 유효성 검사
func (c *WorkerConfig) Validate() error {
	if c.Node.NodeID == "" {
		return fmt.Errorf("노드 ID가 설정되지 않음")
	}

	if c.Node.NautilusEndpoint == "" {
		return fmt.Errorf("Nautilus 엔드포인트가 설정되지 않음")
	}

	if c.Node.DataDir == "" {
		return fmt.Errorf("데이터 디렉토리가 설정되지 않음")
	}

	if c.Staking.MinStakeAmount <= 0 {
		return fmt.Errorf("최소 스테이킹 양이 0보다 작거나 같음")
	}

	// 프로덕션 환경에서는 프라이빗 키 필요
	if os.Getenv("ENVIRONMENT") == "production" && c.Staking.PrivateKey == "" {
		return fmt.Errorf("프로덕션 환경에서는 SUI_WORKER_PRIVATE_KEY 환경변수가 필요함")
	}

	return nil
}

// 워커 설정 요약 출력
func (c *WorkerConfig) PrintSummary() {
	fmt.Printf("📋 K3s-DaaS 워커 노드 설정 요약:\n")
	fmt.Printf("  🏷️  노드 ID: %s\n", c.Node.NodeID)
	fmt.Printf("  🔗 Nautilus: %s\n", c.Node.NautilusEndpoint)
	fmt.Printf("  📁 데이터 디렉토리: %s\n", c.Node.DataDir)
	fmt.Printf("  🐳 컨테이너 런타임: %s\n", c.Node.ContainerRuntime)
	fmt.Printf("  💰 최소 스테이킹: %d MIST\n", c.Staking.MinStakeAmount)
	fmt.Printf("  🌊 Sui 네트워크: %s\n", c.Staking.SuiNetworkURL)
	fmt.Printf("  📊 로그 레벨: %s\n", c.Logging.Level)
}