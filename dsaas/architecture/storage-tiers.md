# Nautilus TEE 3-Tier Storage Architecture

## Overview

The Nautilus TEE Kubernetes system implements a sophisticated 3-tier storage architecture optimized for different access patterns, performance requirements, and data lifecycle stages. This design ensures optimal performance while managing cost and complexity across hot, warm, and cold storage layers.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                    TEE Master Node                              │
├─────────────────────────────────────────────────────────────────┤
│  HOT LAYER (TEE Memory) - <50ms                                │
│  ┌─────────────────┬─────────────────┬─────────────────────────┐ │
│  │ Pod States      │ Node Health     │ Recent Events (1h)      │ │
│  │ • Running       │ • CPU/Memory    │ • Pod lifecycle         │ │
│  │ • Pending       │ • Network       │ • Node events           │ │
│  │ • Failed        │ • Disk I/O      │ • Scheduling decisions  │ │
│  │ • Terminating   │ • Heartbeat     │ • Resource allocations  │ │
│  └─────────────────┴─────────────────┴─────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                                │
                                │ Migration (age-based)
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│  WARM LAYER (Sui Blockchain) - 1-3 seconds                     │
│  ┌─────────────────┬─────────────────┬─────────────────────────┐ │
│  │ Service Config  │ ConfigMaps      │ RBAC Policies           │ │
│  │ • Endpoints     │ • App configs   │ • Roles                 │ │
│  │ • Load balancer │ • Environment   │ • RoleBindings          │ │
│  │ • Ingress rules │ • Secrets refs  │ • ServiceAccounts       │ │
│  │ • DNS records   │ • Certificates  │ • NetworkPolicies       │ │
│  └─────────────────┴─────────────────┴─────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                                │
                                │ Archival (24h+ retention)
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│  COLD LAYER (Walrus) - 5-30 seconds                            │
│  ┌─────────────────┬─────────────────┬─────────────────────────┐ │
│  │ Logs Archive    │ Backups         │ Container Images        │ │
│  │ • Application   │ • Cluster state │ • Base images           │ │
│  │ • System logs   │ • Config backup │ • Application images    │ │
│  │ • Audit trails  │ • Data exports  │ • Security patches      │ │
│  │ • Metrics       │ • Snapshots     │ • Custom layers         │ │
│  └─────────────────┴─────────────────┴─────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Tier 1: Hot Layer (TEE Memory)

### Purpose
Ultra-fast access to frequently used, time-sensitive data that requires immediate response times.

### Performance Targets
- **Latency**: <50ms for all operations
- **Throughput**: 10,000+ operations/second
- **Memory Allocation**: 3GB of 4GB total TEE memory
- **Availability**: 99.99% within TEE enclave

### Data Categories

#### 1. Current Pod States (1.5GB allocation)
```javascript
// Optimized pod state structure
const PodState = {
    namespace: string,           // 8 bytes
    name: string,               // 32 bytes avg
    phase: enum,                // 1 byte (Pending=1, Running=2, etc.)
    nodeName: string,           // 16 bytes
    podIP: ipv4,               // 4 bytes
    containerStates: Array,     // 64 bytes avg per container
    lastUpdate: timestamp,      // 8 bytes
    resourceUsage: {           // 24 bytes
        cpu: float32,
        memory: int64,
        network: int32
    }
    // Total: ~160 bytes per pod
    // 10,000 pods = 1.6MB base + indexes = ~1.5GB
};
```

**Storage Optimization:**
- Compressed binary format using MessagePack
- Memory pools for frequent allocations
- Copy-on-write for shared data structures
- LRU eviction for pods older than 1 hour

#### 2. Node Health Metrics (800MB allocation)
```javascript
const NodeHealth = {
    nodeId: string,            // 16 bytes
    cpuUtilization: float32,   // 4 bytes
    memoryUtilization: float32, // 4 bytes
    diskUtilization: float32,  // 4 bytes
    networkLatency: int16,     // 2 bytes
    lastHeartbeat: timestamp,  // 8 bytes
    daasStake: int64,          // 8 bytes
    connectionStatus: enum,    // 1 byte
    // Total: ~48 bytes per node
    // 1,000 nodes + historical data (15min) = ~800MB
};
```

**Collection Strategy:**
- Real-time updates via WebSocket from worker nodes
- 15-minute rolling window for trend analysis
- Automatic failover detection with <30s detection time
- Performance baseline establishment for scoring

#### 3. Recent Events Buffer (700MB allocation)
```javascript
const EventEntry = {
    timestamp: int64,          // 8 bytes
    type: enum,               // 1 byte
    namespace: string,        // 8 bytes avg
    objectName: string,       // 32 bytes avg
    reason: string,           // 64 bytes avg
    message: string,          // 128 bytes avg
    involvedObject: {         // 32 bytes
        kind: string,
        name: string,
        uid: string
    }
    // Total: ~280 bytes per event
    // 1-hour buffer = ~700MB
};
```

**Event Categories:**
- Pod lifecycle events (created, scheduled, started, failed)
- Node events (ready, not ready, resource pressure)
- Scheduling decisions and their rationale
- Resource allocation and deallocation events
- Security and authentication events

### Access Patterns
- **Read Heavy**: 95% reads, 5% writes
- **Query Types**: Point lookups by name/namespace, range scans by time
- **Caching**: L1 CPU cache optimization with hot data preloading
- **Indexing**: Hash maps for O(1) lookup, B-trees for range queries

### Data Consistency
- **TEE-internal**: Strong consistency within single TEE instance
- **Cross-TEE**: Eventual consistency with 100ms propagation target
- **Conflict Resolution**: Last-writer-wins with timestamp ordering
- **Backup**: Continuous streaming to Warm layer every 30 seconds

## Tier 2: Warm Layer (Sui Blockchain)

### Purpose
Persistent storage for configuration data, policies, and service definitions that require strong consistency and audit trails.

### Performance Targets
- **Latency**: 1-3 seconds for reads, 2-5 seconds for writes
- **Throughput**: 100-500 transactions/second
- **Consistency**: Strong consistency with Byzantine fault tolerance
- **Availability**: 99.9% with global replication

### Data Categories

#### 1. Service Endpoints and Configuration (Smart Contract: `service_registry`)
```move
module nautilus::service_registry {
    struct ServiceEndpoint has key {
        id: UID,
        name: String,
        namespace: String,
        cluster_ip: vector<u8>,    // IPv4 address
        ports: vector<ServicePort>,
        selector: Table<String, String>, // Label selectors
        session_affinity: String,
        load_balancer_ip: Option<vector<u8>>,
        ingress_rules: vector<IngressRule>,
        created_at: u64,
        updated_at: u64,
    }

    struct ServicePort has store, copy, drop {
        name: String,
        port: u16,
        target_port: u16,
        protocol: String, // TCP/UDP/SCTP
    }
}
```

**Update Patterns:**
- Service creation/deletion: Immediate write to blockchain
- Endpoint updates: Batched every 10 seconds for efficiency
- Load balancer changes: Real-time updates with event emission
- DNS record management: Automatic TTL-based propagation

#### 2. ConfigMaps and Secrets Metadata (Smart Contract: `config_store`)
```move
module nautilus::config_store {
    struct ConfigMap has key {
        id: UID,
        name: String,
        namespace: String,
        data_hash: vector<u8>,      // SHA-256 of actual data
        walrus_blob_id: String,     // Reference to Walrus storage
        immutable: bool,
        created_at: u64,
        updated_at: u64,
        access_count: u64,
    }

    struct SecretMetadata has key {
        id: UID,
        name: String,
        namespace: String,
        secret_type: String,        // Opaque, TLS, DockerRegistry, etc.
        encrypted_hash: vector<u8>, // Encrypted reference
        walrus_blob_id: String,     // Encrypted data in Walrus
        last_rotated: u64,
        expires_at: Option<u64>,
    }
}
```

**Security Model:**
- ConfigMap data: Stored in Walrus, hash verification on Sui
- Secrets: Double-encrypted (TEE + Walrus) with key rotation
- Access control: Integrated with RBAC system
- Audit trail: All access logged with user attribution

#### 3. RBAC Policies and Access Control (Smart Contract: `rbac_system`)
```move
module nautilus::rbac_system {
    struct Role has key {
        id: UID,
        name: String,
        namespace: Option<String>,  // None for cluster-wide roles
        rules: vector<PolicyRule>,
        created_at: u64,
        stake_requirement: u64,     // DaaS integration
    }

    struct RoleBinding has key {
        id: UID,
        name: String,
        namespace: Option<String>,
        subjects: vector<Subject>,  // Users, groups, service accounts
        role_ref: ID,              // Reference to Role
        granted_by: address,       // Who granted the permission
        granted_at: u64,
        expires_at: Option<u64>,
    }

    struct PolicyRule has store, copy, drop {
        api_groups: vector<String>,
        resources: vector<String>,
        verbs: vector<String>,      // get, list, create, update, patch, delete
        resource_names: vector<String>,
    }
}
```

**Policy Evaluation:**
- Permission checks: 1-3 second blockchain reads
- Policy updates: Immediate propagation to TEE cache
- Inheritance: Namespace roles inherit from cluster roles
- DaaS integration: Stake-based access controls

### Blockchain Integration Strategy

#### Transaction Optimization
```typescript
// Batched transaction strategy
interface BatchTransaction {
    operations: BlockchainOperation[];
    maxBatchSize: 50;           // Operations per batch
    batchTimeout: 10_000;       // 10 seconds max wait
    priorityLevels: {
        CRITICAL: 0,    // Security, RBAC changes
        HIGH: 1,        // Service endpoints
        NORMAL: 2,      // ConfigMap updates
        LOW: 3          // Metadata changes
    };
}
```

#### Data Synchronization
- **TEE → Blockchain**: Write-through cache with async batching
- **Blockchain → TEE**: Event subscription with guaranteed delivery
- **Conflict Resolution**: Blockchain as source of truth
- **Rollback Strategy**: Automatic revert on TEE failure

## Tier 3: Cold Layer (Walrus Decentralized Storage)

### Purpose
Long-term archival storage for logs, backups, and infrequently accessed data with cost optimization and durability guarantees.

### Performance Targets
- **Latency**: 5-30 seconds for data retrieval
- **Throughput**: Variable based on blob size and network conditions
- **Durability**: 99.999999999% (11 9's) with erasure coding
- **Cost**: <$0.01 per GB per month

### Data Categories

#### 1. Logs Archive (Retention: 1 year)
```typescript
interface LogArchive {
    blobId: string;              // Walrus blob identifier
    timeRange: {
        start: Date;
        end: Date;
    };
    logSources: {
        pods: string[];          // Pod names
        nodes: string[];         // Node names
        components: string[];    // System components
    };
    compression: 'gzip' | 'lz4' | 'zstd';
    encryptionKey: string;       // TEE-managed key reference
    indexData: {
        lineCount: number;
        errorCount: number;
        warnCount: number;
        keywords: string[];      // For search acceleration
    };
    size: {
        original: number;        // Bytes before compression
        compressed: number;      // Bytes after compression
        ratio: number;          // Compression ratio
    };
}
```

**Archival Strategy:**
- **Real-time Logs**: Streamed to TEE memory buffer (15-minute window)
- **Batch Processing**: Every 15 minutes, compress and upload to Walrus
- **Indexing**: Extract keywords and error patterns for fast search
- **Retention**: 24 hours in TEE → 7 days in Sui → 1 year in Walrus

#### 2. Cluster State Backups (Retention: 3 months)
```typescript
interface ClusterBackup {
    blobId: string;
    timestamp: Date;
    backupType: 'full' | 'incremental' | 'differential';
    clusterMetadata: {
        version: string;
        nodeCount: number;
        podCount: number;
        namespaces: string[];
    };
    resources: {
        pods: WalrusRef;         // Reference to pod definitions
        services: WalrusRef;     // Service configurations
        configMaps: WalrusRef;   // Application configs
        secrets: WalrusRef;      // Encrypted secrets
        rbac: WalrusRef;        // RBAC policies
    };
    integrity: {
        checksum: string;        // SHA-256 of entire backup
        signature: string;       // TEE attestation signature
    };
}
```

**Backup Schedule:**
- **Full Backup**: Weekly, complete cluster state
- **Incremental**: Daily, changes since last backup
- **Differential**: Hourly, changes since last full backup
- **Emergency**: Triggered by critical events or manual request

#### 3. Container Images and Artifacts (Retention: 6 months)
```typescript
interface ContainerRegistry {
    imageDigest: string;         // SHA-256 of image
    repository: string;
    tag: string;
    walrusBlobId: string;       // Image layers in Walrus
    manifest: {
        mediaType: string;
        schemaVersion: number;
        layers: LayerReference[];
    };
    securityScan: {
        vulnerabilities: VulnerabilityReport;
        lastScanned: Date;
        riskLevel: 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';
    };
    usage: {
        pullCount: number;
        lastPulled: Date;
        usedByPods: string[];
    };
}
```

**Image Management:**
- **Layer Deduplication**: Store unique layers once across all images
- **Progressive Loading**: Stream image layers during pod startup
- **Security Integration**: Automatic vulnerability scanning
- **Garbage Collection**: Remove unused images after 30 days

### Walrus Integration Architecture

#### Data Upload Strategy
```typescript
class WalrusStorageManager {
    async uploadBatch(data: BlobData[]): Promise<UploadResult[]> {
        // 1. Compress data using optimal algorithm
        const compressed = await this.compressData(data);

        // 2. Encrypt with TEE-managed keys
        const encrypted = await this.encryptData(compressed);

        // 3. Split large files into chunks (max 32MB per blob)
        const chunks = this.chunkData(encrypted, MAX_BLOB_SIZE);

        // 4. Upload with erasure coding for durability
        const uploadPromises = chunks.map(chunk =>
            this.walrusClient.store(chunk, {
                redundancy: 3,       // 3x replication
                erasureCoding: true, // Additional protection
                locality: 'global'   // Distribute globally
            })
        );

        return Promise.all(uploadPromises);
    }
}
```

#### Retrieval Optimization
```typescript
class WalrusRetrievalCache {
    private cache = new Map<string, CachedBlob>();
    private preloadQueue = new PriorityQueue<PreloadRequest>();

    async get(blobId: string): Promise<Blob> {
        // 1. Check local cache first
        if (this.cache.has(blobId)) {
            return this.cache.get(blobId).data;
        }

        // 2. Predictive preloading based on access patterns
        this.scheduleRelatedPreloads(blobId);

        // 3. Parallel retrieval from multiple Walrus nodes
        const blob = await this.parallelRetrieve(blobId);

        // 4. Cache for future access
        this.cache.set(blobId, {
            data: blob,
            accessTime: Date.now(),
            accessCount: 1
        });

        return blob;
    }
}
```

## Data Migration Strategy

### Migration Triggers

#### Time-Based Migration
```typescript
interface MigrationPolicy {
    hotToCold: {
        podStates: '1 hour',       // Pod data after termination
        events: '1 hour',          // Events to Sui for audit
        metrics: '15 minutes'      // Aggregate and archive
    };
    warmToCold: {
        configMaps: '7 days',      // Unused configs
        oldServices: '30 days',    // Deprecated services
        auditLogs: '24 hours'     // Security audit trail
    };
    retention: {
        hotLayer: '1 hour',
        warmLayer: '30 days',
        coldLayer: '1 year'
    };
}
```

#### Event-Driven Migration
```typescript
enum MigrationTrigger {
    PodTermination = 'pod_terminated',
    ServiceDeleted = 'service_deleted',
    ConfigMapUnused = 'configmap_unused',
    NodeOffline = 'node_offline',
    SecurityEvent = 'security_event',
    ManualArchive = 'manual_archive'
}

class MigrationOrchestrator {
    async handleEvent(trigger: MigrationTrigger, data: any) {
        switch (trigger) {
            case MigrationTrigger.PodTermination:
                await this.migratePodData(data.podId);
                break;
            case MigrationTrigger.ServiceDeleted:
                await this.archiveServiceConfig(data.serviceId);
                break;
            // ... other cases
        }
    }
}
```

### Migration Implementation

#### Hot → Warm Migration
```typescript
class HotToWarmMigrator {
    async migratePodState(pod: PodState): Promise<void> {
        // 1. Create blockchain transaction for pod lifecycle record
        const txBuilder = new TransactionBuilder();
        txBuilder.moveCall({
            target: '0x1::pod_lifecycle::record_termination',
            arguments: [
                pod.namespace,
                pod.name,
                pod.finalStatus,
                pod.exitCode,
                pod.resourceUsage,
                pod.terminationTime
            ]
        });

        // 2. Submit to Sui blockchain
        await this.suiClient.executeTransaction(txBuilder.build());

        // 3. Remove from hot storage
        this.hotStorage.deletePod(pod.id);

        // 4. Emit migration event
        this.emit('pod-migrated', {
            podId: pod.id,
            fromTier: 'hot',
            toTier: 'warm',
            timestamp: Date.now()
        });
    }

    async migrateEvents(events: EventEntry[]): Promise<void> {
        // Batch events by time window and migrate to blockchain
        const batches = this.batchEventsByTime(events, 300); // 5-minute batches

        for (const batch of batches) {
            await this.createEventBatch(batch);
        }
    }
}
```

#### Warm → Cold Migration
```typescript
class WarmToColdMigrator {
    async migrateConfigMap(configMapId: string): Promise<void> {
        // 1. Retrieve full config data from blockchain
        const config = await this.suiClient.getObject(configMapId);

        // 2. Compress and encrypt the data
        const compressedData = await gzip(JSON.stringify(config.data));
        const encryptedData = await this.teeEncrypt(compressedData);

        // 3. Upload to Walrus
        const blobId = await this.walrusClient.store(encryptedData);

        // 4. Update blockchain record with Walrus reference
        await this.updateConfigMapReference(configMapId, blobId);

        // 5. Remove large data from blockchain, keep metadata
        await this.trimBlockchainData(configMapId);
    }

    async migrateLogs(timeWindow: TimeWindow): Promise<void> {
        // 1. Collect all logs in time window from various sources
        const logs = await this.collectLogs(timeWindow);

        // 2. Create searchable index
        const index = this.createLogIndex(logs);

        // 3. Compress logs with high ratio
        const compressed = await this.compressLogs(logs, 'zstd');

        // 4. Upload to Walrus with metadata
        const archiveBlob = await this.walrusClient.store(compressed);
        const indexBlob = await this.walrusClient.store(index);

        // 5. Create archive record on blockchain
        await this.createArchiveRecord({
            timeWindow,
            logBlob: archiveBlob,
            indexBlob: indexBlob,
            stats: this.calculateLogStats(logs)
        });
    }
}
```

### Migration Monitoring and Recovery

#### Performance Monitoring
```typescript
interface MigrationMetrics {
    migrationLatency: {
        hotToWarm: number;     // Average time in ms
        warmToCold: number;    // Average time in seconds
    };
    throughput: {
        itemsPerSecond: number;
        bytesPerSecond: number;
    };
    reliability: {
        successRate: number;    // Percentage of successful migrations
        retryRate: number;      // Percentage requiring retries
        errorRate: number;      // Percentage of failures
    };
    costs: {
        suiTransactionFees: number;   // SUI tokens per migration
        walrusStorageCosts: number;   // USD per GB stored
        teeComputeCosts: number;      // Compute cost per operation
    };
}
```

#### Recovery Mechanisms
```typescript
class MigrationRecovery {
    async handleFailedMigration(migrationId: string): Promise<void> {
        const migration = await this.getMigrationRecord(migrationId);

        switch (migration.failureType) {
            case 'blockchain_timeout':
                await this.retryBlockchainTransaction(migration);
                break;
            case 'walrus_upload_failed':
                await this.retryWalrusUpload(migration);
                break;
            case 'data_corruption':
                await this.recoverFromBackup(migration);
                break;
            case 'tee_encryption_error':
                await this.regenerateEncryptionKeys(migration);
                break;
        }
    }

    async verifyMigrationIntegrity(migrationId: string): Promise<boolean> {
        // 1. Verify data exists in target tier
        // 2. Verify data integrity with checksums
        // 3. Verify data is accessible and decryptable
        // 4. Verify source data has been properly cleaned up
        return true;
    }
}
```

## Performance Benchmarks and SLA

### Service Level Agreements

| Tier | Operation | Target Latency | Throughput | Availability |
|------|-----------|----------------|------------|--------------|
| Hot (TEE) | Pod state read | <10ms | 10,000 ops/sec | 99.99% |
| Hot (TEE) | Event query | <50ms | 5,000 ops/sec | 99.99% |
| Hot (TEE) | Node health | <5ms | 15,000 ops/sec | 99.99% |
| Warm (Sui) | Service lookup | 1-3s | 500 ops/sec | 99.9% |
| Warm (Sui) | Config read | 2-5s | 200 ops/sec | 99.9% |
| Warm (Sui) | RBAC check | 1-2s | 1,000 ops/sec | 99.9% |
| Cold (Walrus) | Log retrieval | 5-30s | 10-100 MB/sec | 99.5% |
| Cold (Walrus) | Backup restore | 30s-5min | 50-200 MB/sec | 99.5% |
| Cold (Walrus) | Image pull | 10-60s | 20-100 MB/sec | 99.5% |

### Cost Optimization Targets

| Resource | Current Cost | Target Cost | Optimization Strategy |
|----------|--------------|-------------|----------------------|
| TEE Memory | $0.10/GB/hour | $0.08/GB/hour | Compression + efficient data structures |
| Sui Transactions | $0.001/tx | $0.0005/tx | Batching + priority optimization |
| Walrus Storage | $0.01/GB/month | $0.007/GB/month | Deduplication + compression |
| Total TCO | $100/node/month | $75/node/month | 25% reduction through optimization |

## Future Enhancements

### Intelligent Data Placement
- **ML-based prediction**: Predict data access patterns for optimal placement
- **Dynamic tier adjustment**: Automatically move frequently accessed cold data to warm tier
- **Geographic optimization**: Place data closer to requesting regions

### Advanced Compression
- **Context-aware compression**: Use K8s schema knowledge for better compression
- **Differential compression**: Store only changes between similar objects
- **Real-time compression**: Compress data as it's written to maximize space efficiency

### Enhanced Security
- **Zero-knowledge proofs**: Verify data integrity without revealing content
- **Homomorphic encryption**: Perform computations on encrypted data
- **Quantum-resistant encryption**: Prepare for post-quantum cryptography

This 3-tier storage architecture provides a robust foundation for the Nautilus TEE Kubernetes system, balancing performance, cost, and reliability across different data access patterns and lifecycle stages.