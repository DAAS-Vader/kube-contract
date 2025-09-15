# DaaS-K3s Testing Framework

This comprehensive testing framework validates the integration of blockchain-based authentication (DaaS) with K3s Kubernetes distribution, including Sui blockchain stake verification, Walrus decentralized storage, and Nautilus code attestation.

## 🎯 Overview

The testing framework provides end-to-end validation of:

- **Agent Registration**: DaaS-enabled K3s agent registration with blockchain authentication
- **Stake Verification**: Sui blockchain stake validation for worker nodes
- **Code Deployment**: Walrus-based decentralized code deployment and execution
- **Attestation**: Nautilus code attestation and runtime integrity monitoring

## 📁 Directory Structure

```
testing/
├── docker-compose.yml              # Main orchestration file
├── Dockerfile.k3s-agent           # DaaS-enabled K3s agent image
├── Dockerfile.test-runner          # Test execution environment
├── requirements.txt                # Python test dependencies
├── README.md                       # This file
├── configs/                        # Service configurations
│   ├── sui/                        # Sui node configuration
│   ├── walrus/                     # Walrus simulator configuration
│   ├── k3s/                        # K3s server configuration
│   ├── k3s-agent/                  # DaaS agent configuration
│   ├── nautilus/                   # Nautilus attestation configuration
│   ├── prometheus/                 # Monitoring configuration
│   └── grafana/                    # Dashboard configuration
├── integration-tests/              # Test suites
│   ├── test_agent_registration.py  # Agent registration tests
│   ├── test_stake_verification.py  # Stake verification tests
│   ├── test_walrus_deployment.py   # Walrus deployment tests
│   ├── test_nautilus_attestation.py # Attestation tests
│   └── utils/                      # Test utilities and helpers
├── scripts/                        # Setup and utility scripts
│   ├── setup-test-environment.sh   # Environment setup
│   └── agent-health.sh            # Agent health checks
└── test-data/                      # Test data and sample applications
    ├── test-deployment.yaml        # Sample K8s deployment
    └── sample-app.js              # Sample application
```

## 🚀 Quick Start

### Prerequisites

- Docker 20.10+ with Docker Compose
- 8GB+ RAM available for containers
- 20GB+ disk space for blockchain data

### 1. Environment Setup

```bash
# Clone the repository
git clone <repository-url>
cd dsaas/testing

# Set up the test environment
chmod +x scripts/setup-test-environment.sh
./scripts/setup-test-environment.sh

# Verify Docker Compose configuration
docker-compose config --quiet
```

### 2. Run All Tests

```bash
# Start infrastructure and run complete test suite
docker-compose up -d
docker-compose --profile testing run test-runner

# View logs
docker-compose logs -f
```

### 3. Run Specific Test Suites

```bash
# Agent registration tests only
docker-compose run test-runner pytest tests/test_agent_registration.py -v

# Stake verification tests
docker-compose run test-runner pytest tests/test_stake_verification.py -v

# Walrus deployment tests
docker-compose run test-runner pytest tests/test_walrus_deployment.py -v

# Nautilus attestation tests
docker-compose run test-runner pytest tests/test_nautilus_attestation.py -v
```

## 🏗️ Infrastructure Components

### Core Services

#### Sui Node (`sui-node`)
- **Purpose**: Local Sui blockchain for stake validation
- **Port**: 9000 (RPC), 9184 (Metrics)
- **Data**: Persistent volume for blockchain state
- **Health**: `http://localhost:9000/health`

#### Walrus Simulator (`walrus-simulator`)
- **Purpose**: Decentralized storage simulation
- **Port**: 31415 (API), 31416 (Aggregator)
- **Features**: Blob storage, attestation integration
- **Health**: `http://localhost:31415/v1/health`

#### K3s Server (`k3s-server`)
- **Purpose**: Kubernetes control plane
- **Port**: 6443 (API), 8001 (Metrics)
- **Features**: Standard K3s with DaaS admission webhooks
- **Health**: `https://localhost:6443/healthz`

#### K3s DaaS Agent (`k3s-agent-daas`)
- **Purpose**: DaaS-enabled worker node
- **Features**: Blockchain authentication, stake validation
- **Configuration**: Seal token authentication
- **Health**: Custom health check script

#### Nautilus Attestation (`nautilus-attestation`)
- **Purpose**: Code attestation and runtime monitoring
- **Port**: 8090 (API)
- **Features**: Code verification, integrity monitoring
- **Health**: `http://localhost:8090/health`

### Optional Services

#### Monitoring Stack
```bash
# Enable monitoring
docker-compose --profile monitoring up -d prometheus grafana

# Access dashboards
open http://localhost:9090  # Prometheus
open http://localhost:3000  # Grafana (admin/daas-testing)
```

## 🧪 Test Suites

### 1. Agent Registration Tests (`test_agent_registration.py`)

Tests the complete DaaS agent registration flow:

**Test Cases:**
- ✅ Successful registration with valid stake and signature
- ❌ Registration failure with insufficient stake
- ❌ Registration failure with invalid signature
- 🔄 Fallback to traditional authentication
- 👥 Multiple agent registration
- 🔄 Agent reconnection after restart

**Key Validations:**
- Seal token generation and validation
- Sui blockchain stake verification
- K3s cluster node registration
- Node labeling and annotation
- Health check validation

### 2. Stake Verification Tests (`test_stake_verification.py`)

Tests Sui blockchain stake validation system:

**Test Cases:**
- 💰 Minimum stake requirement validation
- 📊 Worker status validation (active/inactive/slashed)
- 🎯 Stake amount precision testing
- 📈 Dynamic stake monitoring
- ⚡ Slashing detection and response
- 🏆 Performance score impact on requirements
- 🚀 Concurrent stake validations
- 💾 Stake validation caching

**Key Validations:**
- Stake amount thresholds
- Worker eligibility checks
- Real-time monitoring
- Performance optimization
- Error handling

### 3. Walrus Deployment Tests (`test_walrus_deployment.py`)

Tests decentralized code deployment through Walrus:

**Test Cases:**
- 📦 Simple blob storage and retrieval
- 🐳 Docker image deployment from Walrus
- ⚙️ Configuration deployment
- 📂 Multi-file application deployment
- 🔄 Version management and rollback
- 📈 Large blob deployment

**Key Validations:**
- Blob storage integrity
- Deployment orchestration
- Version control
- Resource management
- Performance at scale

### 4. Nautilus Attestation Tests (`test_nautilus_attestation.py`)

Tests code attestation and runtime integrity:

**Test Cases:**
- 📋 Code attestation creation
- ✅ Deployment with attestation verification
- ❌ Attestation verification failure
- 🔍 Runtime integrity monitoring
- 📏 Compliance level enforcement
- 👥 Multi-attestor consensus

**Key Validations:**
- Attestation record creation
- Verification workflows
- Runtime monitoring
- Compliance frameworks
- Consensus mechanisms

## 🔧 Configuration

### Environment Variables

```bash
# Sui Configuration
SUI_RPC_ENDPOINT=http://sui-node:9000
SUI_CONTRACT_PACKAGE=0x1234567890abcdef1234567890abcdef12345678
SUI_MAX_GAS_BUDGET=1000000

# Walrus Configuration
WALRUS_ENDPOINT=http://walrus-simulator:31415
WALRUS_BLOB_STORE_ID=test-blob-store

# DaaS Configuration
DAAS_ENABLED=true
DAAS_MIN_STAKE=1000000000
SEAL_WALLET_ADDRESS=0x1234567890abcdef1234567890abcdef12345678

# Test Configuration
PYTEST_ARGS=--verbose --tb=short
TEST_WALLET_ADDRESS=0x1234567890abcdef1234567890abcdef12345678
```

### Service Configuration Files

#### K3s Server (`configs/k3s/config.yaml`)
```yaml
cluster-init: true
token: "daas-test-token-12345"
disable: [traefik, servicelb]
kube-apiserver-arg:
  - "enable-admission-plugins=NodeRestriction,MutatingAdmissionWebhook,ValidatingAdmissionWebhook"
```

#### DaaS Agent (`configs/k3s-agent/config.yaml`)
```yaml
server: "https://k3s-server:6443"
daas:
  enabled: true
  sui:
    rpc_endpoint: "http://sui-node:9000"
    min_stake: "1000000000"
  seal:
    wallet_address: "0x1234567890abcdef1234567890abcdef12345678"
```

## 🐛 Debugging and Troubleshooting

### Common Issues

#### 1. Services Not Starting

```bash
# Check service health
docker-compose ps
docker-compose logs <service-name>

# Restart specific service
docker-compose restart <service-name>
```

#### 2. Test Failures

```bash
# Run tests with detailed output
docker-compose run test-runner pytest tests/ -v -s --tb=long

# Run single test with debugging
docker-compose run test-runner pytest tests/test_agent_registration.py::TestAgentRegistration::test_successful_agent_registration -v -s
```

#### 3. Network Issues

```bash
# Check network connectivity
docker-compose exec test-runner curl -f http://sui-node:9000/health
docker-compose exec test-runner curl -f http://walrus-simulator:31415/v1/health

# Inspect network
docker network inspect testing_daas-network
```

#### 4. Resource Issues

```bash
# Check resource usage
docker stats

# Clean up resources
docker-compose down -v
docker system prune -f
```

### Log Analysis

```bash
# Comprehensive log collection
mkdir -p logs
docker-compose logs > logs/all-services.log
docker-compose logs sui-node > logs/sui-node.log
docker-compose logs k3s-server > logs/k3s-server.log
docker-compose logs k3s-agent-daas > logs/k3s-agent.log

# Search for errors
grep -i error logs/*.log
grep -i "failed" logs/*.log
```

### Performance Monitoring

```bash
# Enable monitoring stack
docker-compose --profile monitoring up -d

# Key metrics to monitor:
# - Sui node sync status
# - K3s API server response time
# - Walrus blob storage latency
# - Agent registration success rate
```

## 📊 Performance Benchmarks

### Expected Performance

| Operation | Target Time | Acceptable Range |
|-----------|-------------|------------------|
| Agent Registration | < 30s | 10-45s |
| Stake Validation | < 5s | 2-10s |
| Blob Storage (1MB) | < 10s | 5-20s |
| Attestation Creation | < 15s | 10-30s |
| Deployment (Small App) | < 60s | 30-120s |

### Load Testing

```bash
# Run performance tests
docker-compose run test-runner pytest tests/ -m "performance" -v

# Stress test with multiple agents
for i in {1..10}; do
  docker-compose run test-runner pytest tests/test_agent_registration.py::TestAgentRegistration::test_multiple_agent_registration &
done
wait
```

## 🔒 Security Considerations

### Test Security

- All test data uses deterministic, non-production keys
- Sui node runs in isolated test mode
- Network isolation through Docker networks
- Temporary credential cleanup after tests

### Production Differences

- Real Sui mainnet/testnet integration
- Hardware Security Module (HSM) key storage
- TLS/mTLS for all inter-service communication
- Production-grade monitoring and alerting

## 🚀 CI/CD Integration

### GitHub Actions

The testing framework integrates with GitHub Actions for automated testing:

```yaml
# Trigger test runs
.github/workflows/test.yml

# Manual test execution
gh workflow run test.yml -f test_suite=agent_registration
gh workflow run test.yml -f environment=kubernetes
```

### Local CI Simulation

```bash
# Simulate CI environment
export CI=true
export GITHUB_ACTIONS=true

# Run tests in CI mode
docker-compose run test-runner pytest tests/ --junit-xml=results.xml --html=report.html
```

## 📈 Advanced Usage

### Custom Test Configuration

Create `testing/.env.local` for custom configuration:

```bash
# Custom Sui endpoint
SUI_RPC_ENDPOINT=https://sui-testnet.mysten.io:443

# Custom test timeouts
TEST_TIMEOUT_AGENT_REGISTRATION=60
TEST_TIMEOUT_STAKE_VALIDATION=10

# Debug mode
DEBUG=true
LOG_LEVEL=debug
```

### Extending Tests

```python
# Add new test case
# testing/integration-tests/test_custom.py

import pytest
from .utils.daas_client import DaaSClient

class TestCustom:
    @pytest.mark.asyncio
    async def test_custom_functionality(self):
        client = DaaSClient()
        result = await client.custom_operation()
        assert result["status"] == "success"
```

### Integration with External Systems

```bash
# Connect to external Sui network
export SUI_RPC_ENDPOINT=https://sui-mainnet.rpc.com

# Use external Walrus network
export WALRUS_ENDPOINT=https://walrus.network.com

# Run tests against external infrastructure
docker-compose run test-runner pytest tests/ --external-mode
```

## 📚 Additional Resources

- [K3s Documentation](https://docs.k3s.io/)
- [Sui Blockchain Documentation](https://docs.sui.io/)
- [Walrus Protocol Specification](https://docs.walrus.network/)
- [Nautilus Attestation Guide](https://docs.nautilus.attestation/)
- [Docker Compose Reference](https://docs.docker.com/compose/)

## 🤝 Contributing

### Adding New Tests

1. Create test file in `integration-tests/`
2. Follow existing naming conventions
3. Add configuration to `docker-compose.yml`
4. Update this README with test descriptions
5. Add CI/CD pipeline integration

### Reporting Issues

Include the following information:
- Docker and Docker Compose versions
- Full error logs from failed services
- Test environment configuration
- Steps to reproduce the issue

---

**Happy Testing! 🎉**

For questions or support, please refer to the project documentation or create an issue in the repository.