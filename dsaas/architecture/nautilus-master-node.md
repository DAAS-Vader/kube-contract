# Nautilus TEE Kubernetes Master Node Architecture

## Overview

This document describes the complete architecture for a Kubernetes master node running within Nautilus TEE, designed to replace all traditional K8s master components while achieving <50ms response times for kubectl commands and supporting up to 10,000 pods within a 2GB memory budget.

## Table of Contents

1. [System Architecture](#system-architecture)
2. [In-Memory etcd Replacement](#in-memory-etcd-replacement)
3. [Scheduler Implementation](#scheduler-implementation)
4. [API Server Design](#api-server-design)
5. [Controller Manager Components](#controller-manager-components)
6. [Performance Optimization](#performance-optimization)
7. [TEE Integration](#tee-integration)
8. [Implementation Roadmap](#implementation-roadmap)

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Nautilus TEE Environment                 │
│                                                             │
│  ┌─────────────────────────────────────────────────────────┐│
│  │              K8s Master Node (JavaScript)              ││
│  │                                                         ││
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    ││
│  │  │ API Server  │  │  Scheduler  │  │ Controller  │    ││
│  │  │             │  │             │  │ Manager     │    ││
│  │  │ • kubectl   │  │ • Node      │  │ • ReplicaSet│    ││
│  │  │   handlers  │  │   scoring   │  │ • Service   │    ││
│  │  │ • WebSocket │  │ • Resource  │  │ • Endpoint  │    ││
│  │  │   endpoints │  │   placement │  │   controllers│   ││
│  │  │ • Auth      │  │ • Fast      │  │             │    ││
│  │  │   middleware│  │   scheduling│  │             │    ││
│  │  └─────────────┘  └─────────────┘  └─────────────┘    ││
│  │           │               │                │           ││
│  │           └───────────────┼────────────────┘           ││
│  │                           │                            ││
│  │  ┌─────────────────────────────────────────────────────┐││
│  │  │         In-Memory etcd Replacement                  │││
│  │  │                                                     │││
│  │  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐  │││
│  │  │  │ Pod     │ │ Node    │ │ Service │ │Endpoint │  │││
│  │  │  │ Store   │ │ Store   │ │ Store   │ │ Store   │  │││
│  │  │  │ Map<K,V>│ │ Map<K,V>│ │ Map<K,V>│ │ Map<K,V>│  │││
│  │  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘  │││
│  │  │                                                     │││
│  │  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐  │││
│  │  │  │ConfigMap│ │ Secret  │ │  PV/PVC │ │ Events  │  │││
│  │  │  │ Store   │ │ Store   │ │ Store   │ │ Store   │  │││
│  │  │  │ Map<K,V>│ │ Map<K,V>│ │ Map<K,V>│ │ Map<K,V>│  │││
│  │  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘  │││
│  │  └─────────────────────────────────────────────────────┘││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
                                │
                ┌───────────────────────────────┐
                │        External Interface      │
                │                               │
                │  ┌─────────┐  ┌─────────────┐ │
                │  │kubectl  │  │   Node      │ │
                │  │clients  │  │   agents    │ │
                │  │         │  │   (kubelet) │ │
                │  └─────────┘  └─────────────┘ │
                └───────────────────────────────┘
```

## In-Memory etcd Replacement

### Design Principles

The in-memory storage system replaces etcd with optimized data structures designed for the 2GB memory constraint while supporting 10,000 pods.

### Memory Budget Allocation

```javascript
// Memory budget breakdown for 10,000 pods
const MEMORY_BUDGET = {
  pods: 1200 * 1024 * 1024,        // 1.2GB - Pod objects (~120 bytes/pod)
  nodes: 200 * 1024 * 1024,        // 200MB - Node objects (~20KB/node for 100 nodes)
  services: 100 * 1024 * 1024,     // 100MB - Service objects
  endpoints: 200 * 1024 * 1024,    // 200MB - Endpoint objects
  configmaps: 150 * 1024 * 1024,   // 150MB - ConfigMap objects
  secrets: 50 * 1024 * 1024,       // 50MB - Secret objects (encrypted)
  events: 50 * 1024 * 1024,        // 50MB - Event objects (circular buffer)
  indexes: 50 * 1024 * 1024,       // 50MB - Secondary indexes
  total: 2000 * 1024 * 1024        // 2GB total
};
```

### Core Data Structures

```javascript
class InMemoryStore {
  constructor() {
    // Primary stores using Maps for O(1) lookup
    this.pods = new Map();           // namespace/name -> Pod object
    this.nodes = new Map();          // name -> Node object
    this.services = new Map();       // namespace/name -> Service object
    this.endpoints = new Map();      // namespace/name -> Endpoints object
    this.configMaps = new Map();     // namespace/name -> ConfigMap object
    this.secrets = new Map();        // namespace/name -> Secret object
    this.events = new CircularBuffer(1000); // Last 1000 events

    // Secondary indexes for fast queries
    this.podsByNode = new Map();     // node -> Set<pod keys>
    this.podsByNamespace = new Map(); // namespace -> Set<pod keys>
    this.servicesByNamespace = new Map(); // namespace -> Set<service keys>
    this.labelSelectors = new Map(); // selector -> Set<resource keys>

    // Watch streams for real-time updates
    this.watchers = new Map();       // resource type -> Set<WebSocket>

    // Performance metrics
    this.metrics = {
      totalObjects: 0,
      memoryUsage: 0,
      queryCount: 0,
      avgQueryTime: 0
    };
  }

  // Fast pod storage optimized for memory efficiency
  storePod(namespace, name, podSpec) {
    const key = `${namespace}/${name}`;
    const compactPod = this.compressPodSpec(podSpec);

    this.pods.set(key, compactPod);
    this.updateIndexes('pod', key, compactPod);
    this.notifyWatchers('pods', 'ADDED', compactPod);

    return compactPod;
  }

  // Optimized pod spec compression
  compressPodSpec(podSpec) {
    return {
      // Core fields only (reduced from ~2KB to ~120 bytes)
      name: podSpec.metadata.name,
      namespace: podSpec.metadata.namespace,
      uid: podSpec.metadata.uid,
      node: podSpec.spec.nodeName,
      phase: podSpec.status.phase,
      containers: podSpec.spec.containers.map(c => ({
        name: c.name,
        image: c.image,
        resources: c.resources
      })),
      labels: podSpec.metadata.labels || {},
      annotations: this.compressAnnotations(podSpec.metadata.annotations),
      created: podSpec.metadata.creationTimestamp,
      conditions: this.compressConditions(podSpec.status.conditions)
    };
  }

  // Fast lookup methods with <1ms response time
  getPod(namespace, name) {
    const start = performance.now();
    const key = `${namespace}/${name}`;
    const pod = this.pods.get(key);

    this.metrics.queryCount++;
    this.updateQueryTime(performance.now() - start);

    return pod;
  }

  // Efficient list operations with filtering
  listPods(namespace, labelSelector, fieldSelector) {
    const start = performance.now();
    let results = [];

    if (namespace) {
      const podKeys = this.podsByNamespace.get(namespace) || new Set();
      results = Array.from(podKeys).map(key => this.pods.get(key));
    } else {
      results = Array.from(this.pods.values());
    }

    // Apply label selector filtering
    if (labelSelector) {
      results = this.filterByLabels(results, labelSelector);
    }

    // Apply field selector filtering
    if (fieldSelector) {
      results = this.filterByFields(results, fieldSelector);
    }

    this.updateQueryTime(performance.now() - start);
    return results;
  }

  // Memory-efficient indexing
  updateIndexes(resourceType, key, resource) {
    switch (resourceType) {
      case 'pod':
        this.updatePodIndexes(key, resource);
        break;
      case 'service':
        this.updateServiceIndexes(key, resource);
        break;
    }
  }

  updatePodIndexes(key, pod) {
    // Node index
    if (pod.node) {
      if (!this.podsByNode.has(pod.node)) {
        this.podsByNode.set(pod.node, new Set());
      }
      this.podsByNode.get(pod.node).add(key);
    }

    // Namespace index
    if (!this.podsByNamespace.has(pod.namespace)) {
      this.podsByNamespace.set(pod.namespace, new Set());
    }
    this.podsByNamespace.get(pod.namespace).add(key);

    // Label selector indexes
    this.updateLabelIndexes('pod', key, pod.labels);
  }

  // Watch functionality for real-time updates
  addWatcher(resourceType, websocket, options = {}) {
    const watchKey = `${resourceType}-${Date.now()}`;

    if (!this.watchers.has(resourceType)) {
      this.watchers.set(resourceType, new Map());
    }

    this.watchers.get(resourceType).set(watchKey, {
      socket: websocket,
      namespace: options.namespace,
      labelSelector: options.labelSelector,
      resourceVersion: options.resourceVersion || '0'
    });

    return watchKey;
  }

  notifyWatchers(resourceType, eventType, resource) {
    const typeWatchers = this.watchers.get(resourceType);
    if (!typeWatchers) return;

    typeWatchers.forEach((watcher, watchKey) => {
      if (this.matchesWatcher(watcher, resource)) {
        const event = {
          type: eventType,
          object: resource,
          timestamp: Date.now()
        };

        try {
          watcher.socket.send(JSON.stringify(event));
        } catch (error) {
          // Remove broken watchers
          typeWatchers.delete(watchKey);
        }
      }
    });
  }
}

// Specialized storage for different resource types
class NodeStore {
  constructor() {
    this.nodes = new Map();
    this.nodesByZone = new Map();
    this.nodesByInstance = new Map();
    this.capacityIndex = new Map(); // For scheduler optimization
  }

  storeNode(name, nodeSpec) {
    const compactNode = {
      name: nodeSpec.metadata.name,
      labels: nodeSpec.metadata.labels || {},
      capacity: nodeSpec.status.capacity,
      allocatable: nodeSpec.status.allocatable,
      conditions: this.compressNodeConditions(nodeSpec.status.conditions),
      addresses: nodeSpec.status.addresses,
      zone: nodeSpec.metadata.labels?.['topology.kubernetes.io/zone'],
      instance: nodeSpec.metadata.labels?.['node.kubernetes.io/instance-type']
    };

    this.nodes.set(name, compactNode);
    this.updateNodeIndexes(name, compactNode);

    return compactNode;
  }

  // Fast node selection for scheduler
  getAvailableNodes(resourceRequirements) {
    const candidates = [];

    for (const [name, node] of this.nodes) {
      if (this.nodeHasCapacity(node, resourceRequirements)) {
        candidates.push(name);
      }
    }

    return candidates;
  }

  nodeHasCapacity(node, requirements) {
    const cpu = parseInt(node.allocatable.cpu) || 0;
    const memory = parseInt(node.allocatable.memory) || 0;

    return cpu >= (requirements.cpu || 0) &&
           memory >= (requirements.memory || 0);
  }
}

class ServiceStore {
  constructor() {
    this.services = new Map();
    this.servicesBySelector = new Map(); // For endpoint controller
  }

  storeService(namespace, name, serviceSpec) {
    const key = `${namespace}/${name}`;
    const compactService = {
      name: serviceSpec.metadata.name,
      namespace: serviceSpec.metadata.namespace,
      uid: serviceSpec.metadata.uid,
      selector: serviceSpec.spec.selector,
      ports: serviceSpec.spec.ports,
      clusterIP: serviceSpec.spec.clusterIP,
      type: serviceSpec.spec.type
    };

    this.services.set(key, compactService);
    this.updateServiceIndexes(key, compactService);

    return compactService;
  }
}
```

### Memory Management

```javascript
class MemoryManager {
  constructor(budget = 2 * 1024 * 1024 * 1024) { // 2GB
    this.budget = budget;
    this.used = 0;
    this.stores = new Map();
    this.compressionEnabled = true;
    this.gcThreshold = 0.85; // Trigger GC at 85% usage
  }

  allocateStore(name, size) {
    if (this.used + size > this.budget) {
      this.triggerGarbageCollection();
    }

    this.stores.set(name, size);
    this.used += size;
  }

  triggerGarbageCollection() {
    // Remove old events
    this.cleanupEvents();

    // Compress large objects
    this.compressLargeObjects();

    // Remove unused indexes
    this.cleanupIndexes();
  }

  estimateObjectSize(obj) {
    return JSON.stringify(obj).length * 2; // Rough estimate
  }
}
```

## Scheduler Implementation

### JavaScript-based Scheduler

```javascript
class NautilusScheduler {
  constructor(store) {
    this.store = store;
    this.nodeScorer = new NodeScorer();
    this.predicates = new SchedulingPredicates();
    this.priorities = new SchedulingPriorities();
    this.cache = new SchedulerCache();

    // Performance tracking
    this.metrics = {
      schedulingLatency: [],
      throughput: 0,
      successRate: 0
    };
  }

  // Main scheduling function - target <10ms
  async schedulePod(pod) {
    const startTime = performance.now();

    try {
      // Phase 1: Node filtering (predicates) - ~2ms
      const candidateNodes = await this.filterNodes(pod);

      if (candidateNodes.length === 0) {
        return { error: 'No suitable nodes found' };
      }

      // Phase 2: Node scoring (priorities) - ~5ms
      const scoredNodes = await this.scoreNodes(pod, candidateNodes);

      // Phase 3: Select best node - ~1ms
      const selectedNode = this.selectBestNode(scoredNodes);

      // Phase 4: Update pod binding - ~1ms
      await this.bindPodToNode(pod, selectedNode);

      const latency = performance.now() - startTime;
      this.recordSchedulingMetrics(latency, true);

      return {
        node: selectedNode,
        latency: latency,
        candidateCount: candidateNodes.length
      };

    } catch (error) {
      const latency = performance.now() - startTime;
      this.recordSchedulingMetrics(latency, false);
      return { error: error.message };
    }
  }

  // Fast node filtering using predicates
  async filterNodes(pod) {
    const allNodes = Array.from(this.store.nodes.keys());
    const candidates = [];

    for (const nodeName of allNodes) {
      const node = this.store.nodes.get(nodeName);

      // Check all predicates
      if (await this.predicates.checkAll(pod, node)) {
        candidates.push(nodeName);
      }
    }

    return candidates;
  }

  // Node scoring algorithm
  async scoreNodes(pod, candidateNodes) {
    const scores = new Map();

    for (const nodeName of candidateNodes) {
      const node = this.store.nodes.get(nodeName);
      let totalScore = 0;

      // Resource utilization score (0-100)
      totalScore += this.priorities.resourceUtilization(pod, node) * 0.3;

      // Node affinity score (0-100)
      totalScore += this.priorities.nodeAffinity(pod, node) * 0.2;

      // Pod affinity/anti-affinity score (0-100)
      totalScore += await this.priorities.podAffinity(pod, node) * 0.2;

      // Load balancing score (0-100)
      totalScore += this.priorities.loadBalance(node) * 0.2;

      // Zone spreading score (0-100)
      totalScore += this.priorities.zoneSpreading(pod, node) * 0.1;

      scores.set(nodeName, totalScore);
    }

    return scores;
  }

  selectBestNode(scoredNodes) {
    let bestNode = null;
    let bestScore = -1;

    for (const [nodeName, score] of scoredNodes) {
      if (score > bestScore) {
        bestScore = score;
        bestNode = nodeName;
      }
    }

    return bestNode;
  }

  async bindPodToNode(pod, nodeName) {
    // Update pod spec with node assignment
    pod.spec.nodeName = nodeName;
    pod.status.phase = 'Pending';

    // Store updated pod
    this.store.storePod(pod.metadata.namespace, pod.metadata.name, pod);

    // Update node allocation tracking
    this.updateNodeAllocation(nodeName, pod);
  }
}

// Scheduling predicates (filters)
class SchedulingPredicates {
  async checkAll(pod, node) {
    return (
      this.nodeResourcesFit(pod, node) &&
      this.nodeAffinity(pod, node) &&
      this.podFitsHostPorts(pod, node) &&
      this.nodeUnschedulable(node) &&
      this.checkTaints(pod, node)
    );
  }

  nodeResourcesFit(pod, node) {
    const podResources = this.getPodResourceRequests(pod);
    const nodeCapacity = node.allocatable;

    // Check CPU
    const cpuRequest = podResources.cpu || 0;
    const availableCPU = parseInt(nodeCapacity.cpu) - this.getAllocatedCPU(node);

    if (cpuRequest > availableCPU) return false;

    // Check Memory
    const memoryRequest = podResources.memory || 0;
    const availableMemory = parseInt(nodeCapacity.memory) - this.getAllocatedMemory(node);

    if (memoryRequest > availableMemory) return false;

    return true;
  }

  nodeAffinity(pod, node) {
    const affinity = pod.spec.affinity?.nodeAffinity;
    if (!affinity) return true;

    // Check required node affinity
    if (affinity.requiredDuringSchedulingIgnoredDuringExecution) {
      return this.checkNodeSelector(
        affinity.requiredDuringSchedulingIgnoredDuringExecution,
        node
      );
    }

    return true;
  }

  checkTaints(pod, node) {
    const taints = node.spec?.taints || [];
    const tolerations = pod.spec?.tolerations || [];

    for (const taint of taints) {
      if (taint.effect === 'NoSchedule' || taint.effect === 'NoExecute') {
        if (!this.tolerationMatches(tolerations, taint)) {
          return false;
        }
      }
    }

    return true;
  }
}

// Scheduling priorities (scoring)
class SchedulingPriorities {
  resourceUtilization(pod, node) {
    const podResources = this.getPodResourceRequests(pod);
    const nodeCapacity = node.allocatable;

    const cpuScore = this.calculateResourceScore(
      podResources.cpu,
      nodeCapacity.cpu,
      this.getAllocatedCPU(node)
    );

    const memoryScore = this.calculateResourceScore(
      podResources.memory,
      nodeCapacity.memory,
      this.getAllocatedMemory(node)
    );

    return (cpuScore + memoryScore) / 2;
  }

  calculateResourceScore(request, capacity, allocated) {
    const available = capacity - allocated;
    const utilization = (allocated + request) / capacity;

    // Prefer nodes with ~70% utilization (avoid both underutilization and overutilization)
    const optimal = 0.7;
    const score = 100 - Math.abs(utilization - optimal) * 100;

    return Math.max(0, score);
  }

  loadBalance(node) {
    // Prefer nodes with fewer pods
    const podCount = this.getNodePodCount(node);
    const maxPods = 110; // Typical node limit

    return Math.max(0, 100 - (podCount / maxPods) * 100);
  }

  async podAffinity(pod, node) {
    const affinity = pod.spec.affinity?.podAffinity;
    const antiAffinity = pod.spec.affinity?.podAntiAffinity;

    let score = 50; // Neutral score

    if (affinity) {
      score += await this.calculateAffinityScore(pod, node, affinity, true);
    }

    if (antiAffinity) {
      score += await this.calculateAffinityScore(pod, node, antiAffinity, false);
    }

    return Math.min(100, Math.max(0, score));
  }
}

// Scheduler cache for performance optimization
class SchedulerCache {
  constructor() {
    this.nodeInfoCache = new Map();
    this.podAffinityCache = new Map();
    this.resourceUsageCache = new Map();
    this.cacheTimeout = 30000; // 30 seconds
  }

  getNodeInfo(nodeName) {
    const cached = this.nodeInfoCache.get(nodeName);
    if (cached && Date.now() - cached.timestamp < this.cacheTimeout) {
      return cached.data;
    }
    return null;
  }

  setNodeInfo(nodeName, info) {
    this.nodeInfoCache.set(nodeName, {
      data: info,
      timestamp: Date.now()
    });
  }
}
```

## API Server Design

### Core API Server Structure

```javascript
class NautilusAPIServer {
  constructor(store, scheduler, controllerManager) {
    this.store = store;
    this.scheduler = scheduler;
    this.controllerManager = controllerManager;
    this.server = null;
    this.wsServer = null;

    // Authentication and authorization
    this.authMiddleware = new AuthenticationMiddleware();
    this.rbac = new RBACAuthorizer();

    // Performance tracking for <50ms goal
    this.metrics = {
      requestCount: 0,
      averageLatency: 0,
      p99Latency: 0,
      errorRate: 0
    };

    this.setupRoutes();
  }

  setupRoutes() {
    // Core kubectl command handlers
    this.routes = {
      // Pod operations
      'GET /api/v1/pods': this.listPods.bind(this),
      'GET /api/v1/namespaces/:namespace/pods': this.listNamespacedPods.bind(this),
      'GET /api/v1/namespaces/:namespace/pods/:name': this.getPod.bind(this),
      'POST /api/v1/namespaces/:namespace/pods': this.createPod.bind(this),
      'PUT /api/v1/namespaces/:namespace/pods/:name': this.updatePod.bind(this),
      'DELETE /api/v1/namespaces/:namespace/pods/:name': this.deletePod.bind(this),

      // Node operations
      'GET /api/v1/nodes': this.listNodes.bind(this),
      'GET /api/v1/nodes/:name': this.getNode.bind(this),

      // Service operations
      'GET /api/v1/services': this.listServices.bind(this),
      'GET /api/v1/namespaces/:namespace/services': this.listNamespacedServices.bind(this),
      'GET /api/v1/namespaces/:namespace/services/:name': this.getService.bind(this),

      // Watch endpoints for real-time updates
      'GET /api/v1/watch/pods': this.watchPods.bind(this),
      'GET /api/v1/watch/nodes': this.watchNodes.bind(this),
      'GET /api/v1/watch/services': this.watchServices.bind(this)
    };
  }

  // High-performance kubectl get pods handler
  async listPods(req, res) {
    const startTime = performance.now();

    try {
      // Extract query parameters
      const namespace = req.query.namespace;
      const labelSelector = req.query.labelSelector;
      const fieldSelector = req.query.fieldSelector;
      const limit = parseInt(req.query.limit) || 500;
      const continue_ = req.query.continue;

      // Fast path for cached responses
      const cacheKey = this.generateCacheKey('pods', req.query);
      let pods = this.cache.get(cacheKey);

      if (!pods) {
        // Fetch from store with optimized query
        pods = this.store.listPods(namespace, labelSelector, fieldSelector);

        // Cache for 5 seconds to handle rapid kubectl calls
        this.cache.set(cacheKey, pods, 5000);
      }

      // Apply pagination
      const paginatedResult = this.paginate(pods, limit, continue_);

      // Format response for kubectl compatibility
      const response = {
        apiVersion: 'v1',
        kind: 'PodList',
        metadata: {
          resourceVersion: this.store.getResourceVersion(),
          remainingItemCount: paginatedResult.remainingCount
        },
        items: paginatedResult.items
      };

      // Add continue token if more items exist
      if (paginatedResult.continueToken) {
        response.metadata.continue = paginatedResult.continueToken;
      }

      const latency = performance.now() - startTime;
      this.recordMetrics('GET /api/v1/pods', latency, true);

      // Ensure <50ms response time
      if (latency > 50) {
        console.warn(`Slow query detected: ${latency}ms for GET /api/v1/pods`);
      }

      res.json(response);

    } catch (error) {
      const latency = performance.now() - startTime;
      this.recordMetrics('GET /api/v1/pods', latency, false);
      this.handleError(res, error);
    }
  }

  // Fast single pod retrieval
  async getPod(req, res) {
    const startTime = performance.now();

    try {
      const { namespace, name } = req.params;

      // Direct hash lookup - O(1) operation
      const pod = this.store.getPod(namespace, name);

      if (!pod) {
        return res.status(404).json({
          kind: 'Status',
          apiVersion: 'v1',
          status: 'Failure',
          reason: 'NotFound',
          message: `pods "${name}" not found`
        });
      }

      const latency = performance.now() - startTime;
      this.recordMetrics('GET /api/v1/pods/:name', latency, true);

      res.json({
        apiVersion: 'v1',
        kind: 'Pod',
        ...pod
      });

    } catch (error) {
      const latency = performance.now() - startTime;
      this.recordMetrics('GET /api/v1/pods/:name', latency, false);
      this.handleError(res, error);
    }
  }

  // Pod creation with scheduler integration
  async createPod(req, res) {
    const startTime = performance.now();

    try {
      const { namespace } = req.params;
      const podSpec = req.body;

      // Validate pod specification
      const validation = this.validatePodSpec(podSpec);
      if (!validation.valid) {
        return res.status(400).json({
          kind: 'Status',
          status: 'Failure',
          reason: 'Invalid',
          message: validation.message
        });
      }

      // Add metadata
      podSpec.metadata.namespace = namespace;
      podSpec.metadata.uid = this.generateUID();
      podSpec.metadata.creationTimestamp = new Date().toISOString();

      // Store pod in pending state
      podSpec.status = { phase: 'Pending' };
      const storedPod = this.store.storePod(namespace, podSpec.metadata.name, podSpec);

      // Trigger scheduling asynchronously
      if (!podSpec.spec.nodeName) {
        setImmediate(() => {
          this.scheduler.schedulePod(storedPod);
        });
      }

      const latency = performance.now() - startTime;
      this.recordMetrics('POST /api/v1/pods', latency, true);

      res.status(201).json({
        apiVersion: 'v1',
        kind: 'Pod',
        ...storedPod
      });

    } catch (error) {
      const latency = performance.now() - startTime;
      this.recordMetrics('POST /api/v1/pods', latency, false);
      this.handleError(res, error);
    }
  }

  // WebSocket handler for real-time updates
  async watchPods(req, res) {
    // Upgrade to WebSocket
    const ws = await this.upgradeToWebSocket(req, res);

    const namespace = req.query.namespace;
    const labelSelector = req.query.labelSelector;
    const resourceVersion = req.query.resourceVersion || '0';

    // Register watcher
    const watchKey = this.store.addWatcher('pods', ws, {
      namespace,
      labelSelector,
      resourceVersion
    });

    // Send initial state if resourceVersion is 0
    if (resourceVersion === '0') {
      const pods = this.store.listPods(namespace, labelSelector);
      for (const pod of pods) {
        ws.send(JSON.stringify({
          type: 'ADDED',
          object: pod
        }));
      }
    }

    // Handle WebSocket close
    ws.on('close', () => {
      this.store.removeWatcher('pods', watchKey);
    });
  }

  // Performance optimization methods
  generateCacheKey(resource, params) {
    return `${resource}:${JSON.stringify(params)}`;
  }

  paginate(items, limit, continueToken) {
    let startIndex = 0;

    if (continueToken) {
      startIndex = parseInt(continueToken) || 0;
    }

    const endIndex = startIndex + limit;
    const paginatedItems = items.slice(startIndex, endIndex);

    return {
      items: paginatedItems,
      remainingCount: Math.max(0, items.length - endIndex),
      continueToken: endIndex < items.length ? endIndex.toString() : null
    };
  }

  recordMetrics(endpoint, latency, success) {
    this.metrics.requestCount++;

    // Update average latency
    this.metrics.averageLatency =
      (this.metrics.averageLatency * (this.metrics.requestCount - 1) + latency) /
      this.metrics.requestCount;

    // Update P99 latency (simplified)
    if (latency > this.metrics.p99Latency) {
      this.metrics.p99Latency = latency;
    }

    // Update error rate
    if (!success) {
      this.metrics.errorRate =
        (this.metrics.errorRate * (this.metrics.requestCount - 1) + 100) /
        this.metrics.requestCount;
    } else {
      this.metrics.errorRate =
        (this.metrics.errorRate * (this.metrics.requestCount - 1)) /
        this.metrics.requestCount;
    }
  }
}

// Authentication middleware
class AuthenticationMiddleware {
  constructor() {
    this.tokenCache = new Map();
    this.cacheTimeout = 300000; // 5 minutes
  }

  async authenticate(req, res, next) {
    const authHeader = req.headers.authorization;

    if (!authHeader) {
      return res.status(401).json({
        kind: 'Status',
        status: 'Failure',
        reason: 'Unauthorized',
        message: 'Authorization header required'
      });
    }

    try {
      const token = authHeader.replace('Bearer ', '');
      const user = await this.validateToken(token);

      req.user = user;
      next();

    } catch (error) {
      res.status(401).json({
        kind: 'Status',
        status: 'Failure',
        reason: 'Unauthorized',
        message: 'Invalid token'
      });
    }
  }

  async validateToken(token) {
    // Check cache first
    const cached = this.tokenCache.get(token);
    if (cached && Date.now() - cached.timestamp < this.cacheTimeout) {
      return cached.user;
    }

    // Validate token (JWT or similar)
    const user = await this.verifyJWT(token);

    // Cache result
    this.tokenCache.set(token, {
      user,
      timestamp: Date.now()
    });

    return user;
  }

  async verifyJWT(token) {
    // JWT verification logic here
    // Return user object with name, groups, etc.
    return {
      name: 'system:admin',
      groups: ['system:masters'],
      uid: 'admin'
    };
  }
}

// RBAC Authorization
class RBACAuthorizer {
  constructor() {
    this.roleBindings = new Map();
    this.clusterRoleBindings = new Map();
  }

  authorize(user, resource, verb, namespace) {
    // Check cluster-wide permissions
    if (this.hasClusterPermission(user, resource, verb)) {
      return true;
    }

    // Check namespace-specific permissions
    if (namespace && this.hasNamespacePermission(user, resource, verb, namespace)) {
      return true;
    }

    return false;
  }

  hasClusterPermission(user, resource, verb) {
    // Simplified RBAC check
    return user.groups?.includes('system:masters');
  }

  hasNamespacePermission(user, resource, verb, namespace) {
    // Check role bindings in namespace
    return false;
  }
}
```

## Controller Manager Components

### Controller Manager Architecture

```javascript
class NautilusControllerManager {
  constructor(store, apiServer) {
    this.store = store;
    this.apiServer = apiServer;
    this.controllers = new Map();
    this.eventQueue = new EventQueue();
    this.metrics = {
      controllersRunning: 0,
      eventsProcessed: 0,
      reconciliationErrors: 0
    };

    this.initializeControllers();
  }

  initializeControllers() {
    // Register core controllers
    this.controllers.set('replicaset', new ReplicaSetController(this.store, this.apiServer));
    this.controllers.set('service', new ServiceController(this.store, this.apiServer));
    this.controllers.set('endpoint', new EndpointController(this.store, this.apiServer));
    this.controllers.set('node', new NodeController(this.store, this.apiServer));
    this.controllers.set('namespace', new NamespaceController(this.store, this.apiServer));
  }

  async start() {
    // Start all controllers
    for (const [name, controller] of this.controllers) {
      try {
        await controller.start();
        console.log(`Controller ${name} started successfully`);
        this.metrics.controllersRunning++;
      } catch (error) {
        console.error(`Failed to start controller ${name}:`, error);
      }
    }

    // Start event processing loop
    this.startEventProcessing();
  }

  startEventProcessing() {
    setInterval(() => {
      this.processEvents();
    }, 100); // Process events every 100ms
  }

  async processEvents() {
    const events = this.eventQueue.drain();

    for (const event of events) {
      try {
        await this.routeEvent(event);
        this.metrics.eventsProcessed++;
      } catch (error) {
        console.error('Error processing event:', error);
        this.metrics.reconciliationErrors++;
      }
    }
  }

  async routeEvent(event) {
    // Route events to appropriate controllers
    for (const [name, controller] of this.controllers) {
      if (controller.handles(event)) {
        await controller.processEvent(event);
      }
    }
  }
}

// ReplicaSet Controller
class ReplicaSetController {
  constructor(store, apiServer) {
    this.store = store;
    this.apiServer = apiServer;
    this.reconcileInterval = 30000; // 30 seconds
    this.maxConcurrentReconciles = 5;
  }

  handles(event) {
    return event.resource === 'replicasets' ||
           (event.resource === 'pods' && event.ownerReference?.kind === 'ReplicaSet');
  }

  async start() {
    // Start reconciliation loop
    setInterval(() => {
      this.reconcileAll();
    }, this.reconcileInterval);

    // Watch for ReplicaSet changes
    this.store.addWatcher('replicasets', null, {
      callback: (event) => this.handleReplicaSetEvent(event)
    });
  }

  async reconcileAll() {
    const replicaSets = this.store.listReplicaSets();

    // Process in batches to avoid overwhelming the system
    const batches = this.chunkArray(replicaSets, this.maxConcurrentReconciles);

    for (const batch of batches) {
      await Promise.all(batch.map(rs => this.reconcileReplicaSet(rs)));
    }
  }

  async reconcileReplicaSet(replicaSet) {
    try {
      const desiredReplicas = replicaSet.spec.replicas || 0;
      const selector = replicaSet.spec.selector.matchLabels;

      // Find owned pods
      const ownedPods = this.findOwnedPods(replicaSet, selector);
      const activePods = ownedPods.filter(pod =>
        pod.status.phase !== 'Failed' && pod.status.phase !== 'Succeeded'
      );

      const currentReplicas = activePods.length;

      if (currentReplicas < desiredReplicas) {
        // Scale up - create missing pods
        const podsToCreate = desiredReplicas - currentReplicas;
        await this.createPods(replicaSet, podsToCreate);

      } else if (currentReplicas > desiredReplicas) {
        // Scale down - delete excess pods
        const podsToDelete = currentReplicas - desiredReplicas;
        await this.deletePods(activePods, podsToDelete);
      }

      // Update ReplicaSet status
      await this.updateReplicaSetStatus(replicaSet, {
        replicas: currentReplicas,
        readyReplicas: this.countReadyPods(activePods),
        availableReplicas: this.countAvailablePods(activePods)
      });

    } catch (error) {
      console.error(`Error reconciling ReplicaSet ${replicaSet.metadata.name}:`, error);
    }
  }

  findOwnedPods(replicaSet, selector) {
    const allPods = this.store.listPods(replicaSet.metadata.namespace);

    return allPods.filter(pod => {
      // Check owner reference
      const ownerRef = pod.metadata.ownerReferences?.find(ref =>
        ref.kind === 'ReplicaSet' && ref.name === replicaSet.metadata.name
      );

      if (ownerRef) return true;

      // Check label selector
      return this.matchesSelector(pod.metadata.labels, selector);
    });
  }

  async createPods(replicaSet, count) {
    const podTemplate = replicaSet.spec.template;

    for (let i = 0; i < count; i++) {
      const podSpec = {
        apiVersion: 'v1',
        kind: 'Pod',
        metadata: {
          ...podTemplate.metadata,
          name: this.generatePodName(replicaSet),
          namespace: replicaSet.metadata.namespace,
          ownerReferences: [{
            apiVersion: replicaSet.apiVersion,
            kind: 'ReplicaSet',
            name: replicaSet.metadata.name,
            uid: replicaSet.metadata.uid,
            controller: true
          }]
        },
        spec: podTemplate.spec
      };

      await this.apiServer.createPod(replicaSet.metadata.namespace, podSpec);
    }
  }

  async deletePods(pods, count) {
    // Sort pods to delete the least desirable ones first
    const sortedPods = this.sortPodsForDeletion(pods);

    for (let i = 0; i < count && i < sortedPods.length; i++) {
      const pod = sortedPods[i];
      await this.apiServer.deletePod(pod.metadata.namespace, pod.metadata.name);
    }
  }

  sortPodsForDeletion(pods) {
    return pods.sort((a, b) => {
      // Prefer deleting pods that are not ready
      if (this.isPodReady(a) !== this.isPodReady(b)) {
        return this.isPodReady(a) ? 1 : -1;
      }

      // Prefer deleting newer pods
      return new Date(b.metadata.creationTimestamp) - new Date(a.metadata.creationTimestamp);
    });
  }

  generatePodName(replicaSet) {
    const randomSuffix = Math.random().toString(36).substring(2, 8);
    return `${replicaSet.metadata.name}-${randomSuffix}`;
  }
}

// Service Controller
class ServiceController {
  constructor(store, apiServer) {
    this.store = store;
    this.apiServer = apiServer;
  }

  handles(event) {
    return event.resource === 'services';
  }

  async start() {
    // Watch for Service changes
    this.store.addWatcher('services', null, {
      callback: (event) => this.handleServiceEvent(event)
    });
  }

  async handleServiceEvent(event) {
    const service = event.object;

    switch (event.type) {
      case 'ADDED':
      case 'MODIFIED':
        await this.reconcileService(service);
        break;
      case 'DELETED':
        await this.handleServiceDeletion(service);
        break;
    }
  }

  async reconcileService(service) {
    // Ensure service has necessary fields
    if (!service.spec.clusterIP && service.spec.type === 'ClusterIP') {
      service.spec.clusterIP = this.allocateClusterIP();
      await this.updateService(service);
    }

    // Trigger endpoint controller to update endpoints
    this.triggerEndpointReconciliation(service);
  }

  allocateClusterIP() {
    // Simple IP allocation from service CIDR
    const serviceSubnet = '10.96.0.0/12';
    // Implementation would track allocated IPs
    return '10.96.0.1';
  }
}

// Endpoint Controller
class EndpointController {
  constructor(store, apiServer) {
    this.store = store;
    this.apiServer = apiServer;
  }

  handles(event) {
    return event.resource === 'services' || event.resource === 'pods';
  }

  async start() {
    // Watch for Service and Pod changes
    this.store.addWatcher('services', null, {
      callback: (event) => this.handleServiceEvent(event)
    });

    this.store.addWatcher('pods', null, {
      callback: (event) => this.handlePodEvent(event)
    });
  }

  async handleServiceEvent(event) {
    const service = event.object;
    await this.reconcileEndpoints(service);
  }

  async handlePodEvent(event) {
    // Find services that might be affected by this pod change
    const pod = event.object;
    const services = this.findServicesForPod(pod);

    for (const service of services) {
      await this.reconcileEndpoints(service);
    }
  }

  async reconcileEndpoints(service) {
    if (!service.spec.selector) {
      // Service without selector - endpoints managed manually
      return;
    }

    // Find pods matching the service selector
    const matchingPods = this.findPodsMatchingSelector(
      service.metadata.namespace,
      service.spec.selector
    );

    // Filter to ready pods only
    const readyPods = matchingPods.filter(pod => this.isPodReady(pod));

    // Build endpoint subsets
    const subsets = this.buildEndpointSubsets(readyPods, service.spec.ports);

    // Create or update endpoints object
    const endpoints = {
      apiVersion: 'v1',
      kind: 'Endpoints',
      metadata: {
        name: service.metadata.name,
        namespace: service.metadata.namespace
      },
      subsets: subsets
    };

    await this.store.storeEndpoints(
      endpoints.metadata.namespace,
      endpoints.metadata.name,
      endpoints
    );
  }

  findPodsMatchingSelector(namespace, selector) {
    const pods = this.store.listPods(namespace);

    return pods.filter(pod => this.matchesSelector(pod.metadata.labels, selector));
  }

  buildEndpointSubsets(pods, servicePorts) {
    if (pods.length === 0) return [];

    const addresses = pods.map(pod => ({
      ip: pod.status.podIP,
      targetRef: {
        kind: 'Pod',
        name: pod.metadata.name,
        namespace: pod.metadata.namespace,
        uid: pod.metadata.uid
      }
    }));

    const ports = servicePorts.map(servicePort => ({
      name: servicePort.name,
      port: servicePort.targetPort || servicePort.port,
      protocol: servicePort.protocol || 'TCP'
    }));

    return [{
      addresses: addresses,
      ports: ports
    }];
  }

  matchesSelector(labels, selector) {
    if (!labels || !selector) return false;

    for (const [key, value] of Object.entries(selector)) {
      if (labels[key] !== value) {
        return false;
      }
    }

    return true;
  }

  isPodReady(pod) {
    if (pod.status.phase !== 'Running') return false;

    const readyCondition = pod.status.conditions?.find(c => c.type === 'Ready');
    return readyCondition?.status === 'True';
  }
}

// Event Queue for controller coordination
class EventQueue {
  constructor(maxSize = 10000) {
    this.queue = [];
    this.maxSize = maxSize;
  }

  enqueue(event) {
    if (this.queue.length >= this.maxSize) {
      this.queue.shift(); // Remove oldest event
    }

    this.queue.push({
      ...event,
      timestamp: Date.now()
    });
  }

  drain() {
    const events = [...this.queue];
    this.queue.length = 0;
    return events;
  }

  size() {
    return this.queue.length;
  }
}
```

## Performance Optimization

### Response Time Optimization

```javascript
class PerformanceOptimizer {
  constructor() {
    this.cache = new ResponseCache();
    this.indexManager = new IndexManager();
    this.queryOptimizer = new QueryOptimizer();

    // Target metrics
    this.targetLatency = 50; // 50ms for kubectl commands
    this.targetThroughput = 10000; // 10k requests/second
  }

  // Response caching for frequently accessed data
  class ResponseCache {
    constructor() {
      this.cache = new Map();
      this.ttl = 5000; // 5 second TTL
      this.maxSize = 1000; // Limit memory usage
    }

    get(key) {
      const entry = this.cache.get(key);
      if (!entry) return null;

      if (Date.now() - entry.timestamp > this.ttl) {
        this.cache.delete(key);
        return null;
      }

      return entry.data;
    }

    set(key, data) {
      // Implement LRU eviction
      if (this.cache.size >= this.maxSize) {
        const firstKey = this.cache.keys().next().value;
        this.cache.delete(firstKey);
      }

      this.cache.set(key, {
        data: data,
        timestamp: Date.now()
      });
    }
  }

  // Index management for fast queries
  class IndexManager {
    constructor() {
      this.indexes = new Map();
    }

    createIndex(resourceType, field, extractor) {
      const indexKey = `${resourceType}.${field}`;
      this.indexes.set(indexKey, {
        index: new Map(),
        extractor: extractor
      });
    }

    updateIndex(resourceType, field, key, resource) {
      const indexKey = `${resourceType}.${field}`;
      const indexData = this.indexes.get(indexKey);

      if (indexData) {
        const fieldValue = indexData.extractor(resource);
        if (!indexData.index.has(fieldValue)) {
          indexData.index.set(fieldValue, new Set());
        }
        indexData.index.get(fieldValue).add(key);
      }
    }

    query(resourceType, field, value) {
      const indexKey = `${resourceType}.${field}`;
      const indexData = this.indexes.get(indexKey);

      if (indexData && indexData.index.has(value)) {
        return Array.from(indexData.index.get(value));
      }

      return [];
    }
  }
}

// Memory optimization techniques
class MemoryOptimizer {
  constructor() {
    this.compressionThreshold = 1024; // Compress objects > 1KB
    this.gcInterval = 30000; // GC every 30 seconds

    this.startGarbageCollection();
  }

  startGarbageCollection() {
    setInterval(() => {
      this.runGarbageCollection();
    }, this.gcInterval);
  }

  runGarbageCollection() {
    // Remove expired cache entries
    this.cleanupExpiredEntries();

    // Compress large objects
    this.compressLargeObjects();

    // Run JS garbage collection if available
    if (global.gc) {
      global.gc();
    }
  }

  compressObject(obj) {
    const jsonStr = JSON.stringify(obj);

    if (jsonStr.length > this.compressionThreshold) {
      // Use simple compression (in real implementation, use proper compression)
      return {
        compressed: true,
        data: this.simpleCompress(jsonStr)
      };
    }

    return obj;
  }

  simpleCompress(str) {
    // Placeholder for actual compression algorithm
    return str;
  }
}
```

## TEE Integration

### TEE Security and Attestation

```javascript
class TEEIntegration {
  constructor() {
    this.attestationService = new AttestationService();
    this.secureStorage = new SecureStorage();
    this.encryptionManager = new EncryptionManager();
  }

  async initializeTEE() {
    // Initialize TEE environment
    await this.attestationService.performAttestation();
    await this.secureStorage.initialize();
    await this.encryptionManager.setupKeys();

    console.log('TEE environment initialized successfully');
  }

  // Secure key management within TEE
  class SecureStorage {
    constructor() {
      this.sealedKeys = new Map();
    }

    async initialize() {
      // Initialize secure storage within TEE
      // Load sealed keys from persistent storage
    }

    async sealData(key, data) {
      // Use TEE sealing to encrypt data with hardware keys
      const sealedData = await this.performSealing(data);
      this.sealedKeys.set(key, sealedData);
      return sealedData;
    }

    async unsealData(key) {
      // Decrypt data using TEE unsealing
      const sealedData = this.sealedKeys.get(key);
      if (!sealedData) return null;

      return await this.performUnsealing(sealedData);
    }
  }

  // Memory encryption within TEE
  class EncryptionManager {
    constructor() {
      this.encryptionKey = null;
    }

    async setupKeys() {
      // Generate or derive encryption keys within TEE
      this.encryptionKey = await this.generateTEEKey();
    }

    encryptMemoryPage(data) {
      // Encrypt memory pages for additional security
      return this.encrypt(data, this.encryptionKey);
    }

    decryptMemoryPage(encryptedData) {
      // Decrypt memory pages
      return this.decrypt(encryptedData, this.encryptionKey);
    }
  }

  // Remote attestation
  class AttestationService {
    async performAttestation() {
      // Generate attestation quote
      const quote = await this.generateQuote();

      // Verify quote with attestation service
      const verified = await this.verifyQuote(quote);

      if (!verified) {
        throw new Error('TEE attestation failed');
      }

      return quote;
    }

    async generateQuote() {
      // Generate TEE attestation quote
      return {
        version: 1,
        signType: 1,
        qeSvn: 1,
        pceSvn: 1,
        quote: new Uint8Array(432) // Placeholder
      };
    }
  }
}
```

## Implementation Roadmap

### Phase 1: Core Infrastructure (Weeks 1-4)

1. **Week 1-2: In-Memory Store**
   - Implement basic Map-based storage
   - Add secondary indexes
   - Memory budget enforcement
   - Basic performance monitoring

2. **Week 3-4: API Server Foundation**
   - HTTP server setup
   - Basic kubectl command handlers
   - Authentication middleware
   - Response caching

### Phase 2: Scheduler and Controllers (Weeks 5-8)

1. **Week 5-6: Scheduler Implementation**
   - Port core scheduling logic to JavaScript
   - Node scoring algorithms
   - Resource-aware placement
   - Performance optimization

2. **Week 7-8: Controller Manager**
   - ReplicaSet controller
   - Service controller
   - Endpoint controller
   - Event processing system

### Phase 3: Performance Optimization (Weeks 9-12)

1. **Week 9-10: Response Time Optimization**
   - Advanced caching strategies
   - Query optimization
   - Index management
   - Memory optimization

2. **Week 11-12: TEE Integration**
   - Secure storage implementation
   - Memory encryption
   - Attestation service
   - Security hardening

### Phase 4: Testing and Validation (Weeks 13-16)

1. **Week 13-14: Performance Testing**
   - Load testing framework
   - Latency benchmarking
   - Memory usage validation
   - Throughput testing

2. **Week 15-16: Integration Testing**
   - kubectl compatibility testing
   - End-to-end scenarios
   - Failure testing
   - Documentation

## Success Metrics

### Performance Targets

- **API Response Time**: < 50ms (99th percentile) for kubectl get commands
- **Throughput**: > 10,000 requests/second
- **Memory Usage**: < 2GB for 10,000 pods
- **Scheduling Latency**: < 10ms per pod
- **Controller Reconciliation**: < 30 seconds

### Functional Requirements

- **100% kubectl Compatibility**: All standard kubectl commands work
- **Resource Support**: Pods, Nodes, Services, Endpoints, ConfigMaps, Secrets
- **Watch API**: Real-time updates via WebSocket
- **Authentication**: Token-based authentication
- **Authorization**: Basic RBAC support

### TEE Security Goals

- **Memory Encryption**: All data encrypted in TEE memory
- **Attestation**: Remote attestation verification
- **Sealed Storage**: Persistent data sealed with TEE keys
- **Isolation**: Complete isolation from host OS

This architecture provides a complete roadmap for implementing a high-performance Kubernetes master node within Nautilus TEE, achieving the target <50ms response times while maintaining security and functionality.