package security

import (
	"time"
)

// DaaSConfig represents DaaS-specific configuration
type DaaSConfig struct {
	Enabled     bool        `json:"enabled" yaml:"enabled"`
	SuiConfig   *SuiConfig  `json:"sui" yaml:"sui"`
	SealConfig  *SealConfig `json:"seal" yaml:"seal"`
	StakeConfig *StakeConfig `json:"stake" yaml:"stake"`
}

// SuiConfig contains Sui blockchain configuration
type SuiConfig struct {
	RPCEndpoint     string        `json:"rpc_endpoint" yaml:"rpc_endpoint" env:"SUI_RPC_ENDPOINT"`
	ContractPackage string        `json:"contract_package" yaml:"contract_package" env:"SUI_CONTRACT_PACKAGE"`
	WalletPath      string        `json:"wallet_path" yaml:"wallet_path" env:"SUI_WALLET_PATH"`
	MaxGasBudget    uint64        `json:"max_gas_budget" yaml:"max_gas_budget" env:"SUI_MAX_GAS_BUDGET"`
	TimeoutDuration time.Duration `json:"timeout_duration" yaml:"timeout_duration" env:"SUI_TIMEOUT_DURATION"`
}

// SealConfig contains Seal authentication configuration
type SealConfig struct {
	WalletAddress   string        `json:"wallet_address" yaml:"wallet_address" env:"SEAL_WALLET_ADDRESS"`
	PrivateKeyPath  string        `json:"private_key_path" yaml:"private_key_path" env:"SEAL_PRIVATE_KEY_PATH"`
	ChallengeExpiry time.Duration `json:"challenge_expiry" yaml:"challenge_expiry" env:"SEAL_CHALLENGE_EXPIRY"`
}

// StakeConfig contains staking configuration
type StakeConfig struct {
	MinStake         string        `json:"min_stake" yaml:"min_stake" env:"DAAS_MIN_STAKE"`
	ValidatorCacheTTL time.Duration `json:"validator_cache_ttl" yaml:"validator_cache_ttl" env:"DAAS_VALIDATOR_CACHE_TTL"`
	CheckInterval    time.Duration `json:"check_interval" yaml:"check_interval" env:"DAAS_STAKE_CHECK_INTERVAL"`
}

// DefaultDaaSConfig returns default DaaS configuration
func DefaultDaaSConfig() *DaaSConfig {
	return &DaaSConfig{
		Enabled: false,
		SuiConfig: &SuiConfig{
			RPCEndpoint:     "https://sui-testnet.rpc.com",
			ContractPackage: "0x1234567890abcdef1234567890abcdef12345678",
			WalletPath:      "/etc/k3s/sui-wallet.key",
			MaxGasBudget:    1000000,
			TimeoutDuration: 30 * time.Second,
		},
		SealConfig: &SealConfig{
			WalletAddress:   "",
			PrivateKeyPath:  "/etc/k3s/seal-private.key",
			ChallengeExpiry: 5 * time.Minute,
		},
		StakeConfig: &StakeConfig{
			MinStake:         "1000000000", // 1 SUI
			ValidatorCacheTTL: 5 * time.Minute,
			CheckInterval:    30 * time.Second,
		},
	}
}

// DaaSValidator handles DaaS-specific validation
type DaaSValidator struct {
	config    *DaaSConfig
	suiClient *SuiClient
	sealAuth  *SealAuthenticator
}

// NewDaaSValidator creates a new DaaS validator
func NewDaaSValidator(config *DaaSConfig) (*DaaSValidator, error) {
	validator := &DaaSValidator{
		config: config,
	}

	// Initialize Sui client
	if config.SuiConfig != nil {
		validator.suiClient = NewSuiClient(config.SuiConfig.RPCEndpoint)
	}

	// Initialize Seal authenticator
	if config.SealConfig != nil && config.SealConfig.WalletAddress != "" {
		validator.sealAuth = NewSealAuthenticator(config.SealConfig.WalletAddress)
	}

	return validator, nil
}

// IsEnabled checks if DaaS authentication is enabled
func (v *DaaSValidator) IsEnabled() bool {
	return v.config.Enabled
}

// GetSuiClient returns the Sui client
func (v *DaaSValidator) GetSuiClient() *SuiClient {
	return v.suiClient
}

// GetSealAuth returns the Seal authenticator
func (v *DaaSValidator) GetSealAuth() *SealAuthenticator {
	return v.sealAuth
}