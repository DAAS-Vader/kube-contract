// Contract-First API Gateway
// kubectl → Move Contract → Nautilus Event-Driven Architecture
package main

import (
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

// SuiTransactionResult - 수정된 Sui 트랜잭션 결과
type SuiTransactionResult struct {
	Result struct {
		Digest  string                 `json:"digest"`
		Effects map[string]interface{} `json:"effects"`
		Events  []interface{}          `json:"events"`
	} `json:"result"`
	Error interface{} `json:"error"`
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
	http.HandleFunc("/api", g.handleAPIGroups)
	http.HandleFunc("/apis", g.handleAPIGroups)
	http.HandleFunc("/api/v1", g.handleAPIResources)
	http.HandleFunc("/apis/apps/v1", g.handleAPIResources)

	// 응답 정리 고루틴 시작
	go g.cleanupExpiredResponses()

	port := ":8080"
	g.logger.Infof("🎯 API Gateway listening on %s", port)
	g.logger.Info("📝 kubectl 설정:")
	g.logger.Info("   kubectl config set-cluster k3s-daas --server=http://localhost:8080")
	g.logger.Info("   kubectl config set-credentials user --token=seal_YOUR_WALLET_SIGNATURE")
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

	// 3. Move Contract 호출 시뮬레이션 (실제 계약 없이 테스트용)
	g.logger.WithFields(logrus.Fields{
		"request_id": requestID,
		"method":     kubectlReq.Method,
		"path":       kubectlReq.Path,
	}).Info("🔗 Simulating contract call for testing")

	// 4. 모의 응답 생성 (테스트용)
	response := &K8sResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "v1", "kind": "PodList", "items": []}`),
		ProcessedAt: time.Now(),
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

// 유틸리티 함수들
func (g *ContractAPIGateway) extractSealToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (g *ContractAPIGateway) parseK8sPath(path string) (namespace, resourceType string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")

	namespace = "default"
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

func (g *ContractAPIGateway) writeKubectlResponse(w http.ResponseWriter, response *K8sResponse) {
	for key, value := range response.Headers {
		w.Header().Set(key, value)
	}

	w.WriteHeader(response.StatusCode)
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

func (g *ContractAPIGateway) handleAPIGroups(w http.ResponseWriter, r *http.Request) {
	g.logger.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("📋 API Groups request received")

	w.Header().Set("Content-Type", "application/json")

	// K8s API 그룹 정보 제공
	if r.URL.Path == "/api" {
		apiGroupResponse := map[string]interface{}{
			"kind":       "APIVersions",
			"apiVersion": "v1",
			"versions":   []string{"v1"},
			"serverAddressByClientCIDRs": []map[string]string{
				{"clientCIDR": "0.0.0.0/0", "serverAddress": "localhost:8080"},
			},
		}
		json.NewEncoder(w).Encode(apiGroupResponse)
	} else if r.URL.Path == "/apis" {
		apiGroupListResponse := map[string]interface{}{
			"kind":       "APIGroupList",
			"apiVersion": "v1",
			"groups": []map[string]interface{}{
				{
					"name": "apps",
					"versions": []map[string]interface{}{
						{"groupVersion": "apps/v1", "version": "v1"},
					},
					"preferredVersion": map[string]string{
						"groupVersion": "apps/v1",
						"version":      "v1",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(apiGroupListResponse)
	}
}

func (g *ContractAPIGateway) handleAPIResources(w http.ResponseWriter, r *http.Request) {
	g.logger.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("📋 API Resources request received")

	w.Header().Set("Content-Type", "application/json")

	if r.URL.Path == "/api/v1" {
		// Core API 리소스
		coreAPIResources := map[string]interface{}{
			"kind":       "APIResourceList",
			"apiVersion": "v1",
			"groupVersion": "v1",
			"resources": []map[string]interface{}{
				{
					"name":         "pods",
					"singularName": "pod",
					"namespaced":   true,
					"kind":         "Pod",
					"verbs":        []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				},
				{
					"name":         "services",
					"singularName": "service",
					"namespaced":   true,
					"kind":         "Service",
					"verbs":        []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				},
				{
					"name":         "nodes",
					"singularName": "node",
					"namespaced":   false,
					"kind":         "Node",
					"verbs":        []string{"get", "list", "watch"},
				},
			},
		}
		json.NewEncoder(w).Encode(coreAPIResources)
	} else if r.URL.Path == "/apis/apps/v1" {
		// Apps API 리소스
		appsAPIResources := map[string]interface{}{
			"kind":       "APIResourceList",
			"apiVersion": "v1",
			"groupVersion": "apps/v1",
			"resources": []map[string]interface{}{
				{
					"name":         "deployments",
					"singularName": "deployment",
					"namespaced":   true,
					"kind":         "Deployment",
					"verbs":        []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				},
			},
		}
		json.NewEncoder(w).Encode(appsAPIResources)
	}
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

// main 함수
func main() {
	gateway := NewContractAPIGateway(
		"https://fullnode.testnet.sui.io:443",
		"0x0", // Contract address - 실제 배포 후 설정
		"",    // Private key - 환경변수에서 로드
	)

	gateway.Start()
}