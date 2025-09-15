# Nautilus TEE Integration for K3s DaaS

## Overview

This document provides a comprehensive architecture for integrating Nautilus Trusted Execution Environment (TEE) technology into K3s agents, enabling hardware-based attestation, secure performance monitoring, and cryptographic proof generation for on-chain verification. This integration ensures that DaaS worker nodes provide verifiable performance guarantees and secure computation environments.

## Architectural Goals

1. **Hardware Attestation**: Verify authentic TEE hardware capabilities and integrity
2. **Performance Verification**: Provide cryptographically signed performance metrics
3. **Secure Computation**: Protect sensitive workloads within TEE environments
4. **On-chain Verification**: Generate proofs that can be verified by Sui smart contracts
5. **Reward Optimization**: Link performance attestation to economic incentives

## Nautilus TEE Architecture Overview

### TEE Technology Stack
- **Hardware Layer**: Intel SGX, AMD SEV, ARM TrustZone, RISC-V Keystone
- **Runtime Layer**: Nautilus TEE Runtime with attestation capabilities
- **Application Layer**: K3s agent components running in secure enclaves
- **Communication Layer**: Secure channels between TEE and external world

## K3s Agent Lifecycle Analysis

### Key Attestation Points Identified

Based on analysis of `pkg/agent/run.go`, the following lifecycle stages require attestation:

1. **Agent Initialization** (Lines 58-63): Configuration loading and validation
2. **Network Setup** (Lines 64-94): IP validation and dual-stack configuration
3. **Runtime Bootstrap** (Lines 109-111): Executor and containerd initialization
4. **Registry Startup** (Lines 113-121): Embedded registry and image handling
5. **Metrics Collection** (Lines 123-127): Performance monitoring activation
6. **Network Components** (Lines 139-148): Flannel and CNI initialization
7. **Workload Execution**: Container and pod lifecycle events
8. **Resource Monitoring**: Ongoing performance and security monitoring

## Nautilus TEE Integration Architecture

### 1. TEE Client Architecture

#### A. Nautilus TEE Client Core

```go
// NEW: pkg/nautilus/client.go
type NautilusClient struct {
    teeConfig      *TEEConfig
    enclaveManager *EnclaveManager
    attestationSvc *AttestationService
    metricsSvc     *MetricsService
    proofGenerator *ProofGenerator
    secureComm     *SecureComm
    keyManager     *KeyManager
    performance    *PerformanceMonitor
    verificationSvc *VerificationService
}

type TEEConfig struct {
    Enabled          bool              `yaml:"enabled" env:"NAUTILUS_ENABLED"`
    TEEType          string            `yaml:"tee_type" env:"NAUTILUS_TEE_TYPE"` // sgx, sev, trustzone, keystone
    EnclaveImage     string            `yaml:"enclave_image" env:"NAUTILUS_ENCLAVE_IMAGE"`
    AttestationURL   string            `yaml:"attestation_url" env:"NAUTILUS_ATTESTATION_URL"`
    KeyProvisionURL  string            `yaml:"key_provision_url" env:"NAUTILUS_KEY_PROVISION_URL"`
    MetricsInterval  time.Duration     `yaml:"metrics_interval" env:"NAUTILUS_METRICS_INTERVAL"`
    ProofBatchSize   int              `yaml:"proof_batch_size" env:"NAUTILUS_PROOF_BATCH_SIZE"`
    SecureChannels   bool             `yaml:"secure_channels" env:"NAUTILUS_SECURE_CHANNELS"`
    DebugMode        bool             `yaml:"debug_mode" env:"NAUTILUS_DEBUG_MODE"`
    QuoteProvider    string           `yaml:"quote_provider" env:"NAUTILUS_QUOTE_PROVIDER"`
    PCRPolicy        map[string]string `yaml:"pcr_policy" env:"NAUTILUS_PCR_POLICY"`
}

func NewNautilusClient(config *TEEConfig, sealAuth *seal.AuthClient) (*NautilusClient, error) {
    // 1. Initialize TEE hardware detection
    teeType, err := detectTEEHardware()
    if err != nil {
        return nil, fmt.Errorf("TEE hardware detection failed: %w", err)
    }

    if config.TEEType != "" && config.TEEType != teeType {
        return nil, fmt.Errorf("configured TEE type %s doesn't match detected %s", config.TEEType, teeType)
    }
    config.TEEType = teeType

    // 2. Initialize enclave manager
    enclaveManager, err := NewEnclaveManager(&EnclaveConfig{
        TEEType:      teeType,
        EnclaveImage: config.EnclaveImage,
        DebugMode:    config.DebugMode,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize enclave manager: %w", err)
    }

    // 3. Initialize key management
    keyManager, err := NewKeyManager(&KeyManagerConfig{
        TEEType:         teeType,
        ProvisionURL:    config.KeyProvisionURL,
        SealIntegration: sealAuth,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize key manager: %w", err)
    }

    // 4. Initialize attestation service
    attestationSvc, err := NewAttestationService(&AttestationConfig{
        TEEType:       teeType,
        AttestationURL: config.AttestationURL,
        QuoteProvider: config.QuoteProvider,
        PCRPolicy:    config.PCRPolicy,
        KeyManager:   keyManager,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize attestation service: %w", err)
    }

    // 5. Initialize secure communication
    secureComm, err := NewSecureComm(&SecureCommConfig{
        TEEType:        teeType,
        SecureChannels: config.SecureChannels,
        KeyManager:     keyManager,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize secure communication: %w", err)
    }

    // 6. Initialize performance monitoring
    performance, err := NewPerformanceMonitor(&PerformanceConfig{
        MetricsInterval: config.MetricsInterval,
        TEEEnabled:     true,
        SecureMetrics:  true,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize performance monitor: %w", err)
    }

    return &NautilusClient{
        teeConfig:       config,
        enclaveManager:  enclaveManager,
        attestationSvc:  attestationSvc,
        keyManager:     keyManager,
        secureComm:     secureComm,
        performance:    performance,
    }, nil
}

func detectTEEHardware() (string, error) {
    // 1. Check for Intel SGX
    if sgxAvailable, err := checkSGXSupport(); err == nil && sgxAvailable {
        return "sgx", nil
    }

    // 2. Check for AMD SEV
    if sevAvailable, err := checkSEVSupport(); err == nil && sevAvailable {
        return "sev", nil
    }

    // 3. Check for ARM TrustZone
    if tzAvailable, err := checkTrustZoneSupport(); err == nil && tzAvailable {
        return "trustzone", nil
    }

    // 4. Check for RISC-V Keystone
    if keystoneAvailable, err := checkKeystoneSupport(); err == nil && keystoneAvailable {
        return "keystone", nil
    }

    return "", fmt.Errorf("no supported TEE hardware detected")
}
```

#### B. Enclave Management

```go
type EnclaveManager struct {
    teeType        string
    enclaveImage   string
    activeEnclaves map[string]*EnclaveInstance
    enclaveMutex   sync.RWMutex
    debugMode      bool
    healthChecker  *EnclaveHealthChecker
}

type EnclaveInstance struct {
    ID              string            `json:"id"`
    TEEType         string            `json:"tee_type"`
    Image           string            `json:"image"`
    Status          EnclaveStatus     `json:"status"`
    CreatedAt       time.Time         `json:"created_at"`
    LastAttestation time.Time         `json:"last_attestation"`
    Measurements    *EnclaveMeasurements `json:"measurements"`
    SecureChannel   *SecureChannel    `json:"secure_channel"`
    PerformanceData *EnclavePerformance `json:"performance_data"`
}

type EnclaveStatus string

const (
    StatusCreated     EnclaveStatus = "created"
    StatusInitialized EnclaveStatus = "initialized"
    StatusAttested    EnclaveStatus = "attested"
    StatusRunning     EnclaveStatus = "running"
    StatusSuspended   EnclaveStatus = "suspended"
    StatusTerminated  EnclaveStatus = "terminated"
    StatusError       EnclaveStatus = "error"
)

type EnclaveMeasurements struct {
    MRENCLAVE    string            `json:"mrenclave"`     // SGX measurement
    MRSIGNER     string            `json:"mrsigner"`      // SGX signer
    PCRValues    map[string]string `json:"pcr_values"`    // TPM PCR values
    HashChain    []string          `json:"hash_chain"`    // Boot hash chain
    Attributes   map[string]string `json:"attributes"`    // TEE-specific attributes
    Timestamp    time.Time         `json:"timestamp"`
    Signature    string            `json:"signature"`
}

func NewEnclaveManager(config *EnclaveConfig) (*EnclaveManager, error) {
    manager := &EnclaveManager{
        teeType:        config.TEEType,
        enclaveImage:   config.EnclaveImage,
        activeEnclaves: make(map[string]*EnclaveInstance),
        debugMode:      config.DebugMode,
    }

    // Initialize health checker
    healthChecker, err := NewEnclaveHealthChecker(manager)
    if err != nil {
        return nil, err
    }
    manager.healthChecker = healthChecker

    return manager, nil
}

func (em *EnclaveManager) CreateEnclave(ctx context.Context, name string) (*EnclaveInstance, error) {
    em.enclaveMutex.Lock()
    defer em.enclaveMutex.Unlock()

    // Check if enclave already exists
    if _, exists := em.activeEnclaves[name]; exists {
        return nil, fmt.Errorf("enclave %s already exists", name)
    }

    // Create enclave instance based on TEE type
    var instance *EnclaveInstance
    var err error

    switch em.teeType {
    case "sgx":
        instance, err = em.createSGXEnclave(ctx, name)
    case "sev":
        instance, err = em.createSEVEnclave(ctx, name)
    case "trustzone":
        instance, err = em.createTrustZoneEnclave(ctx, name)
    case "keystone":
        instance, err = em.createKeystoneEnclave(ctx, name)
    default:
        return nil, fmt.Errorf("unsupported TEE type: %s", em.teeType)
    }

    if err != nil {
        return nil, fmt.Errorf("failed to create %s enclave: %w", em.teeType, err)
    }

    // Store and monitor enclave
    em.activeEnclaves[name] = instance
    go em.monitorEnclave(ctx, instance)

    logrus.Infof("Created %s enclave: %s (ID: %s)", em.teeType, name, instance.ID)
    return instance, nil
}

func (em *EnclaveManager) createSGXEnclave(ctx context.Context, name string) (*EnclaveInstance, error) {
    // 1. Load enclave image
    enclaveID, err := em.loadSGXEnclave(em.enclaveImage)
    if err != nil {
        return nil, fmt.Errorf("failed to load SGX enclave: %w", err)
    }

    // 2. Initialize enclave
    if err := em.initializeSGXEnclave(enclaveID); err != nil {
        return nil, fmt.Errorf("failed to initialize SGX enclave: %w", err)
    }

    // 3. Get enclave measurements
    measurements, err := em.getSGXMeasurements(enclaveID)
    if err != nil {
        return nil, fmt.Errorf("failed to get SGX measurements: %w", err)
    }

    // 4. Create secure channel
    secureChannel, err := em.createSGXSecureChannel(enclaveID)
    if err != nil {
        return nil, fmt.Errorf("failed to create SGX secure channel: %w", err)
    }

    instance := &EnclaveInstance{
        ID:            enclaveID,
        TEEType:       "sgx",
        Image:         em.enclaveImage,
        Status:        StatusInitialized,
        CreatedAt:     time.Now(),
        Measurements:  measurements,
        SecureChannel: secureChannel,
        PerformanceData: &EnclavePerformance{},
    }

    return instance, nil
}
```

### 2. Attestation Flow and Verification

#### A. Attestation Service

```go
// NEW: pkg/nautilus/attestation.go
type AttestationService struct {
    teeType          string
    attestationURL   string
    quoteProvider    string
    pcrPolicy        map[string]string
    keyManager       *KeyManager
    verificationCache sync.Map
    attestationQueue chan *AttestationRequest
    workers          []*AttestationWorker
}

type AttestationRequest struct {
    EnclaveID     string                 `json:"enclave_id"`
    RequestType   AttestationRequestType `json:"request_type"`
    Challenge     []byte                 `json:"challenge"`
    UserData      []byte                 `json:"user_data"`
    PolicyID      string                 `json:"policy_id"`
    Timestamp     time.Time              `json:"timestamp"`
    ResponseChan  chan *AttestationResponse `json:"-"`
}

type AttestationRequestType string

const (
    RequestBootAttestation        AttestationRequestType = "boot"
    RequestRuntimeAttestation     AttestationRequestType = "runtime"
    RequestPerformanceAttestation AttestationRequestType = "performance"
    RequestWorkloadAttestation    AttestationRequestType = "workload"
)

type AttestationResponse struct {
    Quote         *TEEQuote         `json:"quote"`
    Certificate   *AttestationCert  `json:"certificate"`
    Measurements  *EnclaveMeasurements `json:"measurements"`
    Evidence      *AttestationEvidence `json:"evidence"`
    Signature     string            `json:"signature"`
    Timestamp     time.Time         `json:"timestamp"`
    ExpiresAt     time.Time         `json:"expires_at"`
    Verified      bool              `json:"verified"`
    Error         error             `json:"error,omitempty"`
}

type TEEQuote struct {
    TEEType      string            `json:"tee_type"`
    Version      string            `json:"version"`
    QuoteData    []byte            `json:"quote_data"`
    Signature    []byte            `json:"signature"`
    Certificates [][]byte          `json:"certificates"`
    Collateral   map[string][]byte `json:"collateral"`
}

type AttestationCert struct {
    Subject       string    `json:"subject"`
    Issuer        string    `json:"issuer"`
    SerialNumber  string    `json:"serial_number"`
    NotBefore     time.Time `json:"not_before"`
    NotAfter      time.Time `json:"not_after"`
    PublicKey     []byte    `json:"public_key"`
    Certificate   []byte    `json:"certificate"`
    CertChain     [][]byte  `json:"cert_chain"`
}

func NewAttestationService(config *AttestationConfig) (*AttestationService, error) {
    service := &AttestationService{
        teeType:          config.TEEType,
        attestationURL:   config.AttestationURL,
        quoteProvider:    config.QuoteProvider,
        pcrPolicy:        config.PCRPolicy,
        keyManager:      config.KeyManager,
        verificationCache: sync.Map{},
        attestationQueue: make(chan *AttestationRequest, 100),
    }

    // Start attestation workers
    for i := 0; i < 3; i++ {
        worker := &AttestationWorker{
            id:      i,
            service: service,
        }
        service.workers = append(service.workers, worker)
        go worker.run()
    }

    return service, nil
}

func (as *AttestationService) RequestAttestation(ctx context.Context, enclaveID string,
                                               requestType AttestationRequestType,
                                               challenge []byte) (*AttestationResponse, error) {

    request := &AttestationRequest{
        EnclaveID:    enclaveID,
        RequestType:  requestType,
        Challenge:    challenge,
        Timestamp:    time.Now(),
        ResponseChan: make(chan *AttestationResponse, 1),
    }

    // Add to queue
    select {
    case as.attestationQueue <- request:
    case <-ctx.Done():
        return nil, ctx.Err()
    case <-time.After(5 * time.Second):
        return nil, fmt.Errorf("attestation queue full")
    }

    // Wait for response
    select {
    case response := <-request.ResponseChan:
        return response, response.Error
    case <-ctx.Done():
        return nil, ctx.Err()
    case <-time.After(30 * time.Second):
        return nil, fmt.Errorf("attestation timeout")
    }
}

func (as *AttestationService) processAttestation(request *AttestationRequest) *AttestationResponse {
    start := time.Now()
    defer func() {
        logrus.Debugf("Attestation for enclave %s took %v", request.EnclaveID[:8], time.Since(start))
    }()

    // 1. Generate quote based on TEE type
    quote, err := as.generateQuote(request.EnclaveID, request.Challenge, request.UserData)
    if err != nil {
        return &AttestationResponse{Error: fmt.Errorf("failed to generate quote: %w", err)}
    }

    // 2. Get enclave measurements
    measurements, err := as.getMeasurements(request.EnclaveID)
    if err != nil {
        return &AttestationResponse{Error: fmt.Errorf("failed to get measurements: %w", err)}
    }

    // 3. Verify quote and measurements
    verified, err := as.verifyQuote(quote, measurements)
    if err != nil {
        return &AttestationResponse{Error: fmt.Errorf("failed to verify quote: %w", err)}
    }

    // 4. Generate attestation certificate
    cert, err := as.generateAttestationCert(quote, measurements)
    if err != nil {
        return &AttestationResponse{Error: fmt.Errorf("failed to generate certificate: %w", err)}
    }

    // 5. Create attestation evidence
    evidence, err := as.createAttestationEvidence(quote, measurements, cert)
    if err != nil {
        return &AttestationResponse{Error: fmt.Errorf("failed to create evidence: %w", err)}
    }

    // 6. Sign with TEE key
    signature, err := as.keyManager.SignAttestation(evidence)
    if err != nil {
        return &AttestationResponse{Error: fmt.Errorf("failed to sign attestation: %w", err)}
    }

    response := &AttestationResponse{
        Quote:        quote,
        Certificate:  cert,
        Measurements: measurements,
        Evidence:     evidence,
        Signature:    signature,
        Timestamp:    time.Now(),
        ExpiresAt:    time.Now().Add(24 * time.Hour),
        Verified:     verified,
    }

    return response
}

func (as *AttestationService) generateQuote(enclaveID string, challenge, userData []byte) (*TEEQuote, error) {
    switch as.teeType {
    case "sgx":
        return as.generateSGXQuote(enclaveID, challenge, userData)
    case "sev":
        return as.generateSEVQuote(enclaveID, challenge, userData)
    case "trustzone":
        return as.generateTrustZoneQuote(enclaveID, challenge, userData)
    case "keystone":
        return as.generateKeystoneQuote(enclaveID, challenge, userData)
    default:
        return nil, fmt.Errorf("unsupported TEE type: %s", as.teeType)
    }
}

func (as *AttestationService) generateSGXQuote(enclaveID string, challenge, userData []byte) (*TEEQuote, error) {
    // 1. Prepare report data (challenge + user data)
    reportData := make([]byte, 64) // SGX report data is 64 bytes
    copy(reportData, challenge)
    if len(userData) > 0 {
        hash := sha256.Sum256(userData)
        copy(reportData[32:], hash[:])
    }

    // 2. Get enclave report
    report, err := as.getSGXReport(enclaveID, reportData)
    if err != nil {
        return nil, fmt.Errorf("failed to get SGX report: %w", err)
    }

    // 3. Generate quote from report
    quoteData, signature, certificates, err := as.getSGXQuote(report)
    if err != nil {
        return nil, fmt.Errorf("failed to get SGX quote: %w", err)
    }

    // 4. Get collateral (certificates, CRL, etc.)
    collateral, err := as.getSGXCollateral(certificates)
    if err != nil {
        logrus.Warnf("Failed to get SGX collateral: %v", err)
        collateral = make(map[string][]byte)
    }

    quote := &TEEQuote{
        TEEType:      "sgx",
        Version:      "3.0",
        QuoteData:    quoteData,
        Signature:    signature,
        Certificates: certificates,
        Collateral:   collateral,
    }

    return quote, nil
}
```

### 3. Performance Metrics Collection

#### A. Secure Performance Monitor

```go
// NEW: pkg/nautilus/performance.go
type PerformanceMonitor struct {
    metricsInterval   time.Duration
    teeEnabled       bool
    secureMetrics    bool
    collectors       map[string]MetricsCollector
    aggregator       *MetricsAggregator
    enclave          *EnclaveInstance
    attestationSvc   *AttestationService
    storage          *MetricsStorage
    scheduler        *MetricsScheduler
}

type MetricsCollector interface {
    CollectMetrics(ctx context.Context) (*PerformanceData, error)
    GetCollectorType() string
    IsSecure() bool
}

type PerformanceData struct {
    Timestamp        time.Time                   `json:"timestamp"`
    NodeID          string                      `json:"node_id"`
    EnclaveID       string                      `json:"enclave_id,omitempty"`
    SystemMetrics   *SystemMetrics              `json:"system_metrics"`
    ContainerMetrics *ContainerMetrics           `json:"container_metrics"`
    NetworkMetrics  *NetworkMetrics             `json:"network_metrics"`
    SecurityMetrics *SecurityMetrics            `json:"security_metrics"`
    CustomMetrics   map[string]interface{}      `json:"custom_metrics"`
    Attestation     *AttestationResponse        `json:"attestation,omitempty"`
    Signature       string                      `json:"signature"`
    Hash            string                      `json:"hash"`
}

type SystemMetrics struct {
    CPUUsage         float64           `json:"cpu_usage"`
    MemoryUsage      float64           `json:"memory_usage"`
    DiskUsage        float64           `json:"disk_usage"`
    NetworkIO        *NetworkIOStats   `json:"network_io"`
    DiskIO           *DiskIOStats      `json:"disk_io"`
    LoadAverage      []float64         `json:"load_average"`
    ProcessCount     int               `json:"process_count"`
    ThreadCount      int               `json:"thread_count"`
    FileDescriptors  int               `json:"file_descriptors"`
    ContextSwitches  int64             `json:"context_switches"`
    Interrupts       int64             `json:"interrupts"`
    TEEStatus        *TEEStatusMetrics `json:"tee_status,omitempty"`
}

type ContainerMetrics struct {
    TotalContainers  int                          `json:"total_containers"`
    RunningContainers int                         `json:"running_containers"`
    PodMetrics       map[string]*PodMetrics       `json:"pod_metrics"`
    ImagePullMetrics *ImagePullMetrics            `json:"image_pull_metrics"`
}

type NetworkMetrics struct {
    ThroughputIn     float64           `json:"throughput_in"`
    ThroughputOut    float64           `json:"throughput_out"`
    PacketsIn        int64             `json:"packets_in"`
    PacketsOut       int64             `json:"packets_out"`
    ErrorsIn         int64             `json:"errors_in"`
    ErrorsOut        int64             `json:"errors_out"`
    ConnectionCount  int               `json:"connection_count"`
    Latency          map[string]float64 `json:"latency"`
}

type SecurityMetrics struct {
    TEEIntegrity     bool              `json:"tee_integrity"`
    EnclaveTrust     float64           `json:"enclave_trust"`
    AttestationValid bool              `json:"attestation_valid"`
    SecurityEvents   []*SecurityEvent  `json:"security_events"`
    ThreatLevel      string            `json:"threat_level"`
    ComplianceScore  float64           `json:"compliance_score"`
}

func NewPerformanceMonitor(config *PerformanceConfig) (*PerformanceMonitor, error) {
    monitor := &PerformanceMonitor{
        metricsInterval: config.MetricsInterval,
        teeEnabled:     config.TEEEnabled,
        secureMetrics:  config.SecureMetrics,
        collectors:     make(map[string]MetricsCollector),
    }

    // Initialize collectors
    if err := monitor.initializeCollectors(); err != nil {
        return nil, fmt.Errorf("failed to initialize collectors: %w", err)
    }

    // Initialize aggregator
    aggregator, err := NewMetricsAggregator(&AggregatorConfig{
        BufferSize:     1000,
        FlushInterval: 30 * time.Second,
        TEEEnabled:    config.TEEEnabled,
    })
    if err != nil {
        return nil, err
    }
    monitor.aggregator = aggregator

    // Initialize storage
    storage, err := NewMetricsStorage(&StorageConfig{
        StorageType:    "encrypted",
        RetentionDays: 30,
        CompressionEnabled: true,
    })
    if err != nil {
        return nil, err
    }
    monitor.storage = storage

    return monitor, nil
}

func (pm *PerformanceMonitor) StartCollection(ctx context.Context) error {
    // Start metrics collection routine
    go pm.collectionRoutine(ctx)

    // Start aggregation routine
    go pm.aggregator.Start(ctx)

    // Start storage routine
    go pm.storage.Start(ctx)

    logrus.Info("Performance monitoring started with TEE attestation")
    return nil
}

func (pm *PerformanceMonitor) collectionRoutine(ctx context.Context) {
    ticker := time.NewTicker(pm.metricsInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := pm.collectAndProcess(ctx); err != nil {
                logrus.Errorf("Failed to collect metrics: %v", err)
            }
        }
    }
}

func (pm *PerformanceMonitor) collectAndProcess(ctx context.Context) error {
    startTime := time.Now()

    // 1. Collect metrics from all collectors
    metricsData := &PerformanceData{
        Timestamp:      startTime,
        NodeID:        pm.getNodeID(),
        CustomMetrics: make(map[string]interface{}),
    }

    // Collect system metrics
    if collector, exists := pm.collectors["system"]; exists {
        systemData, err := collector.CollectMetrics(ctx)
        if err != nil {
            logrus.Errorf("Failed to collect system metrics: %v", err)
        } else {
            metricsData.SystemMetrics = systemData.SystemMetrics
        }
    }

    // Collect container metrics
    if collector, exists := pm.collectors["container"]; exists {
        containerData, err := collector.CollectMetrics(ctx)
        if err != nil {
            logrus.Errorf("Failed to collect container metrics: %v", err)
        } else {
            metricsData.ContainerMetrics = containerData.ContainerMetrics
        }
    }

    // Collect network metrics
    if collector, exists := pm.collectors["network"]; exists {
        networkData, err := collector.CollectMetrics(ctx)
        if err != nil {
            logrus.Errorf("Failed to collect network metrics: %v", err)
        } else {
            metricsData.NetworkMetrics = networkData.NetworkMetrics
        }
    }

    // 2. Get TEE attestation if enabled
    if pm.teeEnabled && pm.attestationSvc != nil {
        challenge := pm.generateChallenge(metricsData)
        attestation, err := pm.attestationSvc.RequestAttestation(ctx, pm.enclave.ID,
                                                                RequestPerformanceAttestation, challenge)
        if err != nil {
            logrus.Errorf("Failed to get performance attestation: %v", err)
        } else {
            metricsData.Attestation = attestation
        }
    }

    // 3. Sign metrics data
    if err := pm.signMetricsData(metricsData); err != nil {
        logrus.Errorf("Failed to sign metrics data: %v", err)
    }

    // 4. Send to aggregator
    pm.aggregator.AddMetrics(metricsData)

    collectionTime := time.Since(startTime)
    logrus.Debugf("Metrics collection completed in %v", collectionTime)

    return nil
}

func (pm *PerformanceMonitor) signMetricsData(data *PerformanceData) error {
    // 1. Serialize metrics data
    dataBytes, err := json.Marshal(data)
    if err != nil {
        return err
    }

    // 2. Calculate hash
    hasher := sha256.New()
    hasher.Write(dataBytes)
    data.Hash = hex.EncodeToString(hasher.Sum(nil))

    // 3. Sign with TEE key or Seal wallet
    if pm.teeEnabled && pm.enclave != nil {
        signature, err := pm.signWithTEE(dataBytes)
        if err != nil {
            return err
        }
        data.Signature = signature
    } else {
        // Fallback to Seal signature
        signature, err := pm.signWithSeal(dataBytes)
        if err != nil {
            return err
        }
        data.Signature = signature
    }

    return nil
}
```

#### B. Specialized Collectors

```go
// System Metrics Collector
type SystemMetricsCollector struct {
    nodeID     string
    teeEnabled bool
    lastStats  *SystemStats
}

func (smc *SystemMetricsCollector) CollectMetrics(ctx context.Context) (*PerformanceData, error) {
    // 1. CPU metrics
    cpuUsage, err := smc.getCPUUsage()
    if err != nil {
        return nil, err
    }

    // 2. Memory metrics
    memUsage, err := smc.getMemoryUsage()
    if err != nil {
        return nil, err
    }

    // 3. Disk metrics
    diskUsage, err := smc.getDiskUsage()
    if err != nil {
        return nil, err
    }

    // 4. Network I/O
    networkIO, err := smc.getNetworkIO()
    if err != nil {
        return nil, err
    }

    // 5. TEE-specific metrics
    var teeStatus *TEEStatusMetrics
    if smc.teeEnabled {
        teeStatus, err = smc.getTEEStatus()
        if err != nil {
            logrus.Warnf("Failed to get TEE status: %v", err)
        }
    }

    systemMetrics := &SystemMetrics{
        CPUUsage:        cpuUsage,
        MemoryUsage:     memUsage,
        DiskUsage:       diskUsage,
        NetworkIO:       networkIO,
        LoadAverage:     smc.getLoadAverage(),
        ProcessCount:    smc.getProcessCount(),
        ThreadCount:     smc.getThreadCount(),
        FileDescriptors: smc.getFileDescriptorCount(),
        TEEStatus:       teeStatus,
    }

    return &PerformanceData{
        SystemMetrics: systemMetrics,
    }, nil
}

// Container Metrics Collector
type ContainerMetricsCollector struct {
    containerdAddr string
    criClient      runtimeapi.RuntimeServiceClient
    imageClient    runtimeapi.ImageServiceClient
}

func (cmc *ContainerMetricsCollector) CollectMetrics(ctx context.Context) (*PerformanceData, error) {
    // 1. Get container stats
    containerStats, err := cmc.getContainerStats(ctx)
    if err != nil {
        return nil, err
    }

    // 2. Get pod metrics
    podMetrics, err := cmc.getPodMetrics(ctx)
    if err != nil {
        return nil, err
    }

    // 3. Get image pull metrics
    imagePullMetrics, err := cmc.getImagePullMetrics(ctx)
    if err != nil {
        return nil, err
    }

    containerMetrics := &ContainerMetrics{
        TotalContainers:   containerStats.Total,
        RunningContainers: containerStats.Running,
        PodMetrics:       podMetrics,
        ImagePullMetrics: imagePullMetrics,
    }

    return &PerformanceData{
        ContainerMetrics: containerMetrics,
    }, nil
}

// Security Metrics Collector
type SecurityMetricsCollector struct {
    attestationSvc *AttestationService
    enclave       *EnclaveInstance
    eventStore    *SecurityEventStore
}

func (smc *SecurityMetricsCollector) CollectMetrics(ctx context.Context) (*PerformanceData, error) {
    securityMetrics := &SecurityMetrics{
        TEEIntegrity:     true,
        EnclaveTrust:     1.0,
        AttestationValid: false,
        SecurityEvents:   []*SecurityEvent{},
        ThreatLevel:      "low",
        ComplianceScore:  1.0,
    }

    // 1. Check TEE integrity
    if smc.enclave != nil {
        integrity, trust := smc.checkTEEIntegrity()
        securityMetrics.TEEIntegrity = integrity
        securityMetrics.EnclaveTrust = trust
    }

    // 2. Verify recent attestation
    if smc.attestationSvc != nil {
        valid := smc.checkRecentAttestation()
        securityMetrics.AttestationValid = valid
    }

    // 3. Get security events
    events := smc.eventStore.GetRecentEvents(time.Hour)
    securityMetrics.SecurityEvents = events

    // 4. Calculate threat level
    securityMetrics.ThreatLevel = smc.calculateThreatLevel(events)

    // 5. Calculate compliance score
    securityMetrics.ComplianceScore = smc.calculateComplianceScore()

    return &PerformanceData{
        SecurityMetrics: securityMetrics,
    }, nil
}
```

### 4. Proof Generation for On-Chain Verification

#### A. Cryptographic Proof Generator

```go
// NEW: pkg/nautilus/proofs.go
type ProofGenerator struct {
    teeType        string
    keyManager     *KeyManager
    attestationSvc *AttestationService
    merkleTree     *MerkleTree
    proofBatcher   *ProofBatcher
    verificationKey *VerificationKey
}

type PerformanceProof struct {
    ProofID         string                 `json:"proof_id"`
    NodeID          string                 `json:"node_id"`
    TEEType         string                 `json:"tee_type"`
    TimeRange       *TimeRange             `json:"time_range"`
    MetricsHash     string                 `json:"metrics_hash"`
    AttestationHash string                 `json:"attestation_hash"`
    MerkleRoot      string                 `json:"merkle_root"`
    MerkleProof     []string               `json:"merkle_proof"`
    TEESignature    string                 `json:"tee_signature"`
    SealSignature   string                 `json:"seal_signature"`
    ZKProof         *ZKPerformanceProof    `json:"zk_proof,omitempty"`
    Timestamp       time.Time              `json:"timestamp"`
    ExpiresAt       time.Time              `json:"expires_at"`
    VerificationKey string                 `json:"verification_key"`
}

type ZKPerformanceProof struct {
    Proof        []byte            `json:"proof"`
    PublicInputs []string          `json:"public_inputs"`
    Circuit      string            `json:"circuit"`
    Constraints  map[string]string `json:"constraints"`
}

type TimeRange struct {
    Start    time.Time `json:"start"`
    End      time.Time `json:"end"`
    Duration int64     `json:"duration"` // seconds
}

func NewProofGenerator(config *ProofConfig) (*ProofGenerator, error) {
    // 1. Initialize Merkle tree for batch proofs
    merkleTree, err := NewMerkleTree()
    if err != nil {
        return nil, err
    }

    // 2. Initialize proof batcher
    batcher, err := NewProofBatcher(&BatcherConfig{
        BatchSize:     config.BatchSize,
        FlushInterval: config.FlushInterval,
        MaxWaitTime:   config.MaxWaitTime,
    })
    if err != nil {
        return nil, err
    }

    return &ProofGenerator{
        teeType:        config.TEEType,
        keyManager:     config.KeyManager,
        attestationSvc: config.AttestationSvc,
        merkleTree:     merkleTree,
        proofBatcher:   batcher,
    }, nil
}

func (pg *ProofGenerator) GeneratePerformanceProof(ctx context.Context,
                                                  metricsData []*PerformanceData,
                                                  timeRange *TimeRange) (*PerformanceProof, error) {

    proofID := pg.generateProofID()

    // 1. Create metrics hash
    metricsHash, err := pg.hashMetrics(metricsData)
    if err != nil {
        return nil, fmt.Errorf("failed to hash metrics: %w", err)
    }

    // 2. Get attestation for this proof
    challenge := pg.createAttestationChallenge(proofID, metricsHash, timeRange)
    attestation, err := pg.attestationSvc.RequestAttestation(ctx,
                                                            pg.getEnclaveID(),
                                                            RequestPerformanceAttestation,
                                                            challenge)
    if err != nil {
        return nil, fmt.Errorf("failed to get attestation: %w", err)
    }

    attestationHash := pg.hashAttestation(attestation)

    // 3. Create Merkle proof
    leaves := []string{metricsHash, attestationHash}
    merkleRoot, merkleProof, err := pg.merkleTree.GenerateProof(leaves)
    if err != nil {
        return nil, fmt.Errorf("failed to generate Merkle proof: %w", err)
    }

    // 4. Generate ZK proof for privacy
    zkProof, err := pg.generateZKProof(metricsData, timeRange)
    if err != nil {
        logrus.Warnf("Failed to generate ZK proof: %v", err)
        // Continue without ZK proof
    }

    // 5. Create proof structure
    proof := &PerformanceProof{
        ProofID:         proofID,
        NodeID:          pg.getNodeID(),
        TEEType:         pg.teeType,
        TimeRange:       timeRange,
        MetricsHash:     metricsHash,
        AttestationHash: attestationHash,
        MerkleRoot:      merkleRoot,
        MerkleProof:     merkleProof,
        ZKProof:         zkProof,
        Timestamp:       time.Now(),
        ExpiresAt:       time.Now().Add(24 * time.Hour),
    }

    // 6. Sign with TEE
    if err := pg.signProofWithTEE(proof); err != nil {
        return nil, fmt.Errorf("failed to sign with TEE: %w", err)
    }

    // 7. Sign with Seal wallet
    if err := pg.signProofWithSeal(proof); err != nil {
        return nil, fmt.Errorf("failed to sign with Seal: %w", err)
    }

    return proof, nil
}

func (pg *ProofGenerator) generateZKProof(metricsData []*PerformanceData,
                                        timeRange *TimeRange) (*ZKPerformanceProof, error) {

    // 1. Define performance constraints
    constraints := map[string]string{
        "min_cpu_efficiency":    "0.8",
        "max_memory_usage":      "0.9",
        "min_uptime_percentage": "0.99",
        "max_response_time":     "100ms",
    }

    // 2. Prepare public inputs (performance claims)
    publicInputs := []string{
        pg.calculateAverageCPU(metricsData),
        pg.calculateAverageMemory(metricsData),
        pg.calculateUptime(metricsData),
        pg.calculateResponseTime(metricsData),
    }

    // 3. Generate ZK proof (using appropriate ZK system)
    circuit := "performance_verification_circuit"
    proof, err := pg.generateZKProofWithCircuit(circuit, publicInputs, constraints, metricsData)
    if err != nil {
        return nil, err
    }

    return &ZKPerformanceProof{
        Proof:        proof,
        PublicInputs: publicInputs,
        Circuit:      circuit,
        Constraints:  constraints,
    }, nil
}

func (pg *ProofGenerator) VerifyProof(ctx context.Context, proof *PerformanceProof) (bool, error) {
    // 1. Verify timestamps
    if time.Now().After(proof.ExpiresAt) {
        return false, fmt.Errorf("proof has expired")
    }

    // 2. Verify Merkle proof
    valid := pg.merkleTree.VerifyProof(proof.MerkleRoot, proof.MerkleProof,
                                     []string{proof.MetricsHash, proof.AttestationHash})
    if !valid {
        return false, fmt.Errorf("invalid Merkle proof")
    }

    // 3. Verify TEE signature
    teeValid, err := pg.verifyTEESignature(proof)
    if err != nil || !teeValid {
        return false, fmt.Errorf("invalid TEE signature: %w", err)
    }

    // 4. Verify Seal signature
    sealValid, err := pg.verifySealSignature(proof)
    if err != nil || !sealValid {
        return false, fmt.Errorf("invalid Seal signature: %w", err)
    }

    // 5. Verify ZK proof if present
    if proof.ZKProof != nil {
        zkValid, err := pg.verifyZKProof(proof.ZKProof)
        if err != nil || !zkValid {
            return false, fmt.Errorf("invalid ZK proof: %w", err)
        }
    }

    return true, nil
}
```

### 5. Secure TEE Communication Protocol

#### A. Secure Communication Layer

```go
// NEW: pkg/nautilus/secure_comm.go
type SecureComm struct {
    teeType         string
    secureChannels  bool
    keyManager      *KeyManager
    channels        sync.Map // map[string]*SecureChannel
    sessionManager  *SessionManager
    messageQueue    chan *SecureMessage
    workers         []*CommWorker
}

type SecureChannel struct {
    ChannelID       string            `json:"channel_id"`
    LocalID         string            `json:"local_id"`
    RemoteID        string            `json:"remote_id"`
    SessionKey      []byte            `json:"-"` // Never serialize
    MacKey          []byte            `json:"-"` // Never serialize
    CreatedAt       time.Time         `json:"created_at"`
    LastUsed        time.Time         `json:"last_used"`
    MessageCount    int64             `json:"message_count"`
    Status          ChannelStatus     `json:"status"`
    Authenticated   bool              `json:"authenticated"`
    Attestation     *AttestationResponse `json:"attestation,omitempty"`
}

type SecureMessage struct {
    MessageID    string            `json:"message_id"`
    ChannelID    string            `json:"channel_id"`
    MessageType  MessageType       `json:"message_type"`
    Payload      []byte            `json:"payload"`
    MAC          []byte            `json:"mac"`
    Timestamp    time.Time         `json:"timestamp"`
    SequenceNum  int64             `json:"sequence_num"`
    Encrypted    bool              `json:"encrypted"`
    Compressed   bool              `json:"compressed"`
}

type MessageType string

const (
    MessageHandshake        MessageType = "handshake"
    MessageAttestation      MessageType = "attestation"
    MessagePerformanceData  MessageType = "performance_data"
    MessageProofRequest     MessageType = "proof_request"
    MessageProofResponse    MessageType = "proof_response"
    MessageHeartbeat        MessageType = "heartbeat"
    MessageKeyRotation      MessageType = "key_rotation"
    MessageTermination      MessageType = "termination"
)

func NewSecureComm(config *SecureCommConfig) (*SecureComm, error) {
    sc := &SecureComm{
        teeType:        config.TEEType,
        secureChannels: config.SecureChannels,
        keyManager:     config.KeyManager,
        channels:       sync.Map{},
        messageQueue:   make(chan *SecureMessage, 1000),
    }

    // Initialize session manager
    sessionManager, err := NewSessionManager(&SessionConfig{
        SessionTimeout: 24 * time.Hour,
        MaxSessions:   100,
        KeyRotationInterval: 4 * time.Hour,
    })
    if err != nil {
        return nil, err
    }
    sc.sessionManager = sessionManager

    // Start communication workers
    for i := 0; i < 3; i++ {
        worker := &CommWorker{
            id:      i,
            secComm: sc,
        }
        sc.workers = append(sc.workers, worker)
        go worker.run()
    }

    return sc, nil
}

func (sc *SecureComm) EstablishChannel(ctx context.Context, remoteID string,
                                     attestation *AttestationResponse) (*SecureChannel, error) {

    channelID := sc.generateChannelID(remoteID)

    // 1. Perform handshake
    sessionKey, macKey, err := sc.performHandshake(ctx, remoteID, attestation)
    if err != nil {
        return nil, fmt.Errorf("handshake failed: %w", err)
    }

    // 2. Create secure channel
    channel := &SecureChannel{
        ChannelID:     channelID,
        LocalID:       sc.getLocalID(),
        RemoteID:      remoteID,
        SessionKey:    sessionKey,
        MacKey:        macKey,
        CreatedAt:     time.Now(),
        LastUsed:      time.Now(),
        Status:        ChannelStatusActive,
        Authenticated: true,
        Attestation:   attestation,
    }

    // 3. Store channel
    sc.channels.Store(channelID, channel)

    // 4. Register with session manager
    sc.sessionManager.RegisterSession(channelID, channel)

    logrus.Infof("Established secure channel %s with %s", channelID[:8], remoteID)
    return channel, nil
}

func (sc *SecureComm) SendMessage(ctx context.Context, channelID string,
                                messageType MessageType, payload []byte) error {

    // 1. Get channel
    channelInterface, exists := sc.channels.Load(channelID)
    if !exists {
        return fmt.Errorf("channel %s not found", channelID)
    }
    channel := channelInterface.(*SecureChannel)

    // 2. Create message
    message := &SecureMessage{
        MessageID:   sc.generateMessageID(),
        ChannelID:   channelID,
        MessageType: messageType,
        Payload:     payload,
        Timestamp:   time.Now(),
        SequenceNum: atomic.AddInt64(&channel.MessageCount, 1),
        Encrypted:   sc.secureChannels,
        Compressed:  len(payload) > 1024, // Compress large payloads
    }

    // 3. Process message
    if err := sc.processOutboundMessage(message, channel); err != nil {
        return fmt.Errorf("failed to process message: %w", err)
    }

    // 4. Queue for transmission
    select {
    case sc.messageQueue <- message:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(5 * time.Second):
        return fmt.Errorf("message queue full")
    }
}

func (sc *SecureComm) processOutboundMessage(message *SecureMessage, channel *SecureChannel) error {
    // 1. Compress if needed
    payload := message.Payload
    if message.Compressed {
        compressed, err := sc.compressPayload(payload)
        if err != nil {
            return fmt.Errorf("compression failed: %w", err)
        }
        payload = compressed
    }

    // 2. Encrypt if secure channels enabled
    if message.Encrypted && sc.secureChannels {
        encrypted, err := sc.encryptPayload(payload, channel.SessionKey)
        if err != nil {
            return fmt.Errorf("encryption failed: %w", err)
        }
        payload = encrypted
    }

    message.Payload = payload

    // 3. Generate MAC
    mac, err := sc.generateMAC(message, channel.MacKey)
    if err != nil {
        return fmt.Errorf("MAC generation failed: %w", err)
    }
    message.MAC = mac

    // 4. Update channel
    channel.LastUsed = time.Now()

    return nil
}

func (sc *SecureComm) ReceiveMessage(ctx context.Context, data []byte) (*SecureMessage, error) {
    // 1. Deserialize message
    var message SecureMessage
    if err := json.Unmarshal(data, &message); err != nil {
        return nil, fmt.Errorf("failed to deserialize message: %w", err)
    }

    // 2. Get channel
    channelInterface, exists := sc.channels.Load(message.ChannelID)
    if !exists {
        return nil, fmt.Errorf("unknown channel: %s", message.ChannelID)
    }
    channel := channelInterface.(*SecureChannel)

    // 3. Verify MAC
    if err := sc.verifyMAC(&message, channel.MacKey); err != nil {
        return nil, fmt.Errorf("MAC verification failed: %w", err)
    }

    // 4. Process inbound message
    if err := sc.processInboundMessage(&message, channel); err != nil {
        return nil, fmt.Errorf("failed to process message: %w", err)
    }

    return &message, nil
}

func (sc *SecureComm) processInboundMessage(message *SecureMessage, channel *SecureChannel) error {
    // 1. Check sequence number
    if message.SequenceNum <= channel.MessageCount {
        return fmt.Errorf("replay attack detected: invalid sequence number")
    }

    // 2. Decrypt if encrypted
    payload := message.Payload
    if message.Encrypted && sc.secureChannels {
        decrypted, err := sc.decryptPayload(payload, channel.SessionKey)
        if err != nil {
            return fmt.Errorf("decryption failed: %w", err)
        }
        payload = decrypted
    }

    // 3. Decompress if compressed
    if message.Compressed {
        decompressed, err := sc.decompressPayload(payload)
        if err != nil {
            return fmt.Errorf("decompression failed: %w", err)
        }
        payload = decompressed
    }

    message.Payload = payload

    // 4. Update channel
    channel.LastUsed = time.Now()
    channel.MessageCount = message.SequenceNum

    return nil
}
```

### 6. Attestation Flow Diagrams

#### A. Boot Attestation Flow

```
K3s Agent          Nautilus TEE           Attestation Service         Sui Network
    |                     |                         |                        |
    |--1. Agent Start---->|                         |                        |
    |                     |--2. Initialize TEE----->|                        |
    |                     |<--3. TEE Ready----------|                        |
    |                     |                         |                        |
    |--4. Request Boot--->|                         |                        |
    |    Attestation      |--5. Generate Quote----->|                        |
    |                     |<--6. Quote + Cert-------|                        |
    |<--7. Attestation----|                         |                        |
    |    Response         |                         |                        |
    |                     |                         |                        |
    |--8. Submit to Sui----------------------------------------->|
    |<--9. On-chain Verification----------------------------------|
    |                     |                         |                        |
    |--10. Agent Ready--->|                         |                        |
```

#### B. Performance Attestation Flow

```
Performance Monitor   Nautilus TEE       Metrics Collector       Proof Generator      Sui Contract
        |                   |                    |                      |                   |
        |--1. Collect------->|                    |                      |                   |
        |   Metrics          |--2. Secure------->|                      |                   |
        |                    |   Collection       |                      |                   |
        |                    |<--3. TEE Metrics---|                      |                   |
        |<--4. Signed--------|                    |                      |                   |
        |    Metrics         |                    |                      |                   |
        |                    |                    |                      |                   |
        |--5. Generate Proof---------------------------->|                   |
        |                    |                    |      |--6. Request--->|                   |
        |                    |                    |      |   Attestation  |                   |
        |                    |<-------------------       |<--7. TEE Quote-|                   |
        |                    |                    |      |                |                   |
        |<--8. Performance Proof-------------------------|                   |
        |                    |                    |                      |                   |
        |--9. Submit Proof to Sui---------------------------------------------->|
        |<--10. Reward Calculation--------------------------------------------|
```

#### C. Runtime Attestation Flow

```
Container Runtime     Nautilus TEE        Security Monitor       Sui Validator
        |                   |                    |                    |
        |--1. Container----->|                    |                    |
        |   Start            |--2. Attest-------->|                    |
        |                    |   Container        |                    |
        |                    |<--3. Security------|                    |
        |                    |    Evidence        |                    |
        |<--2. Secured-------|                    |                    |
        |    Container       |                    |                    |
        |                    |                    |                    |
        |--3. Runtime Proof---------------------->|                    |
        |                    |                    |--4. Validate------>|
        |                    |                    |<--5. Verification--|
        |<--4. Validation--------------------------------|                    |
        |    Result          |                    |                    |
```

## Integration Points in K3s Agent Lifecycle

### Key Modification Points

1. **pkg/agent/run.go:58-63** - Agent initialization with TEE setup
2. **pkg/agent/run.go:109-111** - Bootstrap with attestation
3. **pkg/agent/run.go:123-127** - Metrics with TEE integration
4. **pkg/daemons/agent/agent.go** - Runtime attestation hooks
5. **pkg/metrics/** - Enhanced metrics with TEE signatures

### Configuration Integration

```yaml
daas:
  nautilus:
    enabled: true
    tee_type: "sgx"  # sgx, sev, trustzone, keystone
    enclave_image: "/opt/nautilus/k3s-enclave.signed"
    attestation_url: "https://attestation.nautilus.network"
    metrics_interval: "30s"
    proof_batch_size: 100
    secure_channels: true
    pcr_policy:
      pcr0: "expected_pcr0_value"
      pcr1: "expected_pcr1_value"
```

This comprehensive Nautilus TEE integration provides hardware-verified performance attestation, secure computation environments, and cryptographic proofs that enable trustless verification of DaaS worker node performance and security guarantees.