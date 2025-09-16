# K3s-DaaS Demo Environment

🚀 **K3s-DaaS (Kubernetes Distributed-as-a-Service)** - A revolutionary Kubernetes distribution that integrates Nautilus TEE, Sui blockchain, and Walrus storage for secure, decentralized container orchestration.

## 🏗️ Architecture Overview

```
kubectl → Nautilus TEE Master → K3s-DaaS Flow
    ↓           ↓                    ↓
🔥 Hot Tier  🌡️ Warm Tier       🧊 Cold Tier
TEE Memory   Sui Blockchain    Walrus Storage
<50ms        1-3s             5-30s
```

### Core Components

- **🛡️ Nautilus TEE**: Replaces etcd with Intel SGX/TDX secure memory storage
- **⛓️ Sui Blockchain**: Handles staker authentication and governance contracts
- **🗄️ Walrus Storage**: Distributed storage for YAML files and archives
- **🔐 Seal Authentication**: Token-based worker node participation with stake verification

## ⚡ Performance Targets

| Tier | Storage Backend | Target Response Time | Use Case |
|------|----------------|---------------------|----------|
| Hot | Nautilus TEE Memory | <50ms | Active cluster operations |
| Warm | Sui Blockchain | 1-3s | Metadata and configuration |
| Cold | Walrus Storage | 5-30s | Archives and large files |

## 🚀 Quick Start

### Prerequisites

- Docker and Docker Compose
- 8GB+ RAM recommended
- 20GB+ free disk space

### Start Demo Environment

```bash
# Clone and enter directory
cd k3s-daas

# Start the complete demo environment
./start-demo.sh
```

### Run Tests

```bash
# Basic functionality test
docker-compose -f docker-compose.demo.yml exec kubectl-demo /scripts/demo-test.sh

# Performance test (50 iterations per tier)
docker-compose -f docker-compose.demo.yml exec kubectl-demo /scripts/performance-test.sh
```

### Stop Demo

```bash
docker-compose -f docker-compose.demo.yml down
```

## 📊 Demo Services

| Service | Port | URL | Description |
|---------|------|-----|-------------|
| Nautilus TEE | 8080 | http://localhost:8080 | TEE simulation with secure memory |
| Sui Blockchain | 9000 | http://localhost:9000 | Blockchain simulation for staking |
| Walrus Storage | 9002 | http://localhost:9002 | Distributed storage simulation |
| K3s API Server | 6443 | https://localhost:6443 | Kubernetes API endpoint |

## 🧪 Test Scenarios

### 1. Basic Functionality Test (`demo-test.sh`)

- ✅ Cluster connectivity
- ✅ Node status verification
- ✅ Namespace creation
- ✅ Application deployment
- ✅ Service connectivity
- ✅ ConfigMap storage (Nautilus TEE)
- ✅ Secret storage (Nautilus TEE secure)
- ✅ Performance measurement

### 2. Performance Test (`performance-test.sh`)

- 🔥 **Hot Tier**: kubectl get nodes (TEE Memory) - Target: <50ms
- 🌡️ **Warm Tier**: kubectl get pods (Sui Blockchain) - Target: <3s
- 🧊 **Cold Tier**: kubectl get configmap (Walrus Storage) - Target: <30s
- 📝 **Resource Creation**: Real-time performance analysis

### Expected Output

```
🎉 K3s-DaaS Demo Test Summary
=============================
✅ All tests completed successfully!

📊 Cluster Information:
NAME              STATUS   ROLES                  AGE   VERSION
k3s-daas-master   Ready    control-plane,master   5m    v1.28.0+k3s1
k3s-daas-worker   Ready    <none>                 4m    v1.28.0+k3s1

⚡ Performance Target: <50ms response times achieved through 3-tier storage
🔐 Security: Intel SGX/TDX simulation with TEE attestation
🏗️ Architecture: kubectl → Nautilus TEE → K3s-DaaS flow demonstrated
```

## 🔧 Configuration

### DaaS Configuration (`k3s-daas-config.yaml`)

```yaml
daas:
  enabled: true
  nautilus:
    tee_endpoint: "http://nautilus-tee:8080"
    api_key: "demo-key-nautilus"
    performance_target: "50ms"
  sui:
    rpc_endpoint: "http://sui-blockchain:9000"
    contract_package: "0xabcdef1234567890"
  walrus:
    api_endpoint: "http://walrus-storage:9002"
  seal:
    min_stake: 1000000
    token_validity: "24h"
```

## 🏭 Production Deployment

For production deployment, replace simulation services with:

- **Real Nautilus TEE**: Intel SGX/TDX hardware with actual enclaves
- **Live Sui Network**: Mainnet or devnet with deployed DaaS contracts
- **Walrus Network**: Production Walrus storage cluster
- **Stake Management**: Real SUI tokens for worker node participation

## 🔍 Monitoring and Debugging

### Check Service Health

```bash
# Nautilus TEE health
curl http://localhost:8080/api/v1/health

# Sui blockchain health
curl http://localhost:9000/health

# Walrus storage health
curl http://localhost:9002/health

# K3s cluster health
curl -k https://localhost:6443/livez
```

### View Logs

```bash
# All services
docker-compose -f docker-compose.demo.yml logs

# Specific service
docker-compose -f docker-compose.demo.yml logs k3s-daas-master
docker-compose -f docker-compose.demo.yml logs nautilus-tee
docker-compose -f docker-compose.demo.yml logs sui-blockchain
docker-compose -f docker-compose.demo.yml logs walrus-storage
```

### Interactive Debugging

```bash
# Access kubectl container
docker-compose -f docker-compose.demo.yml exec kubectl-demo bash

# Access master node
docker-compose -f docker-compose.demo.yml exec k3s-daas-master bash

# Check kubeconfig
docker-compose -f docker-compose.demo.yml exec kubectl-demo cat /k3s-data/k3s.yaml
```

## 📁 Project Structure

```
k3s-daas/
├── pkg/
│   ├── nautilus/client.go          # Nautilus TEE integration
│   ├── storage/router.go           # 3-tier storage routing
│   ├── walrus/storage.go           # Walrus distributed storage
│   ├── sui/client.go               # Sui blockchain client
│   └── security/daas_config.go     # DaaS configuration
├── docker-compose.demo.yml         # Demo environment
├── Dockerfile.k3s-daas            # K3s-DaaS container
├── start-demo.sh                  # Demo startup script
├── demo-scripts/
│   ├── demo-test.sh               # Functionality tests
│   └── performance-test.sh        # Performance benchmarks
└── DEMO-README.md                 # This file
```

## 🔬 Technical Details

### Nautilus TEE Integration

- Replaces etcd with Intel SGX/TDX secure enclaves
- Provides hardware-level security guarantees
- Maintains cluster state in encrypted memory
- Supports remote attestation for trust verification

### Sui Blockchain Integration

- Manages staker authentication through smart contracts
- Handles governance and voting mechanisms
- Provides transparent stake verification
- Enables decentralized node management

### Walrus Storage Integration

- Distributed storage for large files and archives
- Erasure coding for redundancy and availability
- Content-addressed storage with cryptographic proofs
- Seamless integration with Kubernetes volumes

### 3-Tier Storage Architecture

1. **Hot Tier (TEE Memory)**: Active cluster operations requiring <50ms response
2. **Warm Tier (Sui Blockchain)**: Metadata and configuration requiring 1-3s response
3. **Cold Tier (Walrus Storage)**: Archives and large files requiring 5-30s response

## 🚨 Security Features

- **Hardware Security**: Intel SGX/TDX enclaves protect cluster state
- **Blockchain Authentication**: Sui smart contracts verify node stakes
- **Token-based Access**: Seal tokens authenticate worker node participation
- **Encrypted Storage**: All data encrypted at rest and in transit
- **Remote Attestation**: TEE quote verification for trust establishment

## 📈 Performance Optimization

- **Memory Caching**: Frequently accessed data cached in TEE memory
- **Intelligent Routing**: Automatic tier selection based on access patterns
- **Batch Operations**: Multiple requests combined for efficiency
- **Prefetching**: Predictive loading of related resources
- **Load Balancing**: Distributed across multiple storage backends

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Implement changes following the DaaS architecture
4. Add tests for new functionality
5. Update documentation
6. Submit a pull request

## 📄 License

MIT License - see LICENSE file for details

## 🆘 Support

- **Issues**: GitHub Issues for bug reports
- **Discussions**: GitHub Discussions for questions
- **Documentation**: See `architecture/` directory for detailed designs
- **Demo Issues**: Check Docker logs and service health endpoints

---

**K3s-DaaS** - Bringing hardware security, blockchain governance, and decentralized storage to Kubernetes! 🚀