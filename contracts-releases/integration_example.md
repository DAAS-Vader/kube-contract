# K8s-DaaS Contract Integration Example

## Overview
이 문서는 Sui Move 컨트랙트와 nautilus-control 간의 실제 통합 시나리오를 보여줍니다.

## Event Flow Architecture

```
[User/DApp] → [Sui Contract] → [Event] → [nautilus-control] → [K3s API] → [Worker Nodes]
```

## 1. Contract Event Structures

### K8sAPIRequestEvent
```move
struct K8sAPIRequestEvent has copy, drop {
    request_id: String,        // "req_1695123456_0x123..."
    method: String,            // "POST", "GET", "DELETE", etc.
    resource: String,          // "pods", "services", "deployments"
    namespace: String,         // "default", "production", etc.
    name: String,             // "nginx-pod", "redis-service"
    payload: String,          // YAML/JSON for creation
    seal_token: String,       // TEE authentication
    requester: address,       // 0x123...
    priority: u8,             // 1-10
    timestamp: u64,           // Unix timestamp ms
}
```

### WorkerNodeEvent
```move
struct WorkerNodeEvent has copy, drop {
    action: String,           // "register", "unregister", "heartbeat"
    node_id: String,          // "worker-node-001"
    seal_token: String,       // TEE token
    stake_amount: u64,        // Staking amount in SUI
    worker_address: address,  // Worker node address
    timestamp: u64,           // Unix timestamp ms
}
```

## 2. Usage Examples

### Example 1: Pod Creation via Contract
```bash
# 1. Call Sui contract function
sui client call \
  --package $PACKAGE_ID \
  --module main \
  --function create_pod \
  --args \
    $CLUSTER_OBJECT_ID \
    "nginx-pod" \
    "default" \
    "nginx:latest" \
    "seal_token_123" \
    5

# 2. nautilus-control receives WebSocket event:
{
  "type": "k8s_daas::main::K8sAPIRequestEvent",
  "parsedJson": {
    "request_id": "req_1695123456_0x123",
    "method": "POST",
    "resource": "pods",
    "namespace": "default",
    "name": "nginx-pod",
    "payload": "apiVersion: v1\nkind: Pod\n...",
    "seal_token": "seal_token_123",
    "requester": "0x123...",
    "priority": 5,
    "timestamp": 1695123456789
  }
}

# 3. nautilus-control executes:
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: nginx-pod
  namespace: default
spec:
  containers:
  - name: nginx
    image: nginx:latest
EOF
```

### Example 2: Worker Node Registration
```bash
# 1. Worker node calls contract
sui client call \
  --package $PACKAGE_ID \
  --module main \
  --function register_worker_node \
  --args \
    $CLUSTER_OBJECT_ID \
    "worker-node-001" \
    "seal_token_worker_123" \
    $STAKE_COIN_ID

# 2. nautilus-control receives event:
{
  "type": "k8s_daas::main::WorkerNodeEvent",
  "parsedJson": {
    "action": "register",
    "node_id": "worker-node-001",
    "seal_token": "seal_token_worker_123",
    "stake_amount": 1000000000,
    "worker_address": "0x456...",
    "timestamp": 1695123456789
  }
}

# 3. nautilus-control generates join token and notifies worker
```

## 3. nautilus-control WebSocket Subscription

### Subscription Code
```go
// WebSocket subscription filter
subscriptionRequest := map[string]interface{}{
    "method": "suix_subscribeEvent",
    "params": []interface{}{
        map[string]interface{}{
            "Package": contractPackageID,
            "EventType": map[string]interface{}{
                "Module": "main",
                "EventName": "K8sAPIRequestEvent",
            },
        },
    },
}
```

### Event Processing
```go
func (s *SuiIntegration) processContractEvent(event *SuiContractEvent) {
    switch event.Type {
    case "k8s_daas::main::K8sAPIRequestEvent":
        s.handleK8sAPIRequest(event)
    case "k8s_daas::main::WorkerNodeEvent":
        s.handleWorkerNodeEvent(event)
    case "k8s_daas::main::K8sAPIResultEvent":
        s.handleAPIResult(event)
    }
}
```

## 4. Complete Integration Test Scenario

### Step 1: Deploy Contract
```bash
cd contracts-releases
sui client publish --gas-budget 100000000
export PACKAGE_ID=<published_package_id>
export CLUSTER_ID=<shared_cluster_object_id>
```

### Step 2: Start nautilus-control
```bash
export CONTRACT_ADDRESS=$PACKAGE_ID
export PRIVATE_KEY="your_private_key"
export SUI_RPC_URL="wss://fullnode.testnet.sui.io:443"

docker run -d \
  --name nautilus-control \
  -p 6443:6443 \
  -p 8080:8080 \
  -e CONTRACT_ADDRESS=$CONTRACT_ADDRESS \
  -e PRIVATE_KEY=$PRIVATE_KEY \
  -e SUI_RPC_URL=$SUI_RPC_URL \
  nautilus-control:v2
```

### Step 3: Execute K8s Operations via Contract
```bash
# Create namespace
sui client call \
  --package $PACKAGE_ID \
  --module main \
  --function execute_k8s_api \
  --args \
    $CLUSTER_ID \
    "POST" \
    "namespaces" \
    "" \
    "production" \
    '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"production"}}' \
    "seal_token_123" \
    8

# Deploy application
sui client call \
  --package $PACKAGE_ID \
  --module main \
  --function deploy_pods_batch \
  --args \
    $CLUSTER_ID \
    "web-app" \
    "production" \
    "nginx:latest" \
    3 \
    "seal_token_123"

# Check pods
sui client call \
  --package $PACKAGE_ID \
  --module main \
  --function execute_k8s_api \
  --args \
    $CLUSTER_ID \
    "GET" \
    "pods" \
    "production" \
    "" \
    "" \
    "seal_token_123" \
    1
```

### Step 4: Verify Results
```bash
# Check nautilus-control logs
docker logs nautilus-control

# Check K8s cluster directly
kubectl get pods -n production
kubectl get namespaces
```

## 5. Expected Event Flow

1. **Contract Call** → Sui transaction creates K8sAPIRequestEvent
2. **Event Emission** → WebSocket delivers event to nautilus-control
3. **Event Processing** → nautilus-control parses and validates event
4. **kubectl Execution** → Actual K8s API commands executed
5. **Result Emission** → nautilus-control emits K8sAPIResultEvent back to contract
6. **State Update** → Contract state and user applications can react to results

## 6. Key Integration Points

### Authentication
- **Seal Tokens**: TEE-based authentication for secure operations
- **Address Verification**: Contract verifies sender permissions
- **Staking Requirements**: Worker nodes must stake SUI tokens

### Event Filtering
- **Package ID**: Filter events from specific contract package
- **Module Names**: Filter by contract modules (main, events)
- **Event Types**: Filter specific event structures

### Error Handling
- **Invalid Requests**: Contract validates before emitting events
- **kubectl Failures**: nautilus-control reports errors via events
- **Network Issues**: Retry logic and fallback mechanisms

This architecture provides a complete blockchain-to-Kubernetes bridge where smart contract calls trigger real cluster operations.