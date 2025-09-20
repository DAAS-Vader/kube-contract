// Contract-First API Gateway
// kubectl → Move Contract → Nautilus Event-Driven Architecture
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"
)

// ContractAPIGateway - kubectl과 Move Contract 간의 브릿지
type ContractAPIGateway struct {
	suiRPCURL       string
	contractAddress string
	privateKeyHex   string
	logger          *logrus.Logger
	client          *resty.Client
	responseCache   map[string]*PendingResponse
}

// PendingResponse - 비동기 응답 대기 중인 요청
type PendingResponse struct {
	RequestID   string
	StartTime   time.Time
	Method      string
	Path        string
	Requester   string
	Completed   bool
	Response    *K8sResponse
	WaitChannel chan *K8sResponse
}

// K8sResponse - Contract에서 받는 응답
type K8sResponse struct {
	StatusCode  int               `json:"status_code"`
	Headers     map[string]string `json:"headers"`
	Body        json.RawMessage   `json:"body"`
	ProcessedAt time.Time         `json:"processed_at"`
}

// KubectlRequest - kubectl 요청 구조체
type KubectlRequest struct {
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Namespace    string            `json:"namespace"`
	ResourceType string            `json:"resource_type"`
	Payload      []byte            `json:"payload"`
	SealToken    string            `json:"seal_token"`
	Headers      map[string]string `json:"headers"`
	UserAgent    string            `json:"user_agent"`
}

// SuiTransaction - Sui 트랜잭션 구조체
type SuiTransaction struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

func NewContractAPIGateway(suiRPCURL, contractAddr, privateKey string) *ContractAPIGateway {
	return &ContractAPIGateway{
		suiRPCURL:       suiRPCURL,
		contractAddress: contractAddr,
		privateKeyHex:   privateKey,
		logger:          logrus.New(),
		client:          resty.New().SetTimeout(30 * time.Second),
		responseCache:   make(map[string]*PendingResponse),
	}
}

func (g *ContractAPIGateway) Start() {
	g.logger.Info("🚀 Contract-First API Gateway starting...")

	// HTTP 핸들러 등록
	http.HandleFunc("/", g.handleKubectlRequest)
	http.HandleFunc("/healthz", g.handleHealth)
	http.HandleFunc("/readyz", g.handleReady)

	// 응답 정리 고루틴 시작
	go g.cleanupExpiredResponses()

	port := ":8080"
	g.logger.Infof("🎯 API Gateway listening on %s", port)
	g.logger.Info("📝 kubectl 설정:")
	g.logger.Info("   kubectl config set-cluster k3s-daas --server=http://localhost:8080")
	g.logger.Info("   kubectl config set-credentials user --token=seal_YOUR_WALLET_SIGNATURE_CHALLENGE_TIMESTAMP")
	g.logger.Info("   kubectl config use-context k3s-daas")

	if err := http.ListenAndServe(port, nil); err != nil {
		g.logger.Fatalf("❌ Failed to start API Gateway: %v", err)
	}
}

// handleKubectlRequest - kubectl 요청의 메인 진입점
func (g *ContractAPIGateway) handleKubectlRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := g.generateRequestID()

	g.logger.WithFields(logrus.Fields{
		"request_id": requestID,
		"method":     r.Method,
		"path":       r.URL.Path,
		"user_agent": r.UserAgent(),
	}).Info("📨 kubectl request received")

	// 1. Seal Token 추출
	sealToken := g.extractSealToken(r)
	if sealToken == "" {
		g.returnK8sError(w, "Unauthorized", "Missing or invalid Seal token", 401)
		return
	}

	// 2. kubectl 요청 파싱
	kubectlReq, err := g.parseKubectlRequest(r, sealToken)
	if err != nil {
		g.logger.WithError(err).Error("Failed to parse kubectl request")
		g.returnK8sError(w, "BadRequest", err.Error(), 400)
		return
	}

	// 3. Move Contract 호출
	txResult, err := g.callMoveContract(requestID, kubectlReq)
	if err != nil {
		g.logger.WithError(err).Error("Failed to call Move Contract")
		g.returnK8sError(w, "InternalServerError", "Blockchain validation failed", 500)
		return
	}

	g.logger.WithFields(logrus.Fields{
		"request_id": requestID,
		"tx_digest":  txResult.Digest,
	}).Info("✅ Move Contract call successful")

	// 4. 응답 대기 (비동기)
	response, err := g.waitForContractResponse(requestID, 30*time.Second)
	if err != nil {
		g.logger.WithError(err).Error("Response timeout or error")
		g.returnK8sError(w, "RequestTimeout", "Operation timeout", 504)
		return
	}

	// 5. kubectl에 응답
	g.writeKubectlResponse(w, response)

	duration := time.Since(startTime)
	g.logger.WithFields(logrus.Fields{
		"request_id": requestID,
		"duration":   duration,
		"status":     response.StatusCode,
	}).Info("✅ Request completed")
}

// parseKubectlRequest - kubectl 요청을 Contract 호출 형태로 변환
func (g *ContractAPIGateway) parseKubectlRequest(r *http.Request, sealToken string) (*KubectlRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %v", err)
	}
	defer r.Body.Close()

	// URL 경로에서 namespace와 resource type 추출
	namespace, resourceType := g.parseK8sPath(r.URL.Path)

	return &KubectlRequest{
		Method:       r.Method,
		Path:         r.URL.Path,
		Namespace:    namespace,
		ResourceType: resourceType,
		Payload:      body,
		SealToken:    sealToken,
		Headers:      g.extractHeaders(r),
		UserAgent:    r.UserAgent(),
	}, nil
}

// callMoveContract - Move Contract의 execute_kubectl_command 함수 호출
func (g *ContractAPIGateway) callMoveContract(requestID string, req *KubectlRequest) (*SuiTransactionResult, error) {
	// Move Call 트랜잭션 구성
	moveCall := map[string]interface{}{
		"packageObjectId": g.contractAddress,
		"module":          "k8s_gateway",
		"function":        "execute_kubectl_command_with_id",
		"typeArguments":   []string{},
		"arguments": []interface{}{
			requestID,                    // request_id
			req.SealToken,                // seal_token (object ID)
			req.Method,                   // method
			req.Path,                     // path
			req.Namespace,                // namespace
			req.ResourceType,             // resource_type
			g.bytesToVector(req.Payload), // payload as vector<u8>
		},
	}

	// 트랜잭션 블록 구성
	txBlock := map[string]interface{}{
		"version":    1,
		"sender":     g.getSenderAddress(),
		"gasPayment": nil,
		"gasBudget":  "10000000", // 10M MIST
		"gasPrice":   "1000",
		"transactions": []interface{}{
			map[string]interface{}{
				"MoveCall": moveCall,
			},
		},
	}

	// 직렬화 및 서명
	txBytes, err := g.serializeTransaction(txBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction: %v", err)
	}

	// Sui RPC 호출
	suiTx := SuiTransaction{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sui_executeTransactionBlock",
		Params: []interface{}{
			txBytes,
			[]string{g.privateKeyHex},
			map[string]interface{}{
				"requestType": "WaitForLocalExecution",
				"options": map[string]bool{
					"showEvents":        true,
					"showObjectChanges": true,
					"showEffects":       true,
				},
			},
		},
	}

	resp, err := g.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(suiTx).
		Post(g.suiRPCURL)

	if err != nil {
		return nil, fmt.Errorf("Sui RPC call failed: %v", err)
	}

	var result SuiTransactionResult
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse Sui response: %v", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("Sui transaction failed: %v", result.Error)
	}

	return &result, nil
}

// waitForContractResponse - Contract에서 응답이 올 때까지 대기
func (g *ContractAPIGateway) waitForContractResponse(requestID string, timeout time.Duration) (*K8sResponse, error) {
	// PendingResponse 등록
	pending := &PendingResponse{
		RequestID:   requestID,
		StartTime:   time.Now(),
		WaitChannel: make(chan *K8sResponse, 1),
	}
	g.responseCache[requestID] = pending

	// 폴링으로 Contract 응답 확인
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for {
		select {
		case <-timeoutTimer.C:
			delete(g.responseCache, requestID)
			return nil, fmt.Errorf("response timeout after %v", timeout)

		case <-ticker.C:
			// Contract에서 응답 조회
			response, err := g.queryContractResponse(requestID)
			if err != nil {
				g.logger.WithError(err).Debug("Response not ready yet")
				continue
			}

			if response != nil {
				delete(g.responseCache, requestID)
				return response, nil
			}

		case response := <-pending.WaitChannel:
			delete(g.responseCache, requestID)
			return response, nil
		}
	}
}

// queryContractResponse - Contract에서 응답 조회
func (g *ContractAPIGateway) queryContractResponse(requestID string) (*K8sResponse, error) {
	// Contract의 get_k8s_response 함수 호출
	queryCall := map[string]interface{}{
		"packageObjectId": g.contractAddress,
		"module":          "k8s_gateway",
		"function":        "get_k8s_response",
		"typeArguments":   []string{},
		"arguments": []interface{}{
			requestID,
		},
	}

	suiCall := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "sui_devInspectTransactionBlock",
		"params": []interface{}{
			g.getSenderAddress(),
			map[string]interface{}{
				"kind": "MoveCall",
				"data": queryCall,
			},
		},
	}

	resp, err := g.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(suiCall).
		Post(g.suiRPCURL)

	if err != nil {
		return nil, err
	}

	// 응답 파싱 (간단화)
	var result map[string]interface{}
	json.Unmarshal(resp.Body(), &result)

	// 응답이 준비되었는지 확인
	if result["result"] == nil {
		return nil, fmt.Errorf("response not ready")
	}

	// TODO: 실제 응답 데이터 파싱
	return &K8sResponse{
		StatusCode:  200,
		Headers:     map[string]string{"Content-Type": "application/json"},
		Body:        json.RawMessage(`{"items": []}`),
		ProcessedAt: time.Now(),
	}, nil
}

// 유틸리티 함수들

func (g *ContractAPIGateway) extractSealToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (g *ContractAPIGateway) parseK8sPath(path string) (namespace, resourceType string) {
	// /api/v1/namespaces/default/pods → namespace=default, resourceType=pods
	// /api/v1/pods → namespace=default, resourceType=pods
	parts := strings.Split(strings.Trim(path, "/"), "/")

	namespace = "default" // 기본값
	for i, part := range parts {
		if part == "namespaces" && i+1 < len(parts) {
			namespace = parts[i+1]
		}
		if part == "pods" || part == "services" || part == "deployments" {
			resourceType = part
		}
	}

	if resourceType == "" {
		resourceType = "unknown"
	}

	return
}

func (g *ContractAPIGateway) extractHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return headers
}

func (g *ContractAPIGateway) generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

func (g *ContractAPIGateway) bytesToVector(data []byte) []int {
	vector := make([]int, len(data))
	for i, b := range data {
		vector[i] = int(b)
	}
	return vector
}

func (g *ContractAPIGateway) serializeTransaction(txBlock map[string]interface{}) (string, error) {
	txJSON, err := json.Marshal(txBlock)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(txJSON), nil
}

func (g *ContractAPIGateway) getSenderAddress() string {
	// TODO: 개인키에서 주소 추출
	return "0x1234567890abcdef" // 임시
}

func (g *ContractAPIGateway) writeKubectlResponse(w http.ResponseWriter, response *K8sResponse) {
	// 헤더 설정
	for key, value := range response.Headers {
		w.Header().Set(key, value)
	}

	// 상태 코드 설정
	w.WriteHeader(response.StatusCode)

	// 응답 본문 작성
	w.Write(response.Body)
}

func (g *ContractAPIGateway) returnK8sError(w http.ResponseWriter, reason, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	errorResponse := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Status",
		"status":     "Failure",
		"message":    message,
		"reason":     reason,
		"code":       code,
	}

	json.NewEncoder(w).Encode(errorResponse)
}

func (g *ContractAPIGateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	fmt.Fprintf(w, "OK")
}

func (g *ContractAPIGateway) handleReady(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	fmt.Fprintf(w, "Ready")
}

func (g *ContractAPIGateway) cleanupExpiredResponses() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		for id, pending := range g.responseCache {
			if now.Sub(pending.StartTime) > 5*time.Minute {
				delete(g.responseCache, id)
			}
		}
	}
}

// SuiTransactionResult - Sui 트랜잭션 결과
type SuiTransactionResult struct {
	Result struct {
		Digest  string                 `json:"digest"`
		Effects map[string]interface{} `json:"effects"`
		Events  []interface{}          `json:"events"`
	} `json:"result"`
	Error interface{} `json:"error"`
}

// main 함수
func main() {
	gateway := NewContractAPIGateway(
		"https://fullnode.testnet.sui.io:443",
		"0x0", // Contract address - 실제 배포 후 설정
		"",    // Private key - 환경변수에서 로드
	)

	gateway.Start()
}
