// Package security provides kubectl authentication through Seal tokens
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// KubectlAuthHandler handles kubectl authentication through Seal tokens
type KubectlAuthHandler struct {
	suiClient     SuiClientInterface
	sealAuth      *SealAuthenticator
	minStake      uint64
	tokenCache    map[string]*AuthCache
}

// SuiClientInterface defines the interface for Sui blockchain interaction
type SuiClientInterface interface {
	ValidateStake(ctx context.Context, nodeID string, minStake uint64) (*StakeInfo, error)
	ValidateSealToken(token *SealToken, minStake uint64) error
}

// NewKubectlAuthHandler creates a new kubectl authentication handler
func NewKubectlAuthHandler(suiClient SuiClientInterface, minStake uint64) *KubectlAuthHandler {
	return &KubectlAuthHandler{
		suiClient:  suiClient,
		minStake:   minStake,
		tokenCache: make(map[string]*AuthCache),
	}
}

// AuthenticateKubectlRequest authenticates a kubectl request using Seal token
func (h *KubectlAuthHandler) AuthenticateKubectlRequest(req *http.Request) (*AuthResult, error) {
	// Extract Seal token from request headers or Bearer token
	sealToken, err := h.extractSealToken(req)
	if err != nil {
		return nil, fmt.Errorf("failed to extract Seal token: %v", err)
	}

	// Check cache first
	if cached := h.getCachedAuth(sealToken.WalletAddress); cached != nil {
		if time.Now().Before(cached.ValidUntil) {
			logrus.Debugf("Using cached auth for wallet: %s", sealToken.WalletAddress)
			return &AuthResult{
				Authenticated: true,
				Username:      cached.Username,
				Groups:        cached.Groups,
				WalletAddress: cached.WalletAddr,
			}, nil
		}
		// Remove expired cache
		delete(h.tokenCache, sealToken.WalletAddress)
	}

	// Validate Seal token
	sealAuth := NewSealAuthenticator(sealToken.WalletAddress)
	if err := sealAuth.ValidateToken(sealToken); err != nil {
		return nil, fmt.Errorf("Seal token validation failed: %v", err)
	}

	// Validate stake on Sui blockchain
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stakeInfo, err := h.suiClient.ValidateStake(ctx, sealToken.WalletAddress, h.minStake)
	if err != nil {
		return nil, fmt.Errorf("stake validation failed: %v", err)
	}

	if stakeInfo.Status != "active" {
		return nil, fmt.Errorf("user stake is not active: %s", stakeInfo.Status)
	}

	// Determine user groups based on stake amount
	groups := h.determineUserGroups(stakeInfo.StakeAmount)

	// Create authentication result
	result := &AuthResult{
		Authenticated: true,
		Username:      fmt.Sprintf("seal:%s", sealToken.WalletAddress),
		Groups:        groups,
		WalletAddress: sealToken.WalletAddress,
		StakeAmount:   stakeInfo.StakeAmount,
	}

	// Cache the result
	h.cacheAuth(sealToken.WalletAddress, &AuthCache{
		Username:    result.Username,
		Groups:      result.Groups,
		ValidUntil:  time.Now().Add(5 * time.Minute), // 5 minute cache
		WalletAddr:  sealToken.WalletAddress,
		StakeAmount: stakeInfo.StakeAmount,
	})

	logrus.Infof("kubectl authentication successful for wallet: %s (stake: %d)",
		sealToken.WalletAddress, stakeInfo.StakeAmount)

	return result, nil
}


// extractSealToken extracts Seal token from various sources in the request
func (h *KubectlAuthHandler) extractSealToken(req *http.Request) (*SealToken, error) {
	// Method 1: Check for Seal headers (direct Seal integration)
	if req.Header.Get("X-Seal-Wallet") != "" {
		return ParseSealToken(req)
	}

	// Method 2: Check Authorization Bearer token
	authHeader := req.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if IsSealToken(token) {
			return ParseSealTokenString(token)
		}
	}

	// Method 3: Check custom kubectl token header
	kubectlToken := req.Header.Get("X-Kubectl-Token")
	if kubectlToken != "" && IsSealToken(kubectlToken) {
		return ParseSealTokenString(kubectlToken)
	}

	return nil, fmt.Errorf("no valid Seal token found in request")
}

// determineUserGroups determines RBAC groups based on stake amount
func (h *KubectlAuthHandler) determineUserGroups(stakeAmount uint64) []string {
	groups := []string{"system:authenticated"}

	// Add groups based on stake tiers (MIST 단위, 1 SUI = 1,000,000,000 MIST)
	if stakeAmount >= 10000000000 { // 10 SUI
		groups = append(groups, "daas:admin", "daas:cluster-admin")
	} else if stakeAmount >= 5000000000 { // 5 SUI
		groups = append(groups, "daas:operator", "daas:namespace-admin")
	} else if stakeAmount >= 1000000000 { // 1 SUI (minimum)
		groups = append(groups, "daas:user", "daas:developer")
	}

	return groups
}

// getCachedAuth retrieves cached authentication result
func (h *KubectlAuthHandler) getCachedAuth(walletAddr string) *AuthCache {
	if cached, exists := h.tokenCache[walletAddr]; exists {
		return cached
	}
	return nil
}

// cacheAuth stores authentication result in cache
func (h *KubectlAuthHandler) cacheAuth(walletAddr string, cache *AuthCache) {
	h.tokenCache[walletAddr] = cache
}

// HandleKubectlAuth is an HTTP middleware for kubectl authentication
func (h *KubectlAuthHandler) HandleKubectlAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for certain paths
		if h.shouldSkipAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Authenticate the request
		authResult, err := h.AuthenticateKubectlRequest(r)
		if err != nil {
			logrus.Warnf("kubectl authentication failed: %v", err)
			h.writeAuthError(w, err)
			return
		}

		if !authResult.Authenticated {
			h.writeAuthError(w, fmt.Errorf("authentication failed"))
			return
		}

		// Add authentication info to request context/headers
		r.Header.Set("X-Remote-User", authResult.Username)
		r.Header.Set("X-Remote-Groups", strings.Join(authResult.Groups, ","))
		r.Header.Set("X-Wallet-Address", authResult.WalletAddress)

		logrus.Debugf("kubectl request authenticated: user=%s groups=%v",
			authResult.Username, authResult.Groups)

		next.ServeHTTP(w, r)
	})
}

// shouldSkipAuth determines if authentication should be skipped for certain paths
func (h *KubectlAuthHandler) shouldSkipAuth(path string) bool {
	skipPaths := []string{
		"/livez",
		"/readyz",
		"/healthz",
		"/version",
		"/openapi",
	}

	for _, skipPath := range skipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}

// writeAuthError writes authentication error response
func (h *KubectlAuthHandler) writeAuthError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	response := map[string]interface{}{
		"kind":    "Status",
		"status":  "Failure",
		"message": fmt.Sprintf("Unauthorized: %v", err),
		"reason":  "Unauthorized",
		"code":    401,
	}

	json.NewEncoder(w).Encode(response)
}

// GenerateKubectlConfig generates a kubeconfig with Seal token authentication
func GenerateKubectlConfig(serverURL, walletAddress, sealToken string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
    insecure-skip-tls-verify: true
  name: k3s-daas
contexts:
- context:
    cluster: k3s-daas
    user: %s
  name: k3s-daas
current-context: k3s-daas
users:
- name: %s
  user:
    token: %s
`, serverURL, walletAddress, walletAddress, sealToken)
}