// Package sui provides the comprehensive Sui blockchain client for K3s-DaaS
package sui

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k3s-io/k3s/pkg/security"
)

// SuiClient provides comprehensive Sui blockchain interaction for K3s-DaaS
type SuiClient struct {
	endpoint       string
	httpClient     *http.Client
	privateKey     ed25519.PrivateKey
	publicKey      ed25519.PublicKey
	address        string
	contractPackage string
	metrics        *SuiMetrics
	cache          *SuiCache
	mu             sync.RWMutex
}

// SuiMetrics tracks blockchain interaction performance
type SuiMetrics struct {
	RequestCount       int64
	SuccessCount       int64
	ErrorCount         int64
	AvgResponseTime    time.Duration
	LastRequestTime    time.Time
	TransactionsFailed int64
	TransactionsSuccess int64
	mu                 sync.RWMutex
}

// SuiCache provides caching for frequently accessed data
type SuiCache struct {
	stakes     map[string]*StakeInfo
	objects    map[string]*SuiObject
	lastUpdate time.Time
	ttl        time.Duration
	mu         sync.RWMutex
}

// StakeInfo represents staking information for a node
type StakeInfo struct {
	NodeID        string    `json:"node_id"`
	StakeAmount   uint64    `json:"stake_amount"`
	ValidatorAddr string    `json:"validator_addr"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	LastUpdate    time.Time `json:"last_update"`
}

// NodeRegistration represents node registration data
type NodeRegistration struct {
	NodeID       string            `json:"node_id"`
	PublicKey    string            `json:"public_key"`
	Capabilities []string          `json:"capabilities"`
	Metadata     map[string]string `json:"metadata"`
	StakeProof   string            `json:"stake_proof"`
}

// SuiObject represents a generic Sui object
type SuiObject struct {
	ObjectID string                 `json:"objectId"`
	Version  string                 `json:"version"`
	Digest   string                 `json:"digest"`
	Type     string                 `json:"type"`
	Content  map[string]interface{} `json:"content"`
}

// TransactionResponse represents Sui transaction response
type TransactionResponse struct {
	Digest  string                 `json:"digest"`
	Effects map[string]interface{} `json:"effects"`
	Events  []interface{}          `json:"events"`
	Status  string                 `json:"status"`
}

// NewSuiClient creates a new Sui blockchain client
func NewSuiClient(endpoint, privateKeyHex, contractPackage string) (*SuiClient, error) {
	// Decode private key
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %v", err)
	}

	if len(privateKeyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: expected %d, got %d",
			ed25519.PrivateKeySize, len(privateKeyBytes))
	}

	privateKey := ed25519.PrivateKey(privateKeyBytes)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	// Generate Sui address from public key
	address := generateSuiAddress(publicKey)

	client := &SuiClient{
		endpoint:        endpoint,
		privateKey:      privateKey,
		publicKey:       publicKey,
		address:         address,
		contractPackage: contractPackage,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		metrics: &SuiMetrics{},
		cache: &SuiCache{
			stakes:  make(map[string]*StakeInfo),
			objects: make(map[string]*SuiObject),
			ttl:     5 * time.Minute,
		},
	}

	return client, nil
}

// ValidateStake validates if a node has sufficient stake for participation
func (c *SuiClient) ValidateStake(ctx context.Context, nodeID string, minStake uint64) (*StakeInfo, error) {
	c.updateMetrics(func() { c.metrics.RequestCount++ })

	// Check cache first
	if stakeInfo := c.getCachedStake(nodeID); stakeInfo != nil {
		if time.Since(stakeInfo.LastUpdate) < c.cache.ttl {
			if stakeInfo.StakeAmount >= minStake && stakeInfo.Status == "active" {
				c.updateMetrics(func() { c.metrics.SuccessCount++ })
				return stakeInfo, nil
			}
			return nil, fmt.Errorf("insufficient stake: has %d, requires %d",
				stakeInfo.StakeAmount, minStake)
		}
	}

	// Query blockchain for current stake
	stakeInfo, err := c.queryStakeInfo(ctx, nodeID)
	if err != nil {
		c.updateMetrics(func() { c.metrics.ErrorCount++ })
		return nil, fmt.Errorf("failed to query stake: %v", err)
	}

	// Cache the result
	c.setCachedStake(nodeID, stakeInfo)

	if stakeInfo.StakeAmount >= minStake && stakeInfo.Status == "active" {
		c.updateMetrics(func() { c.metrics.SuccessCount++ })
		return stakeInfo, nil
	}

	return nil, fmt.Errorf("insufficient stake: has %d, requires %d",
		stakeInfo.StakeAmount, minStake)
}

// RegisterNode registers a new node in the DaaS system
func (c *SuiClient) RegisterNode(ctx context.Context, registration *NodeRegistration) (*TransactionResponse, error) {
	c.updateMetrics(func() { c.metrics.RequestCount++ })

	// Prepare transaction data
	moveCall := map[string]interface{}{
		"package":  c.contractPackage,
		"module":   "daas_registry",
		"function": "register_node",
		"arguments": []interface{}{
			registration.NodeID,
			registration.PublicKey,
			registration.Capabilities,
			registration.Metadata,
			registration.StakeProof,
		},
	}

	// Execute transaction
	resp, err := c.executeTransaction(ctx, moveCall)
	if err != nil {
		c.updateMetrics(func() {
			c.metrics.ErrorCount++
			c.metrics.TransactionsFailed++
		})
		return nil, fmt.Errorf("failed to register node: %v", err)
	}

	c.updateMetrics(func() {
		c.metrics.SuccessCount++
		c.metrics.TransactionsSuccess++
	})

	return resp, nil
}

// UpdateNodeStatus updates the status of a registered node
func (c *SuiClient) UpdateNodeStatus(ctx context.Context, nodeID, status string) error {
	c.updateMetrics(func() { c.metrics.RequestCount++ })

	moveCall := map[string]interface{}{
		"package":  c.contractPackage,
		"module":   "daas_registry",
		"function": "update_node_status",
		"arguments": []interface{}{
			nodeID,
			status,
		},
	}

	_, err := c.executeTransaction(ctx, moveCall)
	if err != nil {
		c.updateMetrics(func() {
			c.metrics.ErrorCount++
			c.metrics.TransactionsFailed++
		})
		return fmt.Errorf("failed to update node status: %v", err)
	}

	c.updateMetrics(func() {
		c.metrics.SuccessCount++
		c.metrics.TransactionsSuccess++
	})

	return nil
}

// GetRegisteredNodes retrieves all registered nodes in the system
func (c *SuiClient) GetRegisteredNodes(ctx context.Context) ([]NodeRegistration, error) {
	c.updateMetrics(func() { c.metrics.RequestCount++ })

	// Query objects from the registry
	objects, err := c.queryObjects(ctx, map[string]interface{}{
		"StructType": fmt.Sprintf("%s::daas_registry::NodeRegistration", c.contractPackage),
	})
	if err != nil {
		c.updateMetrics(func() { c.metrics.ErrorCount++ })
		return nil, fmt.Errorf("failed to query registered nodes: %v", err)
	}

	var nodes []NodeRegistration
	for _, obj := range objects {
		var node NodeRegistration
		if err := mapToStruct(obj.Content, &node); err != nil {
			continue // Skip invalid entries
		}
		nodes = append(nodes, node)
	}

	c.updateMetrics(func() { c.metrics.SuccessCount++ })
	return nodes, nil
}

// StoreK8sMetadata stores Kubernetes metadata on the blockchain
func (c *SuiClient) StoreK8sMetadata(ctx context.Context, objectType, objectName string, metadata map[string]interface{}) (*TransactionResponse, error) {
	c.updateMetrics(func() { c.metrics.RequestCount++ })

	// Serialize metadata to JSON
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize metadata: %v", err)
	}

	moveCall := map[string]interface{}{
		"package":  c.contractPackage,
		"module":   "k8s_metadata",
		"function": "store_metadata",
		"arguments": []interface{}{
			objectType,
			objectName,
			string(metadataBytes),
		},
	}

	resp, err := c.executeTransaction(ctx, moveCall)
	if err != nil {
		c.updateMetrics(func() {
			c.metrics.ErrorCount++
			c.metrics.TransactionsFailed++
		})
		return nil, fmt.Errorf("failed to store metadata: %v", err)
	}

	c.updateMetrics(func() {
		c.metrics.SuccessCount++
		c.metrics.TransactionsSuccess++
	})

	return resp, nil
}

// GetK8sMetadata retrieves Kubernetes metadata from the blockchain
func (c *SuiClient) GetK8sMetadata(ctx context.Context, objectType, objectName string) (map[string]interface{}, error) {
	c.updateMetrics(func() { c.metrics.RequestCount++ })

	// Query specific metadata object
	objects, err := c.queryObjects(ctx, map[string]interface{}{
		"StructType": fmt.Sprintf("%s::k8s_metadata::Metadata", c.contractPackage),
		"filter": map[string]interface{}{
			"object_type": objectType,
			"object_name": objectName,
		},
	})
	if err != nil {
		c.updateMetrics(func() { c.metrics.ErrorCount++ })
		return nil, fmt.Errorf("failed to query metadata: %v", err)
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("metadata not found for %s/%s", objectType, objectName)
	}

	// Parse metadata from the first matching object
	metadataStr, ok := objects[0].Content["metadata"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata format")
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %v", err)
	}

	c.updateMetrics(func() { c.metrics.SuccessCount++ })
	return metadata, nil
}

// GetMetrics returns current client metrics
func (c *SuiClient) GetMetrics() *SuiMetrics {
	c.metrics.mu.RLock()
	defer c.metrics.mu.RUnlock()

	return &SuiMetrics{
		RequestCount:        c.metrics.RequestCount,
		SuccessCount:        c.metrics.SuccessCount,
		ErrorCount:          c.metrics.ErrorCount,
		AvgResponseTime:     c.metrics.AvgResponseTime,
		LastRequestTime:     c.metrics.LastRequestTime,
		TransactionsFailed:  c.metrics.TransactionsFailed,
		TransactionsSuccess: c.metrics.TransactionsSuccess,
	}
}

// Private helper methods

func (c *SuiClient) queryStakeInfo(ctx context.Context, nodeID string) (*StakeInfo, error) {
	// Query stake information from blockchain
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "suix_queryObjects",
		"params": []interface{}{
			map[string]interface{}{
				"StructType": fmt.Sprintf("%s::staking::StakeInfo", c.contractPackage),
				"filter": map[string]interface{}{
					"node_id": nodeID,
				},
			},
		},
	}

	resp, err := c.makeRequest(ctx, request)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result struct {
			Data []SuiObject `json:"data"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if len(response.Result.Data) == 0 {
		return nil, fmt.Errorf("no stake found for node %s", nodeID)
	}

	// Parse stake info from the first object
	obj := response.Result.Data[0]
	stakeInfo := &StakeInfo{
		NodeID:     nodeID,
		LastUpdate: time.Now(),
	}

	if amount, ok := obj.Content["stake_amount"].(float64); ok {
		stakeInfo.StakeAmount = uint64(amount)
	}
	if status, ok := obj.Content["status"].(string); ok {
		stakeInfo.Status = status
	}
	if validatorAddr, ok := obj.Content["validator_addr"].(string); ok {
		stakeInfo.ValidatorAddr = validatorAddr
	}

	return stakeInfo, nil
}

func (c *SuiClient) queryObjects(ctx context.Context, query map[string]interface{}) ([]SuiObject, error) {
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "suix_queryObjects",
		"params":  []interface{}{query},
	}

	resp, err := c.makeRequest(ctx, request)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result struct {
			Data []SuiObject `json:"data"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return response.Result.Data, nil
}

func (c *SuiClient) executeTransaction(ctx context.Context, moveCall map[string]interface{}) (*TransactionResponse, error) {
	// Build transaction
	txBuilder := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "sui_executeTransactionBlock",
		"params": []interface{}{
			map[string]interface{}{
				"sender":      c.address,
				"moveCall":    moveCall,
				"gasBudget":   "10000000",
				"gasPrice":    "1000",
			},
		},
	}

	// Sign and execute
	resp, err := c.makeRequest(ctx, txBuilder)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result TransactionResponse `json:"result"`
		Error  interface{}         `json:"error"`
	}

	if err := json.Unmarshal(resp, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("transaction failed: %v", response.Error)
	}

	return &response.Result, nil
}

func (c *SuiClient) makeRequest(ctx context.Context, request map[string]interface{}) ([]byte, error) {
	startTime := time.Now()
	defer func() {
		c.updateMetrics(func() {
			duration := time.Since(startTime)
			c.metrics.AvgResponseTime = (c.metrics.AvgResponseTime + duration) / 2
			c.metrics.LastRequestTime = time.Now()
		})
	}()

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *SuiClient) getCachedStake(nodeID string) *StakeInfo {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	if stake, exists := c.cache.stakes[nodeID]; exists {
		if time.Since(stake.LastUpdate) < c.cache.ttl {
			return stake
		}
	}
	return nil
}

func (c *SuiClient) setCachedStake(nodeID string, stake *StakeInfo) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.stakes[nodeID] = stake
	c.cache.lastUpdate = time.Now()
}

func (c *SuiClient) updateMetrics(fn func()) {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()
	fn()
}

func generateSuiAddress(publicKey ed25519.PublicKey) string {
	// Simplified address generation - in real implementation would use proper Sui address derivation
	hash := fmt.Sprintf("%x", publicKey)
	return "0x" + hash[:40] // Take first 40 chars as simplified address
}

func mapToStruct(data map[string]interface{}, target interface{}) error {
	// Simple map to struct conversion
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, target)
}

// Integration with existing security package
func (c *SuiClient) ValidateSealToken(token *security.SealToken, minStake uint64) error {
	if token == nil {
		return fmt.Errorf("nil seal token")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stakeInfo, err := c.ValidateStake(ctx, token.NodeID, minStake)
	if err != nil {
		return fmt.Errorf("stake validation failed: %v", err)
	}

	// Additional validation based on token specifics
	if stakeInfo.Status != "active" {
		return fmt.Errorf("node not active: %s", stakeInfo.Status)
	}

	if time.Since(stakeInfo.LastUpdate) > 24*time.Hour {
		return fmt.Errorf("stake information too old")
	}

	return nil
}