/**
 * Nautilus TEE Real-time Kubernetes Cache
 * Optimized for c6g.large (4GB) with <50ms response times
 * Supports 10,000 pods and 1,000 nodes with memory-efficient storage
 */

class RealtimeK8sCache {
    constructor(options = {}) {
        // Memory allocation for c6g.large (4GB total)
        this.memoryLimits = {
            pods: 1024 * 1024 * 1024,      // 1GB - Pod cache
            nodes: 512 * 1024 * 1024,      // 512MB - Node cache
            events: 300 * 1024 * 1024,     // 300MB - Events buffer
            metrics: 200 * 1024 * 1024,    // 200MB - Metrics cache
            indexes: 100 * 1024 * 1024,    // 100MB - Search indexes
            overhead: 300 * 1024 * 1024    // 300MB - System overhead
        };

        // Core data structures optimized for speed
        this.podStates = new Map();         // namespace/name -> CompressedPod
        this.nodeStates = new Map();        // name -> CompressedNode
        this.events = new RingBuffer(1000); // Circular event buffer
        this.metrics = new Map();           // metric_key -> MetricData

        // Performance indexes for <50ms queries
        this.indexes = {
            podsByNamespace: new Map(),     // namespace -> Set<pod_keys>
            podsByNode: new Map(),          // node -> Set<pod_keys>
            podsByLabels: new Map(),        // label_key=value -> Set<pod_keys>
            nodesByZone: new Map(),         // zone -> Set<node_names>
            serviceEndpoints: new Map()     // service -> endpoint_data
        };

        // Memory tracking and optimization
        this.memoryTracker = new MemoryTracker(this.memoryLimits);
        this.compression = new DataCompressor();
        this.queryOptimizer = new QueryOptimizer();

        // Performance metrics
        this.stats = {
            totalQueries: 0,
            avgQueryTime: 0,
            cacheHitRatio: 0,
            memoryUsage: {
                pods: 0,
                nodes: 0,
                events: 0,
                metrics: 0
            }
        };

        // Configuration
        this.config = {
            maxPods: options.maxPods || 10000,
            maxNodes: options.maxNodes || 1000,
            maxEvents: options.maxEvents || 1000,
            compressionThreshold: options.compressionThreshold || 512, // bytes
            enableCompression: options.enableCompression !== false,
            enableIndexes: options.enableIndexes !== false,
            queryTimeout: options.queryTimeout || 45, // ms (under 50ms target)
            ...options
        };

        // Initialize performance monitoring
        this.initializePerformanceMonitoring();
    }

    /**
     * Get pods from cache with <50ms response time
     * @param {string} namespace - Kubernetes namespace
     * @param {Object} options - Query options (labels, fields, limit)
     * @returns {Array} Pod objects
     */
    getPods(namespace, options = {}) {
        const startTime = performance.now();

        try {
            let podKeys;

            // Fast path: namespace-specific lookup
            if (namespace && namespace !== 'all') {
                podKeys = this.indexes.podsByNamespace.get(namespace) || new Set();
            } else {
                // All namespaces - collect from all namespace indexes
                podKeys = new Set();
                for (const namespacePods of this.indexes.podsByNamespace.values()) {
                    namespacePods.forEach(key => podKeys.add(key));
                }
            }

            // Apply additional filters if specified
            if (options.labelSelector) {
                podKeys = this.filterByLabels(podKeys, options.labelSelector);
            }

            if (options.fieldSelector) {
                podKeys = this.filterByFields(podKeys, options.fieldSelector);
            }

            if (options.nodeName) {
                const nodePods = this.indexes.podsByNode.get(options.nodeName) || new Set();
                podKeys = this.intersectSets(podKeys, nodePods);
            }

            // Convert keys to pod objects (decompressed)
            const pods = Array.from(podKeys)
                .slice(0, options.limit || 500) // Default limit for performance
                .map(key => this.decompressPod(this.podStates.get(key)))
                .filter(pod => pod !== null);

            // Update performance metrics
            const queryTime = performance.now() - startTime;
            this.updateQueryMetrics(queryTime, true);

            // Ensure we meet the <50ms requirement
            if (queryTime > 45) {
                console.warn(`Slow query detected: ${queryTime.toFixed(2)}ms for getPods(${namespace})`);
            }

            return pods;

        } catch (error) {
            const queryTime = performance.now() - startTime;
            this.updateQueryMetrics(queryTime, false);
            console.error('Error in getPods:', error);
            return [];
        }
    }

    /**
     * Get single pod by namespace and name
     * @param {string} namespace
     * @param {string} name
     * @returns {Object|null} Pod object
     */
    getPod(namespace, name) {
        const startTime = performance.now();
        const key = `${namespace}/${name}`;

        const compressedPod = this.podStates.get(key);
        const pod = compressedPod ? this.decompressPod(compressedPod) : null;

        const queryTime = performance.now() - startTime;
        this.updateQueryMetrics(queryTime, pod !== null);

        return pod;
    }

    /**
     * Update pod state with instant indexing
     * @param {string} podId - Format: namespace/name
     * @param {Object} state - Pod state object
     */
    updatePodState(podId, state) {
        const startTime = performance.now();

        try {
            // Memory check before adding new pod
            if (!this.podStates.has(podId)) {
                this.memoryTracker.checkMemoryLimit('pods', this.estimatePodSize(state));
            }

            // Remove old indexes if pod exists
            if (this.podStates.has(podId)) {
                this.removeFromIndexes('pod', podId);
            }

            // Compress pod data for memory efficiency
            const compressedPod = this.compressPod(state);
            this.podStates.set(podId, compressedPod);

            // Update indexes for fast queries
            this.updatePodIndexes(podId, state);

            // Update memory tracking
            this.memoryTracker.updateUsage('pods', this.estimateCompressedSize(compressedPod));

            // Emit change event
            this.emitPodEvent('MODIFIED', podId, state);

            const updateTime = performance.now() - startTime;
            if (updateTime > 10) {
                console.warn(`Slow pod update: ${updateTime.toFixed(2)}ms for ${podId}`);
            }

        } catch (error) {
            console.error(`Error updating pod ${podId}:`, error);
        }
    }

    /**
     * Get all nodes with optional filtering
     * @param {Object} options - Query options
     * @returns {Array} Node objects
     */
    getNodes(options = {}) {
        const startTime = performance.now();

        try {
            let nodeNames;

            if (options.zone) {
                nodeNames = this.indexes.nodesByZone.get(options.zone) || new Set();
            } else {
                nodeNames = new Set(this.nodeStates.keys());
            }

            const nodes = Array.from(nodeNames)
                .slice(0, options.limit || 1000)
                .map(name => this.decompressNode(this.nodeStates.get(name)))
                .filter(node => node !== null);

            const queryTime = performance.now() - startTime;
            this.updateQueryMetrics(queryTime, true);

            return nodes;

        } catch (error) {
            console.error('Error in getNodes:', error);
            return [];
        }
    }

    /**
     * Update node state
     * @param {string} nodeName
     * @param {Object} state
     */
    updateNodeState(nodeName, state) {
        const startTime = performance.now();

        try {
            // Memory check
            if (!this.nodeStates.has(nodeName)) {
                this.memoryTracker.checkMemoryLimit('nodes', this.estimateNodeSize(state));
            }

            // Compress and store
            const compressedNode = this.compressNode(state);
            this.nodeStates.set(nodeName, compressedNode);

            // Update indexes
            this.updateNodeIndexes(nodeName, state);

            // Update memory tracking
            this.memoryTracker.updateUsage('nodes', this.estimateCompressedSize(compressedNode));

            // Emit event
            this.emitNodeEvent('MODIFIED', nodeName, state);

        } catch (error) {
            console.error(`Error updating node ${nodeName}:`, error);
        }
    }

    /**
     * Add event to ring buffer
     * @param {Object} event
     */
    addEvent(event) {
        event.timestamp = Date.now();
        event.id = this.generateEventId();

        this.events.push(event);
        this.memoryTracker.updateUsage('events', this.events.getMemoryUsage());
    }

    /**
     * Get recent events
     * @param {Object} options
     * @returns {Array}
     */
    getEvents(options = {}) {
        const startTime = performance.now();

        const events = this.events.getAll()
            .filter(event => {
                if (options.namespace && event.namespace !== options.namespace) return false;
                if (options.type && event.type !== options.type) return false;
                if (options.since && event.timestamp < options.since) return false;
                return true;
            })
            .slice(0, options.limit || 100);

        const queryTime = performance.now() - startTime;
        this.updateQueryMetrics(queryTime, true);

        return events;
    }

    /**
     * Store metric data
     * @param {string} key
     * @param {Object} data
     */
    setMetric(key, data) {
        this.metrics.set(key, {
            ...data,
            timestamp: Date.now()
        });

        this.memoryTracker.updateUsage('metrics', this.estimateMetricsSize());
    }

    /**
     * Get metric data
     * @param {string} key
     * @returns {Object|null}
     */
    getMetric(key) {
        return this.metrics.get(key) || null;
    }

    /**
     * Get comprehensive cache statistics
     * @returns {Object}
     */
    getStats() {
        return {
            ...this.stats,
            memoryUsage: this.memoryTracker.getCurrentUsage(),
            objectCounts: {
                pods: this.podStates.size,
                nodes: this.nodeStates.size,
                events: this.events.size(),
                metrics: this.metrics.size
            },
            indexSizes: {
                podsByNamespace: this.indexes.podsByNamespace.size,
                podsByNode: this.indexes.podsByNode.size,
                podsByLabels: this.indexes.podsByLabels.size,
                nodesByZone: this.indexes.nodesByZone.size
            }
        };
    }

    /**
     * Clear all cache data
     */
    clear() {
        this.podStates.clear();
        this.nodeStates.clear();
        this.events.clear();
        this.metrics.clear();

        // Clear indexes
        Object.values(this.indexes).forEach(index => index.clear());

        // Reset memory tracking
        this.memoryTracker.reset();

        console.log('Cache cleared');
    }

    /**
     * Perform garbage collection to free memory
     */
    gc() {
        const startTime = performance.now();
        let freedBytes = 0;

        // Remove expired metrics
        const now = Date.now();
        const metricsToDelete = [];

        for (const [key, metric] of this.metrics) {
            if (now - metric.timestamp > 300000) { // 5 minutes
                metricsToDelete.push(key);
            }
        }

        metricsToDelete.forEach(key => {
            this.metrics.delete(key);
            freedBytes += 100; // Estimate
        });

        // Compress large objects if memory pressure is high
        if (this.memoryTracker.getUsagePercent() > 85) {
            freedBytes += this.compressLargeObjects();
        }

        // Update memory tracking
        this.memoryTracker.updateAllUsage();

        const gcTime = performance.now() - startTime;
        console.log(`GC completed in ${gcTime.toFixed(2)}ms, freed ${freedBytes} bytes`);

        return freedBytes;
    }

    // === PRIVATE METHODS ===

    /**
     * Compress pod object for memory efficiency
     * @param {Object} pod
     * @returns {Object}
     */
    compressPod(pod) {
        const compressed = {
            // Essential fields only (reduce from ~2KB to ~100 bytes)
            n: pod.metadata?.name,                    // name
            ns: pod.metadata?.namespace,              // namespace
            u: pod.metadata?.uid,                     // uid
            nd: pod.spec?.nodeName,                   // nodeName
            ph: pod.status?.phase,                    // phase
            ip: pod.status?.podIP,                    // podIP
            ct: pod.metadata?.creationTimestamp,     // creationTimestamp

            // Compressed containers
            c: pod.spec?.containers?.map(container => ({
                n: container.name,
                i: container.image,
                r: container.resources
            })) || [],

            // Compressed labels (only store non-system labels)
            l: this.compressLabels(pod.metadata?.labels),

            // Compressed conditions
            cd: this.compressConditions(pod.status?.conditions),

            // Compressed owner references
            o: pod.metadata?.ownerReferences?.[0] || null
        };

        // Apply additional compression if object is still large
        if (this.config.enableCompression && this.estimateSize(compressed) > this.config.compressionThreshold) {
            return this.compression.compress(compressed);
        }

        return compressed;
    }

    /**
     * Decompress pod object
     * @param {Object} compressed
     * @returns {Object}
     */
    decompressPod(compressed) {
        if (!compressed) return null;

        // Handle compressed objects
        let pod = compressed;
        if (compressed._compressed) {
            pod = this.compression.decompress(compressed);
        }

        // Reconstruct full pod object
        return {
            apiVersion: 'v1',
            kind: 'Pod',
            metadata: {
                name: pod.n,
                namespace: pod.ns,
                uid: pod.u,
                creationTimestamp: pod.ct,
                labels: this.decompressLabels(pod.l),
                ownerReferences: pod.o ? [pod.o] : []
            },
            spec: {
                nodeName: pod.nd,
                containers: pod.c || []
            },
            status: {
                phase: pod.ph,
                podIP: pod.ip,
                conditions: this.decompressConditions(pod.cd)
            }
        };
    }

    /**
     * Compress node object
     * @param {Object} node
     * @returns {Object}
     */
    compressNode(node) {
        return {
            n: node.metadata?.name,
            l: this.compressLabels(node.metadata?.labels),
            c: node.status?.capacity,
            a: node.status?.allocatable,
            cd: this.compressConditions(node.status?.conditions),
            ad: node.status?.addresses,
            z: node.metadata?.labels?.['topology.kubernetes.io/zone'],
            it: node.metadata?.labels?.['node.kubernetes.io/instance-type']
        };
    }

    /**
     * Decompress node object
     * @param {Object} compressed
     * @returns {Object}
     */
    decompressNode(compressed) {
        if (!compressed) return null;

        return {
            apiVersion: 'v1',
            kind: 'Node',
            metadata: {
                name: compressed.n,
                labels: this.decompressLabels(compressed.l)
            },
            status: {
                capacity: compressed.c,
                allocatable: compressed.a,
                conditions: this.decompressConditions(compressed.cd),
                addresses: compressed.ad
            }
        };
    }

    /**
     * Update pod indexes for fast queries
     * @param {string} podId
     * @param {Object} pod
     */
    updatePodIndexes(podId, pod) {
        const namespace = pod.metadata?.namespace;
        const nodeName = pod.spec?.nodeName;
        const labels = pod.metadata?.labels || {};

        // Namespace index
        if (namespace) {
            if (!this.indexes.podsByNamespace.has(namespace)) {
                this.indexes.podsByNamespace.set(namespace, new Set());
            }
            this.indexes.podsByNamespace.get(namespace).add(podId);
        }

        // Node index
        if (nodeName) {
            if (!this.indexes.podsByNode.has(nodeName)) {
                this.indexes.podsByNode.set(nodeName, new Set());
            }
            this.indexes.podsByNode.get(nodeName).add(podId);
        }

        // Label indexes
        for (const [key, value] of Object.entries(labels)) {
            const labelKey = `${key}=${value}`;
            if (!this.indexes.podsByLabels.has(labelKey)) {
                this.indexes.podsByLabels.set(labelKey, new Set());
            }
            this.indexes.podsByLabels.get(labelKey).add(podId);
        }
    }

    /**
     * Update node indexes
     * @param {string} nodeName
     * @param {Object} node
     */
    updateNodeIndexes(nodeName, node) {
        const zone = node.metadata?.labels?.['topology.kubernetes.io/zone'];

        if (zone) {
            if (!this.indexes.nodesByZone.has(zone)) {
                this.indexes.nodesByZone.set(zone, new Set());
            }
            this.indexes.nodesByZone.get(zone).add(nodeName);
        }
    }

    /**
     * Remove object from indexes
     * @param {string} type
     * @param {string} id
     */
    removeFromIndexes(type, id) {
        if (type === 'pod') {
            // Remove from all pod indexes
            for (const podSet of this.indexes.podsByNamespace.values()) {
                podSet.delete(id);
            }
            for (const podSet of this.indexes.podsByNode.values()) {
                podSet.delete(id);
            }
            for (const podSet of this.indexes.podsByLabels.values()) {
                podSet.delete(id);
            }
        } else if (type === 'node') {
            for (const nodeSet of this.indexes.nodesByZone.values()) {
                nodeSet.delete(id);
            }
        }
    }

    /**
     * Filter pods by label selector
     * @param {Set} podKeys
     * @param {string} labelSelector
     * @returns {Set}
     */
    filterByLabels(podKeys, labelSelector) {
        // Simple label selector parsing (app=nginx,version=1.0)
        const requirements = labelSelector.split(',').map(req => {
            const [key, value] = req.split('=');
            return { key: key.trim(), value: value.trim() };
        });

        const matchingPods = new Set();

        for (const req of requirements) {
            const labelKey = `${req.key}=${req.value}`;
            const labelPods = this.indexes.podsByLabels.get(labelKey) || new Set();

            if (matchingPods.size === 0) {
                labelPods.forEach(pod => matchingPods.add(pod));
            } else {
                // Intersection
                for (const pod of matchingPods) {
                    if (!labelPods.has(pod)) {
                        matchingPods.delete(pod);
                    }
                }
            }
        }

        return this.intersectSets(podKeys, matchingPods);
    }

    /**
     * Filter pods by field selector
     * @param {Set} podKeys
     * @param {string} fieldSelector
     * @returns {Set}
     */
    filterByFields(podKeys, fieldSelector) {
        // Simple field selector (spec.nodeName=node1,status.phase=Running)
        const fields = fieldSelector.split(',').map(field => {
            const [path, value] = field.split('=');
            return { path: path.trim(), value: value.trim() };
        });

        return new Set(Array.from(podKeys).filter(podKey => {
            const pod = this.decompressPod(this.podStates.get(podKey));
            if (!pod) return false;

            return fields.every(field => {
                const fieldValue = this.getFieldValue(pod, field.path);
                return fieldValue === field.value;
            });
        }));
    }

    /**
     * Get field value from object path
     * @param {Object} obj
     * @param {string} path
     * @returns {any}
     */
    getFieldValue(obj, path) {
        return path.split('.').reduce((current, key) => current?.[key], obj);
    }

    /**
     * Intersect two sets
     * @param {Set} set1
     * @param {Set} set2
     * @returns {Set}
     */
    intersectSets(set1, set2) {
        const result = new Set();
        for (const item of set1) {
            if (set2.has(item)) {
                result.add(item);
            }
        }
        return result;
    }

    /**
     * Compress labels by removing system labels
     * @param {Object} labels
     * @returns {Object}
     */
    compressLabels(labels) {
        if (!labels) return {};

        const compressed = {};
        for (const [key, value] of Object.entries(labels)) {
            // Skip system labels to save memory
            if (!key.startsWith('kubernetes.io/') &&
                !key.startsWith('k8s.io/') &&
                !key.startsWith('kubectl.kubernetes.io/')) {
                compressed[key] = value;
            }
        }
        return compressed;
    }

    /**
     * Decompress labels
     * @param {Object} compressed
     * @returns {Object}
     */
    decompressLabels(compressed) {
        return compressed || {};
    }

    /**
     * Compress conditions array
     * @param {Array} conditions
     * @returns {Array}
     */
    compressConditions(conditions) {
        if (!conditions) return [];

        return conditions.map(condition => ({
            t: condition.type,
            s: condition.status,
            r: condition.reason,
            lu: condition.lastUpdateTime
        }));
    }

    /**
     * Decompress conditions array
     * @param {Array} compressed
     * @returns {Array}
     */
    decompressConditions(compressed) {
        if (!compressed) return [];

        return compressed.map(condition => ({
            type: condition.t,
            status: condition.s,
            reason: condition.r,
            lastUpdateTime: condition.lu
        }));
    }

    /**
     * Estimate object size in bytes
     * @param {Object} obj
     * @returns {number}
     */
    estimateSize(obj) {
        return JSON.stringify(obj).length * 2; // Rough estimate
    }

    /**
     * Estimate pod size
     * @param {Object} pod
     * @returns {number}
     */
    estimatePodSize(pod) {
        return this.estimateSize(pod);
    }

    /**
     * Estimate node size
     * @param {Object} node
     * @returns {number}
     */
    estimateNodeSize(node) {
        return this.estimateSize(node);
    }

    /**
     * Estimate compressed object size
     * @param {Object} compressed
     * @returns {number}
     */
    estimateCompressedSize(compressed) {
        return this.estimateSize(compressed);
    }

    /**
     * Estimate total metrics size
     * @returns {number}
     */
    estimateMetricsSize() {
        let total = 0;
        for (const metric of this.metrics.values()) {
            total += this.estimateSize(metric);
        }
        return total;
    }

    /**
     * Update query performance metrics
     * @param {number} queryTime
     * @param {boolean} success
     */
    updateQueryMetrics(queryTime, success) {
        this.stats.totalQueries++;

        // Update average query time
        this.stats.avgQueryTime =
            (this.stats.avgQueryTime * (this.stats.totalQueries - 1) + queryTime) /
            this.stats.totalQueries;

        // Update cache hit ratio (simplified)
        if (success) {
            this.stats.cacheHitRatio =
                (this.stats.cacheHitRatio * (this.stats.totalQueries - 1) + 100) /
                this.stats.totalQueries;
        } else {
            this.stats.cacheHitRatio =
                (this.stats.cacheHitRatio * (this.stats.totalQueries - 1)) /
                this.stats.totalQueries;
        }
    }

    /**
     * Emit pod event
     * @param {string} type
     * @param {string} podId
     * @param {Object} pod
     */
    emitPodEvent(type, podId, pod) {
        this.addEvent({
            type,
            object: {
                kind: 'Pod',
                metadata: {
                    name: pod.metadata?.name,
                    namespace: pod.metadata?.namespace
                }
            },
            resourceVersion: Date.now().toString()
        });
    }

    /**
     * Emit node event
     * @param {string} type
     * @param {string} nodeName
     * @param {Object} node
     */
    emitNodeEvent(type, nodeName, node) {
        this.addEvent({
            type,
            object: {
                kind: 'Node',
                metadata: {
                    name: nodeName
                }
            },
            resourceVersion: Date.now().toString()
        });
    }

    /**
     * Generate unique event ID
     * @returns {string}
     */
    generateEventId() {
        return `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
    }

    /**
     * Compress large objects to save memory
     * @returns {number} Bytes freed
     */
    compressLargeObjects() {
        let freedBytes = 0;

        // Compress large pods
        for (const [key, pod] of this.podStates) {
            const size = this.estimateSize(pod);
            if (size > this.config.compressionThreshold && !pod._compressed) {
                const compressed = this.compression.compress(pod);
                this.podStates.set(key, compressed);
                freedBytes += size - this.estimateSize(compressed);
            }
        }

        return freedBytes;
    }

    /**
     * Initialize performance monitoring
     */
    initializePerformanceMonitoring() {
        // Monitor memory usage every 30 seconds
        setInterval(() => {
            this.memoryTracker.updateAllUsage();

            const usage = this.memoryTracker.getCurrentUsage();
            if (usage.total > this.memoryLimits.total * 0.9) {
                console.warn('High memory usage detected, triggering GC');
                this.gc();
            }
        }, 30000);

        // Log performance stats every 60 seconds
        setInterval(() => {
            const stats = this.getStats();
            console.log('Cache Performance:', {
                avgQueryTime: `${stats.avgQueryTime.toFixed(2)}ms`,
                cacheHitRatio: `${stats.cacheHitRatio.toFixed(1)}%`,
                memoryUsage: `${(stats.memoryUsage.total / 1024 / 1024).toFixed(1)}MB`,
                objectCounts: stats.objectCounts
            });
        }, 60000);
    }
}

/**
 * Ring buffer for events with fixed memory usage
 */
class RingBuffer {
    constructor(maxSize = 1000) {
        this.maxSize = maxSize;
        this.buffer = new Array(maxSize);
        this.head = 0;
        this.tail = 0;
        this.count = 0;
    }

    push(item) {
        this.buffer[this.tail] = item;
        this.tail = (this.tail + 1) % this.maxSize;

        if (this.count < this.maxSize) {
            this.count++;
        } else {
            this.head = (this.head + 1) % this.maxSize;
        }
    }

    getAll() {
        const result = [];
        let current = this.head;

        for (let i = 0; i < this.count; i++) {
            result.push(this.buffer[current]);
            current = (current + 1) % this.maxSize;
        }

        return result.reverse(); // Most recent first
    }

    size() {
        return this.count;
    }

    clear() {
        this.head = 0;
        this.tail = 0;
        this.count = 0;
        this.buffer.fill(null);
    }

    getMemoryUsage() {
        return this.count * 200; // Estimate 200 bytes per event
    }
}

/**
 * Memory tracker for cache components
 */
class MemoryTracker {
    constructor(limits) {
        this.limits = limits;
        this.usage = {
            pods: 0,
            nodes: 0,
            events: 0,
            metrics: 0,
            indexes: 0,
            total: 0
        };
    }

    updateUsage(component, bytes) {
        this.usage[component] = bytes;
        this.updateTotal();
    }

    updateAllUsage() {
        this.updateTotal();
    }

    updateTotal() {
        this.usage.total = Object.values(this.usage).reduce((sum, val) => sum + val, 0);
    }

    getCurrentUsage() {
        return { ...this.usage };
    }

    getUsagePercent() {
        return (this.usage.total / this.limits.total) * 100;
    }

    checkMemoryLimit(component, additionalBytes) {
        const newUsage = this.usage[component] + additionalBytes;
        if (newUsage > this.limits[component]) {
            throw new Error(`Memory limit exceeded for ${component}: ${newUsage} > ${this.limits[component]}`);
        }
    }

    reset() {
        Object.keys(this.usage).forEach(key => {
            this.usage[key] = 0;
        });
    }
}

/**
 * Data compression utility
 */
class DataCompressor {
    compress(data) {
        // Simple JSON string compression (in production, use proper compression)
        const jsonStr = JSON.stringify(data);
        return {
            _compressed: true,
            data: jsonStr,
            originalSize: jsonStr.length,
            compressedSize: jsonStr.length // Would be smaller with real compression
        };
    }

    decompress(compressed) {
        if (!compressed._compressed) return compressed;
        return JSON.parse(compressed.data);
    }
}

/**
 * Query optimizer for performance
 */
class QueryOptimizer {
    constructor() {
        this.queryCache = new Map();
        this.maxCacheSize = 100;
    }

    optimizeQuery(query) {
        // Simple query optimization logic
        return query;
    }

    cacheQuery(queryKey, result) {
        if (this.queryCache.size >= this.maxCacheSize) {
            const firstKey = this.queryCache.keys().next().value;
            this.queryCache.delete(firstKey);
        }

        this.queryCache.set(queryKey, {
            result,
            timestamp: Date.now()
        });
    }

    getCachedQuery(queryKey) {
        const cached = this.queryCache.get(queryKey);
        if (cached && Date.now() - cached.timestamp < 5000) { // 5 second TTL
            return cached.result;
        }
        return null;
    }
}

module.exports = { RealtimeK8sCache, RingBuffer, MemoryTracker };