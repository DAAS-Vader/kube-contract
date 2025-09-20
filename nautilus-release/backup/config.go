// Configuration management for K3s-DaaS Nautilus TEE Master
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// 전체 시스템 설정 구조체
type SystemConfig struct {
	// 서버 설정
	Server ServerConfig `json:"server"`

	// K3s 설정
	K3s K3sServerConfig `json:"k3s"`

	// TEE 설정
	TEE TEEConfig `json:"tee"`

	// Sui 블록체인 설정
	Sui SuiConfig `json:"sui"`

	// 로깅 설정
	Logging LoggingConfig `json:"logging"`
}

// 서버 설정
type ServerConfig struct {
	ListenAddress string `json:"listen_address"`
	ListenPort    int    `json:"listen_port"`
	APIBasePath   string `json:"api_base_path"`
}

// K3s 서버 설정
type K3sServerConfig struct {
	DataDir           string   `json:"data_dir"`
	BindAddress       string   `json:"bind_address"`
	HTTPSPort         int      `json:"https_port"`
	ClusterCIDR       string   `json:"cluster_cidr"`
	ServiceCIDR       string   `json:"service_cidr"`
	ClusterDNS        string   `json:"cluster_dns"`
	DisableComponents []string `json:"disable_components"`
	TLSMinVersion     string   `json:"tls_min_version"`
	BootstrapToken    string   `json:"bootstrap_token"`
}

// TEE 설정
type TEEConfig struct {
	Mode                string `json:"mode"` // "real" or "simulation"
	AttestationEndpoint string `json:"attestation_endpoint"`
	EnclaveID          string `json:"enclave_id"`
	MockAttestation    bool   `json:"mock_attestation"`
}

// Sui 블록체인 설정
type SuiConfig struct {
	NetworkURL        string `json:"network_url"`
	GasObjectID       string `json:"gas_object_id"`
	PrivateKey        string `json:"private_key"`
	PackageID         string `json:"package_id"`
	VerificationObject string `json:"verification_object"`
	StakingPool       string `json:"staking_pool"`
}

// 로깅 설정
type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
}

// 전역 설정 변수
var GlobalConfig *SystemConfig

// 설정 초기화
func InitializeConfig() error {
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("설정 로드 실패: %v", err)
	}

	GlobalConfig = config
	return nil
}

// 설정 로드 (환경변수, 파일, 기본값 순서)
func LoadConfig() (*SystemConfig, error) {
	// 1. 기본 설정으로 시작
	config := getDefaultConfig()

	// 2. 설정 파일에서 로드 (있다면)
	if err := loadFromFile(config); err != nil {
		// 파일이 없으면 기본값 사용 (에러 아님)
		fmt.Printf("⚠️ 설정 파일을 찾을 수 없어 기본값 사용: %v\n", err)
	}

	// 3. 환경변수로 오버라이드
	loadFromEnvironment(config)

	return config, nil
}

// 기본 설정 값
func getDefaultConfig() *SystemConfig {
	return &SystemConfig{
		Server: ServerConfig{
			ListenAddress: "0.0.0.0",
			ListenPort:    8080,
			APIBasePath:   "/api/v1",
		},
		K3s: K3sServerConfig{
			DataDir:           "/var/lib/k3s-daas-tee",
			BindAddress:       "0.0.0.0",
			HTTPSPort:         6443,
			ClusterCIDR:       "10.42.0.0/16",
			ServiceCIDR:       "10.43.0.0/16",
			ClusterDNS:        "10.43.0.10",
			DisableComponents: []string{"traefik", "metrics-server"},
			TLSMinVersion:     "1.2",
			BootstrapToken:    "k3s-daas-tee-bootstrap-token",
		},
		TEE: TEEConfig{
			Mode:                "simulation", // 기본값은 시뮬레이션
			AttestationEndpoint: "https://nautilus.sui.io/v1/attestation",
			EnclaveID:          "sui-k3s-daas-master",
			MockAttestation:    true,
		},
		Sui: SuiConfig{
			NetworkURL:        "https://fullnode.testnet.sui.io:443",
			GasObjectID:       "0x2",
			PrivateKey:        "", // 환경변수에서 설정 필요
			PackageID:         "", // 환경변수에서 설정 필요
			VerificationObject: "",
			StakingPool:       "",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
	}
}

// 설정 파일에서 로드
func loadFromFile(config *SystemConfig) error {
	// 설정 파일 경로들을 순서대로 시도
	configPaths := []string{
		os.Getenv("K3S_DAAS_CONFIG"),                    // 환경변수
		"./config.json",                                 // 현재 디렉토리
		"/etc/k3s-daas/config.json",                    // 시스템 설정
		filepath.Join(os.Getenv("HOME"), ".k3s-daas", "config.json"), // 사용자 홈
	}

	for _, path := range configPaths {
		if path == "" {
			continue
		}

		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, config); err != nil {
				return fmt.Errorf("설정 파일 파싱 실패 (%s): %v", path, err)
			}
			fmt.Printf("✅ 설정 파일 로드 완료: %s\n", path)
			return nil
		}
	}

	return fmt.Errorf("설정 파일을 찾을 수 없음")
}

// 환경변수에서 로드
func loadFromEnvironment(config *SystemConfig) {
	// 서버 설정
	if val := os.Getenv("K3S_DAAS_LISTEN_ADDRESS"); val != "" {
		config.Server.ListenAddress = val
	}
	if val := os.Getenv("K3S_DAAS_LISTEN_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.Server.ListenPort = port
		}
	}

	// K3s 설정
	if val := os.Getenv("K3S_DAAS_DATA_DIR"); val != "" {
		config.K3s.DataDir = val
	}
	if val := os.Getenv("K3S_DAAS_BIND_ADDRESS"); val != "" {
		config.K3s.BindAddress = val
	}
	if val := os.Getenv("K3S_DAAS_HTTPS_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.K3s.HTTPSPort = port
		}
	}
	if val := os.Getenv("K3S_DAAS_CLUSTER_CIDR"); val != "" {
		config.K3s.ClusterCIDR = val
	}
	if val := os.Getenv("K3S_DAAS_SERVICE_CIDR"); val != "" {
		config.K3s.ServiceCIDR = val
	}
	if val := os.Getenv("K3S_DAAS_CLUSTER_DNS"); val != "" {
		config.K3s.ClusterDNS = val
	}
	if val := os.Getenv("K3S_DAAS_BOOTSTRAP_TOKEN"); val != "" {
		config.K3s.BootstrapToken = val
	}

	// TEE 설정
	if val := os.Getenv("K3S_DAAS_TEE_MODE"); val != "" {
		config.TEE.Mode = val
	}
	if val := os.Getenv("K3S_DAAS_TEE_ENDPOINT"); val != "" {
		config.TEE.AttestationEndpoint = val
	}
	if val := os.Getenv("K3S_DAAS_ENCLAVE_ID"); val != "" {
		config.TEE.EnclaveID = val
	}
	if val := os.Getenv("K3S_DAAS_MOCK_ATTESTATION"); val != "" {
		config.TEE.MockAttestation = val == "true"
	}

	// Sui 설정
	if val := os.Getenv("SUI_NETWORK_URL"); val != "" {
		config.Sui.NetworkURL = val
	}
	if val := os.Getenv("SUI_GAS_OBJECT_ID"); val != "" {
		config.Sui.GasObjectID = val
	}
	if val := os.Getenv("SUI_PRIVATE_KEY"); val != "" {
		config.Sui.PrivateKey = val
	}
	if val := os.Getenv("SUI_PACKAGE_ID"); val != "" {
		config.Sui.PackageID = val
	}
	if val := os.Getenv("SUI_VERIFICATION_OBJECT"); val != "" {
		config.Sui.VerificationObject = val
	}
	if val := os.Getenv("SUI_STAKING_POOL"); val != "" {
		config.Sui.StakingPool = val
	}

	// 로깅 설정
	if val := os.Getenv("K3S_DAAS_LOG_LEVEL"); val != "" {
		config.Logging.Level = val
	}
	if val := os.Getenv("K3S_DAAS_LOG_FORMAT"); val != "" {
		config.Logging.Format = val
	}
}

// 설정을 파일로 저장 (기본값 생성용)
func SaveDefaultConfig(path string) error {
	config := getDefaultConfig()

	// 디렉토리 생성
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("설정 디렉토리 생성 실패: %v", err)
	}

	// JSON으로 저장
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("설정 직렬화 실패: %v", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("설정 파일 저장 실패: %v", err)
	}

	fmt.Printf("✅ 기본 설정 파일 생성: %s\n", path)
	return nil
}

// 설정 유효성 검사
func (c *SystemConfig) Validate() error {
	// 필수 설정 확인
	if c.Server.ListenPort <= 0 || c.Server.ListenPort > 65535 {
		return fmt.Errorf("잘못된 서버 포트: %d", c.Server.ListenPort)
	}

	if c.K3s.HTTPSPort <= 0 || c.K3s.HTTPSPort > 65535 {
		return fmt.Errorf("잘못된 K3s HTTPS 포트: %d", c.K3s.HTTPSPort)
	}

	if c.K3s.DataDir == "" {
		return fmt.Errorf("K3s 데이터 디렉토리가 설정되지 않음")
	}

	// Sui 설정 확인 (프로덕션 모드에서)
	if c.TEE.Mode == "real" {
		if c.Sui.PrivateKey == "" {
			return fmt.Errorf("프로덕션 모드에서는 SUI_PRIVATE_KEY 환경변수가 필요함")
		}
		if c.Sui.PackageID == "" {
			return fmt.Errorf("프로덕션 모드에서는 SUI_PACKAGE_ID 환경변수가 필요함")
		}
	}

	return nil
}

// 설정 요약 출력
func (c *SystemConfig) PrintSummary() {
	fmt.Printf("📋 K3s-DaaS 설정 요약:\n")
	fmt.Printf("  🌐 서버: %s:%d\n", c.Server.ListenAddress, c.Server.ListenPort)
	fmt.Printf("  🎯 K3s API: %s:%d\n", c.K3s.BindAddress, c.K3s.HTTPSPort)
	fmt.Printf("  📁 데이터 디렉토리: %s\n", c.K3s.DataDir)
	fmt.Printf("  🔒 TEE 모드: %s\n", c.TEE.Mode)
	fmt.Printf("  🌊 Sui 네트워크: %s\n", c.Sui.NetworkURL)
	fmt.Printf("  📊 로그 레벨: %s\n", c.Logging.Level)
}