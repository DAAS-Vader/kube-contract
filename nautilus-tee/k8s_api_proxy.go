// Kubernetes API Proxy for kubectl compatibility
// This file handles kubectl requests and forwards them to the internal K3s API server

package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// kubectl 요청을 내부 K3s API 서버로 프록시
func (n *NautilusMaster) handleKubernetesAPIProxy(w http.ResponseWriter, r *http.Request) {
	n.logger.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
		"user":   r.Header.Get("User-Agent"),
	}).Info("Processing kubectl API request")

	// 1. Seal 토큰 인증 확인
	if !n.authenticateKubectlRequest(r) {
		n.logger.Warn("Unauthorized kubectl request")
		http.Error(w, "Unauthorized: Invalid or missing Seal token", http.StatusUnauthorized)
		return
	}

	// 2. 내부 K3s API 서버로 프록시
	n.proxyToK3sAPIServer(w, r)
}

// kubectl 요청 인증 (Seal 토큰 기반)
func (n *NautilusMaster) authenticateKubectlRequest(r *http.Request) bool {
	// Authorization 헤더에서 토큰 추출
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		// X-Seal-Token 헤더 확인 (대안)
		sealToken := r.Header.Get("X-Seal-Token")
		if sealToken == "" {
			n.logger.Debug("No authentication token found in request")
			return false
		}
		// Enhanced Seal Token 검증 사용
		if n.enhancedSealValidator != nil {
			return n.enhancedSealValidator.ValidateSealToken(sealToken)
		}

		// 기존 검증 fallback
		return n.sealTokenValidator.ValidateSealToken(sealToken)
	}

	// Bearer 토큰 형식 확인
	if !strings.HasPrefix(authHeader, "Bearer ") {
		n.logger.Debug("Invalid authorization header format")
		return false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Seal 토큰 검증
	// Enhanced Seal Token 검증 사용
	if n.enhancedSealValidator != nil {
		return n.enhancedSealValidator.ValidateSealToken(token)
	}

	// 기존 검증 fallback
	return n.sealTokenValidator.ValidateSealToken(token)
}

// 내부 K3s API 서버로 요청 프록시
func (n *NautilusMaster) proxyToK3sAPIServer(w http.ResponseWriter, r *http.Request) {
	// K3s API 서버 URL (TEE 내부)
	k3sAPIURL, err := url.Parse("https://localhost:6443")
	if err != nil {
		n.logger.Errorf("Failed to parse K3s API URL: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Reverse Proxy 생성
	proxy := httputil.NewSingleHostReverseProxy(k3sAPIURL)

	// 요청 헤더 수정
	r.Header.Set("X-Forwarded-For", r.RemoteAddr)
	r.Header.Set("X-Forwarded-Proto", "https")

	// TLS 설정 (K3s 내부 인증서 신뢰)
	proxy.Transport = &http.Transport{
		TLSClientConfig: n.getTLSConfig(),
	}

	// 에러 핸들링
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		n.logger.Errorf("K3s API proxy error: %v", err)
		http.Error(w, fmt.Sprintf("K3s API server error: %v", err), http.StatusBadGateway)
	}

	// 요청 로깅
	n.logger.WithFields(logrus.Fields{
		"target": k3sAPIURL.String() + r.URL.Path,
		"method": r.Method,
	}).Debug("Proxying to K3s API server")

	// 프록시 실행
	proxy.ServeHTTP(w, r)
}

// K3s API 서버 TLS 설정
func (n *NautilusMaster) getTLSConfig() *tls.Config {
	// 실제 구현에서는 K3s 인증서를 로드
	// 지금은 개발용으로 InsecureSkipVerify 사용
	return &tls.Config{
		InsecureSkipVerify: true,
	}
}

// kubectl 전용 헬스체크 엔드포인트
func (n *NautilusMaster) handleKubectlHealthCheck(w http.ResponseWriter, r *http.Request) {
	// K3s API 서버 상태 확인
	healthy := n.isK3sAPIServerHealthy()

	response := map[string]interface{}{
		"status": func() string {
			if healthy {
				return "healthy"
			}
			return "unhealthy"
		}(),
		"components": map[string]string{
			"tee":           "healthy",
			"k3s-api":      func() string {
				if healthy {
					return "healthy"
				}
				return "unhealthy"
			}(),
			"seal-auth":    "healthy",
			"blockchain":   "connected",
		},
		"kubectl_ready": healthy,
		"endpoints": []string{
			"/api/v1/pods",
			"/api/v1/services",
			"/api/v1/nodes",
			"/apis/apps/v1/deployments",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(response)
}

// K3s API 서버 헬스체크
func (n *NautilusMaster) isK3sAPIServerHealthy() bool {
	// localhost:6443/healthz 확인
	resp, err := http.Get("https://localhost:6443/healthz")
	if err != nil {
		n.logger.Debugf("K3s API server health check failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	return string(body) == "ok"
}

// kubectl config 생성 도우미
func (n *NautilusMaster) generateKubectlConfig(sealToken string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Config",
		"clusters": []map[string]interface{}{
			{
				"name": "k3s-daas-cluster",
				"cluster": map[string]interface{}{
					"server":                "https://localhost:8080",
					"insecure-skip-tls-verify": true,
				},
			},
		},
		"users": []map[string]interface{}{
			{
				"name": "k3s-daas-user",
				"user": map[string]interface{}{
					"token": sealToken,
				},
			},
		},
		"contexts": []map[string]interface{}{
			{
				"name": "k3s-daas-context",
				"context": map[string]interface{}{
					"cluster": "k3s-daas-cluster",
					"user":    "k3s-daas-user",
				},
			},
		},
		"current-context": "k3s-daas-context",
	}
}

// kubectl config 엔드포인트 (워커 노드용)
func (n *NautilusMaster) handleKubectlConfig(w http.ResponseWriter, r *http.Request) {
	// Seal 토큰 확인
	sealToken := r.Header.Get("X-Seal-Token")
	if sealToken == "" || !n.validateSealTokenForKubectl(sealToken) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// kubectl config 생성
	config := n.generateKubectlConfig(sealToken)

	w.Header().Set("Content-Type", "application/yaml")
	yaml.NewEncoder(w).Encode(config)
}

// kubectl용 Seal 토큰 검증 헬퍼
func (n *NautilusMaster) validateSealTokenForKubectl(token string) bool {
	// Enhanced Seal Token 검증 사용
	if n.enhancedSealValidator != nil {
		return n.enhancedSealValidator.ValidateSealToken(token)
	}

	// 기존 검증 fallback
	return n.sealTokenValidator.ValidateSealToken(token)
}