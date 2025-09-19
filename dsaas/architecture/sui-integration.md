# Sui Integration Architecture for K3s DaaS

## Overview

This document provides a comprehensive architecture for integrating Sui blockchain into K3s agents to create a Decentralized-as-a-Service (DaaS) system. The integration transforms K3s agents into blockchain-enabled worker nodes with stake-based authentication, smart contract interactions, and decentralized governance.

## Architectural Goals

1. **Seamless Integration**: Maintain K3s compatibility while adding DaaS capabilities
2. **Economic Security**: Implement stake-based participation and verification
3. **Decentralized Authentication**: Replace traditional tokens with Sui signatures
4. **Smart Contract Governance**: Enable on-chain worker registration and management
5. **Performance Attestation**: Integrate Nautilus for hardware verification

## Core Components

### 1. Sui Client Integration

#### A. Client Initialization Architecture

```go
// NEW: pkg/sui/client.go
type SuiClient struct {
    rpcClient      *sui.Client
    walletManager  *WalletManager
    contractAddr   string
    gasObjectID    string
    maxGasBudget   uint64
    retryConfig    *RetryConfig
    circuitBreaker *CircuitBreaker
}

type SuiConfig struct {
    RPCEndpoint     string        `yaml:"rpc_endpoint" env:"SUI_RPC_ENDPOINT"`
    WalletPath      string        `yaml:"wallet_path" env:"SUI_WALLET_PATH"`
    ContractPackage string        `yaml:"contract_package" env:"SUI_CONTRACT_PACKAGE"`
    MaxGasBudget    uint64        `yaml:"max_gas_budget" env:"SUI_MAX_GAS_BUDGET"`
    RetryAttempts   int           `yaml:"retry_attempts" env:"SUI_RETRY_ATTEMPTS"`
    RetryDelay      time.Duration `yaml:"retry_delay" env:"SUI_RETRY_DELAY"`
    CircuitThreshold int          `yaml:"circuit_threshold" env:"SUI_CIRCUIT_THRESHOLD"`
}

func NewSuiClient(config *SuiConfig) (*SuiClient, error) {
    // 1. Initialize RPC client with connection pooling
    rpcClient, err := sui.NewClient(config.RPCEndpoint, &sui.ClientOptions{
        MaxConnections:    10,
        ConnectionTimeout: 30 * time.Second,
        RequestTimeout:    60 * time.Second,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create Sui RPC client: %w", err)
    }

    // 2. Initialize wallet manager
    walletManager, err := NewWalletManager(config.WalletPath)
    if err != nil {
        return nil, fmt.Errorf("failed to initialize wallet: %w", err)
    }

    // 3. Setup circuit breaker for resilience
    circuitBreaker := NewCircuitBreaker(&CircuitBreakerConfig{
        Threshold:   config.CircuitThreshold,
        Timeout:     60 * time.Second,
        MaxRequests: 3,
    })

    // 4. Initialize gas object
    gasObjectID, err := ensureGasObject(rpcClient, walletManager)
    if err != nil {
        return nil, fmt.Errorf("failed to setup gas object: %w", err)
    }

    return &SuiClient{
        rpcClient:      rpcClient,
        walletManager:  walletManager,
        contractAddr:   config.ContractPackage,
        gasObjectID:    gasObjectID,
        maxGasBudget:   config.MaxGasBudget,
        retryConfig:    NewRetryConfig(config.RetryAttempts, config.RetryDelay),
        circuitBreaker: circuitBreaker,
    }, nil
}
```

#### B. Connection Management

```go
func (sc *SuiClient) WithRetry(operation func() error) error {
    return sc.retryConfig.Execute(func() error {
        return sc.circuitBreaker.Execute(operation)
    })
}

func (sc *SuiClient) HealthCheck() error {
    return sc.WithRetry(func() error {
        _, err := sc.rpcClient.GetLatestSuiSystemState(context.Background())
        return err
    })
}

func (sc *SuiClient) RefreshGasObject() error {
    // Ensure we have sufficient gas for transactions
    balance, err := sc.rpcClient.GetBalance(context.Background(),
                                           sc.walletManager.Address(),
                                           nil)
    if err != nil {
        return err
    }

    if balance.TotalBalance < sc.maxGasBudget {
        return fmt.Errorf("insufficient gas balance: %d < %d",
                         balance.TotalBalance, sc.maxGasBudget)
    }

    return nil
}
```

### 2. Smart Contract Design

#### A. Worker Registration Contract (Move Language)

```move
// contracts/worker_registry.move
module daas::worker_registry {
    use sui::object::{Self, UID};
    use sui::transfer;
    use sui::coin::{Self, Coin};
    use sui::sui::SUI;
    use sui::clock::{Self, Clock};
    use sui::table::{Self, Table};
    use sui::event;

    // Worker registration structure
    struct WorkerNode has key, store {
        id: UID,
        wallet_address: address,
        node_name: vector<u8>,
        stake_amount: u64,
        performance_score: u64,
        registration_time: u64,
        last_heartbeat: u64,
        ip_addresses: vector<vector<u8>>,
        capabilities: vector<u8>,
        status: u8, // 0: pending, 1: active, 2: suspended, 3: slashed
    }

    // Registry state
    struct WorkerRegistry has key {
        id: UID,
        workers: Table<address, WorkerNode>,
        min_stake: u64,
        slash_percentage: u64,
        total_staked: u64,
        admin_cap: AdminCap,
    }

    struct AdminCap has key, store {
        id: UID,
    }

    // Events
    struct WorkerRegistered has copy, drop {
        worker_address: address,
        node_name: vector<u8>,
        stake_amount: u64,
    }

    struct WorkerSlashed has copy, drop {
        worker_address: address,
        slash_amount: u64,
        reason: vector<u8>,
    }

    // Initialize registry
    fun init(ctx: &mut TxContext) {
        let admin_cap = AdminCap { id: object::new(ctx) };
        let registry = WorkerRegistry {
            id: object::new(ctx),
            workers: table::new(ctx),
            min_stake: 1000000000, // 1 SUI minimum
            slash_percentage: 10,   // 10% slash rate
            total_staked: 0,
            admin_cap,
        };
        transfer::share_object(registry);
    }

    // Register worker node
    public entry fun register_worker(
        registry: &mut WorkerRegistry,
        stake_coin: Coin<SUI>,
        node_name: vector<u8>,
        ip_addresses: vector<vector<u8>>,
        capabilities: vector<u8>,
        clock: &Clock,
        ctx: &mut TxContext
    ) {
        let stake_amount = coin::value(&stake_coin);
        assert!(stake_amount >= registry.min_stake, 1);

        let worker_address = tx_context::sender(ctx);
        assert!(!table::contains(&registry.workers, worker_address), 2);

        // Create worker node entry
        let worker_node = WorkerNode {
            id: object::new(ctx),
            wallet_address: worker_address,
            node_name,
            stake_amount,
            performance_score: 100, // Initial score
            registration_time: clock::timestamp_ms(clock),
            last_heartbeat: clock::timestamp_ms(clock),
            ip_addresses,
            capabilities,
            status: 1, // Active
        };

        // Store stake
        transfer::public_transfer(stake_coin, @daas_treasury);
        registry.total_staked = registry.total_staked + stake_amount;

        // Register worker
        table::add(&mut registry.workers, worker_address, worker_node);

        // Emit event
        event::emit(WorkerRegistered {
            worker_address,
            node_name,
            stake_amount,
        });
    }

    // Update worker heartbeat
    public entry fun heartbeat(
        registry: &mut WorkerRegistry,
        performance_metrics: vector<u8>,
        clock: &Clock,
        ctx: &mut TxContext
    ) {
        let worker_address = tx_context::sender(ctx);
        assert!(table::contains(&registry.workers, worker_address), 3);

        let worker = table::borrow_mut(&mut registry.workers, worker_address);
        worker.last_heartbeat = clock::timestamp_ms(clock);

        // Update performance score based on metrics
        update_performance_score(worker, performance_metrics);
    }

    // Slash worker for poor performance
    public entry fun slash_worker(
        _: &AdminCap,
        registry: &mut WorkerRegistry,
        worker_address: address,
        reason: vector<u8>,
        ctx: &mut TxContext
    ) {
        assert!(table::contains(&registry.workers, worker_address), 4);

        let worker = table::borrow_mut(&mut registry.workers, worker_address);
        let slash_amount = (worker.stake_amount * registry.slash_percentage) / 100;

        worker.stake_amount = worker.stake_amount - slash_amount;
        worker.status = 3; // Slashed
        registry.total_staked = registry.total_staked - slash_amount;

        event::emit(WorkerSlashed {
            worker_address,
            slash_amount,
            reason,
        });
    }

    // Helper functions
    fun update_performance_score(worker: &mut WorkerNode, metrics: vector<u8>) {
        // Parse performance metrics and update score
        // Implementation depends on Nautilus attestation format
    }

    // View functions
    public fun get_worker_info(
        registry: &WorkerRegistry,
        worker_address: address
    ): (vector<u8>, u64, u64, u8) {
        let worker = table::borrow(&registry.workers, worker_address);
        (worker.node_name, worker.stake_amount, worker.performance_score, worker.status)
    }

    public fun is_worker_registered(
        registry: &WorkerRegistry,
        worker_address: address
    ): bool {
        table::contains(&registry.workers, worker_address)
    }
}
```

#### B. Staking Verification Contract

```move
// contracts/stake_verifier.move
module daas::stake_verifier {
    use sui::coin::{Self, Coin};
    use sui::sui::SUI;
    use sui::clock::{Self, Clock};
    use sui::math;

    // Stake verification functions
    public fun verify_minimum_stake(
        stake_coin: &Coin<SUI>,
        min_required: u64
    ): bool {
        coin::value(stake_coin) >= min_required
    }

    public fun calculate_stake_power(
        stake_amount: u64,
        performance_score: u64,
        stake_duration: u64
    ): u64 {
        // Stake power = base_stake * performance_multiplier * duration_bonus
        let performance_multiplier = performance_score / 100;
        let duration_bonus = math::min(stake_duration / (30 * 24 * 60 * 60 * 1000), 10); // Max 10x for 30 days

        stake_amount * performance_multiplier * (100 + duration_bonus) / 100
    }

    public fun is_stake_sufficient_for_role(
        stake_power: u64,
        required_role_stake: u64
    ): bool {
        stake_power >= required_role_stake
    }
}
```

### 3. Worker Registration Flow

#### A. Registration Sequence Diagram

```
Agent                 Sui Client              Smart Contract         K3s Server
  |                        |                        |                      |
  |--1. Load Wallet------->|                        |                      |
  |<-----Wallet Info-------|                        |                      |
  |                        |                        |                      |
  |--2. Register Worker--->|                        |                      |
  |                        |--3. Call Contract----->|                      |
  |                        |<--4. Transaction Ack---|                      |
  |<--5. Registration------|                        |                      |
  |     Complete           |                        |                      |
  |                        |                        |                      |
  |--6. Generate Seal----->|                        |                      |
  |    Token               |                        |                      |
  |<--7. Seal Token--------|                        |                      |
  |                        |                        |                      |
  |--8. Connect to Server (with Seal Token)----------------------->|
  |<--9. Authentication Challenge-----------------------------------|
  |                        |                        |                      |
  |--10. Sign Challenge--->|                        |                      |
  |<--11. Signature--------|                        |                      |
  |--12. Submit Proof------|                        |                      |
  |                        |--13. Verify on-chain->|                      |
  |                        |<--14. Verification-----|                      |
  |<--15. Access Granted--------------------------------------------|
```

#### B. Registration Implementation

```go
// MODIFICATION POINT: pkg/agent/run.go - Add DaaS registration flow
func registerWithDaaS(agentConfig *daemonconfig.Agent) error {
    // 1. Initialize Sui client
    suiClient, err := sui.NewSuiClient(agentConfig.DaaS.SuiConfig)
    if err != nil {
        return fmt.Errorf("failed to initialize Sui client: %w", err)
    }

    // 2. Check if already registered
    registered, err := suiClient.IsWorkerRegistered(agentConfig.NodeName)
    if err != nil {
        return fmt.Errorf("failed to check registration status: %w", err)
    }

    if !registered {
        // 3. Perform registration
        if err := performWorkerRegistration(suiClient, agentConfig); err != nil {
            return fmt.Errorf("worker registration failed: %w", err)
        }
        logrus.Infof("Successfully registered worker node: %s", agentConfig.NodeName)
    }

    // 4. Start heartbeat routine
    go startHeartbeatRoutine(suiClient, agentConfig)

    return nil
}

func performWorkerRegistration(suiClient *sui.SuiClient, config *daemonconfig.Agent) error {
    // 1. Prepare registration parameters
    registrationParams := &sui.WorkerRegistrationParams{
        NodeName:     config.NodeName,
        IPAddresses:  config.NodeIPs,
        Capabilities: encodeCapabilities(config),
        StakeAmount:  config.DaaS.MinStake,
    }

    // 2. Execute registration transaction
    txDigest, err := suiClient.RegisterWorker(registrationParams)
    if err != nil {
        return err
    }

    // 3. Wait for transaction confirmation
    confirmed, err := suiClient.WaitForTransactionConfirmation(txDigest, 30*time.Second)
    if err != nil {
        return err
    }

    if !confirmed {
        return errors.New("registration transaction not confirmed within timeout")
    }

    // 4. Store registration info locally
    return storeRegistrationInfo(config.DataDir, txDigest, registrationParams)
}

func startHeartbeatRoutine(suiClient *sui.SuiClient, config *daemonconfig.Agent) {
    ticker := time.NewTicker(config.DaaS.HeartbeatInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if err := sendHeartbeat(suiClient, config); err != nil {
                logrus.Errorf("Heartbeat failed: %v", err)
            }
        }
    }
}

func sendHeartbeat(suiClient *sui.SuiClient, config *daemonconfig.Agent) error {
    // 1. Collect performance metrics
    metrics, err := collectPerformanceMetrics()
    if err != nil {
        return err
    }

    // 2. Get Nautilus attestation
    attestation, err := nautilus.GetAttestation()
    if err != nil {
        logrus.Warnf("Failed to get Nautilus attestation: %v", err)
        // Continue without attestation for now
    }

    // 3. Send heartbeat transaction
    heartbeatParams := &sui.HeartbeatParams{
        PerformanceMetrics: metrics,
        NautilusAttestation: attestation,
    }

    return suiClient.SendHeartbeat(heartbeatParams)
}
```

### 4. Staking Verification Mechanism

#### A. Stake Validation Service

```go
// NEW: pkg/daas/stake.go
type StakeValidator struct {
    suiClient       *sui.SuiClient
    contractAddr    string
    minStake        *big.Int
    stakeCache      *sync.Map
    cacheTTL        time.Duration
    validationLock  sync.RWMutex
}

type StakeInfo struct {
    Amount       *big.Int  `json:"amount"`
    PerformanceScore uint64 `json:"performance_score"`
    Status       uint8     `json:"status"`
    ValidUntil   time.Time `json:"valid_until"`
    LastCheck    time.Time `json:"last_check"`
}

func NewStakeValidator(suiClient *sui.SuiClient, contractAddr string, minStake *big.Int) *StakeValidator {
    return &StakeValidator{
        suiClient:    suiClient,
        contractAddr: contractAddr,
        minStake:     minStake,
        stakeCache:   &sync.Map{},
        cacheTTL:     5 * time.Minute,
    }
}

func (sv *StakeValidator) ValidateStake(ctx context.Context, walletAddress string) (*StakeInfo, error) {
    sv.validationLock.RLock()

    // 1. Check cache first
    if cached, exists := sv.stakeCache.Load(walletAddress); exists {
        stakeInfo := cached.(*StakeInfo)
        if time.Now().Before(stakeInfo.ValidUntil) {
            sv.validationLock.RUnlock()
            return stakeInfo, nil
        }
    }
    sv.validationLock.RUnlock()

    // 2. Query blockchain for current stake
    sv.validationLock.Lock()
    defer sv.validationLock.Unlock()

    stakeInfo, err := sv.queryStakeFromContract(ctx, walletAddress)
    if err != nil {
        return nil, fmt.Errorf("failed to query stake: %w", err)
    }

    // 3. Validate minimum stake requirement
    if stakeInfo.Amount.Cmp(sv.minStake) < 0 {
        return nil, fmt.Errorf("insufficient stake: have %s, need %s",
                             stakeInfo.Amount.String(), sv.minStake.String())
    }

    // 4. Check worker status
    if stakeInfo.Status != 1 { // 1 = active
        return nil, fmt.Errorf("worker not in active status: %d", stakeInfo.Status)
    }

    // 5. Update cache
    stakeInfo.ValidUntil = time.Now().Add(sv.cacheTTL)
    stakeInfo.LastCheck = time.Now()
    sv.stakeCache.Store(walletAddress, stakeInfo)

    return stakeInfo, nil
}

func (sv *StakeValidator) queryStakeFromContract(ctx context.Context, walletAddress string) (*StakeInfo, error) {
    // 1. Prepare contract call
    callParams := &sui.ContractCallParams{
        Package:  sv.contractAddr,
        Module:   "worker_registry",
        Function: "get_worker_info",
        Args:     []interface{}{walletAddress},
    }

    // 2. Execute read-only call
    result, err := sv.suiClient.CallContract(ctx, callParams)
    if err != nil {
        return nil, err
    }

    // 3. Parse result
    return parseStakeInfoFromContractResult(result)
}

func (sv *StakeValidator) InvalidateCache(walletAddress string) {
    sv.stakeCache.Delete(walletAddress)
}

// Background cache refresh
func (sv *StakeValidator) StartCacheRefresh(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                sv.refreshStaleEntries()
            }
        }
    }()
}

func (sv *StakeValidator) refreshStaleEntries() {
    now := time.Now()
    sv.stakeCache.Range(func(key, value interface{}) bool {
        stakeInfo := value.(*StakeInfo)
        if now.After(stakeInfo.ValidUntil) {
            walletAddr := key.(string)
            go func() {
                ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
                defer cancel()
                sv.ValidateStake(ctx, walletAddr)
            }()
        }
        return true
    })
}
```

#### B. Performance-Based Staking

```go
type PerformanceStakeCalculator struct {
    baseStakeRequirement *big.Int
    performanceWeights   map[string]float64
    maxBonus            float64
    minPenalty          float64
}

func (psc *PerformanceStakeCalculator) CalculateRequiredStake(
    baseRole string,
    performanceMetrics map[string]float64
) *big.Int {
    // 1. Start with base stake requirement
    multiplier := 1.0

    // 2. Apply performance adjustments
    for metric, value := range performanceMetrics {
        if weight, exists := psc.performanceWeights[metric]; exists {
            adjustment := (value - 1.0) * weight
            multiplier += adjustment
        }
    }

    // 3. Apply bounds
    if multiplier > (1.0 + psc.maxBonus) {
        multiplier = 1.0 + psc.maxBonus
    }
    if multiplier < (1.0 - psc.minPenalty) {
        multiplier = 1.0 - psc.minPenalty
    }

    // 4. Calculate final stake requirement
    requiredStake := new(big.Int).SetUint64(uint64(float64(psc.baseStakeRequirement.Uint64()) * multiplier))
    return requiredStake
}
```

### 5. API Specifications

#### A. Sui Contract API

```go
// Contract interaction APIs
type SuiContractAPI interface {
    // Worker management
    RegisterWorker(params *WorkerRegistrationParams) (string, error)
    UpdateWorkerStatus(address string, status uint8) error
    SlashWorker(address string, reason string) error

    // Staking operations
    GetWorkerStake(address string) (*StakeInfo, error)
    IncreaseStake(amount *big.Int) (string, error)
    WithdrawStake(amount *big.Int) (string, error)

    // Performance tracking
    SubmitPerformanceMetrics(metrics *PerformanceMetrics) (string, error)
    GetPerformanceHistory(address string, period time.Duration) ([]*PerformanceRecord, error)

    // Governance
    ProposeSlashing(targetAddress string, evidence *SlashingEvidence) (string, error)
    VoteOnProposal(proposalID string, vote bool) (string, error)
}

type WorkerRegistrationParams struct {
    NodeName      string            `json:"node_name"`
    IPAddresses   []string          `json:"ip_addresses"`
    Capabilities  map[string]string `json:"capabilities"`
    StakeAmount   *big.Int          `json:"stake_amount"`
    PublicKey     string            `json:"public_key"`
}

type PerformanceMetrics struct {
    CPUUtilization    float64           `json:"cpu_utilization"`
    MemoryUtilization float64           `json:"memory_utilization"`
    NetworkBandwidth  uint64            `json:"network_bandwidth"`
    StorageIOPS       uint64            `json:"storage_iops"`
    UptimePercentage  float64           `json:"uptime_percentage"`
    CustomMetrics     map[string]float64 `json:"custom_metrics"`
    NautilusAttestation *NautilusAttestation `json:"nautilus_attestation,omitempty"`
}

type NautilusAttestation struct {
    HardwareHash    string    `json:"hardware_hash"`
    TrustScore      float64   `json:"trust_score"`
    Timestamp       time.Time `json:"timestamp"`
    Signature       string    `json:"signature"`
    ValidationProof string    `json:"validation_proof"`
}
```

#### B. REST API Endpoints

```go
// HTTP API for external integrations
type DaaSAPIHandler struct {
    suiClient      *sui.SuiClient
    stakeValidator *StakeValidator
    authValidator  *SealAuthValidator
}

// Worker management endpoints
func (h *DaaSAPIHandler) RegisterWorkerHandler(w http.ResponseWriter, r *http.Request) {
    var req WorkerRegistrationRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    // Validate Seal authentication
    if err := h.authValidator.ValidateRequest(r); err != nil {
        http.Error(w, "Authentication failed", http.StatusUnauthorized)
        return
    }

    // Process registration
    txDigest, err := h.suiClient.RegisterWorker(&req.Params)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    response := WorkerRegistrationResponse{
        TransactionDigest: txDigest,
        Status:           "pending",
        Message:          "Registration submitted successfully",
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// Staking endpoints
func (h *DaaSAPIHandler) GetStakeInfoHandler(w http.ResponseWriter, r *http.Request) {
    walletAddress := r.URL.Query().Get("address")
    if walletAddress == "" {
        http.Error(w, "Missing wallet address", http.StatusBadRequest)
        return
    }

    stakeInfo, err := h.stakeValidator.ValidateStake(r.Context(), walletAddress)
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(stakeInfo)
}

// Performance metrics endpoints
func (h *DaaSAPIHandler) SubmitMetricsHandler(w http.ResponseWriter, r *http.Request) {
    var metrics PerformanceMetrics
    if err := json.NewDecoder(r.Body).Decode(&metrics); err != nil {
        http.Error(w, "Invalid metrics format", http.StatusBadRequest)
        return
    }

    // Validate Nautilus attestation if present
    if metrics.NautilusAttestation != nil {
        if err := h.validateNautilusAttestation(metrics.NautilusAttestation); err != nil {
            http.Error(w, "Invalid Nautilus attestation", http.StatusBadRequest)
            return
        }
    }

    txDigest, err := h.suiClient.SubmitPerformanceMetrics(&metrics)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    response := map[string]string{
        "transaction_digest": txDigest,
        "status":            "submitted",
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}
```

### 6. Error Handling Strategy

#### A. Circuit Breaker Implementation

```go
type CircuitBreaker struct {
    state         CircuitState
    failureCount  int64
    lastFailure   time.Time
    timeout       time.Duration
    threshold     int64
    maxRequests   int64
    mu            sync.RWMutex
}

type CircuitState int

const (
    StateClosed CircuitState = iota
    StateHalfOpen
    StateOpen
)

func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
    return &CircuitBreaker{
        state:       StateClosed,
        timeout:     config.Timeout,
        threshold:   int64(config.Threshold),
        maxRequests: int64(config.MaxRequests),
    }
}

func (cb *CircuitBreaker) Execute(operation func() error) error {
    cb.mu.RLock()
    state := cb.state
    cb.mu.RUnlock()

    switch state {
    case StateOpen:
        if cb.shouldAttemptReset() {
            cb.setState(StateHalfOpen)
            return cb.executeHalfOpen(operation)
        }
        return ErrCircuitBreakerOpen

    case StateHalfOpen:
        return cb.executeHalfOpen(operation)

    default: // StateClosed
        return cb.executeClosed(operation)
    }
}

func (cb *CircuitBreaker) executeClosed(operation func() error) error {
    err := operation()
    if err != nil {
        cb.recordFailure()
    } else {
        cb.recordSuccess()
    }
    return err
}

func (cb *CircuitBreaker) executeHalfOpen(operation func() error) error {
    err := operation()
    if err != nil {
        cb.setState(StateOpen)
        cb.recordFailure()
    } else {
        cb.setState(StateClosed)
        cb.recordSuccess()
    }
    return err
}

func (cb *CircuitBreaker) recordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    cb.failureCount++
    cb.lastFailure = time.Now()

    if cb.failureCount >= cb.threshold {
        cb.state = StateOpen
    }
}

func (cb *CircuitBreaker) recordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    cb.failureCount = 0
}
```

#### B. Retry Strategy with Exponential Backoff

```go
type RetryConfig struct {
    MaxAttempts int
    BaseDelay   time.Duration
    MaxDelay    time.Duration
    Multiplier  float64
    Jitter      bool
}

func NewRetryConfig(maxAttempts int, baseDelay time.Duration) *RetryConfig {
    return &RetryConfig{
        MaxAttempts: maxAttempts,
        BaseDelay:   baseDelay,
        MaxDelay:    5 * time.Minute,
        Multiplier:  2.0,
        Jitter:      true,
    }
}

func (rc *RetryConfig) Execute(operation func() error) error {
    var lastErr error

    for attempt := 0; attempt < rc.MaxAttempts; attempt++ {
        if attempt > 0 {
            delay := rc.calculateDelay(attempt)
            time.Sleep(delay)
        }

        lastErr = operation()
        if lastErr == nil {
            return nil
        }

        // Don't retry on certain errors
        if !rc.shouldRetry(lastErr) {
            return lastErr
        }
    }

    return fmt.Errorf("operation failed after %d attempts: %w", rc.MaxAttempts, lastErr)
}

func (rc *RetryConfig) calculateDelay(attempt int) time.Duration {
    delay := time.Duration(float64(rc.BaseDelay) * math.Pow(rc.Multiplier, float64(attempt-1)))

    if delay > rc.MaxDelay {
        delay = rc.MaxDelay
    }

    if rc.Jitter {
        jitter := time.Duration(rand.Float64() * float64(delay) * 0.1)
        delay += jitter
    }

    return delay
}

func (rc *RetryConfig) shouldRetry(err error) bool {
    // Define non-retryable errors
    nonRetryableErrors := []string{
        "insufficient stake",
        "invalid signature",
        "worker already registered",
        "unauthorized",
    }

    errMsg := err.Error()
    for _, nonRetryable := range nonRetryableErrors {
        if strings.Contains(errMsg, nonRetryable) {
            return false
        }
    }

    return true
}
```

#### C. Graceful Degradation

```go
type DegradationManager struct {
    suiClient         *sui.SuiClient
    fallbackValidator *LocalStakeValidator
    degradedMode      bool
    lastSuiCheck      time.Time
    checkInterval     time.Duration
}

func (dm *DegradationManager) ValidateStakeWithFallback(walletAddress string) (*StakeInfo, error) {
    // 1. Try primary Sui validation
    if !dm.degradedMode || time.Since(dm.lastSuiCheck) > dm.checkInterval {
        stakeInfo, err := dm.suiClient.GetWorkerStake(walletAddress)
        if err == nil {
            dm.degradedMode = false
            dm.lastSuiCheck = time.Now()
            return stakeInfo, nil
        }

        logrus.Warnf("Sui validation failed, entering degraded mode: %v", err)
        dm.degradedMode = true
        dm.lastSuiCheck = time.Now()
    }

    // 2. Fallback to local validation
    return dm.fallbackValidator.ValidateStake(walletAddress)
}

type LocalStakeValidator struct {
    localCache map[string]*StakeInfo
    mu         sync.RWMutex
}

func (lsv *LocalStakeValidator) ValidateStake(walletAddress string) (*StakeInfo, error) {
    lsv.mu.RLock()
    defer lsv.mu.RUnlock()

    if stakeInfo, exists := lsv.localCache[walletAddress]; exists {
        // Return cached info with warning
        stakeInfo.Status = 2 // Degraded mode status
        return stakeInfo, nil
    }

    return nil, errors.New("no cached stake information available")
}
```

### 7. Configuration Integration

#### A. Enhanced Configuration Structure

```go
// MODIFICATION POINT: pkg/daemons/config/types.go
type Agent struct {
    // ... existing K3s fields ...

    // DaaS-specific configuration
    DaaS *DaaSConfig `json:"daas,omitempty"`
}

type DaaSConfig struct {
    Enabled          bool          `json:"enabled" yaml:"enabled"`
    SuiConfig        *SuiConfig    `json:"sui" yaml:"sui"`
    StakeConfig      *StakeConfig  `json:"stake" yaml:"stake"`
    PerformanceConfig *PerformanceConfig `json:"performance" yaml:"performance"`
    WalrusConfig     *WalrusConfig `json:"walrus" yaml:"walrus"`
    NautilusConfig   *NautilusConfig `json:"nautilus" yaml:"nautilus"`
}

type SuiConfig struct {
    RPCEndpoint      string        `json:"rpc_endpoint" yaml:"rpc_endpoint" env:"SUI_RPC_ENDPOINT"`
    WalletPath       string        `json:"wallet_path" yaml:"wallet_path" env:"SUI_WALLET_PATH"`
    ContractPackage  string        `json:"contract_package" yaml:"contract_package" env:"SUI_CONTRACT_PACKAGE"`
    MaxGasBudget     uint64        `json:"max_gas_budget" yaml:"max_gas_budget" env:"SUI_MAX_GAS_BUDGET"`
    RetryAttempts    int           `json:"retry_attempts" yaml:"retry_attempts" env:"SUI_RETRY_ATTEMPTS"`
    RetryDelay       time.Duration `json:"retry_delay" yaml:"retry_delay" env:"SUI_RETRY_DELAY"`
    CircuitThreshold int           `json:"circuit_threshold" yaml:"circuit_threshold" env:"SUI_CIRCUIT_THRESHOLD"`
}

type StakeConfig struct {
    MinStake         string        `json:"min_stake" yaml:"min_stake" env:"DAAS_MIN_STAKE"`
    AutoIncreaseStake bool         `json:"auto_increase_stake" yaml:"auto_increase_stake" env:"DAAS_AUTO_INCREASE_STAKE"`
    StakeBuffer      string        `json:"stake_buffer" yaml:"stake_buffer" env:"DAAS_STAKE_BUFFER"`
    ValidatorCacheTTL time.Duration `json:"validator_cache_ttl" yaml:"validator_cache_ttl" env:"DAAS_VALIDATOR_CACHE_TTL"`
}

type PerformanceConfig struct {
    HeartbeatInterval time.Duration `json:"heartbeat_interval" yaml:"heartbeat_interval" env:"DAAS_HEARTBEAT_INTERVAL"`
    MetricsRetention time.Duration `json:"metrics_retention" yaml:"metrics_retention" env:"DAAS_METRICS_RETENTION"`
    EnableAttestation bool         `json:"enable_attestation" yaml:"enable_attestation" env:"DAAS_ENABLE_ATTESTATION"`
}
```

#### B. Configuration Loading Enhancement

```go
// MODIFICATION POINT: pkg/configfilearg/parser.go
func parseDaaSConfig(config map[string]interface{}) (*DaaSConfig, error) {
    daasSection, exists := config["daas"]
    if !exists {
        return nil, nil
    }

    var daasConfig DaaSConfig

    // Parse Sui configuration
    if suiSection, exists := daasSection.(map[string]interface{})["sui"]; exists {
        suiConfig, err := parseSuiConfig(suiSection.(map[string]interface{}))
        if err != nil {
            return nil, fmt.Errorf("failed to parse Sui config: %w", err)
        }
        daasConfig.SuiConfig = suiConfig
    }

    // Parse stake configuration
    if stakeSection, exists := daasSection.(map[string]interface{})["stake"]; exists {
        stakeConfig, err := parseStakeConfig(stakeSection.(map[string]interface{}))
        if err != nil {
            return nil, fmt.Errorf("failed to parse stake config: %w", err)
        }
        daasConfig.StakeConfig = stakeConfig
    }

    return &daasConfig, nil
}
```

### 8. Implementation Roadmap

#### Phase 1: Foundation (Weeks 1-4)
1. **Sui Client Integration**
   - Implement basic RPC client with connection pooling
   - Add wallet management and transaction signing
   - Create circuit breaker and retry mechanisms

2. **Smart Contract Development**
   - Deploy worker registry contract on Sui
   - Implement basic registration and staking functions
   - Add event emission for monitoring

3. **Configuration Integration**
   - Extend K3s configuration structure
   - Add environment variable support
   - Implement configuration validation

#### Phase 2: Core Features (Weeks 5-8)
1. **Worker Registration**
   - Implement on-chain worker registration
   - Add stake verification mechanisms
   - Create heartbeat and performance tracking

2. **Authentication Enhancement**
   - Replace K3s tokens with Seal authentication
   - Integrate Sui signature verification
   - Add challenge-response authentication

3. **Error Handling**
   - Implement comprehensive error handling
   - Add graceful degradation mechanisms
   - Create monitoring and alerting

#### Phase 3: Advanced Features (Weeks 9-12)
1. **Performance Integration**
   - Integrate Nautilus attestation
   - Implement performance-based staking
   - Add slashing mechanisms

2. **Governance Features**
   - Add worker voting mechanisms
   - Implement dispute resolution
   - Create reward distribution

3. **Production Readiness**
   - Comprehensive testing and validation
   - Security audits and penetration testing
   - Documentation and deployment guides

## Security Considerations

### Cryptographic Security
- **Signature Verification**: All Sui signatures validated using ed25519
- **Challenge-Response**: Prevent replay attacks with timestamped challenges
- **Key Management**: Secure wallet storage with encryption at rest

### Economic Security
- **Stake Requirements**: Minimum stake enforced for participation
- **Slashing Mechanisms**: Economic penalties for poor performance
- **Performance Bonds**: Additional stake for high-value operations

### Network Security
- **Circuit Breakers**: Prevent cascade failures
- **Rate Limiting**: Protect against DoS attacks
- **Attestation Verification**: Nautilus hardware validation

### Operational Security
- **Graceful Degradation**: Maintain functionality during partial failures
- **Audit Logging**: Comprehensive security event logging
- **Access Control**: Role-based permissions with Sui addresses

This comprehensive architecture provides a robust foundation for integrating Sui blockchain into K3s agents, creating a production-ready DaaS system with economic incentives, decentralized governance, and enhanced security.