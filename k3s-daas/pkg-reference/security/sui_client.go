package security

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// SuiClient handles interactions with Sui blockchain
type SuiClient struct {
	rpcURL     string
	httpClient *http.Client
}

// StakeInfo represents worker stake information
type StakeInfo struct {
	WalletAddress string `json:"wallet_address"`
	StakeAmount   uint64 `json:"stake_amount"`
	Status        uint8  `json:"status"` // 0: inactive, 1: active, 2: suspended, 3: slashed
	LastUpdate    int64  `json:"last_update"`
}

// NewSuiClient creates a new Sui client instance
func NewSuiClient(rpcURL string) *SuiClient {
	return &SuiClient{
		rpcURL: rpcURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ValidateStake checks if a wallet has sufficient stake
func (c *SuiClient) ValidateStake(ctx context.Context, walletAddress string, minStake uint64) (*StakeInfo, error) {
	// For POC, simulate blockchain call
	// In production, this would call the actual Sui RPC

	// Mock response for testing
	stakeInfo := &StakeInfo{
		WalletAddress: walletAddress,
		StakeAmount:   1000000000, // 1 SUI
		Status:        1,          // Active
		LastUpdate:    time.Now().Unix(),
	}

	if stakeInfo.StakeAmount < minStake {
		return nil, fmt.Errorf("insufficient stake: required %d, have %d", minStake, stakeInfo.StakeAmount)
	}

	if stakeInfo.Status != 1 {
		return nil, fmt.Errorf("worker not in active status: %d", stakeInfo.Status)
	}

	return stakeInfo, nil
}

// GetWorkerInfo retrieves worker information from the registry contract
func (c *SuiClient) GetWorkerInfo(ctx context.Context, walletAddress string) (*WorkerInfo, error) {
	// Mock worker info for POC
	workerInfo := &WorkerInfo{
		WalletAddress:    walletAddress,
		NodeName:         "test-worker-node",
		StakeAmount:      1000000000,
		PerformanceScore: 95,
		RegistrationTime: time.Now().Add(-24 * time.Hour).Unix(),
		LastHeartbeat:    time.Now().Unix(),
		Status:           1, // Active
	}

	return workerInfo, nil
}

// WorkerInfo represents comprehensive worker information
type WorkerInfo struct {
	WalletAddress    string `json:"wallet_address"`
	NodeName         string `json:"node_name"`
	StakeAmount      uint64 `json:"stake_amount"`
	PerformanceScore uint64 `json:"performance_score"`
	RegistrationTime int64  `json:"registration_time"`
	LastHeartbeat    int64  `json:"last_heartbeat"`
	Status           uint8  `json:"status"`
}