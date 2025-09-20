// Real Sui Client Implementation for K3s-DaaS
// This file provides actual Sui blockchain integration

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// RealSuiClient 실제 Sui 블록체인 클라이언트
type RealSuiClient struct {
	rpcURL        string
	httpClient    *http.Client
	packageID     string
	stakingPoolID string
	logger        *logrus.Logger
}

// NewRealSuiClient 실제 Sui 클라이언트 생성
func NewRealSuiClient(rpcURL, packageID, stakingPoolID string, logger *logrus.Logger) *RealSuiClient {
	return &RealSuiClient{
		rpcURL:        rpcURL,
		packageID:     packageID,
		stakingPoolID: stakingPoolID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// SuiRPCRequest Sui RPC 요청 구조체
type SuiRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// SuiRPCResponse Sui RPC 응답 구조체
type SuiRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *SuiRPCError `json:"error,omitempty"`
}

// SuiRPCError Sui RPC 에러 구조체
type SuiRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StakeRecordContent StakeRecord 객체 내용
type StakeRecordContent struct {
	DataType string `json:"dataType"`
	Type     string `json:"type"`
	HasPublicTransfer bool `json:"hasPublicTransfer"`
	Fields   struct {
		ID          string `json:"id"`
		Staker      string `json:"staker"`
		Amount      string `json:"amount"`
		StakedAt    string `json:"staked_at"`
		LockedUntil string `json:"locked_until"`
		Status      string `json:"status"`
		NodeID      string `json:"node_id"`
		StakeType   string `json:"stake_type"`
	} `json:"fields"`
}

// ValidateStakeOnChain 온체인에서 스테이킹 검증
func (c *RealSuiClient) ValidateStakeOnChain(ctx context.Context, walletAddress string, minStake uint64) (*StakeInfo, error) {
	c.logger.WithFields(logrus.Fields{
		"wallet":    walletAddress,
		"min_stake": minStake,
	}).Info("온체인 스테이킹 검증 시작")

	// 1. 스테이킹 풀에서 사용자의 스테이킹 기록 조회
	stakeRecords, err := c.getStakeRecords(ctx, walletAddress)
	if err != nil {
		return nil, fmt.Errorf("스테이킹 기록 조회 실패: %v", err)
	}

	if len(stakeRecords) == 0 {
		return nil, fmt.Errorf("스테이킹 기록이 없습니다")
	}

	// 2. 가장 큰 스테이킹 금액 찾기
	var maxStakeRecord *StakeRecordContent
	var maxStakeAmount uint64 = 0

	for _, record := range stakeRecords {
		// amount는 string으로 저장되므로 변환 필요
		amount, err := parseStakeAmount(record.Fields.Amount)
		if err != nil {
			c.logger.WithError(err).Warn("스테이킹 금액 파싱 실패")
			continue
		}

		if amount > maxStakeAmount && record.Fields.Status == "1" { // STAKE_ACTIVE
			maxStakeAmount = amount
			maxStakeRecord = &record
		}
	}

	// 3. 최소 스테이킹 요구사항 확인
	if maxStakeAmount < minStake {
		return nil, fmt.Errorf("스테이킹 금액 부족: %d < %d", maxStakeAmount, minStake)
	}

	// 4. StakeInfo 반환
	stakeInfo := &StakeInfo{
		WalletAddress: walletAddress,
		StakeAmount:   maxStakeAmount,
		StakeType:     maxStakeRecord.Fields.StakeType,
		NodeID:        maxStakeRecord.Fields.NodeID,
		IsValid:       true,
		ValidatedAt:   time.Now(),
	}

	c.logger.WithFields(logrus.Fields{
		"stake_amount": maxStakeAmount,
		"stake_type":   stakeInfo.StakeType,
		"node_id":      stakeInfo.NodeID,
	}).Info("온체인 스테이킹 검증 성공")

	return stakeInfo, nil
}

// getStakeRecords 사용자의 모든 스테이킹 기록 조회
func (c *RealSuiClient) getStakeRecords(ctx context.Context, walletAddress string) ([]StakeRecordContent, error) {
	// Sui RPC 요청: 사용자가 소유한 StakeRecord 객체들 조회
	request := &SuiRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sui_getOwnedObjects",
		Params: []interface{}{
			walletAddress,
			map[string]interface{}{
				"filter": map[string]interface{}{
					"StructType": fmt.Sprintf("%s::staking::StakeRecord", c.packageID),
				},
				"options": map[string]interface{}{
					"showContent": true,
					"showType":    true,
				},
			},
		},
	}

	response, err := c.makeRPCCall(ctx, request)
	if err != nil {
		return nil, err
	}

	// 응답 파싱
	result, ok := response.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("응답 형식이 잘못됨")
	}

	data, ok := result["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("데이터 형식이 잘못됨")
	}

	var stakeRecords []StakeRecordContent
	for _, item := range data {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// content 파싱
		content, ok := obj["data"].(map[string]interface{})
		if !ok {
			continue
		}

		contentBytes, err := json.Marshal(content)
		if err != nil {
			continue
		}

		var stakeRecord StakeRecordContent
		if err := json.Unmarshal(contentBytes, &stakeRecord); err != nil {
			continue
		}

		stakeRecords = append(stakeRecords, stakeRecord)
	}

	return stakeRecords, nil
}

// ValidateSealToken Seal 토큰 온체인 검증
func (c *RealSuiClient) ValidateSealToken(ctx context.Context, sealToken string) (*SealTokenInfo, error) {
	c.logger.WithField("token_prefix", sealToken[:min(len(sealToken), 10)]).Info("Seal 토큰 온체인 검증 시작")

	// 1. Seal 토큰으로 K8s Gateway에서 토큰 정보 조회
	tokenInfo, err := c.getSealTokenInfo(ctx, sealToken)
	if err != nil {
		return nil, fmt.Errorf("Seal 토큰 정보 조회 실패: %v", err)
	}

	// 2. 토큰 만료 확인
	if time.Now().Unix() > tokenInfo.ExpiresAt {
		return nil, fmt.Errorf("토큰이 만료됨")
	}

	c.logger.WithFields(logrus.Fields{
		"user_id":      tokenInfo.UserID,
		"stake_amount": tokenInfo.StakeAmount,
		"node_id":      tokenInfo.NodeID,
	}).Info("Seal 토큰 온체인 검증 성공")

	return tokenInfo, nil
}

// getSealTokenInfo K8s Gateway 컨트랙트에서 Seal 토큰 정보 조회
func (c *RealSuiClient) getSealTokenInfo(ctx context.Context, sealToken string) (*SealTokenInfo, error) {
	// Sui RPC 요청: 특정 객체 조회 (Seal 토큰 객체 ID 사용)
	request := &SuiRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sui_getObject",
		Params: []interface{}{
			sealToken, // 토큰 = 객체 ID
			map[string]interface{}{
				"showContent": true,
				"showType":    true,
			},
		},
	}

	response, err := c.makeRPCCall(ctx, request)
	if err != nil {
		return nil, err
	}

	// 응답에서 SealToken 객체 정보 파싱
	result, ok := response.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("응답 형식이 잘못됨")
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("토큰 데이터가 없음")
	}

	content, ok := data["content"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("토큰 내용이 없음")
	}

	fields, ok := content["fields"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("토큰 필드가 없음")
	}

	// SealTokenInfo 구성
	tokenInfo := &SealTokenInfo{
		Token:       sealToken,
		UserID:      getString(fields, "wallet_address"),
		StakeAmount: getUint64(fields, "stake_amount"),
		NodeID:      getString(fields, "wallet_address"), // 지갑 주소를 노드 ID로 사용
		ExpiresAt:   getInt64(fields, "expires_at"),
		Permissions: []string{"cluster-read", "cluster-write"}, // 기본 권한
	}

	return tokenInfo, nil
}

// ListenForSuiEvents Sui 이벤트 리스닝 (실시간 블록체인 이벤트 구독)
func (c *RealSuiClient) ListenForSuiEvents(ctx context.Context, eventChannel chan<- SuiEvent) error {
	c.logger.Info("Sui 이벤트 리스닝 시작")

	// 이벤트 구독 요청
	request := &SuiRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sui_subscribeEvent",
		Params: []interface{}{
			map[string]interface{}{
				"Package": c.packageID,
			},
		},
	}

	// WebSocket 연결로 실시간 이벤트 수신 (단순화를 위해 폴링 사용)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Sui 이벤트 리스닝 중지")
			return ctx.Err()
		case <-ticker.C:
			// 주기적으로 최근 이벤트 조회 (실제로는 WebSocket 사용 권장)
			events, err := c.getRecentEvents(ctx)
			if err != nil {
				c.logger.WithError(err).Warn("최근 이벤트 조회 실패")
				continue
			}

			// 이벤트 전송
			for _, event := range events {
				select {
				case eventChannel <- event:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

// getRecentEvents 최근 이벤트 조회
func (c *RealSuiClient) getRecentEvents(ctx context.Context) ([]SuiEvent, error) {
	request := &SuiRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sui_queryEvents",
		Params: []interface{}{
			map[string]interface{}{
				"Package": c.packageID,
			},
			nil, // cursor
			10,  // limit
			false, // descending order
		},
	}

	response, err := c.makeRPCCall(ctx, request)
	if err != nil {
		return nil, err
	}

	// 이벤트 파싱 (단순화된 버전)
	var events []SuiEvent
	// 실제 구현에서는 response.Result에서 이벤트 데이터를 파싱
	// 지금은 예시로 빈 배열 반환
	
	return events, nil
}

// makeRPCCall Sui RPC 호출 실행
func (c *RealSuiClient) makeRPCCall(ctx context.Context, request *SuiRPCRequest) (*SuiRPCResponse, error) {
	// 요청 직렬화
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("요청 직렬화 실패: %v", err)
	}

	// HTTP 요청 생성
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.rpcURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("HTTP 요청 생성 실패: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// 요청 실행
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP 요청 실행 실패: %v", err)
	}
	defer resp.Body.Close()

	// 응답 읽기
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %v", err)
	}

	// 응답 파싱
	var rpcResponse SuiRPCResponse
	if err := json.Unmarshal(body, &rpcResponse); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %v", err)
	}

	// 에러 확인
	if rpcResponse.Error != nil {
		return nil, fmt.Errorf("Sui RPC 에러: %s (코드: %d)", rpcResponse.Error.Message, rpcResponse.Error.Code)
	}

	return &rpcResponse, nil
}

// SuiEvent Sui 블록체인 이벤트
type SuiEvent struct {
	Type      string                 `json:"type"`
	Package   string                 `json:"package"`
	Module    string                 `json:"module"`
	Sender    string                 `json:"sender"`
	Timestamp int64                  `json:"timestamp"`
	Fields    map[string]interface{} `json:"fields"`
}

// SealTokenInfo Seal 토큰 정보 (실제 블록체인에서 조회)
type SealTokenInfo struct {
	Token       string    `json:"token"`
	UserID      string    `json:"user_id"`
	StakeAmount uint64    `json:"stake_amount"`
	NodeID      string    `json:"node_id"`
	ExpiresAt   int64     `json:"expires_at"`
	Permissions []string  `json:"permissions"`
}

// 유틸리티 함수들

// parseStakeAmount 스테이킹 금액 문자열을 uint64로 변환
func parseStakeAmount(amountStr string) (uint64, error) {
	// Sui에서는 보통 문자열로 큰 숫자를 표현
	// "1000000000" -> 1000000000 (1 SUI in MIST)
	
	// 16진수 또는 10진수 처리
	if strings.HasPrefix(amountStr, "0x") {
		// 16진수
		decoded, err := hex.DecodeString(amountStr[2:])
		if err != nil {
			return 0, err
		}
		
		var result uint64
		for _, b := range decoded {
			result = result*256 + uint64(b)
		}
		return result, nil
	} else {
		// 10진수 (단순화)
		var result uint64
		for _, c := range amountStr {
			if c >= '0' && c <= '9' {
				result = result*10 + uint64(c-'0')
			}
		}
		return result, nil
	}
}

// getString map에서 문자열 값 안전하게 가져오기
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// getUint64 map에서 uint64 값 안전하게 가져오기
func getUint64(m map[string]interface{}, key string) uint64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case string:
			result, _ := parseStakeAmount(v)
			return result
		case float64:
			return uint64(v)
		case int64:
			return uint64(v)
		}
	}
	return 0
}

// getInt64 map에서 int64 값 안전하게 가져오기
func getInt64(m map[string]interface{}, key string) int64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case string:
			result, _ := parseStakeAmount(v)
			return int64(result)
		case float64:
			return int64(v)
		case int64:
			return v
		}
	}
	return 0
}

// min 최소값 함수
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}