package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSuiClientValidateStake(t *testing.T) {
	client := NewSuiClient("https://fullnode.testnet.sui.io:443")
	client.SetMockMode(true) // 테스트에서는 mock 모드 사용

	tests := []struct {
		name          string
		walletAddress string
		minStake      uint64
		expectError   bool
	}{
		{
			name:          "sufficient stake",
			walletAddress: "0x123456789abcdef",
			minStake:      500000000, // 0.5 SUI
			expectError:   false,
		},
		{
			name:          "insufficient stake",
			walletAddress: "test_insufficient",
			minStake:      2000000000, // 2 SUI
			expectError:   true,
		},
		{
			name:          "high stake user",
			walletAddress: "test_high_stake",
			minStake:      1000000000, // 1 SUI
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			stakeInfo, err := client.ValidateStake(ctx, tt.walletAddress, tt.minStake)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if stakeInfo == nil {
				t.Errorf("expected stake info but got nil")
				return
			}

			if stakeInfo.WalletAddress != tt.walletAddress {
				t.Errorf("expected wallet %s, got %s", tt.walletAddress, stakeInfo.WalletAddress)
			}

			if stakeInfo.Status != "active" {
				t.Errorf("expected status 'active', got %s", stakeInfo.Status)
			}
		})
	}
}

func TestKubectlAuthHandler(t *testing.T) {
	// Mock SuiClient 설정
	client := NewSuiClient("https://fullnode.testnet.sui.io:443")
	client.SetMockMode(true)

	// AuthHandler 생성
	authHandler := NewKubectlAuthHandler(client, 1000000000) // 1 SUI minimum

	// 테스트용 HTTP 핸들러
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	// 미들웨어 적용
	handler := authHandler.HandleKubectlAuth(nextHandler)

	tests := []struct {
		name           string
		headers        map[string]string
		expectedStatus int
	}{
		{
			name: "valid seal token",
			headers: map[string]string{
				"X-Seal-Wallet":    "0x123456789abcdef",
				"X-Seal-Signature": "valid_signature",
				"X-Seal-Challenge": "test_challenge",
				"X-Seal-Timestamp": "1640995200", // 2022-01-01
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "health check path (skip auth)",
			headers: map[string]string{
				// 헤더 없음
			},
			expectedStatus: http.StatusOK, // /healthz는 인증 생략
		},
		{
			name: "missing seal token",
			headers: map[string]string{
				"Authorization": "Bearer invalid_token",
			},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 요청 생성
			path := "/api/v1/nodes"
			if tt.name == "health check path (skip auth)" {
				path = "/healthz"
			}

			req := httptest.NewRequest("GET", path, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			// 응답 기록
			w := httptest.NewRecorder()

			// 핸들러 실행
			handler.ServeHTTP(w, req)

			// 결과 검증
			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestValidateSealToken(t *testing.T) {
	client := NewSuiClient("https://fullnode.testnet.sui.io:443")
	client.SetMockMode(true)

	validToken := &SealToken{
		WalletAddress: "0x123456789abcdef",
		Signature:     "valid_signature",
		Challenge:     "test_challenge",
		Timestamp:     time.Now().Unix(),
	}

	invalidToken := &SealToken{
		WalletAddress: "test_insufficient",
		Signature:     "invalid_signature",
		Challenge:     "test_challenge",
		Timestamp:     time.Now().Unix(),
	}

	tests := []struct {
		name        string
		token       *SealToken
		minStake    uint64
		expectError bool
	}{
		{
			name:        "valid token with sufficient stake",
			token:       validToken,
			minStake:    500000000, // 0.5 SUI
			expectError: false,
		},
		{
			name:        "valid token but insufficient stake",
			token:       invalidToken,
			minStake:    2000000000, // 2 SUI
			expectError: true,
		},
		{
			name:        "nil token",
			token:       nil,
			minStake:    1000000000,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ValidateSealToken(tt.token, tt.minStake)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDetermineUserGroups(t *testing.T) {
	client := NewSuiClient("https://fullnode.testnet.sui.io:443")
	authHandler := NewKubectlAuthHandler(client, 1000000000)

	tests := []struct {
		name        string
		stakeAmount uint64
		expected    []string
	}{
		{
			name:        "admin level stake",
			stakeAmount: 10000000000, // 10 SUI
			expected:    []string{"system:authenticated", "daas:admin", "daas:cluster-admin"},
		},
		{
			name:        "operator level stake",
			stakeAmount: 5000000000, // 5 SUI
			expected:    []string{"system:authenticated", "daas:operator", "daas:namespace-admin"},
		},
		{
			name:        "user level stake",
			stakeAmount: 1000000000, // 1 SUI
			expected:    []string{"system:authenticated", "daas:user", "daas:developer"},
		},
		{
			name:        "insufficient stake",
			stakeAmount: 500000000, // 0.5 SUI
			expected:    []string{"system:authenticated"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := authHandler.determineUserGroups(tt.stakeAmount)

			if len(groups) != len(tt.expected) {
				t.Errorf("expected %d groups, got %d", len(tt.expected), len(groups))
				return
			}

			for i, expected := range tt.expected {
				if groups[i] != expected {
					t.Errorf("expected group %s at index %d, got %s", expected, i, groups[i])
				}
			}
		})
	}
}