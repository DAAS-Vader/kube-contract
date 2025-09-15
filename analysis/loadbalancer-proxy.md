# K3s Load Balancer and Proxy System Analysis

## Overview
This document analyzes the K3s load balancer system to understand how to integrate Nautilus-based server selection, DaaS control plane connectivity, and custom health checks for blockchain nodes.

## Current Load Balancer Architecture

### 1. Load Balancer Core (`pkg/agent/loadbalancer/loadbalancer.go`)

#### A. LoadBalancer Structure
```go
type LoadBalancer struct {
    serviceName  string        // Service identifier (k3s-agent-load-balancer, etc.)
    configFile   string        // Persistent config file path
    scheme       string        // URL scheme (http/https)
    localAddress string        // Local listening address (127.0.0.1:6444)
    servers      serverList    // Managed server list with health tracking
    proxy        *tcpproxy.Proxy  // TCP proxy for connection forwarding
}
```

#### B. Service Types
```go
var (
    SupervisorServiceName = version.Program + "-agent-load-balancer"          // k3s-agent-load-balancer
    APIServerServiceName  = version.Program + "-api-server-agent-load-balancer" // k3s-api-server-agent-load-balancer
    ETCDServerServiceName = version.Program + "-etcd-server-load-balancer"    // k3s-etcd-server-load-balancer
)
```

#### C. Connection Flow
```go
// LoadBalancer creation and startup
func New(ctx context.Context, dataDir, serviceName, defaultServerURL string,
         lbServerPort int, isIPv6 bool) (*LoadBalancer, error) {
    // 1. Create local TCP listener (127.0.0.1:6444 or [::1]:6444)
    listener, err := config.Listen(ctx, "tcp", localAddress)

    // 2. Parse and validate default server URL
    serverURL, err := url.Parse(defaultServerURL)

    // 3. Initialize server list with default server
    lb.servers.setDefaultAddress(lb.serviceName, serverURL.Host)

    // 4. Set up TCP proxy with custom dial context
    lb.proxy = &tcpproxy.Proxy{
        ListenFunc: func(string, string) (net.Listener, error) {
            return listener, nil
        },
    }

    // 5. Add route with intelligent dialing
    lb.proxy.AddRoute(serviceName, &tcpproxy.DialProxy{
        Addr:        serviceName,
        OnDialError: onDialError,
        DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
            start := time.Now()
            conn, err := lb.servers.dialContext(ctx, network, address)
            metrics.ObserveWithStatus(loadbalancerDials, start, err, serviceName)
            return conn, err
        },
    })

    // 6. Start health checking goroutine
    go lb.servers.runHealthChecks(ctx, lb.serviceName)
}
```

### 2. Server Selection Logic (`pkg/agent/loadbalancer/servers.go`)

#### A. Server State Machine
```go
// Server states in increasing order of preference
type state int
const (
    stateInvalid    state = iota  // 0 - Server being removed
    stateFailed                   // 1 - Failed health check or dial
    stateStandby                  // 2 - Default server not in active list
    stateUnchecked                // 3 - Just added, not yet health checked
    stateRecovering               // 4 - Successfully checked once
    stateHealthy                  // 5 - Normal healthy state
    statePreferred                // 6 - Recently recovered, preferred over others
    stateActive                   // 7 - Currently handling connections
)
```

#### B. Server Selection Algorithm
```go
// Servers are sorted by state (descending) then connection count (descending)
func compareServers(a, b *server) int {
    c := cmp.Compare(b.state, a.state)  // Higher state first
    if c == 0 {
        return cmp.Compare(len(b.connections), len(a.connections))  // More connections first
    }
    return c
}

// dialContext tries servers in sorted order until one succeeds
func (sl *serverList) dialContext(ctx context.Context, network, _ string) (net.Conn, error) {
    for _, s := range sl.getServers() {  // Already sorted by preference
        conn, err := s.dialContext(ctx, network)
        if err == nil {
            sl.recordSuccess(s, reasonDial)
            return conn, nil
        }
        sl.recordFailure(s, reasonDial)
    }
    return nil, errors.New("all servers failed")
}
```

#### C. State Transition Logic
```go
// Success transitions
func (sl *serverList) recordSuccess(srv *server, r reason) {
    switch srv.state {
    case stateFailed, stateUnchecked:
        new_state = stateRecovering  // First success -> recovering
    case stateRecovering:
        if r == reasonHealthCheck {
            if len(srv.connections) > 0 {
                new_state = stateActive     // Has connections -> active
            } else {
                new_state = statePreferred  // No connections -> preferred
            }
        }
    case stateHealthy:
        if r == reasonDial {
            new_state = stateActive  // Dialed while healthy -> active
        }
    case statePreferred:
        if r == reasonDial {
            new_state = stateActive  // Dialed while preferred -> active
        } else if time.Since(srv.lastTransition) > time.Minute {
            new_state = stateHealthy  // Preferred too long -> healthy
        }
    }

    // When a server becomes active, demote others
    if new_state == stateActive {
        for _, s := range sl.servers {
            if s.address != srv.address && s.state == stateActive {
                if len(s.connections) > len(srv.connections) {
                    new_state = statePreferred  // Other server has more connections
                    defer srv.closeAll()
                } else {
                    s.state = statePreferred    // Demote other server
                    defer s.closeAll()
                }
            }
        }
    }
}
```

### 3. Health Checking System

#### A. Health Check Interface
```go
type HealthCheckFunc func() HealthCheckResult

type HealthCheckResult int
const (
    HealthCheckResultUnknown HealthCheckResult = iota  // No recent check
    HealthCheckResultFailed                            // Check failed
    HealthCheckResultOK                                // Check successful
)
```

#### B. Health Check Execution
```go
// Default health check (no-op)
func defaultHealthCheck() HealthCheckResult {
    return HealthCheckResultUnknown
}

// Continuous health checking
func (sl *serverList) runHealthChecks(ctx context.Context, serviceName string) {
    wait.Until(func() {
        for _, s := range sl.getServers() {
            switch s.healthCheck() {
            case HealthCheckResultOK:
                sl.recordSuccess(s, reasonHealthCheck)
            case HealthCheckResultFailed:
                sl.recordFailure(s, reasonHealthCheck)
            // HealthCheckResultUnknown is ignored
            }

            // Update Prometheus metrics
            if s.state != stateInvalid {
                loadbalancerState.WithLabelValues(serviceName, s.address).Set(float64(s.state))
                loadbalancerConnections.WithLabelValues(serviceName, s.address).Set(float64(len(s.connections)))
            }
        }
    }, time.Second, ctx.Done())  // Check every second
}
```

### 4. Connection Pooling Strategy

#### A. Connection Tracking
```go
type server struct {
    mutex          sync.Mutex
    address        string
    isDefault      bool
    state          state
    lastTransition time.Time
    healthCheck    HealthCheckFunc
    connections    map[net.Conn]struct{}  // Track all active connections
}

// Wrapped connection for automatic cleanup
type serverConn struct {
    server *server
    net.Conn
}

func (sc *serverConn) Close() error {
    sc.server.mutex.Lock()
    defer sc.server.mutex.Unlock()

    delete(sc.server.connections, sc)  // Remove from tracking map
    return sc.Conn.Close()
}
```

#### B. Connection Management
```go
// Create new connection and add to tracking
func (s *server) dialContext(ctx context.Context, network string) (net.Conn, error) {
    if s.state == stateInvalid {
        return nil, fmt.Errorf("server %s is stopping", s.address)
    }

    // Use environment proxy settings
    conn, err := defaultDialer.Dial(network, s.address)
    if err != nil {
        return nil, err
    }

    // Wrap and track connection
    s.mutex.Lock()
    defer s.mutex.Unlock()

    wrappedConn := &serverConn{server: s, Conn: conn}
    s.connections[wrappedConn] = struct{}{}
    return wrappedConn, nil
}

// Close all connections (used during failover)
func (s *server) closeAll() {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    if l := len(s.connections); l > 0 {
        logrus.Infof("Closing %d connections to load balancer server %s", l, s)
        for conn := range s.connections {
            go conn.Close()  // Close in goroutine to avoid holding lock
        }
    }
}
```

#### C. Proxy Configuration
```go
// HTTP proxy support for corporate environments
var defaultDialer proxy.Dialer = &net.Dialer{
    Timeout:   10 * time.Second,   // Connection timeout
    KeepAlive: 30 * time.Second,   // TCP keepalive
}

// Proxy configuration from environment
func SetHTTPProxy(address string) error {
    // Check if proxy is enabled via K3S_AGENT_HTTP_PROXY_ALLOWED
    if useProxy, _ := strconv.ParseBool(os.Getenv(version.ProgramUpper + "_AGENT_HTTP_PROXY_ALLOWED")); !useProxy {
        return nil
    }

    // Use standard HTTP proxy environment variables
    proxyFromEnvironment := httpproxy.FromEnvironment().ProxyFunc()
    proxyURL, err := proxyFromEnvironment(serverURL)

    if proxyURL != nil {
        dialer, err := proxyDialer(proxyURL, defaultDialer)
        defaultDialer = dialer  // Replace global dialer
    }
}
```

### 5. Configuration Persistence

#### A. Configuration Structure
```go
type lbConfig struct {
    ServerURL       string   `json:"ServerURL"`        // Default server URL
    ServerAddresses []string `json:"ServerAddresses"`  // All known server addresses
}
```

#### B. State Persistence
```go
// Save configuration to disk
func (lb *LoadBalancer) writeConfig() error {
    config := &lbConfig{
        ServerURL:       lb.scheme + "://" + lb.servers.getDefaultAddress(),
        ServerAddresses: lb.servers.getAddresses(),
    }
    configOut, err := json.MarshalIndent(config, "", "  ")
    return util.WriteFile(lb.configFile, string(configOut))
}

// Load configuration on startup
func (lb *LoadBalancer) updateConfig() error {
    if configBytes, err := os.ReadFile(lb.configFile); err == nil {
        config := &lbConfig{}
        if err := json.Unmarshal(configBytes, config); err == nil {
            // Only load if default server matches current default
            if config.ServerURL == lb.scheme+"://"+lb.servers.getDefaultAddress() {
                lb.Update(config.ServerAddresses)
            }
        }
    }
    return lb.writeConfig()  // Write current config if load failed
}
```

### 6. Metrics and Monitoring

#### A. Prometheus Metrics
```go
var (
    // Connection count per server
    loadbalancerConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: version.Program + "_loadbalancer_server_connections",
        Help: "Count of current connections to loadbalancer server",
    }, []string{"name", "server"})

    // Server health state
    loadbalancerState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: version.Program + "_loadbalancer_server_health",
        Help: "Current health state of loadbalancer backend servers. " +
              "State enum: 0=INVALID, 1=FAILED, 2=STANDBY, 3=UNCHECKED, " +
              "4=RECOVERING, 5=HEALTHY, 6=PREFERRED, 7=ACTIVE.",
    }, []string{"name", "server"})

    // Dial performance
    loadbalancerDials = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    version.Program + "_loadbalancer_dial_duration_seconds",
        Help:    "Time taken to dial a connection to a backend server",
        Buckets: metrics.ExponentialBuckets(0.001, 2, 15),
    }, []string{"name", "status"})
)
```

## DaaS Integration Design

### 1. Nautilus-Based Server Selection

#### A. Enhanced Health Check System
```go
// NEW: Enhanced health check interface for DaaS
type NautilusHealthCheck struct {
    endpoint        string
    apiKey          string
    client          *http.Client
    lastAttestation *NautilusAttestation
    stakeValidator  StakeValidator
}

type NautilusAttestation struct {
    NodeID           string    `json:"node_id"`
    PerformanceScore float64   `json:"performance_score"`
    ReputationScore  float64   `json:"reputation_score"`
    StakeAmount      string    `json:"stake_amount"`
    Timestamp        time.Time `json:"timestamp"`
    ValidUntil       time.Time `json:"valid_until"`
}

func (nhc *NautilusHealthCheck) Check() HealthCheckResult {
    // 1. Get latest Nautilus attestation
    attestation, err := nhc.fetchAttestation()
    if err != nil {
        return HealthCheckResultFailed
    }

    // 2. Validate attestation freshness
    if time.Now().After(attestation.ValidUntil) {
        return HealthCheckResultFailed
    }

    // 3. Check performance threshold
    if attestation.PerformanceScore < nhc.minPerformanceScore {
        return HealthCheckResultFailed
    }

    // 4. Validate stake requirements
    if err := nhc.stakeValidator.ValidateStake(attestation.StakeAmount); err != nil {
        return HealthCheckResultFailed
    }

    nhc.lastAttestation = attestation
    return HealthCheckResultOK
}
```

#### B. Performance-Based Server Ranking
```go
// MODIFICATION POINT: pkg/agent/loadbalancer/servers.go:compareServers
func compareServersWithNautilus(a, b *server) int {
    // First compare by state as before
    c := cmp.Compare(b.state, a.state)
    if c != 0 {
        return c
    }

    // For servers in same state, use Nautilus performance score
    scoreA := getNautilusScore(a)
    scoreB := getNautilusScore(b)

    if scoreA != scoreB {
        return cmp.Compare(scoreB, scoreA)  // Higher score first
    }

    // Fall back to connection count
    return cmp.Compare(len(b.connections), len(a.connections))
}

func getNautilusScore(s *server) float64 {
    if nhc, ok := s.healthCheck.(*NautilusHealthCheck); ok && nhc.lastAttestation != nil {
        // Composite score: performance * reputation * stake_weight
        stakeWeight := calculateStakeWeight(nhc.lastAttestation.StakeAmount)
        return nhc.lastAttestation.PerformanceScore *
               nhc.lastAttestation.ReputationScore *
               stakeWeight
    }
    return 0.0  // No Nautilus data, lowest priority
}
```

### 2. DaaS Control Plane Integration

#### A. Enhanced LoadBalancer for DaaS
```go
// NEW: DaaS-aware load balancer
type DaaSLoadBalancer struct {
    *LoadBalancer                    // Embed standard load balancer
    suiClient          *sui.Client   // Sui blockchain client
    walrusClient       *walrus.Client // Walrus storage client
    attestationCache   *AttestationCache
    stakeValidator     *StakeValidator
    performanceMonitor *PerformanceMonitor
}

func NewDaaSLoadBalancer(ctx context.Context, config *DaaSConfig) (*DaaSLoadBalancer, error) {
    // 1. Create standard load balancer
    standardLB, err := New(ctx, config.DataDir, config.ServiceName,
                          config.DefaultServerURL, config.LBServerPort, config.IsIPv6)

    // 2. Initialize DaaS components
    dlb := &DaaSLoadBalancer{
        LoadBalancer:       standardLB,
        suiClient:          sui.NewClient(config.SuiRPCEndpoint),
        walrusClient:       walrus.NewClient(config.WalrusEndpoints),
        attestationCache:   NewAttestationCache(config.CacheTTL),
        stakeValidator:     NewStakeValidator(config.MinStakeAmount),
        performanceMonitor: NewPerformanceMonitor(config.NautilusEndpoint),
    }

    // 3. Replace health checks with DaaS-aware versions
    dlb.setupDaaSHealthChecks(ctx)

    return dlb, nil
}
```

#### B. Blockchain-Aware Server Discovery
```go
// NEW: Server discovery via blockchain
func (dlb *DaaSLoadBalancer) DiscoverServers(ctx context.Context) error {
    // 1. Query Sui network for registered DaaS nodes
    registeredNodes, err := dlb.suiClient.GetDaaSNodes(ctx)
    if err != nil {
        return err
    }

    // 2. Filter nodes by stake requirements
    validNodes := []string{}
    for _, node := range registeredNodes {
        if err := dlb.stakeValidator.ValidateStake(node.StakeAmount); err == nil {
            validNodes = append(validNodes, node.Endpoint)
        }
    }

    // 3. Update load balancer with discovered servers
    dlb.Update(validNodes)

    // 4. Set up Nautilus health checks for each server
    for _, endpoint := range validNodes {
        healthCheck := &NautilusHealthCheck{
            endpoint:       dlb.performanceMonitor.GetEndpoint(),
            nodeEndpoint:   endpoint,
            stakeValidator: dlb.stakeValidator,
        }
        dlb.SetHealthCheck(endpoint, healthCheck.Check)
    }

    return nil
}
```

### 3. Custom Health Checks for Blockchain Nodes

#### A. Multi-Layer Health Checking
```go
// NEW: Comprehensive blockchain node health check
type BlockchainHealthCheck struct {
    nodeEndpoint       string
    suiClient          *sui.Client
    performanceMonitor *PerformanceMonitor
    lastBlockHeight    uint64
    lastSyncCheck      time.Time
    thresholds         HealthThresholds
}

type HealthThresholds struct {
    MaxBlockLag         uint64        `yaml:"max_block_lag"`
    MaxSyncAge          time.Duration `yaml:"max_sync_age"`
    MinPerformanceScore float64       `yaml:"min_performance_score"`
    MaxResponseTime     time.Duration `yaml:"max_response_time"`
    MinStakeAmount      string        `yaml:"min_stake_amount"`
}

func (bhc *BlockchainHealthCheck) Check() HealthCheckResult {
    start := time.Now()

    // 1. Check basic connectivity
    if !bhc.checkConnectivity() {
        return HealthCheckResultFailed
    }

    // 2. Check blockchain sync status
    if !bhc.checkSyncStatus() {
        return HealthCheckResultFailed
    }

    // 3. Check performance metrics
    if !bhc.checkPerformance() {
        return HealthCheckResultFailed
    }

    // 4. Check stake validation
    if !bhc.checkStake() {
        return HealthCheckResultFailed
    }

    // 5. Check response time
    if time.Since(start) > bhc.thresholds.MaxResponseTime {
        return HealthCheckResultFailed
    }

    return HealthCheckResultOK
}

func (bhc *BlockchainHealthCheck) checkSyncStatus() bool {
    // Get latest block height from node
    height, err := bhc.suiClient.GetLatestBlockHeight(bhc.nodeEndpoint)
    if err != nil {
        return false
    }

    // Get network height for comparison
    networkHeight, err := bhc.suiClient.GetNetworkBlockHeight()
    if err != nil {
        return false
    }

    // Check if node is within acceptable lag
    if networkHeight-height > bhc.thresholds.MaxBlockLag {
        return false
    }

    bhc.lastBlockHeight = height
    bhc.lastSyncCheck = time.Now()
    return true
}

func (bhc *BlockchainHealthCheck) checkPerformance() bool {
    score, err := bhc.performanceMonitor.GetPerformanceScore(bhc.nodeEndpoint)
    if err != nil {
        return false
    }

    return score >= bhc.thresholds.MinPerformanceScore
}
```

#### B. Adaptive Health Check Intervals
```go
// NEW: Adaptive health checking based on server state
func (sl *serverList) runAdaptiveHealthChecks(ctx context.Context, serviceName string) {
    wait.Until(func() {
        for _, s := range sl.getServers() {
            interval := sl.getHealthCheckInterval(s)

            // Skip if not yet time for next check
            if time.Since(s.lastHealthCheck) < interval {
                continue
            }

            switch s.healthCheck() {
            case HealthCheckResultOK:
                sl.recordSuccess(s, reasonHealthCheck)
            case HealthCheckResultFailed:
                sl.recordFailure(s, reasonHealthCheck)
            }

            s.lastHealthCheck = time.Now()

            // Update metrics
            if s.state != stateInvalid {
                loadbalancerState.WithLabelValues(serviceName, s.address).Set(float64(s.state))
                loadbalancerConnections.WithLabelValues(serviceName, s.address).Set(float64(len(s.connections)))
            }
        }
    }, time.Second, ctx.Done())
}

func (sl *serverList) getHealthCheckInterval(s *server) time.Duration {
    switch s.state {
    case stateActive:
        return 5 * time.Second   // Check active servers frequently
    case statePreferred, stateHealthy:
        return 10 * time.Second  // Normal interval
    case stateRecovering:
        return 3 * time.Second   // Check recovering servers more often
    case stateFailed:
        return 30 * time.Second  // Check failed servers less frequently
    default:
        return 15 * time.Second  // Default interval
    }
}
```

### 4. Enhanced Configuration

#### A. DaaS Load Balancer Configuration
```go
// NEW: Extended configuration for DaaS integration
type DaaSConfig struct {
    // Standard load balancer config
    DataDir           string
    ServiceName       string
    DefaultServerURL  string
    LBServerPort      int
    IsIPv6           bool

    // Sui blockchain configuration
    SuiRPCEndpoint    string            `yaml:"sui_rpc_endpoint"`
    SuiWSEndpoint     string            `yaml:"sui_ws_endpoint"`
    SuiWalletPath     string            `yaml:"sui_wallet_path"`

    // Walrus storage configuration
    WalrusEndpoints   []string          `yaml:"walrus_endpoints"`
    WalrusCacheDir    string            `yaml:"walrus_cache_dir"`

    // Nautilus performance monitoring
    NautilusEndpoint  string            `yaml:"nautilus_endpoint"`
    NautilusAPIKey    string            `yaml:"nautilus_api_key"`

    // Health check configuration
    HealthThresholds  HealthThresholds  `yaml:"health_thresholds"`

    // Discovery configuration
    AutoDiscovery     bool              `yaml:"auto_discovery"`
    DiscoveryInterval time.Duration     `yaml:"discovery_interval"`

    // Performance tuning
    CacheTTL          time.Duration     `yaml:"cache_ttl"`
    MaxConcurrentChecks int             `yaml:"max_concurrent_checks"`
}
```

#### B. Integration Points
```go
// MODIFICATION POINT: pkg/agent/proxy/proxy.go - Supervisor proxy setup
func NewSupervisorProxy(ctx context.Context, disabled bool, dataDir, supervisorURL string,
                       lbServerPort int, isIPv6 bool, daasConfig *DaaSConfig) (Proxy, error) {

    if daasConfig != nil && daasConfig.AutoDiscovery {
        // Use DaaS-aware load balancer
        lbProxy, err := NewDaaSLoadBalancer(ctx, daasConfig)
        if err != nil {
            return nil, err
        }

        // Start server discovery
        go func() {
            ticker := time.NewTicker(daasConfig.DiscoveryInterval)
            defer ticker.Stop()

            for {
                select {
                case <-ctx.Done():
                    return
                case <-ticker.C:
                    if err := lbProxy.DiscoverServers(ctx); err != nil {
                        logrus.Warnf("Server discovery failed: %v", err)
                    }
                }
            }
        }()

        return &SupervisorProxy{loadBalancer: lbProxy}, nil
    }

    // Fall back to standard load balancer
    return NewStandardSupervisorProxy(ctx, disabled, dataDir, supervisorURL, lbServerPort, isIPv6)
}
```

### 5. Metrics and Monitoring Enhancements

#### A. DaaS-Specific Metrics
```go
// NEW: DaaS performance metrics
var (
    nautilusPerformanceScore = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "k3s_nautilus_performance_score",
        Help: "Current Nautilus performance score for each server",
    }, []string{"service", "server"})

    stakeAmount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "k3s_server_stake_amount",
        Help: "Current stake amount for each DaaS server",
    }, []string{"service", "server"})

    blockchainSyncLag = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "k3s_blockchain_sync_lag_blocks",
        Help: "Number of blocks behind network for each server",
    }, []string{"service", "server"})

    attestationAge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
        Name: "k3s_attestation_age_seconds",
        Help: "Age of last valid Nautilus attestation",
    }, []string{"service", "server"})
)
```

### 6. Migration Strategy

#### A. Backward Compatibility
```go
// Ensure existing K3s deployments continue to work
func NewCompatibleLoadBalancer(ctx context.Context, config LoadBalancerConfig) (LoadBalancer, error) {
    // Check if DaaS features are enabled
    if config.DaaSEnabled {
        return NewDaaSLoadBalancer(ctx, config.DaaSConfig)
    }

    // Use standard load balancer for legacy deployments
    return New(ctx, config.DataDir, config.ServiceName, config.DefaultServerURL,
              config.LBServerPort, config.IsIPv6)
}
```

#### B. Feature Gates
```go
type FeatureGates struct {
    NautilusHealthChecks  bool `yaml:"nautilus_health_checks"`
    BlockchainDiscovery   bool `yaml:"blockchain_discovery"`
    StakeValidation       bool `yaml:"stake_validation"`
    PerformanceRanking    bool `yaml:"performance_ranking"`
}
```

## Implementation Roadmap

### Phase 1: Core Integration
1. **Extend LoadBalancer struct** with DaaS components
2. **Implement Nautilus health checks** with performance scoring
3. **Add blockchain sync monitoring** for node health validation
4. **Create DaaS-aware server comparison** function

### Phase 2: Advanced Features
1. **Implement automatic server discovery** via Sui network
2. **Add stake-based server filtering** and validation
3. **Integrate Walrus storage** for configuration persistence
4. **Enhance metrics collection** with DaaS-specific data

### Phase 3: Production Features
1. **Add adaptive health check intervals** based on server state
2. **Implement connection pooling optimizations** for blockchain workloads
3. **Create monitoring dashboards** for DaaS load balancer metrics
4. **Add automated failover policies** based on performance degradation

This comprehensive analysis provides the foundation for integrating Nautilus-based server selection, DaaS control plane connectivity, and blockchain-aware health checking into the K3s load balancer system while maintaining compatibility with existing deployments.