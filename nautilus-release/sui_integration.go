// Sui Integration - 실제 Sui Contract 이벤트 연동 및 K8s API 실행
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// SuiIntegration - 실제 Sui 블록체인 연동
type SuiIntegration struct {
	logger        *logrus.Logger
	k3sMgr        *K3sManager
	workerPool    *WorkerPool
	sealTokenMgr  *SealTokenManager
	suiRPCURL     string
	contractAddr  string
	privateKey    string
	wsConn        *websocket.Conn
	eventChan     chan *SuiContractEvent
	stopChan      chan bool
	registryAddr  string
	schedulerAddr string
}

// SuiContractEvent - Sui Contract에서 발생하는 이벤트
type SuiContractEvent struct {
	Type         string                 `json:"type"`
	PackageID    string                 `json:"packageId"`
	Module       string                 `json:"module"`
	Sender       string                 `json:"sender"`
	EventData    map[string]interface{} `json:"parsedJson"`
	TxDigest     string                 `json:"transactionDigest"`
	Timestamp    int64                  `json:"timestampMs"`
}

// K8sAPIRequest - K8s API 요청 (Contract에서 받음)
type K8sAPIRequest struct {
	RequestID    string `json:"request_id"`
	Method       string `json:"method"`        // GET, POST, PUT, DELETE, PATCH
	Resource     string `json:"resource"`      // pods, services, deployments, etc.
	Namespace    string `json:"namespace"`     // default, kube-system, etc.
	Name         string `json:"name"`          // 리소스 이름 (optional)
	Payload      string `json:"payload"`       // YAML/JSON 데이터 (POST/PUT용)
	SealToken    string `json:"seal_token"`    // TEE 인증 토큰
	Requester    string `json:"requester"`     // 요청자 주소
	Priority     int    `json:"priority"`      // 1-10 우선순위
	Timestamp    string `json:"timestamp"`
}

// WorkerNodeRequest - 워커 노드 관리 요청
type WorkerNodeRequest struct {
	Action       string `json:"action"`        // register, unregister, heartbeat
	NodeID       string `json:"node_id"`       // worker-node-001
	SealToken    string `json:"seal_token"`    // TEE 토큰
	StakeAmount  uint64 `json:"stake_amount"`  // 스테이킹 양
	WorkerAddr   string `json:"worker_address"` // 워커 노드 주소
	Timestamp    string `json:"timestamp"`
}

// K8sAPIResult - K8s API 실행 결과
type K8sAPIResult struct {
	RequestID    string `json:"request_id"`
	Success      bool   `json:"success"`
	Output       string `json:"output"`
	Error        string `json:"error"`
	ExecutionTime int64  `json:"execution_time_ms"`
	Timestamp    string `json:"timestamp"`
}

// NewSuiIntegration - 새 Sui Integration 생성
func NewSuiIntegration(logger *logrus.Logger, k3sMgr *K3sManager) *SuiIntegration {
	return &SuiIntegration{
		logger:        logger,
		k3sMgr:        k3sMgr,
		workerPool:    k3sMgr.workerPool,
		sealTokenMgr:  k3sMgr.sealTokenManager,
		suiRPCURL:     getEnvOrDefault("SUI_RPC_URL", "https://fullnode.testnet.sui.io"),
		contractAddr:  getEnvOrDefault("CONTRACT_PACKAGE_ID", "0x664356de3f1ce1df7d8039fb7f244dba3baec08025d791d15245876c76253bfc"),
		registryAddr:  getEnvOrDefault("WORKER_REGISTRY_ID", "0xca7ddf00a634c97b126aac539f0d5e8b8df20ad4e88b5f7b5f18291fbe6f0981"),
		schedulerAddr: getEnvOrDefault("K8S_SCHEDULER_ID", "0xf0f551c41b4056441a167a72ea14607f83aa6b73eb1383f69516ab0a893842a3"),
		privateKey:    getEnvOrDefault("PRIVATE_KEY", ""),
		eventChan:     make(chan *SuiContractEvent, 100),
		stopChan:      make(chan bool, 1),
	}
}

// Start - Sui Integration 시작
func (s *SuiIntegration) Start(ctx context.Context) {
	s.logger.Info("🌊 Starting Sui Integration...")

	if s.contractAddr == "" || s.privateKey == "" {
		s.logger.Warn("⚠️ Sui contract not configured, running in mock mode")
		s.startMockMode(ctx)
		return
	}

	s.logger.Info("🔗 Starting real Sui contract integration...")
	s.startRealMode(ctx)
}

// startRealMode - 실제 Contract 연동 모드
func (s *SuiIntegration) startRealMode(ctx context.Context) {
	// HTTP API 폴링으로 이벤트 수집
	go s.pollSuiEvents(ctx)

	// 이벤트 처리 고루틴 시작
	go s.processContractEvents(ctx)

	// 주기적 상태 체크
	go s.periodicHealthCheck(ctx)

	s.logger.Info("✅ Sui Integration started in real mode with HTTP polling")
}

// pollSuiEvents - HTTP API를 통한 이벤트 폴링
func (s *SuiIntegration) pollSuiEvents(ctx context.Context) {
	// HTTP RPC URL로 변경
	httpRPCURL := strings.Replace(s.suiRPCURL, "wss://", "https://", 1)
	httpRPCURL = strings.Replace(httpRPCURL, "/websocket", "", 1)

	s.logger.Infof("🔍 Starting event polling from: %s", httpRPCURL)

	lastCheckpoint := uint64(0)
	ticker := time.NewTicker(3 * time.Second) // 3초마다 폴링
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, newCheckpoint := s.fetchLatestEvents(httpRPCURL, lastCheckpoint)
			if len(events) > 0 {
				s.logger.Infof("📨 Found %d new events", len(events))
				for _, event := range events {
					select {
					case s.eventChan <- event:
					default:
						s.logger.Warn("⚠️ Event channel full, dropping event")
					}
				}
				lastCheckpoint = newCheckpoint
			}
		}
	}
}

// fetchLatestEvents - 최신 이벤트 가져오기
func (s *SuiIntegration) fetchLatestEvents(rpcURL string, fromCheckpoint uint64) ([]*SuiContractEvent, uint64) {
	// Sui queryEvents API 호출 - All 필터로 모든 이벤트 가져오기 (최근 이벤트부터)
	requestBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "suix_queryEvents",
		"params": []interface{}{
			map[string]interface{}{
				"All": []interface{}{},
			},
			nil,  // cursor
			50,   // limit
			true, // descending_order (최신 이벤트부터)
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		s.logger.Errorf("❌ Failed to marshal request: %v", err)
		return nil, fromCheckpoint
	}

	resp, err := http.Post(rpcURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		s.logger.Errorf("❌ Failed to query events: %v", err)
		return nil, fromCheckpoint
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Errorf("❌ Failed to read response: %v", err)
		return nil, fromCheckpoint
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		s.logger.Errorf("❌ Failed to parse response: %v", err)
		return nil, fromCheckpoint
	}

	// API 에러 확인
	if errorData, ok := result["error"]; ok {
		s.logger.Errorf("❌ Sui API error: %v", errorData)
		return nil, fromCheckpoint
	}

	// 이벤트 데이터 파싱
	events := []*SuiContractEvent{}
	maxCheckpoint := fromCheckpoint

	s.logger.Debugf("📡 API Response: %v", result)

	if data, ok := result["result"].(map[string]interface{}); ok {
		if eventList, ok := data["data"].([]interface{}); ok {
			s.logger.Infof("🔍 Found %d events in API response", len(eventList))
			for _, eventData := range eventList {
				if eventMap, ok := eventData.(map[string]interface{}); ok {
					event := s.parseEventFromAPI(eventMap)
					if event != nil {
						events = append(events, event)
						s.logger.Infof("✅ Parsed event: %s", event.Type)

						// 체크포인트 업데이트
						if timestampMs, ok := eventMap["timestampMs"].(string); ok {
							if ts, err := strconv.ParseUint(timestampMs, 10, 64); err == nil && ts > maxCheckpoint {
								maxCheckpoint = ts
							}
						}
					} else {
						s.logger.Warn("⚠️ Failed to parse event")
					}
				}
			}
		} else {
			s.logger.Warn("⚠️ No 'data' field in API response")
		}
	} else {
		s.logger.Warn("⚠️ No 'result' field in API response")
	}

	return events, maxCheckpoint
}

// parseEventFromAPI - API 응답에서 이벤트 파싱
func (s *SuiIntegration) parseEventFromAPI(eventMap map[string]interface{}) *SuiContractEvent {
	event := &SuiContractEvent{}

	// 기본 필드 파싱
	if eventType, ok := eventMap["type"].(string); ok {
		event.Type = eventType
	}

	if packageID, ok := eventMap["packageId"].(string); ok {
		event.PackageID = packageID
	}

	if sender, ok := eventMap["sender"].(string); ok {
		event.Sender = sender
	}

	if txDigest, ok := eventMap["transactionDigest"].(string); ok {
		event.TxDigest = txDigest
	}

	if timestampMs, ok := eventMap["timestampMs"].(string); ok {
		if ts, err := strconv.ParseInt(timestampMs, 10, 64); err == nil {
			event.Timestamp = ts
		}
	}

	// parsedJson 필드 파싱
	if parsedJson, ok := eventMap["parsedJson"].(map[string]interface{}); ok {
		event.EventData = parsedJson
	}

	// 우리가 관심 있는 이벤트인지 확인 - 새 contract 이벤트 타입
	if event.PackageID == s.contractAddr && (
		strings.Contains(event.Type, "WorkerRegisteredEvent") ||
		strings.Contains(event.Type, "K8sAPIRequestScheduledEvent") ||
		strings.Contains(event.Type, "WorkerStatusChangedEvent") ||
		strings.Contains(event.Type, "StakeDepositedEvent") ||
		strings.Contains(event.Type, "WorkerAssignedEvent") ||
		strings.Contains(event.Type, "K8sAPIResultEvent")) {
		return event
	}

	// Debug: 로그로 필터링된 이벤트 확인
	s.logger.Debugf("🔍 Filtered out event: %s (package: %s)", event.Type, event.PackageID)
	return nil
}

// connectToSuiWebSocket - Sui WebSocket 연결
func (s *SuiIntegration) connectToSuiWebSocket(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			s.logger.Info("🔌 Connecting to Sui WebSocket...")

			// WebSocket 연결 시도
			conn, _, err := websocket.DefaultDialer.Dial(s.suiRPCURL, nil)
			if err != nil {
				s.logger.Errorf("❌ Failed to connect to Sui WebSocket: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			s.wsConn = conn
			s.logger.Info("✅ Connected to Sui WebSocket")

			// Contract 이벤트 구독
			s.subscribeToContractEvents()

			// 메시지 수신 루프
			s.listenForEvents(ctx)

			// 연결이 끊어지면 재연결 시도
			conn.Close()
			s.wsConn = nil
			s.logger.Warn("🔄 WebSocket disconnected, reconnecting in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}
}

// subscribeToContractEvents - Contract 이벤트 구독
func (s *SuiIntegration) subscribeToContractEvents() {
	if s.wsConn == nil {
		return
	}

	// Sui WebSocket subscription 메시지
	subscribeMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "suix_subscribeEvent",
		"params": []interface{}{
			map[string]interface{}{
				"Package": s.contractAddr,
			},
		},
	}

	if err := s.wsConn.WriteJSON(subscribeMsg); err != nil {
		s.logger.Errorf("❌ Failed to subscribe to events: %v", err)
		return
	}

	s.logger.Info("📡 Subscribed to contract events")
}

// listenForEvents - 이벤트 수신 루프
func (s *SuiIntegration) listenForEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var message map[string]interface{}
			err := s.wsConn.ReadJSON(&message)
			if err != nil {
				s.logger.Errorf("❌ Error reading WebSocket message: %v", err)
				return
			}

			// 이벤트 메시지 처리
			s.handleWebSocketMessage(message)
		}
	}
}

// handleWebSocketMessage - WebSocket 메시지 처리
func (s *SuiIntegration) handleWebSocketMessage(message map[string]interface{}) {
	// "params" 필드에서 이벤트 데이터 추출
	if params, ok := message["params"].(map[string]interface{}); ok {
		if result, ok := params["result"].(map[string]interface{}); ok {
			event := &SuiContractEvent{}

			// JSON 데이터를 구조체로 변환
			if data, err := json.Marshal(result); err == nil {
				if err := json.Unmarshal(data, event); err == nil {
					// 이벤트 채널로 전송
					select {
					case s.eventChan <- event:
						s.logger.Debugf("📨 Received contract event: %s", event.Type)
					default:
						s.logger.Warn("⚠️ Event channel full, dropping event")
					}
				}
			}
		}
	}
}

// processContractEvents - Contract 이벤트 처리
func (s *SuiIntegration) processContractEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.eventChan:
			s.processEvent(event)
		}
	}
}

// processEvent - 개별 이벤트 처리
func (s *SuiIntegration) processEvent(event *SuiContractEvent) {
	s.logger.Infof("🔧 Processing event: %s from %s", event.Type, event.Sender)

	switch {
	case strings.Contains(event.Type, "WorkerRegisteredEvent"):
		s.handleWorkerRegisteredEvent(event)
	case strings.Contains(event.Type, "K8sAPIRequestScheduledEvent"):
		s.handleK8sAPIRequest(event)
	case strings.Contains(event.Type, "WorkerStatusChangedEvent"):
		s.handleWorkerStatusEvent(event)
	default:
		s.logger.Warnf("⚠️ Unknown event type: %s", event.Type)
	}
}

// handleWorkerRegisteredEvent - 워커 등록 이벤트 처리
func (s *SuiIntegration) handleWorkerRegisteredEvent(event *SuiContractEvent) {
	s.logger.Infof("👥 Processing worker registration event from contract")

	// 이벤트 데이터 파싱
	nodeID, ok := event.EventData["node_id"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse node_id from event")
		return
	}

	owner, ok := event.EventData["owner"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse owner from event")
		return
	}

	var stakeAmount uint64
	if stakeAmountStr, ok := event.EventData["stake_amount"].(string); ok {
		if parsed, err := strconv.ParseUint(stakeAmountStr, 10, 64); err == nil {
			stakeAmount = parsed
		} else {
			s.logger.Errorf("❌ Failed to parse stake_amount string: %v", err)
			return
		}
	} else if stakeAmountFloat, ok := event.EventData["stake_amount"].(float64); ok {
		stakeAmount = uint64(stakeAmountFloat)
	} else {
		s.logger.Errorf("❌ Failed to parse stake_amount from event")
		return
	}

	sealToken, ok := event.EventData["seal_token"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse seal_token from event")
		return
	}

	// 워커 노드 객체 생성
	worker := &WorkerNode{
		NodeID:        nodeID,
		SealToken:     sealToken,
		Status:        "pending",
		StakeAmount:   uint64(stakeAmount),
		WorkerAddress: owner,
	}

	// 워커 풀에 추가
	if err := s.workerPool.AddWorker(worker); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			s.logger.Warnf("⚠️ Worker %s already exists in pool", nodeID)
		} else {
			s.logger.Errorf("❌ Failed to add worker to pool: %v", err)
			return
		}
	} else {
		s.logger.Infof("👥 Worker %s added to pool successfully", nodeID)
	}

	// Join token 생성 및 설정
	if joinToken, err := s.k3sMgr.GetJoinToken(); err == nil {
		if err := s.workerPool.SetWorkerJoinToken(nodeID, joinToken); err == nil {
			s.logger.Infof("🎟️ Join token assigned to worker %s", nodeID)

			// 조인 토큰을 컨트랙트에 저장
			if err := s.setJoinTokenToContract(nodeID, joinToken); err != nil {
				s.logger.Errorf("❌ Failed to store join token in contract: %v", err)
			} else {
				s.logger.Infof("✅ Join token stored in contract for worker %s", nodeID)
			}
		}
	}

	// 워커를 자동으로 활성화 (실제 환경에서는 검증 후)
	if s.sealTokenMgr.ValidateSealToken(sealToken, nodeID) {
		s.workerPool.UpdateWorkerStatus(nodeID, "active")
		s.logger.Infof("✅ Worker %s activated and ready for scheduling", nodeID)
	} else {
		s.logger.Warnf("⚠️ Invalid seal token for worker %s", nodeID)
	}
}

// handleK8sAPIRequest - K8s API 요청 스케줄링 이벤트 처리
func (s *SuiIntegration) handleK8sAPIRequest(event *SuiContractEvent) {
	s.logger.Infof("📝 Processing K8s API request scheduling event")

	// 이벤트 데이터 파싱
	requestID, ok := event.EventData["request_id"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse request_id from event")
		return
	}

	method, ok := event.EventData["method"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse method from event")
		return
	}

	resource, ok := event.EventData["resource"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse resource from event")
		return
	}

	namespace, ok := event.EventData["namespace"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse namespace from event")
		return
	}

	name := ""
	if nameVal, exists := event.EventData["name"]; exists {
		name, _ = nameVal.(string)
	}

	payload := ""
	if payloadVal, exists := event.EventData["payload"]; exists {
		payload, _ = payloadVal.(string)
	}

	assignedWorker, ok := event.EventData["assigned_worker"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse assigned_worker from event")
		return
	}

	// K8s API 요청 객체 생성
	request := &K8sAPIRequest{
		RequestID:    requestID,
		Method:       method,
		Resource:     resource,
		Namespace:    namespace,
		Name:         name,
		Payload:      payload,
		Timestamp:    fmt.Sprintf("%d", event.Timestamp),
	}

	s.logger.Infof("🎯 Executing K8s API: %s %s in namespace %s (assigned to %s)",
		request.Method, request.Resource, request.Namespace, assignedWorker)

	// K3s가 실행 중인지 확인
	if !s.isK3sActuallyRunning() {
		s.logger.Warn("⚠️ K3s is not ready, queuing request")
		// TODO: 요청을 큐에 저장하고 나중에 처리
		return
	}

	// 실제 K8s API 실행
	result := s.executeK8sAPI(request)

	// 결과를 Contract에 저장
	s.storeResultToContract(result)

	// 워커 상태 업데이트
	if result.Success {
		s.workerPool.UpdateWorkerStatus(assignedWorker, "active")
	} else {
		s.logger.Warnf("⚠️ Request %s failed on worker %s", requestID, assignedWorker)
	}
}

// handleWorkerStatusEvent - 워커 상태 변경 이벤트 처리
func (s *SuiIntegration) handleWorkerStatusEvent(event *SuiContractEvent) {
	s.logger.Infof("🔄 Processing worker status change event")

	// 이벤트 데이터 파싱
	nodeID, ok := event.EventData["node_id"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse node_id from event")
		return
	}

	newStatus, ok := event.EventData["new_status"].(string)
	if !ok {
		s.logger.Errorf("❌ Failed to parse new_status from event")
		return
	}

	oldStatus := ""
	if oldStatusVal, exists := event.EventData["old_status"]; exists {
		oldStatus, _ = oldStatusVal.(string)
	}

	// 로컬 워커 풀 상태 업데이트
	if err := s.workerPool.UpdateWorkerStatus(nodeID, newStatus); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.logger.Warnf("⚠️ Worker %s not found in local pool, may need to sync from contract", nodeID)
			// TODO: 워커가 로컬에 없으면 contract에서 워커 정보를 가져와서 추가
		} else {
			s.logger.Errorf("❌ Failed to update worker status: %v", err)
		}
		return
	}

	s.logger.Infof("✅ Worker %s status updated: %s → %s", nodeID, oldStatus, newStatus)

	// 상태에 따른 추가 작업
	switch newStatus {
	case "active":
		s.logger.Infof("🟢 Worker %s is now available for scheduling", nodeID)
	case "offline":
		s.logger.Warnf("🔴 Worker %s went offline, removing from active pool", nodeID)
	case "busy":
		s.logger.Infof("🟡 Worker %s is busy processing request", nodeID)
	}
}

// executeK8sAPI - 실제 K8s API 실행
func (s *SuiIntegration) executeK8sAPI(request *K8sAPIRequest) *K8sAPIResult {
	startTime := time.Now()

	result := &K8sAPIResult{
		RequestID: request.RequestID,
		Timestamp: fmt.Sprintf("%d", time.Now().Unix()),
	}

	// kubectl 명령 구성
	args := s.buildKubectlCommand(request)
	if args == nil {
		result.Success = false
		result.Error = "Invalid kubectl command"
		return result
	}

	// kubectl 실행
	cmd := exec.Command("kubectl", args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG=/etc/rancher/k3s/k3s.yaml")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result.ExecutionTime = time.Since(startTime).Milliseconds()
	result.Output = stdout.String()

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("Command failed: %v, stderr: %s", err, stderr.String())
		s.logger.Errorf("❌ kubectl command failed: %v", err)
	} else {
		result.Success = true
		s.logger.Infof("✅ kubectl command succeeded in %dms", result.ExecutionTime)
	}

	return result
}

// buildKubectlCommand - kubectl 명령 구성
func (s *SuiIntegration) buildKubectlCommand(request *K8sAPIRequest) []string {
	var args []string

	switch strings.ToUpper(request.Method) {
	case "GET":
		args = []string{"get", request.Resource}
		if request.Name != "" {
			args = append(args, request.Name)
		}
		args = append(args, "-o", "json")

	case "POST":
		if request.Payload == "" {
			return nil
		}
		// POST는 kubectl apply 또는 create 사용
		args = []string{"apply", "-f", "-"}

	case "PUT":
		if request.Payload == "" {
			return nil
		}
		args = []string{"apply", "-f", "-"}

	case "DELETE":
		args = []string{"delete", request.Resource}
		if request.Name != "" {
			args = append(args, request.Name)
		}

	case "PATCH":
		if request.Name == "" || request.Payload == "" {
			return nil
		}
		args = []string{"patch", request.Resource, request.Name, "--patch", request.Payload}

	default:
		return nil
	}

	// 네임스페이스 추가
	if request.Namespace != "" {
		args = append(args, "-n", request.Namespace)
	}

	return args
}


// storeResultToContract - 결과를 Sui Contract에 저장
func (s *SuiIntegration) storeResultToContract(result *K8sAPIResult) {
	if s.contractAddr == "" {
		s.logger.Debugf("📝 Mock result storage: %s -> Success: %v",
			result.RequestID, result.Success)
		return
	}

	// TODO: 실제 Sui Contract 호출로 결과 저장
	// Move 함수 호출: store_k8s_result(request_id, success, output, error)

	s.logger.Infof("💾 Storing result to contract: %s (Success: %v)",
		result.RequestID, result.Success)
}

// isK3sActuallyRunning - K3s가 실제로 실행 중인지 확인
func (s *SuiIntegration) isK3sActuallyRunning() bool {
	// kubeconfig 파일 존재 확인
	if _, err := os.Stat("/etc/rancher/k3s/k3s.yaml"); err != nil {
		return false
	}

	// kubectl 명령으로 API 서버 상태 확인
	cmd := exec.Command("kubectl", "get", "nodes", "--kubeconfig", "/etc/rancher/k3s/k3s.yaml")
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// periodicHealthCheck - 주기적 상태 체크
func (s *SuiIntegration) periodicHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.isK3sActuallyRunning() {
				s.logger.Debug("💚 K3s health check passed")
			} else {
				s.logger.Warn("💛 K3s health check failed")
			}
		}
	}
}

// startMockMode - Mock 모드로 실행 (Contract 없이 테스트)
func (s *SuiIntegration) startMockMode(ctx context.Context) {
	s.logger.Info("🧪 Sui Integration running in mock mode")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("🛑 Sui Integration stopping...")
			return
		case <-ticker.C:
			s.processMockEvent()
		}
	}
}

// processMockEvent - Mock 이벤트 처리
func (s *SuiIntegration) processMockEvent() {
	s.logger.Info("🔄 Processing mock Sui event...")

	// Mock K8s API 요청 생성
	mockRequest := &K8sAPIRequest{
		RequestID:    fmt.Sprintf("mock_%d", time.Now().Unix()),
		Method:       "GET",
		Resource:     "pods",
		Namespace:    "default",
		SealToken:    "mock_seal_token",
		Requester:    "mock_user",
		Priority:     1,
		Timestamp:    fmt.Sprintf("%d", time.Now().Unix()),
	}

	s.logger.Infof("🔧 Processing K8s API request: %s %s", mockRequest.Method, mockRequest.Resource)

	if !s.isK3sActuallyRunning() {
		s.logger.Warn("⚠️ K3s is not running, skipping request")
		return
	}

	// 실제 kubectl 명령 실행
	result := s.executeK8sAPI(mockRequest)
	s.logger.Infof("✅ Mock K8s API request completed: Success=%v", result.Success)

	if result.Success {
		s.logger.Debugf("📊 Output: %s", result.Output)
	} else {
		s.logger.Errorf("❌ Error: %s", result.Error)
	}
}

// setJoinTokenToContract - 조인 토큰을 컨트랙트에 저장
func (s *SuiIntegration) setJoinTokenToContract(nodeID, joinToken string) error {
	// SUI 클라이언트 명령어 구성
	cmd := exec.Command("sui", "client", "call",
		"--package", s.contractPackageID,
		"--module", "worker_registry",
		"--function", "set_join_token",
		"--args", s.workerRegistryID, nodeID, joinToken,
		"--gas-budget", "10000000",
	)

	s.logger.Debugf("🔗 Executing SUI command: %s", strings.Join(cmd.Args, " "))

	// 명령 실행
	output, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Errorf("❌ Failed to execute SUI command: %v", err)
		s.logger.Errorf("❌ Command output: %s", string(output))
		return fmt.Errorf("failed to set join token in contract: %v", err)
	}

	s.logger.Debugf("✅ SUI command output: %s", string(output))
	return nil
}

// getEnvOrDefault - 환경변수 또는 기본값 반환
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}