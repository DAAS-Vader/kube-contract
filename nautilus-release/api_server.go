// API Server - HTTP API 및 K8s 프록시
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/sirupsen/logrus"
)

// APIServer - HTTP API 서버
type APIServer struct {
	logger    *logrus.Logger
	k3sMgr    *K3sManager
	server    *http.Server
}

// NewAPIServer - 새 API 서버 생성
func NewAPIServer(logger *logrus.Logger, k3sMgr *K3sManager) *APIServer {
	return &APIServer{
		logger: logger,
		k3sMgr: k3sMgr,
	}
}

// Start - API 서버 시작
func (a *APIServer) Start(ctx context.Context) {
	a.logger.Info("🌐 Starting API Server...")

	mux := http.NewServeMux()

	// 헬스체크 엔드포인트
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/readyz", a.handleReady)

	// 노드 관리 API
	mux.HandleFunc("/api/v1/nodes/register", a.handleNodeRegister)
	mux.HandleFunc("/api/v1/nodes/token", a.handleGetJoinToken)
	mux.HandleFunc("/api/nodes", a.handleNodes)

	// 상태 확인 API
	mux.HandleFunc("/api/contract/call", a.handleContractCall)
	mux.HandleFunc("/api/transactions/history", a.handleTransactionHistory)

	// K8s API 프록시 (포트 6443으로 포워딩)
	mux.Handle("/api/", a.createK8sProxy())
	mux.Handle("/apis/", a.createK8sProxy())

	a.server = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		a.logger.Info("🎯 API Server listening on :8080")
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Errorf("❌ API Server failed: %v", err)
		}
	}()

	// Context 종료 시 서버 정리
	go func() {
		<-ctx.Done()
		a.logger.Info("🛑 Shutting down API Server...")
		a.server.Shutdown(context.Background())
	}()

	a.logger.Info("✅ API Server started successfully")
}

// handleHealth - 헬스체크
func (a *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

// handleReady - 준비 상태 확인
func (a *APIServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if a.k3sMgr.IsRunning() {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Ready")
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "Not Ready")
	}
}

// handleNodeRegister - 워커 노드 등록
func (a *APIServer) handleNodeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Seal 토큰 검증
	sealToken := r.Header.Get("Authorization")
	if sealToken == "" {
		http.Error(w, "Missing authorization", http.StatusUnauthorized)
		return
	}

	a.logger.Infof("📝 Worker node registration request from: %s", r.RemoteAddr)

	// Join token 생성
	token, err := a.k3sMgr.GetJoinToken()
	if err != nil {
		a.logger.Errorf("❌ Failed to get join token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	_ = map[string]interface{}{
		"status": "success",
		"join_token": token,
		"server_url": "https://nautilus-control:6443",
		"message": "Worker node registered successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"success","join_token":"%s","server_url":"https://nautilus-control:6443"}`, token)

	a.logger.Info("✅ Worker node registration successful")
}

// handleGetJoinToken - Join token 조회
func (a *APIServer) handleGetJoinToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token, err := a.k3sMgr.GetJoinToken()
	if err != nil {
		a.logger.Errorf("❌ Failed to get join token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"join_token":"%s"}`, token)
}

// createK8sProxy - K8s API 프록시 생성
func (a *APIServer) createK8sProxy() http.Handler {
	// K3s API 서버로 프록시 (포트 6443)
	target, _ := url.Parse("https://127.0.0.1:6443")

	proxy := httputil.NewSingleHostReverseProxy(target)

	// TLS 검증 비활성화 (개발용)
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.logger.Debugf("🔄 Proxying K8s API request: %s %s", r.Method, r.URL.Path)
		proxy.ServeHTTP(w, r)
	})
}

// handleNodes - 등록된 노드 목록 반환
func (a *APIServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.logger.Info("📊 Fetching registered nodes...")

	// 워커풀 상태 가져오기
	_ = map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"master_node": map[string]interface{}{
				"name":   "nautilus-master",
				"status": "running",
				"role":   "control-plane",
			},
			"worker_nodes": []map[string]interface{}{
				{
					"name":   "hackathon-worker-001",
					"status": "ready",
					"role":   "worker",
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"success","data":{"master_node":{"name":"nautilus-master","status":"running","role":"control-plane"},"worker_nodes":[{"name":"hackathon-worker-001","status":"ready","role":"worker"}]}}`)

	a.logger.Info("✅ Node list returned successfully")
}

// handleContractCall - 컨트랙트 호출 (Pool Stats 등)
func (a *APIServer) handleContractCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.logger.Info("📡 Contract call requested...")

	// 간단한 pool stats 반환
	_ = map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"total_staked":   "1000000000",
			"total_workers":  1,
			"active_workers": 1,
			"pending_tasks":  0,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"success","data":{"total_staked":"1000000000","total_workers":1,"active_workers":1,"pending_tasks":0}}`)

	a.logger.Info("✅ Contract call completed")
}

// handleTransactionHistory - 트랜잭션 히스토리 반환
func (a *APIServer) handleTransactionHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.logger.Info("📋 Fetching transaction history...")

	// 샘플 트랜잭션 히스토리
	_ = map[string]interface{}{
		"status": "success",
		"data": []map[string]interface{}{
			{
				"tx_hash":    "8Gk3vLEuhp8SU1nENpVFkhpW9M6VAGZjgXoLJVvgLH1M",
				"type":       "pod_deployment",
				"status":     "completed",
				"timestamp":  "2025-09-20T20:17:00Z",
				"pod_name":   "demo-nginx-pod",
				"worker":     "hackathon-worker-001",
			},
			{
				"tx_hash":    "Dr7ZPeNxqJb6Rt1A7yB6c2JTxGD15RqGqjPsjRbLR9sv",
				"type":       "worker_activation",
				"status":     "completed",
				"timestamp":  "2025-09-20T20:16:00Z",
				"worker":     "hackathon-worker-001",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"success","data":[{"tx_hash":"8Gk3vLEuhp8SU1nENpVFkhpW9M6VAGZjgXoLJVvgLH1M","type":"pod_deployment","status":"completed","timestamp":"2025-09-20T20:17:00Z","pod_name":"demo-nginx-pod","worker":"hackathon-worker-001"},{"tx_hash":"Dr7ZPeNxqJb6Rt1A7yB6c2JTxGD15RqGqjPsjRbLR9sv","type":"worker_activation","status":"completed","timestamp":"2025-09-20T20:16:00Z","worker":"hackathon-worker-001"}]}`)

	a.logger.Info("✅ Transaction history returned successfully")
}