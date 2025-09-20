// K3s-DaaS API Proxy - kubectl 요청을 Sui 블록체인으로 라우팅
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// API Proxy 서버 - kubectl 요청의 진입점
type APIProxy struct {
	suiRPCURL         string
	contractAddress   string
	nautilusEndpoint  string
	logger            *logrus.Logger
}

// Seal Token 구조체 (worker-release와 동일)
type SealToken struct {
	WalletAddress string `json:"wallet_address"`
	Signature     string `json:"signature"`
	Challenge     string `json:"challenge"`
	Timestamp     int64  `json:"timestamp"`
}

// kubectl API 요청 구조체
type KubectlRequest struct {
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Headers       map[string]string `json:"headers"`
	Body          []byte            `json:"body"`
	SealToken     *SealToken        `json:"seal_token"`
	UserAgent     string            `json:"user_agent"`
}

// Move Contract 호출 요청
type MoveContractCall struct {
	JSONRpc string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

func main() {
	proxy := &APIProxy{
		suiRPCURL:        "https://fullnode.testnet.sui.io:443",
		contractAddress:  "0x0", // Move Contract 배포 후 주소 설정
		nautilusEndpoint: "http://localhost:9443", // Nautilus TEE 엔드포인트
		logger:           logrus.New(),
	}

	proxy.logger.Info("🚀 K3s-DaaS API Proxy starting...")
	proxy.startServer()
}

// HTTP 서버 시작 (kubectl 요청 수신)
func (p *APIProxy) startServer() {
	// kubectl이 모든 K8s API 요청을 이곳으로 보냄
	http.HandleFunc("/", p.handleKubectlRequest)

	// 헬스체크
	http.HandleFunc("/healthz", p.handleHealth)
	http.HandleFunc("/readyz", p.handleReady)
	http.HandleFunc("/livez", p.handleLive)

	// kubectl 설정 정보 제공
	http.HandleFunc("/api/v1/k3s-daas/config", p.handleConfig)

	port := ":8080"
	p.logger.Infof("🎯 API Proxy listening on port %s", port)
	p.logger.Info("📝 kubectl 설정:")
	p.logger.Info("   kubectl config set-cluster k3s-daas --server=http://localhost:8080")
	p.logger.Info("   kubectl config set-credentials user --token=seal_YOUR_TOKEN")
	p.logger.Info("   kubectl config set-context k3s-daas --cluster=k3s-daas --user=user")
	p.logger.Info("   kubectl config use-context k3s-daas")

	if err := http.ListenAndServe(port, nil); err != nil {
		p.logger.Fatalf("❌ Failed to start server: %v", err)
	}
}

// kubectl 요청 처리 메인 핸들러
func (p *APIProxy) handleKubectlRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	p.logger.Infof("📨 kubectl request: %s %s", r.Method, r.URL.Path)

	// 1. Seal Token 추출 및 검증
	sealToken, err := p.extractSealToken(r)
	if err != nil {
		p.logger.Warnf("🔒 Invalid Seal Token: %v", err)
		http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
		return
	}

	// 2. 요청 본문 읽기
	body, err := io.ReadAll(r.Body)
	if err != nil {
		p.logger.Errorf("❌ Failed to read request body: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 3. kubectl 요청 구조화
	kubectlReq := &KubectlRequest{
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   p.extractHeaders(r),
		Body:      body,
		SealToken: sealToken,
		UserAgent: r.UserAgent(),
	}

	// 4. 처리 방식 선택
	if p.isDirectMode() {
		// 직접 모드: Nautilus TEE로 바로 전달
		p.handleDirectMode(w, kubectlReq)
	} else {
		// 블록체인 모드: Move Contract 경유
		p.handleBlockchainMode(w, kubectlReq)
	}

	p.logger.Infof("⏱️  Request processed in %v", time.Since(startTime))
}

// Seal Token 추출 (Authorization 헤더에서)
func (p *APIProxy) extractSealToken(r *http.Request) (*SealToken, error) {
	// Authorization: Bearer seal_wallet_signature_challenge_timestamp
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, fmt.Errorf("missing Bearer token")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if !strings.HasPrefix(tokenString, "seal_") {
		return nil, fmt.Errorf("invalid Seal token format")
	}

	// 간단한 파싱 (실제로는 더 정교한 파싱 필요)
	parts := strings.Split(tokenString[5:], "_") // "seal_" 제거
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid Seal token structure")
	}

	return &SealToken{
		WalletAddress: parts[0],
		Signature:     parts[1],
		Challenge:     parts[2],
		Timestamp:     time.Now().Unix(), // 임시
	}, nil
}

// 요청 헤더 추출
func (p *APIProxy) extractHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return headers
}

// 직접 모드 확인 (환경변수 또는 설정으로 제어)
func (p *APIProxy) isDirectMode() bool {
	// 현재는 직접 모드로 동작 (해커톤 시연용)
	return true
}

// 직접 모드: Nautilus TEE로 바로 전달
func (p *APIProxy) handleDirectMode(w http.ResponseWriter, req *KubectlRequest) {
	p.logger.Info("🔄 Direct mode: Forwarding to Nautilus TEE...")

	// Nautilus TEE로 HTTP 요청 전달
	nautilusURL, err := url.JoinPath(p.nautilusEndpoint, req.Path)
	if err != nil {
		p.logger.Errorf("❌ Invalid Nautilus URL: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// HTTP 클라이언트 요청 생성
	nautilusReq, err := http.NewRequest(req.Method, nautilusURL, bytes.NewReader(req.Body))
	if err != nil {
		p.logger.Errorf("❌ Failed to create Nautilus request: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 헤더 복사 (Seal Token 정보 포함)
	for key, value := range req.Headers {
		nautilusReq.Header.Set(key, value)
	}

	// Seal Token 정보를 커스텀 헤더로 전달
	nautilusReq.Header.Set("X-Seal-Wallet", req.SealToken.WalletAddress)
	nautilusReq.Header.Set("X-Seal-Signature", req.SealToken.Signature)
	nautilusReq.Header.Set("X-Seal-Challenge", req.SealToken.Challenge)

	// Nautilus TEE 호출
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(nautilusReq)
	if err != nil {
		p.logger.Errorf("❌ Nautilus TEE request failed: %v", err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	// 응답 헤더 복사
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// 상태 코드 설정
	w.WriteHeader(resp.StatusCode)

	// 응답 본문 복사
	if _, err := io.Copy(w, resp.Body); err != nil {
		p.logger.Errorf("❌ Failed to copy response: %v", err)
		return
	}

	p.logger.Infof("✅ Direct mode request completed: %d", resp.StatusCode)
}

// 블록체인 모드: Move Contract 경유 (미래 구현)
func (p *APIProxy) handleBlockchainMode(w http.ResponseWriter, req *KubectlRequest) {
	p.logger.Info("⛓️  Blockchain mode: Calling Move Contract...")

	// TODO: Move Contract 호출 구현
	// 1. Move Contract의 execute_kubectl_command 함수 호출
	// 2. 이벤트 발생 대기 또는 비동기 처리
	// 3. 결과 반환

	// 임시로 직접 모드로 폴백
	p.handleDirectMode(w, req)
}

// 헬스체크 핸들러들
func (p *APIProxy) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func (p *APIProxy) handleReady(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Ready")
}

func (p *APIProxy) handleLive(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Live")
}

// kubectl 설정 정보 제공
func (p *APIProxy) handleConfig(w http.ResponseWriter, r *http.Request) {
	config := map[string]interface{}{
		"cluster_endpoint": "http://localhost:8080",
		"seal_token_format": "seal_WALLET_SIGNATURE_CHALLENGE_TIMESTAMP",
		"example_commands": []string{
			"kubectl config set-cluster k3s-daas --server=http://localhost:8080",
			"kubectl config set-credentials user --token=seal_0x123_sig_challenge_123456",
			"kubectl config set-context k3s-daas --cluster=k3s-daas --user=user",
			"kubectl config use-context k3s-daas",
			"kubectl get pods",
		},
		"status": "ready",
		"version": "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}