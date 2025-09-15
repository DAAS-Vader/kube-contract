package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SealAuthenticator handles Seal-based authentication
type SealAuthenticator struct {
	walletAddress string
	privateKey    []byte // Simulated for POC
}

// SealToken represents a Seal authentication token
type SealToken struct {
	WalletAddress string `json:"wallet_address"`
	Signature     string `json:"signature"`
	Challenge     string `json:"challenge"`
	Timestamp     int64  `json:"timestamp"`
}

// NewSealAuthenticator creates a new Seal authenticator
func NewSealAuthenticator(walletAddress string) *SealAuthenticator {
	// Generate mock private key for POC
	privateKey := make([]byte, 32)
	rand.Read(privateKey)

	return &SealAuthenticator{
		walletAddress: walletAddress,
		privateKey:    privateKey,
	}
}

// GenerateToken creates a Seal authentication token
func (auth *SealAuthenticator) GenerateToken(challenge string) (*SealToken, error) {
	timestamp := time.Now().Unix()

	// Create message to sign: challenge:timestamp:wallet_address
	message := fmt.Sprintf("%s:%d:%s", challenge, timestamp, auth.walletAddress)

	// Simulate signature generation (in production, use actual Sui signature)
	signature := auth.simulateSignature(message)

	return &SealToken{
		WalletAddress: auth.walletAddress,
		Signature:     signature,
		Challenge:     challenge,
		Timestamp:     timestamp,
	}, nil
}

// AddAuthHeaders adds Seal authentication headers to HTTP request
func (auth *SealAuthenticator) AddAuthHeaders(req *http.Request, challenge string) error {
	token, err := auth.GenerateToken(challenge)
	if err != nil {
		return err
	}

	req.Header.Set("X-Seal-Wallet", token.WalletAddress)
	req.Header.Set("X-Seal-Signature", token.Signature)
	req.Header.Set("X-Seal-Challenge", token.Challenge)
	req.Header.Set("X-Seal-Timestamp", strconv.FormatInt(token.Timestamp, 10))

	return nil
}

// ValidateToken verifies a Seal authentication token
func (auth *SealAuthenticator) ValidateToken(token *SealToken) error {
	// Check timestamp (allow 5 minute window)
	now := time.Now().Unix()
	if now-token.Timestamp > 300 || token.Timestamp > now {
		return fmt.Errorf("token timestamp invalid or expired")
	}

	// Recreate message
	message := fmt.Sprintf("%s:%d:%s", token.Challenge, token.Timestamp, token.WalletAddress)

	// Verify signature (simplified for POC)
	expectedSignature := auth.simulateSignature(message)
	if token.Signature != expectedSignature {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// ParseSealToken extracts Seal token from HTTP headers
func ParseSealToken(req *http.Request) (*SealToken, error) {
	walletAddress := req.Header.Get("X-Seal-Wallet")
	signature := req.Header.Get("X-Seal-Signature")
	challenge := req.Header.Get("X-Seal-Challenge")
	timestampStr := req.Header.Get("X-Seal-Timestamp")

	if walletAddress == "" || signature == "" || challenge == "" || timestampStr == "" {
		return nil, fmt.Errorf("missing Seal authentication headers")
	}

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format")
	}

	return &SealToken{
		WalletAddress: walletAddress,
		Signature:     signature,
		Challenge:     challenge,
		Timestamp:     timestamp,
	}, nil
}

// simulateSignature creates a mock signature for POC testing
func (auth *SealAuthenticator) simulateSignature(message string) string {
	// Combine private key and message for deterministic signature
	combined := append(auth.privateKey, []byte(message)...)
	hash := sha256.Sum256(combined)
	return hex.EncodeToString(hash[:])
}

// GenerateChallenge creates a random challenge string
func GenerateChallenge() string {
	randomBytes := make([]byte, 16)
	rand.Read(randomBytes)
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%d:%s", timestamp, hex.EncodeToString(randomBytes))
}

// ValidateSealTokenFromHeaders validates Seal authentication from HTTP headers
func ValidateSealTokenFromHeaders(req *http.Request) (*SealToken, error) {
	token, err := ParseSealToken(req)
	if err != nil {
		return nil, err
	}

	// Create authenticator for validation (in production, derive from wallet address)
	auth := NewSealAuthenticator(token.WalletAddress)

	if err := auth.ValidateToken(token); err != nil {
		return nil, err
	}

	return token, nil
}

// IsSealToken checks if a token string follows Seal format
func IsSealToken(token string) bool {
	return strings.HasPrefix(token, "SEAL")
}

// ParseSealTokenString parses a Seal token string format: SEAL<WALLET>::<SIGNATURE>::<CHALLENGE>
func ParseSealTokenString(token string) (*SealToken, error) {
	if !strings.HasPrefix(token, "SEAL") {
		return nil, fmt.Errorf("not a Seal token")
	}

	// Remove SEAL prefix
	token = strings.TrimPrefix(token, "SEAL")

	// Split by ::
	parts := strings.Split(token, "::")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid Seal token format")
	}

	return &SealToken{
		WalletAddress: parts[0],
		Signature:     parts[1],
		Challenge:     parts[2],
		Timestamp:     time.Now().Unix(),
	}, nil
}