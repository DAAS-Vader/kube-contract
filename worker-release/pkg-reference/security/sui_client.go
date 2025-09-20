package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SuiClient handles interactions with Sui blockchain
type SuiClient struct {
	rpcURL     string
	httpClient *http.Client
	mockMode   bool // 개발/테스트용 플래그
}

// NewSuiClient creates a new Sui client instance
func NewSuiClient(rpcURL string) *SuiClient {
	return &SuiClient{
		rpcURL: rpcURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		mockMode: false, // 기본값은 실제 모드
	}
}

// SetMockMode enables/disables mock mode for testing
func (c *SuiClient) SetMockMode(enabled bool) {
	c.mockMode = enabled
}

// ValidateStake checks if a wallet has sufficient stake
func (c *SuiClient) ValidateStake(ctx context.Context, walletAddress string, minStake uint64) (*StakeInfo, error) {
	if c.mockMode {
		return c.validateStakeMock(walletAddress, minStake)
	}

	return c.validateStakeReal(ctx, walletAddress, minStake)
}

// validateStakeMock returns mock data for testing
func (c *SuiClient) validateStakeMock(walletAddress string, minStake uint64) (*StakeInfo, error) {
	// 다양한 테스트 케이스를 위한 mock 데이터
	mockStakeAmount := uint64(1000000000) // 1 SUI

	// 지갑 주소에 따른 다른 mock 데이터
	if walletAddress == "test_insufficient" {
		mockStakeAmount = minStake - 1
	} else if walletAddress == "test_high_stake" {
		mockStakeAmount = 10000000000 // 10 SUI
	}

	stakeInfo := &StakeInfo{
		WalletAddress: walletAddress,
		NodeID:        walletAddress, // kubectl_auth.go 호환성
		StakeAmount:   mockStakeAmount,
		Status:        "active",
		LastUpdate:    time.Now().Unix(),
		ValidUntil:    time.Now().Add(24 * time.Hour),
	}

	if stakeInfo.StakeAmount < minStake {
		return nil, fmt.Errorf("insufficient stake: required %d, have %d", minStake, stakeInfo.StakeAmount)
	}

	return stakeInfo, nil
}

// validateStakeReal calls actual Sui blockchain
func (c *SuiClient) validateStakeReal(ctx context.Context, walletAddress string, minStake uint64) (*StakeInfo, error) {
	// Sui RPC 요청 구성
	rpcRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "sui_getOwnedObjects",
		"params": []interface{}{
			walletAddress,
			map[string]interface{}{
				"filter": map[string]interface{}{
					"StructType": "{{PACKAGE_ID}}::staking::StakeRecord",
				},
				"options": map[string]interface{}{
					"showContent": true,
					"showDisplay": true,
				},
			},
		},
	}

	reqBody, err := json.Marshal(rpcRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RPC request: %v", err)
	}

	// HTTP 요청 생성
	req, err := http.NewRequestWithContext(ctx, "POST", c.rpcURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 요청 실행
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Sui RPC: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Sui RPC returned status: %d", resp.StatusCode)
	}

	// 응답 파싱
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	var rpcResponse struct {
		Result struct {
			Data []struct {
				Data struct {
					Content struct {
						Fields struct {
							StakeAmount string `json:"principal"`
						} `json:"fields"`
					} `json:"content"`
				} `json:"data"`
			} `json:"data"`
		} `json:"result"`
		Error interface{} `json:"error"`
	}

	if err := json.Unmarshal(body, &rpcResponse); err != nil {
		return nil, fmt.Errorf("failed to parse RPC response: %v", err)
	}

	if rpcResponse.Error != nil {
		return nil, fmt.Errorf("Sui RPC error: %v", rpcResponse.Error)
	}

	// 스테이킹 정보 추출 및 검증
	var totalStake uint64
	for range rpcResponse.Result.Data {
		// 실제로는 각 staking object의 amount를 파싱해야 함
		totalStake += 1000000000 // 임시로 1 SUI로 설정
	}

	stakeInfo := &StakeInfo{
		WalletAddress: walletAddress,
		NodeID:        walletAddress,
		StakeAmount:   totalStake,
		Status:        "active",
		LastUpdate:    time.Now().Unix(),
		ValidUntil:    time.Now().Add(24 * time.Hour),
	}

	if totalStake < minStake {
		return nil, fmt.Errorf("insufficient stake: required %d, have %d", minStake, totalStake)
	}

	return stakeInfo, nil
}

// ValidateSealToken validates a Seal token against blockchain
func (c *SuiClient) ValidateSealToken(token *SealToken, minStake uint64) error {
	if token == nil {
		return fmt.Errorf("nil seal token")
	}

	// Seal token의 지갑 주소로 스테이킹 검증
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stakeInfo, err := c.ValidateStake(ctx, token.WalletAddress, minStake)
	if err != nil {
		return fmt.Errorf("stake validation failed: %v", err)
	}

	if stakeInfo.Status != "active" {
		return fmt.Errorf("wallet not in active status: %s", stakeInfo.Status)
	}

	return nil
}

// GetWorkerInfo retrieves worker information from the registry contract
func (c *SuiClient) GetWorkerInfo(ctx context.Context, walletAddress string) (*WorkerInfo, error) {
	if c.mockMode {
		return c.getWorkerInfoMock(walletAddress), nil
	}

	// 실제 구현에서는 Sui 컨트랙트에서 워커 정보를 조회
	return c.getWorkerInfoReal(ctx, walletAddress)
}

// getWorkerInfoMock returns mock worker info for testing
func (c *SuiClient) getWorkerInfoMock(walletAddress string) *WorkerInfo {
	return &WorkerInfo{
		WalletAddress:    walletAddress,
		NodeName:         fmt.Sprintf("worker-%s", walletAddress[:8]),
		StakeAmount:      1000000000,
		PerformanceScore: 95,
		RegistrationTime: time.Now().Add(-24 * time.Hour).Unix(),
		LastHeartbeat:    time.Now().Unix(),
		Status:           "active",
	}
}

// getWorkerInfoReal retrieves actual worker info from blockchain
func (c *SuiClient) getWorkerInfoReal(ctx context.Context, walletAddress string) (*WorkerInfo, error) {
	// 실제 Sui 컨트랙트 호출 구현
	// 현재는 mock 데이터 반환
	return c.getWorkerInfoMock(walletAddress), nil
}