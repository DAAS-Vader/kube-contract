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