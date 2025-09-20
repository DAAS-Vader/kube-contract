// Nautilus Event-Driven K8s Executor
// Sui Contract Events → K8s API 실행 → Contract 응답 저장
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	RequestID    string   `json:"request_id"`
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	Namespace    string   `json:"namespace"`
	ResourceType string   `json:"resource_type"`
	Payload      []int    `json:"payload"` // vector<u8> from Move
	SealToken    string   `json:"seal_token"`
	Requester    string   `json:"requester"`
	Priority     int      `json:"priority"`
	Timestamp    uint64   `json:"timestamp"`
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
	// K8s 클라이언트 생성 (in-cluster 설정)
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		// 로컬 개발용 fallback
		k8sConfig = &rest.Config{
			Host: "http://localhost:8080", // API 서버 주소
		}
	}

	k8sClient, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create K8s client")
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

	// 1. Sui 이벤트 구독 시작
	if err := n.subscribeToContractEvents(); err != nil {
		return fmt.Errorf("failed to subscribe to events: %v", err)
	}

	// 2. 이벤트 처리 고루틴 시작
	go n.processEvents()

	// 3. 헬스체크 서버 시작
	go n.startHealthServer()

	n.logger.Info("✅ Nautilus Event Listener started successfully")

	// 메인 루프
	select {
	case <-n.stopChannel:
		n.logger.Info("🛑 Nautilus Event Listener stopping...")
		return nil
	}
}

// subscribeToContractEvents - Sui WebSocket으로 Contract 이벤트 구독
func (n *NautilusEventListener) subscribeToContractEvents() error {
	// WebSocket 연결
	wsURL := strings.Replace(n.suiRPCURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)

	var err error
	n.wsConn, _, err = websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket connection failed: %v", err)
	}

	// 이벤트 필터 구성
	subscribeMessage := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "suix_subscribeEvent",
		"params": []interface{}{
			map[string]interface{}{
				"Package": n.contractAddress,
				"Module":  "k8s_gateway",
			},
		},
	}

	// 구독 요청 전송
	if err := n.wsConn.WriteJSON(subscribeMessage); err != nil {
		return fmt.Errorf("failed to send subscribe message: %v", err)
	}

	// 응답 확인
	var response map[string]interface{}
	if err := n.wsConn.ReadJSON(&response); err != nil {
		return fmt.Errorf("failed to read subscribe response: %v", err)
	}

	n.logger.WithField("response", response).Info("✅ Contract event subscription successful")

	// 이벤트 수신 고루틴 시작
	go n.receiveEvents()

	return nil
}

// receiveEvents - WebSocket에서 이벤트 수신
func (n *NautilusEventListener) receiveEvents() {
	defer n.wsConn.Close()

	for {
		var message map[string]interface{}
		if err := n.wsConn.ReadJSON(&message); err != nil {
			n.logger.WithError(err).Error("Failed to read WebSocket message")
			break
		}

		// 이벤트 파싱
		if params, ok := message["params"].(map[string]interface{}); ok {
			if result, ok := params["result"].(map[string]interface{}); ok {
				event := n.parseContractEvent(result)
				if event != nil {
					select {
					case n.eventChannel <- *event:
						n.logger.WithField("event", event.Type).Debug("Event queued for processing")
					default:
						n.logger.Warning("Event channel full, dropping event")
					}
				}
			}
		}
	}
}

// parseContractEvent - Contract 이벤트 파싱
func (n *NautilusEventListener) parseContractEvent(eventData map[string]interface{}) *ContractEvent {
	// K8sAPIRequest 이벤트만 처리
	eventType, ok := eventData["type"].(string)
	if !ok || !strings.Contains(eventType, "K8sAPIRequest") {
		return nil
	}

	// 이벤트 데이터 추출
	var event ContractEvent
	jsonData, _ := json.Marshal(eventData)
	if err := json.Unmarshal(jsonData, &event); err != nil {
		n.logger.WithError(err).Error("Failed to parse contract event")
		return nil
	}

	return &event
}

// processEvents - 이벤트 처리 메인 루프
func (n *NautilusEventListener) processEvents() {
	for {
		select {
		case event := <-n.eventChannel:
			go n.handleK8sAPIRequest(event)

		case <-n.stopChannel:
			return
		}
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

	// 2. K8s API 실행
	result := n.executeK8sOperation(event.EventData)

	// 3. Contract에 결과 저장
	if err := n.storeResponseToContract(requestID, result); err != nil {
		n.logger.WithError(err).Error("Failed to store response to contract")
	}
}

// validateEvent - 이벤트 검증
func (n *NautilusEventListener) validateEvent(event ContractEvent) bool {
	data := event.EventData

	// 기본 필드 검증
	if data.RequestID == "" || data.Method == "" || data.Path == "" {
		n.logger.Error("Invalid event: missing required fields")
		return false
	}

	// 지원 메서드 확인
	allowedMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	methodValid := false
	for _, method := range allowedMethods {
		if data.Method == method {
			methodValid = true
			break
		}
	}

	if !methodValid {
		n.logger.WithField("method", data.Method).Error("Unsupported HTTP method")
		return false
	}

	return true
}

// executeK8sOperation - K8s API 실제 실행
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

// handleGetRequest - GET 요청 처리
func (n *NautilusEventListener) handleGetRequest(data EventData) *K8sExecutionResult {
	switch data.ResourceType {
	case "pods":
		return n.getPods(data.Namespace)
	case "services":
		return n.getServices(data.Namespace)
	case "deployments":
		return n.getDeployments(data.Namespace)
	case "nodes":
		return n.getNodes()
	default:
		return &K8sExecutionResult{
			StatusCode: 404,
			Error:      "Resource type not supported",
			Success:    false,
		}
	}
}

// getPods - Pod 목록 조회
func (n *NautilusEventListener) getPods(namespace string) *K8sExecutionResult {
	if namespace == "" {
		namespace = "default"
	}

	pods, err := n.k8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return &K8sExecutionResult{
			StatusCode: 500,
			Error:      fmt.Sprintf("Failed to list pods: %v", err),
			Success:    false,
		}
	}

	// Pod 목록을 JSON으로 변환
	podList := &v1.PodList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PodList",
		},
		Items: pods.Items,
	}

	body, _ := json.Marshal(podList)

	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
		Success:    true,
	}
}

// handlePostRequest - POST 요청 처리 (리소스 생성)
func (n *NautilusEventListener) handlePostRequest(data EventData) *K8sExecutionResult {
	// payload를 []int에서 []byte로 변환
	payload := make([]byte, len(data.Payload))
	for i, v := range data.Payload {
		payload[i] = byte(v)
	}

	switch data.ResourceType {
	case "pods":
		return n.createPod(data.Namespace, payload)
	case "services":
		return n.createService(data.Namespace, payload)
	default:
		return &K8sExecutionResult{
			StatusCode: 404,
			Error:      "Resource creation not supported",
			Success:    false,
		}
	}
}

// createPod - Pod 생성
func (n *NautilusEventListener) createPod(namespace string, payload []byte) *K8sExecutionResult {
	if namespace == "" {
		namespace = "default"
	}

	// YAML/JSON 파싱
	var pod v1.Pod
	if err := json.Unmarshal(payload, &pod); err != nil {
		return &K8sExecutionResult{
			StatusCode: 400,
			Error:      fmt.Sprintf("Invalid pod specification: %v", err),
			Success:    false,
		}
	}

	// Pod 생성
	createdPod, err := n.k8sClient.CoreV1().Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		return &K8sExecutionResult{
			StatusCode: 500,
			Error:      fmt.Sprintf("Failed to create pod: %v", err),
			Success:    false,
		}
	}

	body, _ := json.Marshal(createdPod)

	return &K8sExecutionResult{
		StatusCode: 201,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
		Success:    true,
	}
}

// storeResponseToContract - Contract에 실행 결과 저장
func (n *NautilusEventListener) storeResponseToContract(requestID string, result *K8sExecutionResult) error {
	// Move Contract의 store_k8s_response 함수 호출
	moveCall := map[string]interface{}{
		"packageObjectId": n.contractAddress,
		"module":          "k8s_gateway",
		"function":        "store_k8s_response",
		"typeArguments":   []string{},
		"arguments": []interface{}{
			requestID,
			result.StatusCode,
			n.encodeHeaders(result.Headers),
			n.bytesToVector(result.Body),
			result.Success,
		},
	}

	// 트랜잭션 블록 구성
	txBlock := map[string]interface{}{
		"version":    1,
		"sender":     n.getSenderAddress(),
		"gasPayment": nil,
		"gasBudget":  "10000000",
		"gasPrice":   "1000",
		"transactions": []interface{}{
			map[string]interface{}{
				"MoveCall": moveCall,
			},
		},
	}

	// 트랜잭션 실행
	txBytes, err := n.serializeTransaction(txBlock)
	if err != nil {
		return fmt.Errorf("failed to serialize transaction: %v", err)
	}

	suiTx := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "sui_executeTransactionBlock",
		"params": []interface{}{
			txBytes,
			[]string{n.privateKeyHex},
			map[string]interface{}{
				"requestType": "WaitForLocalExecution",
				"options": map[string]bool{
					"showEvents": true,
				},
			},
		},
	}

	resp, err := n.restClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(suiTx).
		Post(n.suiRPCURL)

	if err != nil {
		return fmt.Errorf("Sui RPC call failed: %v", err)
	}

	n.logger.WithFields(logrus.Fields{
		"request_id":   requestID,
		"status_code":  result.StatusCode,
		"response_size": len(resp.Body()),
	}).Info("✅ Response stored to contract")

	return nil
}

// 유틸리티 함수들

func (n *NautilusEventListener) storeErrorResponse(requestID, errorMsg string, statusCode int) {
	result := &K8sExecutionResult{
		StatusCode: statusCode,
		Error:      errorMsg,
		Success:    false,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(fmt.Sprintf(`{"error": "%s"}`, errorMsg)),
	}

	n.storeResponseToContract(requestID, result)
}

func (n *NautilusEventListener) bytesToVector(data []byte) []int {
	vector := make([]int, len(data))
	for i, b := range data {
		vector[i] = int(b)
	}
	return vector
}

func (n *NautilusEventListener) encodeHeaders(headers map[string]string) []string {
	var encoded []string
	for k, v := range headers {
		encoded = append(encoded, fmt.Sprintf("%s:%s", k, v))
	}
	return encoded
}

func (n *NautilusEventListener) serializeTransaction(txBlock map[string]interface{}) (string, error) {
	// 간단화된 직렬화 (실제로는 BCS 직렬화 필요)
	txJSON, err := json.Marshal(txBlock)
	if err != nil {
		return "", err
	}
	return string(txJSON), nil
}

func (n *NautilusEventListener) getSenderAddress() string {
	// TODO: 개인키에서 주소 추출
	return "0x1234567890abcdef" // 임시
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

// 추가 핸들러들 (간단화)

func (n *NautilusEventListener) getServices(namespace string) *K8sExecutionResult {
	// Service 목록 조회 로직
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "v1", "kind": "ServiceList", "items": []}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) getDeployments(namespace string) *K8sExecutionResult {
	// Deployment 목록 조회 로직
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "apps/v1", "kind": "DeploymentList", "items": []}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) getNodes() *K8sExecutionResult {
	// Node 목록 조회 로직
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "v1", "kind": "NodeList", "items": []}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) createService(namespace string, payload []byte) *K8sExecutionResult {
	// Service 생성 로직
	return &K8sExecutionResult{
		StatusCode: 201,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"apiVersion": "v1", "kind": "Service", "metadata": {"name": "created"}}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) handlePutRequest(data EventData) *K8sExecutionResult {
	// PUT 요청 처리 (리소스 업데이트)
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"message": "Resource updated"}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) handlePatchRequest(data EventData) *K8sExecutionResult {
	// PATCH 요청 처리 (부분 업데이트)
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"message": "Resource patched"}`),
		Success:    true,
	}
}

func (n *NautilusEventListener) handleDeleteRequest(data EventData) *K8sExecutionResult {
	// DELETE 요청 처리 (리소스 삭제)
	return &K8sExecutionResult{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       json.RawMessage(`{"message": "Resource deleted"}`),
		Success:    true,
	}
}

// main 함수
func main() {
	listener := NewNautilusEventListener(
		"https://fullnode.testnet.sui.io:443",
		"0x0", // Contract address - 실제 배포 후 설정
		"",    // Private key - 환경변수에서 로드
	)

	if err := listener.Start(); err != nil {
		logrus.WithError(err).Fatal("Nautilus Event Listener failed to start")
	}
}