// Nautilus Event-Driven K8s Executor
// Sui Contract Events → K8s API 실행 → Contract 응답 저장
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NautilusEventListener - Contract 이벤트 기반 K8s 실행자
type NautilusEventListener struct {
	suiRPCURL       string
	contractAddress string
	privateKeyHex   string
	k8sClient       kubernetes.Interface
	restClient      *resty.Client
	logger          *logrus.Logger
	wsConn          *websocket.Conn
	eventChannel    chan ContractEvent
	stopChannel     chan bool
}

// ContractEvent - Move Contract에서 발생하는 이벤트
type ContractEvent struct {
	Type      string    `json:"type"`
	PackageID string    `json:"packageId"`
	Module    string    `json:"module"`
	Sender    string    `json:"sender"`
	EventData EventData `json:"parsedJson"`
	TxDigest  string    `json:"transactionDigest"`
	Timestamp time.Time `json:"timestampMs"`
}

// EventData - K8s API 요청 이벤트 데이터
type EventData struct {
	RequestID    string `json:"request_id"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	Namespace    string `json:"namespace"`
	ResourceType string `json:"resource_type"`
	Payload      []int  `json:"payload"` // vector<u8> from Move
	SealToken    string `json:"seal_token"`
	Requester    string `json:"requester"`
	Priority     int    `json:"priority"`
	Timestamp    uint64 `json:"timestamp"`
}

// K8sExecutionResult - K8s 실행 결과
type K8sExecutionResult struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       json.RawMessage   `json:"body"`
	Success    bool              `json:"success"`
	Error      string            `json:"error,omitempty"`
}

func NewNautilusEventListener(suiRPCURL, contractAddr, privateKey string) *NautilusEventListener {
	// K8s 클라이언트 생성 (로컬 개발용)
	k8sConfig := &rest.Config{
		Host: "http://localhost:8080", // API 서버 주소
	}

	k8sClient, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		logrus.WithError(err).Warn("K8s client creation failed, using mock client")
		k8sClient = nil // Mock 모드
	}

	return &NautilusEventListener{
		suiRPCURL:       suiRPCURL,
		contractAddress: contractAddr,
		privateKeyHex:   privateKey,
		k8sClient:       k8sClient,
		restClient:      resty.New().SetTimeout(30 * time.Second),
		logger:          logrus.New(),
		eventChannel:    make(chan ContractEvent, 100),
		stopChannel:     make(chan bool),
	}
}

func (n *NautilusEventListener) Start() error {
	n.logger.Info("🌊 Nautilus Event Listener starting...")

	// 1. 헬스체크 서버 시작
	go n.startHealthServer()

	// 2. Mock 이벤트 처리 모드
	go n.startMockEventProcessor()

	n.logger.Info("✅ Nautilus Event Listener started in TEST mode")

	// 메인 루프
	select {
	case <-n.stopChannel:
		n.logger.Info("🛑 Nautilus Event Listener stopping...")
		return nil
	}
}

// startMockEventProcessor - 테스트용 Mock 이벤트 처리기
func (n *NautilusEventListener) startMockEventProcessor() {
	n.logger.Info("🧪 Starting mock event processor for testing")

	// 30초마다 모의 이벤트 생성
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		mockEvent := ContractEvent{
			Type:      "K8sAPIRequest",
			PackageID: "0x123",
			Module:    "k8s_gateway",
			Sender:    "0xtest",
			EventData: EventData{
				RequestID:    fmt.Sprintf("mock_%d", time.Now().Unix()),
				Method:       "GET",
				Path:         "/api/v1/pods",
				Namespace:    "default",
				ResourceType: "pods",
				Payload:      []int{},
				SealToken:    "mock_token",
				Requester:    "test_user",
				Priority:     1,
				Timestamp:    uint64(time.Now().Unix()),
			},
			TxDigest:  "mock_digest",
			Timestamp: time.Now(),
		}

		go n.handleK8sAPIRequest(mockEvent)
	}
}

// handleK8sAPIRequest - K8s API 요청 이벤트 처리
func (n *NautilusEventListener) handleK8sAPIRequest(event ContractEvent) {
	requestID := event.EventData.RequestID

	n.logger.WithFields(logrus.Fields{
		"request_id":    requestID,
		"method":        event.EventData.Method,
		"path":          event.EventData.Path,
		"resource_type": event.EventData.ResourceType,
	}).Info("🔧 Processing K8s API request")

	// 1. 이벤트 검증
	if !n.validateEvent(event) {
		n.storeErrorResponse(requestID, "Event validation failed", 400)
		return
	}

	// 2. K8s API 실행 (Mock 모드)
	result := n.executeK8sOperation(event.EventData)

	// 3. 결과 로깅
	n.logger.WithFields(logrus.Fields{
		"request_id":  requestID,
		"status_code": result.StatusCode,
		"success":     result.Success,
	}).Info("✅ K8s operation completed")
}

// validateEvent - 이벤트 검증
func (n *NautilusEventListener) validateEvent(event ContractEvent) bool {
	data := event.EventData

	if data.RequestID == "" || data.Method == "" || data.Path == "" {
		n.logger.Error("Invalid event: missing required fields")
		return false
	}

	allowedMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for _, method := range allowedMethods {
		if data.Method == method {
			return true
		}
	}

	n.logger.WithField("method", data.Method).Error("Unsupported HTTP method")
	return false
}

// executeK8sOperation - K8s API 실제 실행 (Mock 버전)
func (n *NautilusEventListener) executeK8sOperation(data EventData) *K8sExecutionResult {
	switch data.Method {
	case "GET":
		return n.handleGetRequest(data)
	case "POST":
		return n.handlePostRequest(data)
	case "PUT":
		return n.handlePutRequest(data)
	case "PATCH":
		return n.handlePatchRequest(data)
	case "DELETE":
		return n.handleDeleteRequest(data)
	default:
		return &K8sExecutionResult{
			StatusCode: 405,
			Error:      "Method not allowed",
			Success:    false,
		}
	}
}

// handleGetRequest - GET 요청 처리 (Mock)
func (n *NautilusEventListener) handleGetRequest(data EventData) *K8sExecutionResult {
	switch data.ResourceType {
	case "pods":
		return n.getMockPods(data.Namespace)
	case "services":
		return n.getMockServices(data.Namespace)
	case "deployments":
		return n.getMockDeployments(data.Namespace)
	case "nodes":
		return n.getMockNodes()
	default:
		return &K8sExecutionResult{
			StatusCode: 404,
			Error:      "Resource type not supported",
			Success:    false,
		}
	}
}

// getMockPods - Mock Pod 목록 반환
func (n *NautilusEventListener) getMockPods(namespace string) *K8sExecutionResult {
	if namespace == "" {
		namespace = "default"
	}

	mockPodList := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "PodList",
		"metadata": map[string]interface{}{
			"namespace": namespace,
		},
		"items": []map[string]interface{}{
			{
				"metadata": map[string]interface{}{
					"name":      "k3s-daas-test-pod",
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name":  "nginx",
							"image": "nginx:latest",
						},
					},
				},
				"status": map[string]interface{}{
					"phase": "Running",
				},
			},
		},
	}

	body, _ := json.Marshal(mockPodList)

	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
		Success:    true,
	}
}

// 다른 핸들러들 (Mock 버전)
func (n *NautilusEventListener) getMockServices(namespace string) *K8sExecutionResult {
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "v1", "kind": "ServiceList", "items": []}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) getMockDeployments(namespace string) *K8sExecutionResult {
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "apps/v1", "kind": "DeploymentList", "items": []}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) getMockNodes() *K8sExecutionResult {
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "v1", "kind": "NodeList", "items": []}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) handlePostRequest(data EventData) *K8sExecutionResult {
	return &K8sExecutionResult{
		StatusCode: 201,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"message": "Resource created"}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) handlePutRequest(data EventData) *K8sExecutionResult {
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"message": "Resource updated"}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) handlePatchRequest(data EventData) *K8sExecutionResult {
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"message": "Resource patched"}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) handleDeleteRequest(data EventData) *K8sExecutionResult {
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"message": "Resource deleted"}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) storeErrorResponse(requestID, errorMsg string, statusCode int) {
	n.logger.WithFields(logrus.Fields{
		"request_id":  requestID,
		"error":       errorMsg,
		"status_code": statusCode,
	}).Error("❌ K8s operation failed")
}

// startHealthServer - 헬스체크 서버
func (n *NautilusEventListener) startHealthServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"status": "healthy", "service": "nautilus-event-listener"}`)
	})

	n.logger.Info("🏥 Health server starting on :10250")
	if err := http.ListenAndServe(":10250", nil); err != nil {
		n.logger.WithError(err).Error("Health server failed")
	}
}

// main 함수
func main() {
	listener := NewNautilusEventListener(
		"https://fullnode.testnet.sui.io:443",
		"0x0", // Contract address
		"",    // Private key
	)

	if err := listener.Start(); err != nil {
		logrus.WithError(err).Fatal("Nautilus Event Listener failed to start")
	}
}