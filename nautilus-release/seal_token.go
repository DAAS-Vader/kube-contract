package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
)

// SealTokenManager handles real Seal Token generation and validation
type SealTokenManager struct {
	logger *logrus.Logger
}

// NewSealTokenManager creates a new seal token manager
func NewSealTokenManager(logger *logrus.Logger) *SealTokenManager {
	return &SealTokenManager{
		logger: logger,
	}
}

// GenerateRealSealToken generates a real seal token based on hardware fingerprint
func (stm *SealTokenManager) GenerateRealSealToken(nodeID string, stakeAmount uint64, workerAddress string) (string, error) {
	// Get hardware fingerprint
	hwFingerprint, err := stm.getHardwareFingerprint()
	if err != nil {
		return "", fmt.Errorf("failed to get hardware fingerprint: %v", err)
	}

	// Get current timestamp
	timestamp := time.Now().Unix()

	// Create unique seed
	seed := fmt.Sprintf("%s:%s:%d:%s:%d",
		nodeID,
		hwFingerprint,
		stakeAmount,
		workerAddress,
		timestamp)

	// Generate cryptographic hash
	hasher := sha256.New()
	hasher.Write([]byte(seed))
	hash := hasher.Sum(nil)

	// Add some randomness
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %v", err)
	}

	// Combine hash and random bytes
	finalHasher := sha256.New()
	finalHasher.Write(hash)
	finalHasher.Write(randomBytes)
	finalHash := finalHasher.Sum(nil)

	// Create seal token (64 characters)
	sealToken := hex.EncodeToString(finalHash)

	stm.logger.Infof("🔐 Generated real seal token for worker %s: %s...", nodeID, sealToken[:16])

	return sealToken, nil
}

// ValidateSealToken validates a seal token (simplified version)
func (stm *SealTokenManager) ValidateSealToken(sealToken, nodeID string) bool {
	if len(sealToken) != 64 {
		stm.logger.Warnf("❌ Invalid seal token length for %s", nodeID)
		return false
	}

	// Check if it's a valid hex string
	if _, err := hex.DecodeString(sealToken); err != nil {
		stm.logger.Warnf("❌ Invalid seal token format for %s", nodeID)
		return false
	}

	stm.logger.Infof("✅ Seal token validated for worker %s", nodeID)
	return true
}

// getHardwareFingerprint generates a hardware-based fingerprint
func (stm *SealTokenManager) getHardwareFingerprint() (string, error) {
	// Collect system information
	info := struct {
		OS       string
		Arch     string
		NumCPU   int
		Hostname string
		PID      int
	}{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		NumCPU:   runtime.NumCPU(),
		Hostname: getHostname(),
		PID:      os.Getpid(),
	}

	// Create fingerprint
	fingerprint := fmt.Sprintf("%s-%s-%d-%s-%d",
		info.OS,
		info.Arch,
		info.NumCPU,
		info.Hostname,
		info.PID)

	// Hash the fingerprint
	hasher := sha256.New()
	hasher.Write([]byte(fingerprint))
	hash := hex.EncodeToString(hasher.Sum(nil))

	return hash[:32], nil // Return first 32 characters
}

// getHostname gets the system hostname with fallback
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		// Fallback to a random identifier
		randomBytes := make([]byte, 8)
		rand.Read(randomBytes)
		return hex.EncodeToString(randomBytes)
	}
	return hostname
}

// CreateWorkerCertificate creates a certificate for worker authentication
func (stm *SealTokenManager) CreateWorkerCertificate(nodeID, sealToken string) map[string]interface{} {
	timestamp := time.Now().Unix()

	cert := map[string]interface{}{
		"node_id":     nodeID,
		"seal_token":  sealToken,
		"issued_at":   timestamp,
		"valid_until": timestamp + (24 * 60 * 60), // Valid for 24 hours
		"issuer":      "nautilus-control",
		"version":     "1.0",
	}

	stm.logger.Infof("📜 Worker certificate created for %s", nodeID)
	return cert
}

// VerifyWorkerCertificate verifies a worker certificate
func (stm *SealTokenManager) VerifyWorkerCertificate(cert map[string]interface{}) bool {
	// Check required fields
	requiredFields := []string{"node_id", "seal_token", "issued_at", "valid_until"}
	for _, field := range requiredFields {
		if _, exists := cert[field]; !exists {
			stm.logger.Warnf("❌ Missing field in certificate: %s", field)
			return false
		}
	}

	// Check expiration
	validUntil, ok := cert["valid_until"].(int64)
	if !ok {
		stm.logger.Warnf("❌ Invalid valid_until field in certificate")
		return false
	}

	if time.Now().Unix() > validUntil {
		stm.logger.Warnf("❌ Certificate expired")
		return false
	}

	nodeID := cert["node_id"].(string)
	stm.logger.Infof("✅ Worker certificate verified for %s", nodeID)
	return true
}