# K3s WebSocket Tunnel Protocol Analysis

## Overview
K3s uses a WebSocket-based tunnel system for secure communication between agents and control-plane nodes. The implementation is built on top of the Rancher `remotedialer` library and provides bidirectional tunneling capabilities.

## Protocol Architecture

### 1. Connection Establishment Process

#### Entry Point: `pkg/agent/tunnel/tunnel.go:Setup()`
```go
func Setup(ctx context.Context, config *daemonconfig.Node, proxy proxy.Proxy) error
```

**Flow Sequence:**
1. **Client Setup**: Create Kubernetes client and TLS config from kubeconfig
2. **Tunnel Initialization**: Create `agentTunnel` struct with:
   - CIDR ranger for authorization
   - TLS configuration for secure connections
   - Egress selector mode configuration
   - Kubelet addressing information
3. **Background Watchers**: Start `startWatches()` goroutine for ongoing operations

#### Connection URL Format
```
wss://<server-address>/v1-k3s/connect
```
- Uses WSS (WebSocket Secure) protocol
- Version-specific endpoint (`v1-k3s`)
- Standard `/connect` path for remotedialer

### 2. Authentication Flow

#### Two-Phase Authentication:

**Phase 1: Token Validation** (`pkg/clientaccess/token.go`)
```go
// Token format: K10<CA_HASH>::<CREDENTIALS>
// Example: K10a1b2c3d4...::node-token
func ParseAndValidateToken(serverURL, token string, options ...ValidationOption) (*Info, error)
```

**Token Structure:**
- **Prefix**: `K10` (constant)
- **CA Hash**: SHA256 of server CA certificate
- **Separator**: `::`
- **Credentials**: Either bootstrap token or username:password

**Phase 2: Certificate-Based Auth** (`pkg/daemons/control/tunnel.go:authorizer`)
```go
func authorizer(req *http.Request) (clientKey string, authed bool, err error) {
    user, ok := request.UserFrom(req.Context())
    if !ok {
        return "", false, nil
    }
    // Must be system:node:<nodename>
    if strings.HasPrefix(user.GetName(), "system:node:") {
        return strings.TrimPrefix(user.GetName(), "system:node:"), true, nil
    }
    return "", false, nil
}
```

### 3. Message Format and Protocol Structure

#### Based on Rancher RemoteDialer v0.5.1+
```go
import "github.com/rancher/remotedialer"
```

**Protocol Messages:**
- Uses standard WebSocket frames
- JSON-based control messages
- Binary data for tunnel payload
- Built-in heartbeat/keepalive mechanism

**Connection Types:**
1. **Control Connection**: WebSocket for management
2. **Data Tunnels**: TCP connections tunneled through WebSocket
3. **Health Checks**: Regular status validation

### 4. Reconnection Logic and Retry Mechanisms

#### Connection Loop (`pkg/agent/tunnel/tunnel.go:connect`)
```go
func (a *agentTunnel) connect(rootCtx context.Context, address string) agentConnection {
    // Infinite retry loop with backoff
    go func() {
        for {
            err := remotedialer.ConnectToProxyWithDialer(ctx, wsURL, nil, auth, ws, a.dialContext, onConnect)
            status = loadbalancer.HealthCheckResultFailed
            if err != nil && !errors.Is(err, context.Canceled) {
                logrus.WithField("url", wsURL).WithError(err).Error("Remotedialer proxy error; reconnecting...")
                time.Sleep(endpointDebounceDelay) // 3 seconds
            }
            if ctx.Err() != nil {
                return // Exit on context cancellation
            }
        }
    }()
}
```

**Retry Strategy:**
- **Infinite retry**: Never give up unless context cancelled
- **Fixed backoff**: 3-second delay between attempts (`endpointDebounceDelay`)
- **Health checking**: Continuous status monitoring
- **Graceful shutdown**: Respect context cancellation

#### Endpoint Debouncing
```go
var endpointDebounceDelay = 3 * time.Second
```
- Prevents connection thrashing during server startup
- Debounces endpoint list changes
- Used for both reconnection and endpoint updates

### 5. Keepalive Mechanism

#### Built-in RemoteDialer Keepalive
- **Library-level**: Handled by `remotedialer.ConnectToProxyWithDialer()`
- **WebSocket pings**: Automatic at transport layer
- **Connection state**: Tracked via `loadbalancer.HealthCheckResult`
- **Status monitoring**: Real-time health check functions

#### Health Check Integration
```go
type agentConnection struct {
    cancel      context.CancelFunc
    healthCheck loadbalancer.HealthCheckFunc
}

healthCheck: func() loadbalancer.HealthCheckResult {
    return status // OK or Failed
}
```

### 6. Authorization System

#### Tunnel Authorizer (`pkg/agent/tunnel/tunnel.go:authorized`)
```go
func (a *agentTunnel) authorized(ctx context.Context, proto, address string) bool
```

**Authorization Rules:**
1. **Kubelet Ports**: Always allowed for loopback (127.0.0.1, ::1)
   - Kubelet port (default: 10250)
   - Stream server port (default: 10010)

2. **CIDR-based**: Allowed if target IP is in managed CIDR ranges
   - Node IPs
   - Cluster CIDRs (Pod networks)
   - Per-pod IPs (in Pod mode)

3. **Host Network Ports**: Special handling for host-network pods

#### Operating Modes:
- **Cluster Mode**: Allow cluster CIDRs + node IPs
- **Pod Mode**: Dynamic pod IP tracking + host network ports

### 7. Server-Side Implementation

#### Tunnel Server (`pkg/daemons/control/tunnel.go`)
```go
type TunnelServer struct {
    sync.Mutex
    cidrs  cidranger.Ranger
    client kubernetes.Interface
    config *config.Control
    server *remotedialer.Server    // Core remotedialer server
    egress map[string]bool
}
```

**Request Handling:**
- **CONNECT requests**: Direct HTTP CONNECT proxy
- **WebSocket requests**: Delegated to remotedialer server
- **Node authorization**: System user validation

## Modification Points for DaaS Integration

### 1. Authentication Integration Points

#### A. Token Validation Replacement (`pkg/clientaccess/token.go:ParseAndValidateToken`)
```go
// MODIFICATION POINT: Replace K10 token validation with Seal authentication
func ParseAndValidateToken(serverURL, token string, options ...ValidationOption) (*Info, error) {
    // Current: K10<CA_HASH>::<CREDENTIALS>
    // Replace with: SEAL<SUI_ADDRESS>::<SIGNATURE>

    // Add Sui wallet signature verification
    // Integrate with Seal authentication system
    // Validate staking requirements
}
```

#### B. Tunnel Authorizer Enhancement (`pkg/daemons/control/tunnel.go:authorizer`)
```go
// MODIFICATION POINT: Add Nautilus attestation headers
func authorizer(req *http.Request) (clientKey string, authed bool, err error) {
    // Current: system:node:<nodename> validation
    // Add: Nautilus attestation header validation
    // Add: Seal signature verification
    // Add: Stake validation check
}
```

### 2. WebSocket Connection Enhancement

#### A. Connection Establishment (`pkg/agent/tunnel/tunnel.go:connect`)
```go
// MODIFICATION POINT: Add custom headers for DaaS
wsURL := fmt.Sprintf("wss://%s/v1-"+version.Program+"/connect", address)
ws := &websocket.Dialer{
    TLSClientConfig: a.tlsConfig,
    // ADD: Custom headers for Nautilus attestation
    Header: http.Header{
        "X-Nautilus-Attestation": []string{attestationData},
        "X-Seal-Signature":       []string{sealSignature},
        "X-Sui-Address":          []string{walletAddress},
    },
}
```

#### B. Connection Callback Enhancement (`pkg/agent/tunnel/tunnel.go:connect`)
```go
// MODIFICATION POINT: Inject DaaS initialization on connect
onConnect := func(ctx context.Context, session *remotedialer.Session) error {
    status = loadbalancer.HealthCheckResultOK
    logrus.WithField("url", wsURL).Info("Remotedialer connected to proxy")

    // ADD: Initialize Walrus client
    // ADD: Start performance monitoring
    // ADD: Begin reward calculation

    return nil
}
```

### 3. Message Protocol Extensions

#### A. Custom Message Types
```go
// NEW: DaaS-specific message types
const (
    MessageTypeCodeFetch    = "code-fetch"
    MessageTypeAttestation  = "attestation"
    MessageTypeReward       = "reward-claim"
)

// NEW: DaaS message structures
type DaaSMessage struct {
    Type    string      `json:"type"`
    Payload interface{} `json:"payload"`
}

type CodeFetchRequest struct {
    CodeHash    string `json:"code_hash"`
    WalrusBlob  string `json:"walrus_blob"`
}

type AttestationData struct {
    Performance map[string]float64 `json:"performance"`
    Timestamp   int64              `json:"timestamp"`
    Signature   string             `json:"signature"`
}
```

#### B. Protocol Handler Extension (`pkg/daemons/control/tunnel.go:ServeHTTP`)
```go
// MODIFICATION POINT: Add DaaS protocol handling
func (t *TunnelServer) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
    // Current: Handle CONNECT and WebSocket requests
    // ADD: Handle DaaS-specific endpoints

    if strings.HasPrefix(req.URL.Path, "/v1-daas/") {
        t.serveDaaS(resp, req)
        return
    }

    // Original logic...
}
```

### 4. Walrus Client Integration Points

#### A. Client Initialization (`pkg/agent/tunnel/tunnel.go:Setup`)
```go
// MODIFICATION POINT: Initialize Walrus client
func Setup(ctx context.Context, config *daemonconfig.Node, proxy proxy.Proxy) error {
    // Current setup...

    // ADD: Walrus client initialization
    walrusClient, err := walrus.NewClient(config.WalrusEndpoint)
    if err != nil {
        return err
    }

    tunnel.walrusClient = walrusClient

    // Continue with existing logic...
}
```

#### B. Code Fetching Service
```go
// NEW: Code fetching via Walrus
func (a *agentTunnel) fetchCode(ctx context.Context, codeHash string) ([]byte, error) {
    // Fetch code from Walrus network
    // Verify integrity against hash
    // Cache locally for execution
}
```

### 5. Performance Monitoring Integration

#### A. Metrics Collection Extension (`pkg/agent/tunnel/tunnel.go`)
```go
// MODIFICATION POINT: Add performance monitoring
type agentTunnel struct {
    // Existing fields...

    // ADD: DaaS-specific fields
    performanceMonitor *nautilus.Monitor
    rewardCalculator   *daas.RewardCalculator
    walrusClient       *walrus.Client
}

// NEW: Performance reporting
func (a *agentTunnel) reportPerformance(ctx context.Context) {
    // Collect execution metrics
    // Generate Nautilus attestation
    // Submit to reward system
}
```

### 6. Configuration Extensions

#### A. DaaS Configuration Structure
```go
// NEW: DaaS-specific configuration
type DaaSConfig struct {
    SuiWalletPath    string `yaml:"sui_wallet_path"`
    WalrusEndpoint   string `yaml:"walrus_endpoint"`
    NautilusEndpoint string `yaml:"nautilus_endpoint"`
    StakeRequirement string `yaml:"stake_requirement"`
}

// MODIFY: Add to existing Node config
type Node struct {
    // Existing fields...
    DaaSConfig DaaSConfig `yaml:"daas_config"`
}
```

## Security Considerations

### Current Security Model:
1. **TLS encryption**: All WebSocket traffic encrypted
2. **Certificate-based auth**: Client certificates for node identity
3. **CIDR authorization**: Strict network access control
4. **Token validation**: Cryptographic token verification

### DaaS Security Enhancements:
1. **Sui signature verification**: Cryptographic proof of node ownership
2. **Stake validation**: Economic security through staking
3. **Nautilus attestation**: Hardware-based execution verification
4. **Code integrity**: Hash-based code verification from Walrus

## Implementation Roadmap

### Phase 1: Authentication Integration
1. Replace K10 token with Seal authentication
2. Add Sui wallet signature verification
3. Implement stake validation checks

### Phase 2: Protocol Extensions
1. Add DaaS-specific WebSocket message types
2. Implement Nautilus attestation headers
3. Create custom endpoint handlers

### Phase 3: Walrus Integration
1. Initialize Walrus client in tunnel setup
2. Implement code fetching mechanisms
3. Add local caching and integrity verification

### Phase 4: Performance Monitoring
1. Integrate Nautilus performance monitoring
2. Implement reward calculation system
3. Add attestation reporting pipeline

## Testing Strategy

### Unit Tests:
- Token parsing and validation
- Authorization logic
- Message serialization/deserialization

### Integration Tests:
- End-to-end tunnel establishment
- Authentication flows
- Reconnection scenarios

### Security Tests:
- Certificate validation
- Signature verification
- Authorization bypass attempts

This comprehensive analysis provides the foundation for integrating K3s with the DaaS system while maintaining security and reliability.