/**
 * TEE Kubernetes Scheduler
 * High-performance pod scheduling with DaaS-specific optimizations
 * Target: <10ms scheduling decisions for most pods
 */

const EventEmitter = require('events');
const crypto = require('crypto');

class TEEScheduler extends EventEmitter {
    constructor(options = {}) {
        super();

        this.config = {
            maxSchedulingTime: options.maxSchedulingTime || 10, // ms
            scoreWeights: {
                resources: 0.3,      // CPU/Memory availability
                affinity: 0.25,      // Pod affinity/anti-affinity
                spread: 0.2,         // Even distribution
                performance: 0.15,   // Node performance metrics
                stake: 0.1          // DaaS stake-based priority
            },
            cacheSize: options.cacheSize || 10000,
            enablePreemption: options.enablePreemption || true,
            ...options
        };

        // Core components
        this.cache = null; // Will be injected
        this.workerComm = null; // Will be injected

        // Internal state
        this.schedulingQueue = [];
        this.nodeCache = new Map();
        this.podAffinityCache = new Map();
        this.schedulingHistory = new Map();

        // Performance metrics
        this.metrics = {
            totalScheduled: 0,
            schedulingTime: [],
            nodeUtilization: new Map(),
            schedulingErrors: 0,
            preemptions: 0
        };

        // Optimization caches
        this.nodeScoreCache = new Map();
        this.affinityCache = new Map();
        this.lastCacheUpdate = 0;

        this.initializeScheduler();
    }

    initializeScheduler() {
        // Start scheduling loop
        this.schedulingLoop();

        // Periodic cache cleanup
        setInterval(() => this.cleanupCaches(), 30000);

        // Performance metrics collection
        setInterval(() => this.collectMetrics(), 5000);

        console.log('TEE Scheduler initialized with config:', this.config);
    }

    // Inject dependencies
    setCache(cache) {
        this.cache = cache;
    }

    setWorkerCommunication(workerComm) {
        this.workerComm = workerComm;

        // Listen for node updates
        this.workerComm.on('node-update', (nodeData) => {
            this.updateNodeCache(nodeData);
        });
    }

    /**
     * Main scheduling function
     * @param {Object} podSpec - Pod specification
     * @returns {Promise<Object>} Scheduling result
     */
    async schedulePod(podSpec) {
        const startTime = Date.now();

        try {
            // Validate pod spec
            this.validatePodSpec(podSpec);

            // 1. Filter nodes by requirements
            const candidates = await this.filterNodes(podSpec);

            if (candidates.length === 0) {
                throw new Error('No suitable nodes found for pod');
            }

            // 2. Score each node
            const scores = await this.scoreNodes(candidates, podSpec);

            // 3. Select best node
            const selected = this.selectBestNode(scores);

            // 4. Bind pod to node
            const result = await this.bindPodToNode(podSpec, selected);

            // Record metrics
            const schedulingTime = Date.now() - startTime;
            this.recordSchedulingSuccess(schedulingTime, selected.nodeId);

            this.emit('pod-scheduled', {
                podName: podSpec.metadata.name,
                nodeName: selected.nodeId,
                schedulingTime,
                score: selected.score
            });

            return result;

        } catch (error) {
            this.metrics.schedulingErrors++;
            this.emit('scheduling-error', {
                podName: podSpec.metadata?.name,
                error: error.message,
                schedulingTime: Date.now() - startTime
            });
            throw error;
        }
    }

    /**
     * Filter nodes based on pod requirements
     * @param {Object} podSpec - Pod specification
     * @returns {Array} Array of candidate nodes
     */
    async filterNodes(podSpec) {
        const startTime = Date.now();
        const nodes = await this.getAvailableNodes();
        const candidates = [];

        for (const node of nodes) {
            if (await this.nodeMatches(node, podSpec)) {
                candidates.push(node);
            }
        }

        console.log(`Node filtering took ${Date.now() - startTime}ms, found ${candidates.length} candidates`);
        return candidates;
    }

    /**
     * Check if node matches pod requirements
     * @param {Object} node - Node information
     * @param {Object} podSpec - Pod specification
     * @returns {boolean} Whether node matches
     */
    async nodeMatches(node, podSpec) {
        // Resource requirements check
        if (!this.checkResourceRequirements(node, podSpec)) {
            return false;
        }

        // Node selector check
        if (!this.checkNodeSelector(node, podSpec)) {
            return false;
        }

        // Taints and tolerations
        if (!this.checkTaintsAndTolerations(node, podSpec)) {
            return false;
        }

        // DaaS-specific checks
        if (!this.checkDaaSRequirements(node, podSpec)) {
            return false;
        }

        return true;
    }

    /**
     * Score nodes for pod placement
     * @param {Array} nodes - Candidate nodes
     * @param {Object} podSpec - Pod specification
     * @returns {Array} Scored nodes
     */
    async scoreNodes(nodes, podSpec) {
        const startTime = Date.now();
        const scoredNodes = [];

        for (const node of nodes) {
            const score = await this.calculateNodeScore(node, podSpec);
            scoredNodes.push({
                nodeId: node.id,
                node: node,
                score: score,
                breakdown: score.breakdown
            });
        }

        console.log(`Node scoring took ${Date.now() - startTime}ms for ${nodes.length} nodes`);
        return scoredNodes;
    }

    /**
     * Calculate comprehensive node score
     * @param {Object} node - Node information
     * @param {Object} podSpec - Pod specification
     * @returns {Object} Score with breakdown
     */
    async calculateNodeScore(node, podSpec) {
        const cacheKey = `${node.id}-${this.getPodFingerprint(podSpec)}`;

        // Check cache first
        if (this.nodeScoreCache.has(cacheKey)) {
            const cached = this.nodeScoreCache.get(cacheKey);
            if (Date.now() - cached.timestamp < 5000) { // 5s cache
                return cached.score;
            }
        }

        const scores = {
            resources: this.scoreResources(node, podSpec),
            affinity: await this.scoreAffinity(node, podSpec),
            spread: this.scoreSpread(node, podSpec),
            performance: this.scorePerformance(node),
            stake: this.scoreStake(node, podSpec)
        };

        // Calculate weighted total
        let totalScore = 0;
        const breakdown = {};

        for (const [metric, score] of Object.entries(scores)) {
            const weight = this.config.scoreWeights[metric];
            const weightedScore = score * weight;
            totalScore += weightedScore;
            breakdown[metric] = { score, weight, weightedScore };
        }

        const result = {
            total: Math.round(totalScore * 100) / 100,
            breakdown
        };

        // Cache result
        this.nodeScoreCache.set(cacheKey, {
            score: result,
            timestamp: Date.now()
        });

        return result;
    }

    /**
     * Score node based on resource availability
     * @param {Object} node - Node information
     * @param {Object} podSpec - Pod specification
     * @returns {number} Resource score (0-100)
     */
    scoreResources(node, podSpec) {
        const requests = this.getPodResourceRequests(podSpec);

        // CPU scoring
        const cpuAvailable = node.status.allocatable.cpu - node.status.allocated.cpu;
        const cpuScore = requests.cpu > 0 ?
            Math.min(100, (cpuAvailable / requests.cpu) * 20) : 100;

        // Memory scoring
        const memAvailable = node.status.allocatable.memory - node.status.allocated.memory;
        const memScore = requests.memory > 0 ?
            Math.min(100, (memAvailable / requests.memory) * 20) : 100;

        // Storage scoring
        const storageAvailable = node.status.allocatable.storage - node.status.allocated.storage;
        const storageScore = requests.storage > 0 ?
            Math.min(100, (storageAvailable / requests.storage) * 20) : 100;

        // Weighted average (CPU and memory more important)
        return (cpuScore * 0.4 + memScore * 0.4 + storageScore * 0.2);
    }

    /**
     * Score node based on pod affinity/anti-affinity
     * @param {Object} node - Node information
     * @param {Object} podSpec - Pod specification
     * @returns {number} Affinity score (0-100)
     */
    async scoreAffinity(node, podSpec) {
        if (!podSpec.spec.affinity) {
            return 50; // Neutral score
        }

        let score = 50;
        const affinity = podSpec.spec.affinity;

        // Node affinity
        if (affinity.nodeAffinity) {
            score += this.scoreNodeAffinity(node, affinity.nodeAffinity);
        }

        // Pod affinity
        if (affinity.podAffinity) {
            score += await this.scorePodAffinity(node, affinity.podAffinity);
        }

        // Pod anti-affinity
        if (affinity.podAntiAffinity) {
            score -= await this.scorePodAntiAffinity(node, affinity.podAntiAffinity);
        }

        return Math.max(0, Math.min(100, score));
    }

    /**
     * Score node based on pod distribution
     * @param {Object} node - Node information
     * @param {Object} podSpec - Pod specification
     * @returns {number} Spread score (0-100)
     */
    scoreSpread(node, podSpec) {
        const deployment = this.getDeploymentName(podSpec);
        if (!deployment) return 50;

        const deploymentPods = this.getPodsForDeployment(deployment);
        const nodePodsCount = deploymentPods.filter(pod => pod.nodeName === node.id).length;
        const avgPodsPerNode = deploymentPods.length / this.nodeCache.size;

        // Prefer nodes with fewer pods from same deployment
        if (nodePodsCount < avgPodsPerNode) {
            return 80;
        } else if (nodePodsCount === avgPodsPerNode) {
            return 50;
        } else {
            return Math.max(20, 80 - (nodePodsCount - avgPodsPerNode) * 20);
        }
    }

    /**
     * Score node based on performance metrics
     * @param {Object} node - Node information
     * @returns {number} Performance score (0-100)
     */
    scorePerformance(node) {
        const metrics = node.metrics || {};

        // CPU utilization (lower is better for new pods)
        const cpuUtil = metrics.cpuUtilization || 0;
        const cpuScore = Math.max(0, 100 - cpuUtil);

        // Memory utilization
        const memUtil = metrics.memoryUtilization || 0;
        const memScore = Math.max(0, 100 - memUtil);

        // Network latency (lower is better)
        const latency = metrics.networkLatency || 10;
        const latencyScore = Math.max(0, 100 - latency * 2);

        // Disk I/O utilization
        const diskUtil = metrics.diskUtilization || 0;
        const diskScore = Math.max(0, 100 - diskUtil);

        return (cpuScore * 0.3 + memScore * 0.3 + latencyScore * 0.2 + diskScore * 0.2);
    }

    /**
     * Score node based on DaaS stake requirements
     * @param {Object} node - Node information
     * @param {Object} podSpec - Pod specification
     * @returns {number} Stake score (0-100)
     */
    scoreStake(node, podSpec) {
        const requiredStake = this.getRequiredStake(podSpec);
        const nodeStake = node.daas?.stake || 0;

        if (requiredStake === 0) return 50;

        const ratio = nodeStake / requiredStake;
        if (ratio >= 2) return 100;
        if (ratio >= 1.5) return 80;
        if (ratio >= 1) return 60;
        if (ratio >= 0.8) return 40;
        return 20;
    }

    /**
     * Select best node from scored candidates
     * @param {Array} scoredNodes - Nodes with scores
     * @returns {Object} Selected node
     */
    selectBestNode(scoredNodes) {
        if (scoredNodes.length === 0) {
            throw new Error('No scored nodes available');
        }

        // Sort by score (highest first)
        scoredNodes.sort((a, b) => b.score.total - a.score.total);

        // Add some randomness to prevent always using same high-score node
        const topCandidates = scoredNodes.filter(
            node => node.score.total >= scoredNodes[0].score.total * 0.95
        );

        const selected = topCandidates[Math.floor(Math.random() * topCandidates.length)];

        console.log(`Selected node ${selected.nodeId} with score ${selected.score.total}`);
        return selected;
    }

    /**
     * Bind pod to selected node
     * @param {Object} podSpec - Pod specification
     * @param {Object} selectedNode - Selected node with score
     * @returns {Object} Binding result
     */
    async bindPodToNode(podSpec, selectedNode) {
        const binding = {
            apiVersion: 'v1',
            kind: 'Binding',
            metadata: {
                name: podSpec.metadata.name,
                namespace: podSpec.metadata.namespace
            },
            target: {
                apiVersion: 'v1',
                kind: 'Node',
                name: selectedNode.nodeId
            }
        };

        // Update pod status
        const updatedPod = {
            ...podSpec,
            spec: {
                ...podSpec.spec,
                nodeName: selectedNode.nodeId
            },
            status: {
                phase: 'Pending',
                scheduledAt: new Date().toISOString()
            }
        };

        // Store in cache
        if (this.cache) {
            await this.cache.updateResource('pods', updatedPod);
        }

        // Notify worker node
        if (this.workerComm) {
            this.workerComm.sendToNode(selectedNode.nodeId, {
                type: 'pod-scheduled',
                pod: updatedPod,
                binding: binding
            });
        }

        return {
            success: true,
            binding,
            pod: updatedPod,
            schedulingDecision: {
                selectedNode: selectedNode.nodeId,
                score: selectedNode.score,
                alternatives: []
            }
        };
    }

    // === Integration Methods ===

    /**
     * Create integrated scheduler instance with cache and worker communication
     * @param {RealtimeK8sCache} cache - Cache instance
     * @param {TEEMasterServer} workerComm - Worker communication instance
     * @returns {TEEScheduler} Configured scheduler
     */
    static createIntegratedScheduler(cache, workerComm, options = {}) {
        const scheduler = new TEEScheduler(options);
        scheduler.setCache(cache);
        scheduler.setWorkerCommunication(workerComm);

        // Set up event listeners for tight integration
        scheduler.setupCacheIntegration(cache);
        scheduler.setupWorkerIntegration(workerComm);

        return scheduler;
    }

    /**
     * Set up cache integration with event listeners
     * @param {RealtimeK8sCache} cache - Cache instance
     */
    setupCacheIntegration(cache) {
        // Listen for pod updates to maintain scheduling state
        cache.on('pod-updated', (pod) => {
            this.handlePodUpdate(pod);
        });

        // Listen for node updates to refresh node cache
        cache.on('node-updated', (node) => {
            this.updateNodeCache(node);
            this.invalidateNodeScoreCache(node.id);
        });

        // Listen for resource quota changes
        cache.on('resource-quota-updated', (quota) => {
            this.handleResourceQuotaUpdate(quota);
        });

        console.log('Scheduler cache integration established');
    }

    /**
     * Set up worker communication integration
     * @param {TEEMasterServer} workerComm - Worker communication instance
     */
    setupWorkerIntegration(workerComm) {
        // Listen for pod scheduling confirmations from workers
        workerComm.on('pod-scheduled-confirmation', (data) => {
            this.handleSchedulingConfirmation(data);
        });

        // Listen for pod failures that require rescheduling
        workerComm.on('pod-failed', (data) => {
            this.handlePodFailure(data);
        });

        // Listen for node resource updates from workers
        workerComm.on('node-resource-update', (data) => {
            this.updateNodeResources(data);
        });

        // Listen for worker disconnections
        workerComm.on('worker-disconnected', (nodeId) => {
            this.handleWorkerDisconnection(nodeId);
        });

        console.log('Scheduler worker communication integration established');
    }

    /**
     * Handle pod updates from cache
     */
    handlePodUpdate(pod) {
        // Remove from scheduling queue if already scheduled
        this.schedulingQueue = this.schedulingQueue.filter(
            queuedPod => queuedPod.metadata.name !== pod.metadata.name ||
                        queuedPod.metadata.namespace !== pod.metadata.namespace
        );

        // Update scheduling history
        if (pod.spec.nodeName) {
            this.schedulingHistory.set(`${pod.metadata.namespace}/${pod.metadata.name}`, {
                nodeName: pod.spec.nodeName,
                scheduledAt: Date.now(),
                status: pod.status.phase
            });
        }
    }

    /**
     * Handle resource quota updates
     */
    handleResourceQuotaUpdate(quota) {
        // Invalidate score caches for affected namespace
        const namespace = quota.metadata.namespace;
        for (const key of this.nodeScoreCache.keys()) {
            if (key.includes(namespace)) {
                this.nodeScoreCache.delete(key);
            }
        }
    }

    /**
     * Handle scheduling confirmation from worker
     */
    handleSchedulingConfirmation(data) {
        const { podName, nodeName, success, error } = data;

        if (success) {
            this.emit('scheduling-confirmed', { podName, nodeName });
        } else {
            // Reschedule the pod if scheduling failed
            this.emit('scheduling-failed', { podName, nodeName, error });
            this.handleRescheduling(podName, error);
        }
    }

    /**
     * Handle pod failure requiring rescheduling
     */
    handlePodFailure(data) {
        const { podName, nodeName, reason } = data;

        // Add to rescheduling queue with higher priority
        if (this.cache) {
            this.cache.getResource('pods', podName).then(pod => {
                if (pod && pod.spec.restartPolicy !== 'Never') {
                    // Reset node assignment
                    pod.spec.nodeName = undefined;
                    pod.status.phase = 'Pending';

                    // Add to front of queue for immediate rescheduling
                    this.schedulingQueue.unshift(pod);
                    this.emit('pod-rescheduled', { podName, previousNode: nodeName, reason });
                }
            });
        }
    }

    /**
     * Handle worker node disconnection
     */
    handleWorkerDisconnection(nodeId) {
        // Mark node as unavailable
        if (this.nodeCache.has(nodeId)) {
            const node = this.nodeCache.get(nodeId);
            node.status.ready = false;
            node.lastDisconnected = Date.now();
            this.nodeCache.set(nodeId, node);
        }

        // Invalidate score cache for this node
        this.invalidateNodeScoreCache(nodeId);

        console.log(`Worker node ${nodeId} disconnected, marked as unavailable`);
    }

    /**
     * Invalidate node score cache entries for a specific node
     */
    invalidateNodeScoreCache(nodeId) {
        for (const key of this.nodeScoreCache.keys()) {
            if (key.startsWith(nodeId)) {
                this.nodeScoreCache.delete(key);
            }
        }
    }

    /**
     * Update node resource information from worker
     */
    updateNodeResources(data) {
        const { nodeId, resources, metrics } = data;

        if (this.nodeCache.has(nodeId)) {
            const node = this.nodeCache.get(nodeId);
            node.status.allocated = resources.allocated;
            node.metrics = { ...node.metrics, ...metrics };
            node.lastResourceUpdate = Date.now();
            this.nodeCache.set(nodeId, node);
        }
    }

    // === Helper Methods ===

    async getAvailableNodes() {
        if (this.cache) {
            const nodes = await this.cache.getResources('nodes');
            return nodes.filter(node =>
                node.status?.ready &&
                !node.spec?.unschedulable
            );
        }
        return Array.from(this.nodeCache.values()).filter(node =>
            node.status?.ready &&
            !node.spec?.unschedulable
        );
    }

    checkResourceRequirements(node, podSpec) {
        const requests = this.getPodResourceRequests(podSpec);
        const available = {
            cpu: node.status.allocatable.cpu - node.status.allocated.cpu,
            memory: node.status.allocatable.memory - node.status.allocated.memory,
            storage: node.status.allocatable.storage - node.status.allocated.storage
        };

        return requests.cpu <= available.cpu &&
               requests.memory <= available.memory &&
               requests.storage <= available.storage;
    }

    checkNodeSelector(node, podSpec) {
        const nodeSelector = podSpec.spec.nodeSelector;
        if (!nodeSelector) return true;

        for (const [key, value] of Object.entries(nodeSelector)) {
            if (node.labels[key] !== value) {
                return false;
            }
        }
        return true;
    }

    checkTaintsAndTolerations(node, podSpec) {
        const taints = node.spec.taints || [];
        const tolerations = podSpec.spec.tolerations || [];

        for (const taint of taints) {
            if (taint.effect === 'NoSchedule') {
                const tolerated = tolerations.some(toleration =>
                    this.tolerationMatches(toleration, taint)
                );
                if (!tolerated) return false;
            }
        }
        return true;
    }

    checkDaaSRequirements(node, podSpec) {
        const daasAnnotations = podSpec.metadata.annotations || {};
        const requiredStake = parseInt(daasAnnotations['daas.io/required-stake'] || '0');
        const nodeStake = node.daas?.stake || 0;

        return nodeStake >= requiredStake;
    }

    getPodResourceRequests(podSpec) {
        let totalCpu = 0;
        let totalMemory = 0;
        let totalStorage = 0;

        for (const container of podSpec.spec.containers) {
            const requests = container.resources?.requests || {};
            totalCpu += this.parseCpuRequest(requests.cpu || '0');
            totalMemory += this.parseMemoryRequest(requests.memory || '0');
            totalStorage += this.parseStorageRequest(requests.storage || '0');
        }

        return { cpu: totalCpu, memory: totalMemory, storage: totalStorage };
    }

    parseCpuRequest(cpu) {
        if (typeof cpu === 'number') return cpu;
        if (cpu.endsWith('m')) return parseInt(cpu) / 1000;
        return parseFloat(cpu);
    }

    parseMemoryRequest(memory) {
        if (typeof memory === 'number') return memory;
        const units = { 'Ki': 1024, 'Mi': 1024**2, 'Gi': 1024**3 };
        for (const [unit, multiplier] of Object.entries(units)) {
            if (memory.endsWith(unit)) {
                return parseInt(memory) * multiplier;
            }
        }
        return parseInt(memory) || 0;
    }

    parseStorageRequest(storage) {
        return this.parseMemoryRequest(storage); // Same parsing logic
    }

    validatePodSpec(podSpec) {
        if (!podSpec.metadata?.name) {
            throw new Error('Pod must have a name');
        }
        if (!podSpec.spec?.containers?.length) {
            throw new Error('Pod must have at least one container');
        }
    }

    getPodFingerprint(podSpec) {
        const key = JSON.stringify({
            containers: podSpec.spec.containers.length,
            nodeSelector: podSpec.spec.nodeSelector,
            affinity: podSpec.spec.affinity,
            resources: this.getPodResourceRequests(podSpec)
        });
        return crypto.createHash('md5').update(key).digest('hex').substring(0, 8);
    }

    updateNodeCache(nodeData) {
        this.nodeCache.set(nodeData.id, {
            ...nodeData,
            lastUpdate: Date.now()
        });
    }

    // === Scheduling Loop ===

    async schedulingLoop() {
        while (true) {
            try {
                if (this.schedulingQueue.length > 0) {
                    const podSpec = this.schedulingQueue.shift();
                    await this.schedulePod(podSpec);
                }
                await new Promise(resolve => setTimeout(resolve, 10)); // 10ms loop
            } catch (error) {
                console.error('Scheduling loop error:', error);
                await new Promise(resolve => setTimeout(resolve, 100));
            }
        }
    }

    // === Performance Monitoring ===

    recordSchedulingSuccess(schedulingTime, nodeId) {
        this.metrics.totalScheduled++;
        this.metrics.schedulingTime.push(schedulingTime);

        // Keep only last 1000 timing measurements
        if (this.metrics.schedulingTime.length > 1000) {
            this.metrics.schedulingTime = this.metrics.schedulingTime.slice(-1000);
        }

        // Update node utilization
        const current = this.metrics.nodeUtilization.get(nodeId) || 0;
        this.metrics.nodeUtilization.set(nodeId, current + 1);
    }

    getSchedulingMetrics() {
        const timings = this.metrics.schedulingTime;
        const avgTime = timings.length > 0 ?
            timings.reduce((a, b) => a + b, 0) / timings.length : 0;
        const p95Time = timings.length > 0 ?
            timings.sort((a, b) => a - b)[Math.floor(timings.length * 0.95)] : 0;

        return {
            totalScheduled: this.metrics.totalScheduled,
            schedulingErrors: this.metrics.schedulingErrors,
            averageSchedulingTime: Math.round(avgTime * 100) / 100,
            p95SchedulingTime: p95Time,
            cacheHitRate: this.getCacheHitRate(),
            nodeUtilization: Object.fromEntries(this.metrics.nodeUtilization)
        };
    }

    getCacheHitRate() {
        const cacheRequests = this.metrics.cacheRequests || 0;
        const cacheHits = this.metrics.cacheHits || 0;
        return cacheRequests > 0 ? (cacheHits / cacheRequests) * 100 : 0;
    }

    // === Missing Helper Methods ===

    tolerationMatches(toleration, taint) {
        // Check if toleration matches taint
        if (toleration.key && toleration.key !== taint.key) return false;
        if (toleration.value && toleration.value !== taint.value) return false;
        if (toleration.effect && toleration.effect !== taint.effect) return false;
        if (toleration.operator === 'Equal') {
            return toleration.value === taint.value;
        }
        if (toleration.operator === 'Exists') {
            return true;
        }
        return false;
    }

    scoreNodeAffinity(node, nodeAffinity) {
        let score = 0;

        // Required node affinity (hard constraint)
        if (nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution) {
            const required = nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution;
            for (const term of required.nodeSelectorTerms) {
                if (this.nodeMatchesSelector(node, term)) {
                    score += 20;
                    break;
                }
            }
        }

        // Preferred node affinity (soft constraint)
        if (nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution) {
            const preferred = nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution;
            for (const weightedTerm of preferred) {
                if (this.nodeMatchesSelector(node, weightedTerm.preference)) {
                    score += (weightedTerm.weight / 100) * 30;
                }
            }
        }

        return Math.min(50, score);
    }

    async scorePodAffinity(node, podAffinity) {
        let score = 0;

        if (podAffinity.preferredDuringSchedulingIgnoredDuringExecution) {
            for (const weightedTerm of podAffinity.preferredDuringSchedulingIgnoredDuringExecution) {
                const matchingPods = await this.getPodsMatchingSelector(
                    weightedTerm.podAffinityTerm.labelSelector,
                    weightedTerm.podAffinityTerm.namespaces
                );

                const nodeMatchingPods = matchingPods.filter(pod => pod.nodeName === node.id);
                if (nodeMatchingPods.length > 0) {
                    score += (weightedTerm.weight / 100) * 30;
                }
            }
        }

        return Math.min(30, score);
    }

    async scorePodAntiAffinity(node, podAntiAffinity) {
        let penalty = 0;

        if (podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution) {
            for (const weightedTerm of podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution) {
                const matchingPods = await this.getPodsMatchingSelector(
                    weightedTerm.podAffinityTerm.labelSelector,
                    weightedTerm.podAffinityTerm.namespaces
                );

                const nodeMatchingPods = matchingPods.filter(pod => pod.nodeName === node.id);
                if (nodeMatchingPods.length > 0) {
                    penalty += (weightedTerm.weight / 100) * 30;
                }
            }
        }

        return Math.min(30, penalty);
    }

    nodeMatchesSelector(node, selector) {
        if (selector.matchLabels) {
            for (const [key, value] of Object.entries(selector.matchLabels)) {
                if (node.labels[key] !== value) return false;
            }
        }

        if (selector.matchExpressions) {
            for (const expr of selector.matchExpressions) {
                if (!this.evaluateMatchExpression(node, expr)) return false;
            }
        }

        return true;
    }

    evaluateMatchExpression(node, expr) {
        const nodeValue = node.labels[expr.key];

        switch (expr.operator) {
            case 'In':
                return expr.values.includes(nodeValue);
            case 'NotIn':
                return !expr.values.includes(nodeValue);
            case 'Exists':
                return nodeValue !== undefined;
            case 'DoesNotExist':
                return nodeValue === undefined;
            default:
                return false;
        }
    }

    async getPodsMatchingSelector(labelSelector, namespaces) {
        if (this.cache) {
            const pods = await this.cache.getResources('pods');
            return pods.filter(pod => {
                // Check namespace
                if (namespaces && !namespaces.includes(pod.metadata.namespace)) {
                    return false;
                }

                // Check labels
                if (labelSelector.matchLabels) {
                    for (const [key, value] of Object.entries(labelSelector.matchLabels)) {
                        if (pod.metadata.labels?.[key] !== value) return false;
                    }
                }

                return true;
            });
        }
        return [];
    }

    getDeploymentName(podSpec) {
        const ownerRefs = podSpec.metadata.ownerReferences || [];
        const replicaSet = ownerRefs.find(ref => ref.kind === 'ReplicaSet');

        if (replicaSet) {
            // Extract deployment name from ReplicaSet name (usually deployment-name-hash)
            const match = replicaSet.name.match(/^(.+)-[a-f0-9]+$/);
            return match ? match[1] : null;
        }

        return null;
    }

    getPodsForDeployment(deploymentName) {
        // This would normally query the cache for pods with deployment label
        return [];
    }

    getRequiredStake(podSpec) {
        const annotations = podSpec.metadata.annotations || {};
        return parseInt(annotations['daas.io/required-stake'] || '0');
    }

    handleRescheduling(podName, error) {
        console.log(`Rescheduling pod ${podName} due to error: ${error}`);
        // Implementation for handling rescheduling logic
    }

    cleanupCaches() {
        const now = Date.now();
        const maxAge = 60000; // 1 minute

        // Clean node score cache
        for (const [key, value] of this.nodeScoreCache) {
            if (now - value.timestamp > maxAge) {
                this.nodeScoreCache.delete(key);
            }
        }

        // Clean affinity cache
        for (const [key, value] of this.affinityCache) {
            if (now - value.timestamp > maxAge) {
                this.affinityCache.delete(key);
            }
        }
    }

    collectMetrics() {
        this.emit('metrics-collected', this.getSchedulingMetrics());
    }

    // === Public API ===

    addPodToQueue(podSpec) {
        this.schedulingQueue.push(podSpec);
        this.emit('pod-queued', podSpec.metadata.name);
    }

    getQueueLength() {
        return this.schedulingQueue.length;
    }

    getNodeCache() {
        return Array.from(this.nodeCache.values());
    }
}

// === Integration Example ===

/**
 * Example: Creating an integrated scheduler system
 *
 * const RealtimeK8sCache = require('./realtime-cache');
 * const { TEEMasterServer } = require('./worker-communication');
 * const TEEScheduler = require('./scheduler');
 *
 * // Initialize components
 * const cache = new RealtimeK8sCache({
 *     memoryLimit: 4 * 1024 * 1024 * 1024 // 4GB
 * });
 *
 * const workerComm = new TEEMasterServer({
 *     port: 8080,
 *     maxConnections: 1000
 * });
 *
 * // Create integrated scheduler
 * const scheduler = TEEScheduler.createIntegratedScheduler(cache, workerComm, {
 *     maxSchedulingTime: 10,
 *     scoreWeights: {
 *         resources: 0.3,
 *         affinity: 0.25,
 *         spread: 0.2,
 *         performance: 0.15,
 *         stake: 0.1
 *     }
 * });
 *
 * // Listen for scheduling events
 * scheduler.on('pod-scheduled', (data) => {
 *     console.log(`Pod ${data.podName} scheduled to ${data.nodeName} in ${data.schedulingTime}ms`);
 * });
 *
 * scheduler.on('scheduling-error', (data) => {
 *     console.error(`Failed to schedule pod ${data.podName}: ${data.error}`);
 * });
 *
 * // Start the system
 * await cache.initialize();
 * await workerComm.start();
 *
 * // Schedule a pod
 * const podSpec = {
 *     apiVersion: 'v1',
 *     kind: 'Pod',
 *     metadata: {
 *         name: 'test-pod',
 *         namespace: 'default',
 *         annotations: {
 *             'daas.io/required-stake': '1000'
 *         }
 *     },
 *     spec: {
 *         containers: [{
 *             name: 'app',
 *             image: 'nginx',
 *             resources: {
 *                 requests: {
 *                     cpu: '100m',
 *                     memory: '128Mi'
 *                 }
 *             }
 *         }]
 *     }
 * };
 *
 * try {
 *     const result = await scheduler.schedulePod(podSpec);
 *     console.log('Scheduling result:', result);
 * } catch (error) {
 *     console.error('Scheduling failed:', error);
 * }
 */

module.exports = TEEScheduler;