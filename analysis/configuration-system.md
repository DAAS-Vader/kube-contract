# K3s Configuration System Analysis

## Overview
This document analyzes the K3s configuration system to understand how to integrate DaaS-specific configurations for Sui RPC endpoints, Walrus storage, and other decentralized infrastructure components.

## Configuration Architecture

### 1. Main Configuration Structures

#### A. Node Configuration (`pkg/daemons/config/types.go:39`)
```go
type Node struct {
    // Core runtime settings
    Docker                   bool
    ContainerRuntimeEndpoint string
    ImageServiceEndpoint     string

    // Network configuration
    NoFlannel                bool
    FlannelBackend           string
    FlannelConfFile          string
    FlannelIface             *net.Interface
    EgressSelectorMode       string

    // Container runtime
    Containerd               Containerd
    CRIDockerd               CRIDockerd

    // Agent configuration (embedded)
    AgentConfig              Agent
    Token                    string
    ServerHTTPSPort          int
    SupervisorPort           int
}
```

#### B. Agent Configuration (`pkg/daemons/config/types.go:107`)
```go
type Agent struct {
    // Identity and networking
    NodeName                string
    NodeConfigPath          string
    NodeIP                  string
    NodeIPs                 []net.IP
    NodeExternalIP          string
    NodeExternalIPs         []net.IP

    // Certificates and authentication
    ClientKubeletCert       string
    ClientKubeletKey        string
    ServingKubeletCert      string
    ServingKubeletKey       string

    // Network configuration
    ServiceCIDR             *net.IPNet
    ServiceCIDRs            []*net.IPNet
    ClusterCIDR             *net.IPNet
    ClusterCIDRs            []*net.IPNet
    ClusterDNS              net.IP
    ClusterDNSs             []net.IP

    // Runtime settings
    KubeletConfigDir        string
    RuntimeSocket           string
    ImageServiceSocket      string
    ListenAddress           string

    // Extensions
    ExtraKubeletArgs        []string
    ExtraKubeProxyArgs      []string
    NodeTaints              []string
    NodeLabels              []string

    // Registry and images
    Registry                *registries.Registry
    SystemDefaultRegistry   string
    AirgapExtraRegistry     []string
    PauseImage              string

    // Security
    MinTLSVersion           string
    CipherSuites            []string
    ProtectKernelDefaults   bool
    EnableIPv4              bool
    EnableIPv6              bool
}
```

#### C. Control Plane Configuration (`pkg/daemons/config/types.go:195`)
```go
type Control struct {
    CriticalControlArgs     // Shared HA parameters

    // API server configuration
    HTTPSPort               int
    SupervisorPort          int
    APIServerPort           int
    APIServerBindAddress    string

    // Authentication and tokens
    AgentToken              string `json:"-"`
    Token                   string `json:"-"`

    // Data storage
    DataDir                 string
    Datastore               endpoint.Config `json:"-"`

    // Component toggles
    DisableAgent            bool
    DisableAPIServer        bool
    DisableControllerManager bool
    DisableETCD             bool
    DisableKubeProxy        bool
    DisableScheduler        bool

    // Extensions
    ExtraAPIArgs            []string
    ExtraControllerArgs     []string
    ExtraEtcdArgs           []string
    ExtraSchedulerAPIArgs   []string

    // Runtime reference
    Runtime                 *ControlRuntime `json:"-"`
    Cluster                 Cluster         `json:"-"`
}
```

### 2. Environment Variable Mappings

#### A. Standard Pattern
K3s uses a consistent pattern for environment variables:
```
Pattern: {PROGRAM_UPPER}_{FLAG_NAME}
Example: K3S_TOKEN, K3S_URL, K3S_DATA_DIR
```

#### B. Agent Environment Variables (`pkg/cli/cmds/agent.go`)
```go
// Core configuration
EnvVars: []string{version.ProgramUpper + "_TOKEN"}           // K3S_TOKEN
EnvVars: []string{version.ProgramUpper + "_TOKEN_FILE"}      // K3S_TOKEN_FILE
EnvVars: []string{version.ProgramUpper + "_URL"}             // K3S_URL
EnvVars: []string{version.ProgramUpper + "_DATA_DIR"}        // K3S_DATA_DIR
EnvVars: []string{version.ProgramUpper + "_NODE_NAME"}       // K3S_NODE_NAME
EnvVars: []string{version.ProgramUpper + "_LB_SERVER_PORT"}  // K3S_LB_SERVER_PORT

// Security and certificates
EnvVars: []string{version.ProgramUpper + "_RESOLV_CONF"}     // K3S_RESOLV_CONF
EnvVars: []string{version.ProgramUpper + "_SELINUX"}         // K3S_SELINUX

// VPN configuration
EnvVars: []string{version.ProgramUpper + "_VPN_AUTH"}        // K3S_VPN_AUTH
EnvVars: []string{version.ProgramUpper + "_VPN_AUTH_FILE"}   // K3S_VPN_AUTH_FILE
```

#### C. Config File Environment Variable (`pkg/cli/cmds/config.go`)
```go
ConfigFlag = &cli.StringFlag{
    Name:    "config",
    Aliases: []string{"c"},
    Usage:   "(config) Load configuration from `FILE`",
    EnvVars: []string{version.ProgramUpper + "_CONFIG_FILE"}, // K3S_CONFIG_FILE
    Value:   "/etc/rancher/" + version.Program + "/config.yaml", // Default: /etc/rancher/k3s/config.yaml
}
```

### 3. CLI Flag Parsing System

#### A. Flag Definition Pattern (`pkg/cli/cmds/agent.go`)
```go
// Standard flag structure
&cli.StringFlag{
    Name:        "flag-name",           // CLI flag name
    Aliases:     []string{"f"},         // Short aliases
    Usage:       "(category) Description of flag",
    EnvVars:     []string{"K3S_FLAG_NAME"},  // Environment variable
    Destination: &AgentConfig.FieldName,     // Target field in config struct
    Value:       "default-value",            // Default value
}

// Examples
AgentTokenFlag = &cli.StringFlag{
    Name:        "token",
    Aliases:     []string{"t"},
    Usage:       "(cluster) Token to use for authentication",
    EnvVars:     []string{version.ProgramUpper + "_TOKEN"},
    Destination: &AgentConfig.Token,
}

NodeIPFlag = &cli.StringSliceFlag{
    Name:        "node-ip",
    Aliases:     []string{"i"},
    Usage:       "(agent/networking) IPv4/IPv6 addresses to advertise for node",
    Destination: &AgentConfig.NodeIP,
}
```

#### B. Flag Categories
K3s organizes flags into logical categories:
- `(cluster)` - Cluster membership and authentication
- `(agent/networking)` - Network configuration
- `(agent/runtime)` - Container runtime settings
- `(agent/node)` - Node-specific configuration
- `(agent/data)` - Data storage and directories

### 4. Default Values and Override Hierarchy

#### A. Configuration Priority (Highest to Lowest)
1. **CLI Flags** - Direct command line arguments
2. **Environment Variables** - OS environment variables
3. **Config File** - YAML configuration file
4. **Config File Drop-ins** - Additional YAML files in `{config}.d/` directory
5. **Built-in Defaults** - Hardcoded default values

#### B. Config File Processing (`pkg/configfilearg/parser.go`)
```go
// Config file override hierarchy
func (p *Parser) Parse(args []string) ([]string, error) {
    // 1. Find config file from CLI args or env vars
    configFile := p.findConfigFileFlag(args)

    // 2. Read base config file
    values, err := readConfigFile(configFile)

    // 3. Read drop-in files from {config}.d/ directory
    dropinFiles, err := dotDFiles(configFile)

    // 4. Apply values in order, later values override earlier ones
    return append(prefix, append(values, suffix...)...), nil
}
```

#### C. Default Values Examples
```go
// Data directory default
Value: "/var/lib/rancher/" + version.Program + "",  // /var/lib/rancher/k3s/

// LB server port default
Value: 6444,

// Pause image default
Value: "rancher/mirrored-pause:3.6",

// Private registry config default
Value: "/etc/rancher/" + version.Program + "/registries.yaml",
```

### 5. Config File Loading Process

#### A. File Location Resolution (`pkg/configfilearg/parser.go:167`)
```go
func (p *Parser) findConfigFileFlag(args []string) string {
    // 1. Check environment variable first
    if envVal := os.Getenv(p.EnvName); p.EnvName != "" && envVal != "" {
        return envVal
    }

    // 2. Check CLI arguments
    for i, arg := range args {
        for _, flagName := range p.ConfigFlags {
            if flagName == arg {
                return args[i+1]  // Next argument is the file path
            } else if strings.HasPrefix(arg, flagName+"=") {
                return arg[len(flagName)+1:]  // Value after =
            }
        }
    }

    // 3. Return default config file path
    return p.DefaultConfig  // /etc/rancher/k3s/config.yaml
}
```

#### B. YAML Processing (`pkg/configfilearg/parser.go:128`)
```go
func readConfigFileData(file string) ([]byte, error) {
    // Support HTTP URLs for remote config
    if strings.HasPrefix(file, "http://") || strings.HasPrefix(file, "https://") {
        resp, err := http.Get(file)
        defer resp.Body.Close()
        return io.ReadAll(resp.Body)
    }

    // Standard file reading
    return os.ReadFile(file)
}

// YAML unmarshaling with type conversion
data := yaml.MapSlice{}
yaml.Unmarshal(bytes, &data)
for _, i := range data {
    k, v := convert.ToString(i.Key), convert.ToString(i.Value)

    // Support for array appending with "+" suffix
    isAppend := strings.HasSuffix(k, "+")
    k = strings.TrimSuffix(k, "+")

    if k == target {
        if isAppend {
            lastVal = lastVal + "," + v  // Append to existing value
        } else {
            lastVal = v  // Override existing value
        }
    }
}
```

#### C. Drop-in Directory Support (`pkg/configfilearg/parser.go:120`)
```go
// Automatically load additional config files from {config}.d/ directory
dropinFiles, err := dotDFiles(configFile)

// Example: If config is /etc/rancher/k3s/config.yaml
// Load all *.yaml and *.yml files from /etc/rancher/k3s/config.yaml.d/
// Files are processed in alphabetical order
```

### 6. Config File Format Examples

#### A. Basic Agent Configuration
```yaml
# /etc/rancher/k3s/config.yaml
token: "super-secret-token"
server: "https://k3s-server:6443"
data-dir: "/opt/k3s/data"
node-name: "worker-01"
node-ip: "10.0.1.100"
node-label:
  - "node-type=worker"
  - "environment=production"
kubelet-arg:
  - "max-pods=50"
  - "cluster-dns=10.43.0.10"
```

#### B. Array Appending with Drop-ins
```yaml
# /etc/rancher/k3s/config.yaml
node-label:
  - "base-label=true"

# /etc/rancher/k3s/config.yaml.d/01-additional.yaml
node-label+:  # "+" means append to existing array
  - "additional-label=true"
  - "environment=staging"
```

## DaaS Integration Points

### 1. Configuration Structure Extensions

#### A. Enhanced Agent Configuration
```go
// MODIFICATION POINT: pkg/daemons/config/types.go:Agent
type Agent struct {
    // ... existing fields ...

    // DaaS-specific configuration
    DaaSConfig              DaaSConfig `yaml:"daas_config"`
}

// NEW: DaaS configuration structure
type DaaSConfig struct {
    // Sui blockchain configuration
    SuiRPCEndpoint          string            `yaml:"sui_rpc_endpoint"`
    SuiWSEndpoint           string            `yaml:"sui_ws_endpoint"`
    SuiWalletPath           string            `yaml:"sui_wallet_path"`
    SuiWalletPassword       string            `yaml:"sui_wallet_password,omitempty"`
    SuiNetworkID            string            `yaml:"sui_network_id"`

    // Walrus storage configuration
    WalrusEndpoints         []string          `yaml:"walrus_endpoints"`
    WalrusObjectStore       string            `yaml:"walrus_object_store"`
    WalrusCacheDir          string            `yaml:"walrus_cache_dir"`
    WalrusTimeout           time.Duration     `yaml:"walrus_timeout"`

    // Nautilus performance monitoring
    NautilusEndpoint        string            `yaml:"nautilus_endpoint"`
    NautilusAPIKey          string            `yaml:"nautilus_api_key,omitempty"`
    NautilusReportInterval  time.Duration     `yaml:"nautilus_report_interval"`

    // Seal identity and staking
    SealEnabled             bool              `yaml:"seal_enabled"`
    MinStakeAmount          string            `yaml:"min_stake_amount"`
    StakeValidationInterval time.Duration     `yaml:"stake_validation_interval"`

    // Execution environment
    CodeExecutionEngine     string            `yaml:"code_execution_engine"`
    ResourceLimits          ResourceLimits    `yaml:"resource_limits"`

    // Security settings
    AttestationRequired     bool              `yaml:"attestation_required"`
    TrustedExecutionOnly    bool              `yaml:"trusted_execution_only"`
}

type ResourceLimits struct {
    MaxCPUCores    int    `yaml:"max_cpu_cores"`
    MaxMemoryMB    int    `yaml:"max_memory_mb"`
    MaxStorageGB   int    `yaml:"max_storage_gb"`
    MaxNetworkMbps int    `yaml:"max_network_mbps"`
}
```

### 2. CLI Flag Extensions

#### A. New DaaS-Specific Flags (`pkg/cli/cmds/agent.go`)
```go
// MODIFICATION POINT: Add to NewAgentCommand flags slice
var (
    // Sui configuration flags
    SuiRPCEndpointFlag = &cli.StringFlag{
        Name:        "sui-rpc-endpoint",
        Usage:       "(daas/sui) Sui RPC endpoint URL",
        EnvVars:     []string{version.ProgramUpper + "_SUI_RPC_ENDPOINT"},
        Destination: &AgentConfig.DaaSConfig.SuiRPCEndpoint,
        Value:       "https://fullnode.mainnet.sui.io:443",
    }

    SuiWalletPathFlag = &cli.StringFlag{
        Name:        "sui-wallet-path",
        Usage:       "(daas/sui) Path to Sui wallet file",
        EnvVars:     []string{version.ProgramUpper + "_SUI_WALLET_PATH"},
        Destination: &AgentConfig.DaaSConfig.SuiWalletPath,
        Value:       "/etc/rancher/" + version.Program + "/sui-wallet.json",
    }

    // Walrus configuration flags
    WalrusEndpointsFlag = &cli.StringSliceFlag{
        Name:        "walrus-endpoint",
        Usage:       "(daas/walrus) Walrus storage endpoint URLs",
        EnvVars:     []string{version.ProgramUpper + "_WALRUS_ENDPOINTS"},
        Destination: &AgentConfig.DaaSConfig.WalrusEndpoints,
    }

    WalrusCacheDirFlag = &cli.StringFlag{
        Name:        "walrus-cache-dir",
        Usage:       "(daas/walrus) Local cache directory for Walrus objects",
        EnvVars:     []string{version.ProgramUpper + "_WALRUS_CACHE_DIR"},
        Destination: &AgentConfig.DaaSConfig.WalrusCacheDir,
        Value:       "/var/cache/rancher/" + version.Program + "/walrus",
    }

    // Nautilus monitoring flags
    NautilusEndpointFlag = &cli.StringFlag{
        Name:        "nautilus-endpoint",
        Usage:       "(daas/nautilus) Nautilus monitoring endpoint",
        EnvVars:     []string{version.ProgramUpper + "_NAUTILUS_ENDPOINT"},
        Destination: &AgentConfig.DaaSConfig.NautilusEndpoint,
    }

    // Seal authentication flags
    SealEnabledFlag = &cli.BoolFlag{
        Name:        "seal-enabled",
        Usage:       "(daas/seal) Enable Seal identity-based authentication",
        EnvVars:     []string{version.ProgramUpper + "_SEAL_ENABLED"},
        Destination: &AgentConfig.DaaSConfig.SealEnabled,
    }

    MinStakeAmountFlag = &cli.StringFlag{
        Name:        "min-stake-amount",
        Usage:       "(daas/seal) Minimum stake amount required for participation",
        EnvVars:     []string{version.ProgramUpper + "_MIN_STAKE_AMOUNT"},
        Destination: &AgentConfig.DaaSConfig.MinStakeAmount,
        Value:       "1000000000", // 1 SUI
    }
)
```

#### B. Environment Variable Mapping
```bash
# Sui configuration
export K3S_SUI_RPC_ENDPOINT="https://fullnode.mainnet.sui.io:443"
export K3S_SUI_WS_ENDPOINT="wss://fullnode.mainnet.sui.io:443"
export K3S_SUI_WALLET_PATH="/opt/k3s/sui-wallet.json"
export K3S_SUI_NETWORK_ID="mainnet"

# Walrus storage
export K3S_WALRUS_ENDPOINTS="https://walrus-node-1.example.com,https://walrus-node-2.example.com"
export K3S_WALRUS_CACHE_DIR="/var/cache/k3s/walrus"
export K3S_WALRUS_TIMEOUT="30s"

# Nautilus monitoring
export K3S_NAUTILUS_ENDPOINT="https://nautilus.monitor.com"
export K3S_NAUTILUS_API_KEY="nautilus-api-key-here"
export K3S_NAUTILUS_REPORT_INTERVAL="5m"

# Seal authentication
export K3S_SEAL_ENABLED="true"
export K3S_MIN_STAKE_AMOUNT="1000000000"
export K3S_STAKE_VALIDATION_INTERVAL="1h"
```

### 3. Config File Extension

#### A. Enhanced YAML Configuration
```yaml
# /etc/rancher/k3s/config.yaml - DaaS Extension
token: "k3s-agent-token"
server: "https://k3s-server:6443"

# DaaS-specific configuration
daas_config:
  # Sui blockchain integration
  sui_rpc_endpoint: "https://fullnode.mainnet.sui.io:443"
  sui_ws_endpoint: "wss://fullnode.mainnet.sui.io:443"
  sui_wallet_path: "/opt/k3s/sui-wallet.json"
  sui_network_id: "mainnet"

  # Walrus decentralized storage
  walrus_endpoints:
    - "https://walrus-node-1.example.com"
    - "https://walrus-node-2.example.com"
    - "https://walrus-node-3.example.com"
  walrus_cache_dir: "/var/cache/k3s/walrus"
  walrus_timeout: "30s"

  # Nautilus performance monitoring
  nautilus_endpoint: "https://nautilus.monitor.com"
  nautilus_report_interval: "5m"

  # Seal identity authentication
  seal_enabled: true
  min_stake_amount: "1000000000"  # 1 SUI
  stake_validation_interval: "1h"

  # Execution environment
  code_execution_engine: "wasm"
  resource_limits:
    max_cpu_cores: 4
    max_memory_mb: 8192
    max_storage_gb: 100
    max_network_mbps: 1000

  # Security settings
  attestation_required: true
  trusted_execution_only: false
```

#### B. Drop-in Configuration Support
```yaml
# /etc/rancher/k3s/config.yaml.d/01-daas-dev.yaml
# Development environment overrides
daas_config:
  sui_rpc_endpoint: "https://fullnode.devnet.sui.io:443"
  sui_network_id: "devnet"
  walrus_endpoints+:  # Append to main list
    - "https://dev-walrus-node.example.com"
  resource_limits:
    max_cpu_cores: 2
    max_memory_mb: 4096
```

### 4. Configuration Validation Hooks

#### A. DaaS Config Validator
```go
// NEW: pkg/daemons/config/daas_validation.go
func ValidateDaaSConfig(cfg *Agent) error {
    daas := &cfg.DaaSConfig

    // Validate Sui configuration
    if daas.SealEnabled {
        if daas.SuiWalletPath == "" {
            return errors.New("sui-wallet-path is required when Seal authentication is enabled")
        }

        if _, err := os.Stat(daas.SuiWalletPath); err != nil {
            return fmt.Errorf("sui wallet file not found: %v", err)
        }

        // Validate stake amount format
        if _, err := strconv.ParseUint(daas.MinStakeAmount, 10, 64); err != nil {
            return fmt.Errorf("invalid min-stake-amount format: %v", err)
        }
    }

    // Validate Walrus endpoints
    for _, endpoint := range daas.WalrusEndpoints {
        if _, err := url.Parse(endpoint); err != nil {
            return fmt.Errorf("invalid walrus endpoint URL: %s", endpoint)
        }
    }

    // Validate resource limits
    if limits := daas.ResourceLimits; limits.MaxCPUCores > 0 {
        if limits.MaxCPUCores > runtime.NumCPU() {
            return fmt.Errorf("max-cpu-cores (%d) exceeds available cores (%d)",
                             limits.MaxCPUCores, runtime.NumCPU())
        }
    }

    return nil
}
```

#### B. Integration Point (`pkg/agent/config/config.go:get`)
```go
// MODIFICATION POINT: Add validation after config retrieval
func get(ctx context.Context, envInfo *cmds.Agent, proxy proxy.Proxy) (*config.Node, error) {
    // ... existing config loading logic ...

    // Add DaaS configuration validation
    if err := config.ValidateDaaSConfig(&nodeConfig.AgentConfig); err != nil {
        return nil, pkgerrors.WithMessage(err, "DaaS configuration validation failed")
    }

    // Initialize DaaS components if enabled
    if nodeConfig.AgentConfig.DaaSConfig.SealEnabled {
        if err := initializeDaaSComponents(ctx, &nodeConfig.AgentConfig.DaaSConfig); err != nil {
            return nil, pkgerrors.WithMessage(err, "DaaS component initialization failed")
        }
    }

    return nodeConfig, nil
}
```

### 5. Configuration Backward Compatibility

#### A. Migration Support
```go
// NEW: pkg/daemons/config/daas_migration.go
func MigrateLegacyConfig(cfg *Agent) {
    // Support legacy environment variables for smooth migration
    if cfg.DaaSConfig.SuiRPCEndpoint == "" {
        if endpoint := os.Getenv("SUI_RPC_URL"); endpoint != "" {
            cfg.DaaSConfig.SuiRPCEndpoint = endpoint
            logrus.Warnf("Using legacy SUI_RPC_URL, consider migrating to K3S_SUI_RPC_ENDPOINT")
        }
    }

    // Migrate old config file locations
    legacyWalletPath := "/etc/sui/wallet.json"
    if cfg.DaaSConfig.SuiWalletPath == "" && fileExists(legacyWalletPath) {
        cfg.DaaSConfig.SuiWalletPath = legacyWalletPath
        logrus.Warnf("Using legacy wallet path %s, consider moving to %s",
                     legacyWalletPath, "/etc/rancher/k3s/sui-wallet.json")
    }
}
```

#### B. Feature Flags
```go
// Support gradual rollout of DaaS features
type FeatureGates struct {
    SealAuthentication    bool `yaml:"seal_authentication"`
    WalrusStorage        bool `yaml:"walrus_storage"`
    NautilusMonitoring   bool `yaml:"nautilus_monitoring"`
    TrustedExecution     bool `yaml:"trusted_execution"`
}
```

## Implementation Roadmap

### Phase 1: Configuration Infrastructure
1. **Extend configuration structures** with DaaS fields
2. **Add CLI flags** for all DaaS parameters
3. **Implement configuration validation** hooks
4. **Create configuration migration** utilities

### Phase 2: Integration Points
1. **Add DaaS config loading** to agent initialization
2. **Implement environment variable** mappings
3. **Create config file examples** and documentation
4. **Add drop-in configuration** support

### Phase 3: Advanced Features
1. **Runtime configuration updates** for dynamic parameters
2. **Configuration templating** for fleet management
3. **Encrypted configuration storage** for sensitive data
4. **Configuration API** for programmatic access

## Security Considerations

### Sensitive Data Handling
- **Wallet passwords**: Use environment variables or external secret management
- **API keys**: Support file-based injection and runtime secret mounting
- **Private keys**: Ensure proper file permissions and access controls

### Configuration Validation
- **URL validation**: Ensure all endpoints are properly formatted and accessible
- **Resource limits**: Validate against available system resources
- **Compatibility checks**: Ensure configuration parameters are compatible

This comprehensive configuration system analysis provides the foundation for seamlessly integrating DaaS functionality into K3s while maintaining compatibility and operational simplicity.