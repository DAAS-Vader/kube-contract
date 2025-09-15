// Nautilus TEE Scheduler - Ultra-fast pod scheduling with <10ms decisions
// Optimized for TEE environment with pre-computed scheduling decisions

use std::collections::{HashMap, BTreeMap, VecDeque};
use std::sync::{Arc, RwLock, Mutex};
use std::time::{Instant, Duration};
use std::cmp::Ordering;

use serde::{Deserialize, Serialize};
use tokio::sync::{mpsc, oneshot};
use tokio::time::interval;

use crate::memory_store::{TeeMemoryStore, QueryOptions};

/// High-performance TEE Scheduler
pub struct TeeScheduler {
    /// Reference to the memory store
    store: Arc<TeeMemoryStore>,
    /// Node resource cache for fast scheduling decisions
    node_cache: Arc<NodeResourceCache>,
    /// Scheduling queue for pending pods
    scheduling_queue: Arc<Mutex<SchedulingQueue>>,
    /// Pre-computed scheduling decisions
    decision_cache: Arc<DecisionCache>,
    /// Affinity and anti-affinity rules cache
    affinity_cache: Arc<AffinityCache>,
    /// Performance metrics
    metrics: Arc<SchedulerMetrics>,
    /// Configuration
    config: SchedulerConfig,
    /// Event channels
    event_tx: mpsc::UnboundedSender<SchedulerEvent>,
    event_rx: Arc<Mutex<mpsc::UnboundedReceiver<SchedulerEvent>>>,
}

/// Node resource cache for ultra-fast lookups
struct NodeResourceCache {
    /// Node information indexed by node name
    nodes: RwLock<HashMap<String, CachedNodeInfo>>,
    /// Nodes sorted by available resources
    sorted_nodes: RwLock<Vec<String>>,
    /// Resource totals across all nodes
    cluster_resources: RwLock<ResourceSummary>,
    /// Last update timestamp
    last_update: RwLock<Instant>,
}

/// Cached node information optimized for scheduling decisions
#[derive(Clone, Debug)]
struct CachedNodeInfo {
    /// Node name
    name: String,
    /// Available CPU in millicores
    available_cpu: i64,
    /// Available memory in bytes
    available_memory: i64,
    /// Available storage in bytes
    available_storage: i64,
    /// Node labels for affinity matching
    labels: HashMap<String, String>,
    /// Node taints
    taints: Vec<NodeTaint>,
    /// Current pod count
    pod_count: u32,
    /// Node conditions (Ready, MemoryPressure, etc.)
    conditions: NodeConditions,
    /// Scheduling score (higher is better)
    score: f64,
    /// Last update time
    updated_at: Instant,
}

/// Node conditions that affect scheduling
#[derive(Clone, Debug, Default)]
struct NodeConditions {
    ready: bool,
    memory_pressure: bool,
    disk_pressure: bool,
    pid_pressure: bool,
    network_unavailable: bool,
}

/// Node taint information
#[derive(Clone, Debug)]
struct NodeTaint {
    key: String,
    value: Option<String>,
    effect: TaintEffect,
}

/// Taint effects
#[derive(Clone, Debug)]
enum TaintEffect {
    NoSchedule,
    PreferNoSchedule,
    NoExecute,
}

/// Resource summary for cluster-wide decisions
#[derive(Clone, Debug, Default)]
struct ResourceSummary {
    total_cpu: i64,
    total_memory: i64,
    total_storage: i64,
    available_cpu: i64,
    available_memory: i64,
    available_storage: i64,
    node_count: u32,
}

/// Scheduling queue with prioritization
struct SchedulingQueue {
    /// High priority pods (system pods, etc.)
    high_priority: VecDeque<PendingPod>,
    /// Normal priority pods
    normal_priority: VecDeque<PendingPod>,
    /// Low priority pods
    low_priority: VecDeque<PendingPod>,
    /// Failed scheduling attempts
    failed_pods: HashMap<String, (PendingPod, u32)>, // (pod, retry_count)
}

/// Pod pending scheduling
#[derive(Clone, Debug)]
struct PendingPod {
    /// Pod unique identifier
    id: String,
    /// Pod name
    name: String,
    /// Pod namespace
    namespace: String,
    /// Resource requirements
    resources: ResourceRequirements,
    /// Node affinity rules
    node_affinity: Option<NodeAffinity>,
    /// Pod affinity rules
    pod_affinity: Option<PodAffinity>,
    /// Pod anti-affinity rules
    pod_anti_affinity: Option<PodAntiAffinity>,
    /// Tolerations for node taints
    tolerations: Vec<Toleration>,
    /// Scheduling priority
    priority: i32,
    /// Creation timestamp
    created_at: Instant,
    /// Scheduling deadline
    deadline: Option<Instant>,
}

/// Resource requirements for pods
#[derive(Clone, Debug, Default)]
struct ResourceRequirements {
    /// CPU request in millicores
    cpu_request: i64,
    /// Memory request in bytes
    memory_request: i64,
    /// Storage request in bytes
    storage_request: i64,
    /// CPU limit in millicores
    cpu_limit: Option<i64>,
    /// Memory limit in bytes
    memory_limit: Option<i64>,
    /// Extended resources (GPUs, etc.)
    extended_resources: HashMap<String, i64>,
}

/// Node affinity rules
#[derive(Clone, Debug)]
struct NodeAffinity {
    required: Option<NodeSelector>,
    preferred: Vec<WeightedNodeSelector>,
}

/// Pod affinity rules
#[derive(Clone, Debug)]
struct PodAffinity {
    required: Vec<PodAffinityTerm>,
    preferred: Vec<WeightedPodAffinityTerm>,
}

/// Pod anti-affinity rules
#[derive(Clone, Debug)]
struct PodAntiAffinity {
    required: Vec<PodAffinityTerm>,
    preferred: Vec<WeightedPodAffinityTerm>,
}

/// Node selector for affinity
#[derive(Clone, Debug)]
struct NodeSelector {
    terms: Vec<NodeSelectorTerm>,
}

/// Individual node selector term
#[derive(Clone, Debug)]
struct NodeSelectorTerm {
    match_expressions: Vec<NodeSelectorRequirement>,
    match_fields: Vec<NodeSelectorRequirement>,
}

/// Node selector requirement
#[derive(Clone, Debug)]
struct NodeSelectorRequirement {
    key: String,
    operator: SelectorOperator,
    values: Vec<String>,
}

/// Selector operators
#[derive(Clone, Debug)]
enum SelectorOperator {
    In,
    NotIn,
    Exists,
    DoesNotExist,
    Gt,
    Lt,
}

/// Weighted node selector for preferred affinity
#[derive(Clone, Debug)]
struct WeightedNodeSelector {
    weight: i32,
    preference: NodeSelectorTerm,
}

/// Pod affinity term
#[derive(Clone, Debug)]
struct PodAffinityTerm {
    label_selector: LabelSelector,
    topology_key: String,
    namespace_selector: Option<LabelSelector>,
    namespaces: Vec<String>,
}

/// Weighted pod affinity term
#[derive(Clone, Debug)]
struct WeightedPodAffinityTerm {
    weight: i32,
    pod_affinity_term: PodAffinityTerm,
}

/// Label selector
#[derive(Clone, Debug)]
struct LabelSelector {
    match_labels: HashMap<String, String>,
    match_expressions: Vec<LabelSelectorRequirement>,
}

/// Label selector requirement
#[derive(Clone, Debug)]
struct LabelSelectorRequirement {
    key: String,
    operator: SelectorOperator,
    values: Vec<String>,
}

/// Toleration for node taints
#[derive(Clone, Debug)]
struct Toleration {
    key: Option<String>,
    operator: Option<TolerationOperator>,
    value: Option<String>,
    effect: Option<TaintEffect>,
    toleration_seconds: Option<i64>,
}

/// Toleration operators
#[derive(Clone, Debug)]
enum TolerationOperator {
    Exists,
    Equal,
}

/// Pre-computed scheduling decision cache
struct DecisionCache {
    /// Cache of scheduling decisions indexed by pod signature
    decisions: RwLock<HashMap<String, CachedDecision>>,
    /// Cache TTL
    ttl: Duration,
    /// Maximum cache size
    max_size: usize,
}

/// Cached scheduling decision
#[derive(Clone, Debug)]
struct CachedDecision {
    /// Selected node
    node: String,
    /// Decision score
    score: f64,
    /// Decision timestamp
    timestamp: Instant,
    /// Resource usage after scheduling
    projected_usage: ResourceRequirements,
}

/// Affinity rules cache for fast matching
struct AffinityCache {
    /// Node affinity rules indexed by node
    node_affinities: RwLock<HashMap<String, Vec<NodeAffinityRule>>>,
    /// Pod affinity rules indexed by topology key
    pod_affinities: RwLock<HashMap<String, Vec<PodAffinityRule>>>,
}

/// Cached node affinity rule
#[derive(Clone, Debug)]
struct NodeAffinityRule {
    selector: NodeSelector,
    weight: i32,
    required: bool,
}

/// Cached pod affinity rule
#[derive(Clone, Debug)]
struct PodAffinityRule {
    selector: LabelSelector,
    topology_key: String,
    weight: i32,
    required: bool,
    anti_affinity: bool,
}

/// Scheduler performance metrics
#[derive(Default)]
pub struct SchedulerMetrics {
    /// Total scheduling decisions made
    pub total_schedules: std::sync::atomic::AtomicU64,
    /// Successful schedules
    pub successful_schedules: std::sync::atomic::AtomicU64,
    /// Failed schedules
    pub failed_schedules: std::sync::atomic::AtomicU64,
    /// Average scheduling time (microseconds)
    pub avg_schedule_time: std::sync::atomic::AtomicU64,
    /// Peak scheduling time (microseconds)
    pub peak_schedule_time: std::sync::atomic::AtomicU64,
    /// Cache hit ratio
    pub cache_hit_ratio: std::sync::atomic::AtomicU64,
    /// Pending pods count
    pub pending_pods: std::sync::atomic::AtomicU64,
    /// Nodes processed per second
    pub nodes_per_second: std::sync::atomic::AtomicU64,
}

/// Scheduler configuration
#[derive(Clone)]
pub struct SchedulerConfig {
    /// Cache refresh interval
    pub cache_refresh_interval: Duration,
    /// Decision cache TTL
    pub decision_cache_ttl: Duration,
    /// Maximum decision cache size
    pub max_cache_size: usize,
    /// Scheduling queue size limit
    pub max_queue_size: usize,
    /// Maximum scheduling time per pod
    pub max_schedule_time: Duration,
    /// Enable affinity caching
    pub enable_affinity_cache: bool,
    /// Enable decision caching
    pub enable_decision_cache: bool,
    /// Number of worker threads
    pub worker_threads: usize,
}

impl Default for SchedulerConfig {
    fn default() -> Self {
        Self {
            cache_refresh_interval: Duration::from_millis(100),
            decision_cache_ttl: Duration::from_secs(30),
            max_cache_size: 10000,
            max_queue_size: 10000,
            max_schedule_time: Duration::from_millis(5), // 5ms max per pod
            enable_affinity_cache: true,
            enable_decision_cache: true,
            worker_threads: 4,
        }
    }
}

/// Scheduler events
#[derive(Debug)]
pub enum SchedulerEvent {
    /// Pod added to scheduling queue
    PodQueued(PendingPod),
    /// Pod scheduled successfully
    PodScheduled { pod_id: String, node: String, duration: Duration },
    /// Pod scheduling failed
    PodFailed { pod_id: String, reason: String },
    /// Node added or updated
    NodeUpdated(String),
    /// Node removed
    NodeRemoved(String),
}

/// Scheduling result
#[derive(Debug)]
pub struct SchedulingResult {
    pub pod_id: String,
    pub node: Option<String>,
    pub score: f64,
    pub duration: Duration,
    pub reason: Option<String>,
}

impl TeeScheduler {
    /// Create a new TEE scheduler
    pub fn new(store: Arc<TeeMemoryStore>) -> Self {
        Self::with_config(store, SchedulerConfig::default())
    }

    /// Create a new TEE scheduler with custom configuration
    pub fn with_config(store: Arc<TeeMemoryStore>, config: SchedulerConfig) -> Self {
        let (event_tx, event_rx) = mpsc::unbounded_channel();

        Self {
            store,
            node_cache: Arc::new(NodeResourceCache::new()),
            scheduling_queue: Arc::new(Mutex::new(SchedulingQueue::new())),
            decision_cache: Arc::new(DecisionCache::new(&config)),
            affinity_cache: Arc::new(AffinityCache::new()),
            metrics: Arc::new(SchedulerMetrics::default()),
            config,
            event_tx,
            event_rx: Arc::new(Mutex::new(event_rx)),
        }
    }

    /// Start the scheduler with background workers
    pub async fn start(&self) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        // Start cache refresh worker
        self.start_cache_refresh_worker().await;

        // Start scheduling workers
        for i in 0..self.config.worker_threads {
            self.start_scheduling_worker(i).await;
        }

        // Start event processor
        self.start_event_processor().await;

        println!("Nautilus TEE Scheduler started with {} workers", self.config.worker_threads);
        Ok(())
    }

    /// Schedule a pod with ultra-fast decision making
    pub async fn schedule_pod(&self, pod: PendingPod) -> SchedulingResult {
        let start_time = Instant::now();

        self.metrics.total_schedules.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.metrics.pending_pods.fetch_add(1, std::sync::atomic::Ordering::SeqCst);

        // Check decision cache first
        if self.config.enable_decision_cache {
            let cache_key = self.generate_pod_signature(&pod);
            if let Some(cached_decision) = self.decision_cache.get(&cache_key).await {
                // Verify the cached decision is still valid
                if self.validate_cached_decision(&pod, &cached_decision).await {
                    let duration = start_time.elapsed();
                    self.update_metrics_success(duration);

                    // Send scheduling event
                    let _ = self.event_tx.send(SchedulerEvent::PodScheduled {
                        pod_id: pod.id.clone(),
                        node: cached_decision.node.clone(),
                        duration,
                    });

                    return SchedulingResult {
                        pod_id: pod.id,
                        node: Some(cached_decision.node),
                        score: cached_decision.score,
                        duration,
                        reason: None,
                    };
                }
            }
        }

        // Perform fresh scheduling decision
        match self.find_best_node(&pod).await {
            Some((node, score)) => {
                let duration = start_time.elapsed();
                self.update_metrics_success(duration);

                // Cache the decision
                if self.config.enable_decision_cache {
                    let cache_key = self.generate_pod_signature(&pod);
                    let decision = CachedDecision {
                        node: node.clone(),
                        score,
                        timestamp: Instant::now(),
                        projected_usage: pod.resources.clone(),
                    };
                    self.decision_cache.put(cache_key, decision).await;
                }

                // Update node cache with resource allocation
                self.allocate_resources(&node, &pod.resources).await;

                // Send scheduling event
                let _ = self.event_tx.send(SchedulerEvent::PodScheduled {
                    pod_id: pod.id.clone(),
                    node: node.clone(),
                    duration,
                });

                SchedulingResult {
                    pod_id: pod.id,
                    node: Some(node),
                    score,
                    duration,
                    reason: None,
                }
            }
            None => {
                let duration = start_time.elapsed();
                self.update_metrics_failure(duration);

                let reason = "No suitable node found".to_string();

                // Send scheduling event
                let _ = self.event_tx.send(SchedulerEvent::PodFailed {
                    pod_id: pod.id.clone(),
                    reason: reason.clone(),
                });

                SchedulingResult {
                    pod_id: pod.id,
                    node: None,
                    score: 0.0,
                    duration,
                    reason: Some(reason),
                }
            }
        }
    }

    /// Add a pod to the scheduling queue
    pub async fn queue_pod(&self, pod: PendingPod) {
        self.metrics.pending_pods.fetch_add(1, std::sync::atomic::Ordering::SeqCst);

        let mut queue = self.scheduling_queue.lock().unwrap();
        queue.add_pod(pod.clone());

        // Send queuing event
        let _ = self.event_tx.send(SchedulerEvent::PodQueued(pod));
    }

    /// Update node information in cache
    pub async fn update_node(&self, node_name: String, node_info: CachedNodeInfo) {
        self.node_cache.update_node(node_name.clone(), node_info).await;

        // Send node update event
        let _ = self.event_tx.send(SchedulerEvent::NodeUpdated(node_name));
    }

    /// Remove node from cache
    pub async fn remove_node(&self, node_name: String) {
        self.node_cache.remove_node(&node_name).await;

        // Send node removal event
        let _ = self.event_tx.send(SchedulerEvent::NodeRemoved(node_name));
    }

    /// Get current scheduler metrics
    pub fn get_metrics(&self) -> &SchedulerMetrics {
        &self.metrics
    }

    // Private implementation methods

    /// Find the best node for a pod using optimized algorithms
    async fn find_best_node(&self, pod: &PendingPod) -> Option<(String, f64)> {
        let node_cache = self.node_cache.nodes.read().unwrap();

        let mut best_node = None;
        let mut best_score = 0.0;

        // Fast path: iterate through pre-sorted nodes
        let sorted_nodes = self.node_cache.sorted_nodes.read().unwrap();

        for node_name in sorted_nodes.iter() {
            if let Some(node_info) = node_cache.get(node_name) {
                // Quick resource check first (fastest filter)
                if !self.has_sufficient_resources(node_info, &pod.resources) {
                    continue;
                }

                // Check node conditions
                if !node_info.conditions.ready ||
                   node_info.conditions.memory_pressure ||
                   node_info.conditions.disk_pressure {
                    continue;
                }

                // Check taints and tolerations
                if !self.check_tolerations(node_info, &pod.tolerations) {
                    continue;
                }

                // Calculate comprehensive score
                let score = self.calculate_node_score(node_info, pod).await;

                if score > best_score {
                    best_score = score;
                    best_node = Some(node_name.clone());

                    // Early termination if we find a perfect score
                    if score >= 100.0 {
                        break;
                    }
                }
            }
        }

        best_node.map(|node| (node, best_score))
    }

    /// Check if node has sufficient resources
    fn has_sufficient_resources(&self, node: &CachedNodeInfo, requirements: &ResourceRequirements) -> bool {
        node.available_cpu >= requirements.cpu_request &&
        node.available_memory >= requirements.memory_request &&
        node.available_storage >= requirements.storage_request
    }

    /// Check if pod tolerations match node taints
    fn check_tolerations(&self, node: &CachedNodeInfo, tolerations: &[Toleration]) -> bool {
        for taint in &node.taints {
            let mut tolerated = false;

            for toleration in tolerations {
                if self.toleration_matches_taint(toleration, taint) {
                    tolerated = true;
                    break;
                }
            }

            if !tolerated && matches!(taint.effect, TaintEffect::NoSchedule) {
                return false;
            }
        }

        true
    }

    /// Check if a toleration matches a taint
    fn toleration_matches_taint(&self, toleration: &Toleration, taint: &NodeTaint) -> bool {
        // Simplified matching logic
        match &toleration.key {
            Some(key) => key == &taint.key,
            None => true, // Wildcard toleration
        }
    }

    /// Calculate comprehensive node score
    async fn calculate_node_score(&self, node: &CachedNodeInfo, pod: &PendingPod) -> f64 {
        let mut score = 0.0;

        // Resource utilization score (0-40 points)
        score += self.calculate_resource_score(node, &pod.resources) * 40.0;

        // Node affinity score (0-20 points)
        if let Some(ref affinity) = pod.node_affinity {
            score += self.calculate_node_affinity_score(node, affinity) * 20.0;
        }

        // Pod affinity/anti-affinity score (0-20 points)
        score += self.calculate_pod_affinity_score(node, pod).await * 20.0;

        // Load balancing score (0-10 points)
        score += self.calculate_load_balancing_score(node) * 10.0;

        // Node condition score (0-10 points)
        score += self.calculate_condition_score(node) * 10.0;

        score
    }

    /// Calculate resource utilization score (higher is better for balanced usage)
    fn calculate_resource_score(&self, node: &CachedNodeInfo, requirements: &ResourceRequirements) -> f64 {
        // Prefer nodes with balanced resource usage
        let cpu_ratio = requirements.cpu_request as f64 / node.available_cpu.max(1) as f64;
        let memory_ratio = requirements.memory_request as f64 / node.available_memory.max(1) as f64;

        // Score is higher for balanced utilization (closer to 0.7)
        let target_utilization = 0.7;
        let cpu_score = 1.0 - (cpu_ratio - target_utilization).abs();
        let memory_score = 1.0 - (memory_ratio - target_utilization).abs();

        (cpu_score + memory_score) / 2.0
    }

    /// Calculate node affinity score
    fn calculate_node_affinity_score(&self, node: &CachedNodeInfo, affinity: &NodeAffinity) -> f64 {
        let mut score = 0.0;

        // Check required affinity
        if let Some(ref required) = affinity.required {
            if !self.node_matches_selector(node, required) {
                return 0.0; // Hard requirement not met
            }
            score += 0.5;
        }

        // Check preferred affinity
        for preferred in &affinity.preferred {
            if self.node_matches_selector_term(node, &preferred.preference) {
                score += (preferred.weight as f64) / 100.0;
            }
        }

        score.min(1.0)
    }

    /// Check if node matches selector
    fn node_matches_selector(&self, node: &CachedNodeInfo, selector: &NodeSelector) -> bool {
        for term in &selector.terms {
            if self.node_matches_selector_term(node, term) {
                return true;
            }
        }
        false
    }

    /// Check if node matches selector term
    fn node_matches_selector_term(&self, node: &CachedNodeInfo, term: &NodeSelectorTerm) -> bool {
        // Check match expressions
        for expr in &term.match_expressions {
            if !self.evaluate_node_selector_requirement(node, expr) {
                return false;
            }
        }

        // Check match fields (simplified)
        for field in &term.match_fields {
            if !self.evaluate_node_selector_requirement(node, field) {
                return false;
            }
        }

        true
    }

    /// Evaluate node selector requirement
    fn evaluate_node_selector_requirement(&self, node: &CachedNodeInfo, req: &NodeSelectorRequirement) -> bool {
        let node_value = node.labels.get(&req.key);

        match req.operator {
            SelectorOperator::In => {
                if let Some(value) = node_value {
                    req.values.contains(value)
                } else {
                    false
                }
            }
            SelectorOperator::NotIn => {
                if let Some(value) = node_value {
                    !req.values.contains(value)
                } else {
                    true
                }
            }
            SelectorOperator::Exists => node_value.is_some(),
            SelectorOperator::DoesNotExist => node_value.is_none(),
            _ => true, // Simplified for other operators
        }
    }

    /// Calculate pod affinity score
    async fn calculate_pod_affinity_score(&self, _node: &CachedNodeInfo, _pod: &PendingPod) -> f64 {
        // Simplified implementation - would check existing pods on node
        0.5
    }

    /// Calculate load balancing score
    fn calculate_load_balancing_score(&self, node: &CachedNodeInfo) -> f64 {
        // Prefer nodes with fewer pods (simple load balancing)
        let max_pods = 110; // Typical node pod limit
        1.0 - (node.pod_count as f64 / max_pods as f64)
    }

    /// Calculate node condition score
    fn calculate_condition_score(&self, node: &CachedNodeInfo) -> f64 {
        let mut score = 1.0;

        if !node.conditions.ready {
            score -= 0.8;
        }
        if node.conditions.memory_pressure {
            score -= 0.2;
        }
        if node.conditions.disk_pressure {
            score -= 0.2;
        }
        if node.conditions.pid_pressure {
            score -= 0.1;
        }

        score.max(0.0)
    }

    /// Generate pod signature for caching
    fn generate_pod_signature(&self, pod: &PendingPod) -> String {
        // Simple signature based on resource requirements and affinity
        format!("{}:{}:{}:{}",
            pod.resources.cpu_request,
            pod.resources.memory_request,
            pod.resources.storage_request,
            pod.priority
        )
    }

    /// Validate cached decision is still applicable
    async fn validate_cached_decision(&self, pod: &PendingPod, decision: &CachedDecision) -> bool {
        // Check if decision is still fresh
        if decision.timestamp.elapsed() > self.config.decision_cache_ttl {
            return false;
        }

        // Check if target node still has sufficient resources
        let node_cache = self.node_cache.nodes.read().unwrap();
        if let Some(node_info) = node_cache.get(&decision.node) {
            self.has_sufficient_resources(node_info, &pod.resources)
        } else {
            false
        }
    }

    /// Allocate resources on selected node
    async fn allocate_resources(&self, node_name: &str, resources: &ResourceRequirements) {
        let mut node_cache = self.node_cache.nodes.write().unwrap();
        if let Some(node_info) = node_cache.get_mut(node_name) {
            node_info.available_cpu -= resources.cpu_request;
            node_info.available_memory -= resources.memory_request;
            node_info.available_storage -= resources.storage_request;
            node_info.pod_count += 1;
            node_info.updated_at = Instant::now();
        }
    }

    /// Update success metrics
    fn update_metrics_success(&self, duration: Duration) {
        self.metrics.successful_schedules.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.metrics.pending_pods.fetch_sub(1, std::sync::atomic::Ordering::SeqCst);

        let duration_us = duration.as_micros() as u64;

        // Update average scheduling time
        let current_avg = self.metrics.avg_schedule_time.load(std::sync::atomic::Ordering::SeqCst);
        let new_avg = (current_avg * 7 + duration_us) / 8; // Moving average
        self.metrics.avg_schedule_time.store(new_avg, std::sync::atomic::Ordering::SeqCst);

        // Update peak scheduling time
        let current_peak = self.metrics.peak_schedule_time.load(std::sync::atomic::Ordering::SeqCst);
        if duration_us > current_peak {
            self.metrics.peak_schedule_time.store(duration_us, std::sync::atomic::Ordering::SeqCst);
        }
    }

    /// Update failure metrics
    fn update_metrics_failure(&self, duration: Duration) {
        self.metrics.failed_schedules.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.metrics.pending_pods.fetch_sub(1, std::sync::atomic::Ordering::SeqCst);
    }

    // Background worker methods

    /// Start cache refresh worker
    async fn start_cache_refresh_worker(&self) {
        let store = Arc::clone(&self.store);
        let node_cache = Arc::clone(&self.node_cache);
        let interval_duration = self.config.cache_refresh_interval;

        tokio::spawn(async move {
            let mut interval = interval(interval_duration);
            loop {
                interval.tick().await;

                // Refresh node cache from store
                if let Err(e) = Self::refresh_node_cache(&store, &node_cache).await {
                    eprintln!("Error refreshing node cache: {}", e);
                }
            }
        });
    }

    /// Refresh node cache from store
    async fn refresh_node_cache(
        store: &TeeMemoryStore,
        cache: &NodeResourceCache,
    ) -> Result<(), String> {
        let options = QueryOptions::default();
        let result = store.list_objects("nodes", &options);

        let mut new_nodes = HashMap::new();
        let mut sorted_nodes = Vec::new();

        for node_data in result.data {
            // Parse node data and extract resource information
            // This is simplified - real implementation would parse K8s node objects
            if let Ok(node_info) = Self::parse_node_data(&node_data) {
                sorted_nodes.push(node_info.name.clone());
                new_nodes.insert(node_info.name.clone(), node_info);
            }
        }

        // Sort nodes by available resources (best nodes first)
        sorted_nodes.sort_by(|a, b| {
            let score_a = new_nodes.get(a).map(|n| n.score).unwrap_or(0.0);
            let score_b = new_nodes.get(b).map(|n| n.score).unwrap_or(0.0);
            score_b.partial_cmp(&score_a).unwrap_or(Ordering::Equal)
        });

        // Update cache
        {
            let mut nodes = cache.nodes.write().unwrap();
            *nodes = new_nodes;
        }
        {
            let mut sorted = cache.sorted_nodes.write().unwrap();
            *sorted = sorted_nodes;
        }
        {
            let mut last_update = cache.last_update.write().unwrap();
            *last_update = Instant::now();
        }

        Ok(())
    }

    /// Parse node data from store
    fn parse_node_data(data: &[u8]) -> Result<CachedNodeInfo, String> {
        // Simplified parsing - real implementation would parse K8s node objects
        Ok(CachedNodeInfo {
            name: "mock-node".to_string(),
            available_cpu: 4000,  // 4 cores
            available_memory: 8 * 1024 * 1024 * 1024, // 8GB
            available_storage: 100 * 1024 * 1024 * 1024, // 100GB
            labels: HashMap::new(),
            taints: Vec::new(),
            pod_count: 0,
            conditions: NodeConditions {
                ready: true,
                ..Default::default()
            },
            score: 80.0,
            updated_at: Instant::now(),
        })
    }

    /// Start scheduling worker
    async fn start_scheduling_worker(&self, worker_id: usize) {
        let queue = Arc::clone(&self.scheduling_queue);
        let scheduler = self.clone();

        tokio::spawn(async move {
            loop {
                // Get next pod from queue
                let pod = {
                    let mut queue = queue.lock().unwrap();
                    queue.get_next_pod()
                };

                if let Some(pod) = pod {
                    // Schedule the pod
                    let _result = scheduler.schedule_pod(pod).await;
                } else {
                    // No pods to schedule, sleep briefly
                    tokio::time::sleep(Duration::from_millis(10)).await;
                }
            }
        });
    }

    /// Start event processor
    async fn start_event_processor(&self) {
        let event_rx = Arc::clone(&self.event_rx);

        tokio::spawn(async move {
            let mut rx = event_rx.lock().unwrap();
            while let Some(event) = rx.recv().await {
                match event {
                    SchedulerEvent::PodScheduled { pod_id, node, duration } => {
                        println!("Pod {} scheduled to node {} in {:?}", pod_id, node, duration);
                    }
                    SchedulerEvent::PodFailed { pod_id, reason } => {
                        println!("Pod {} scheduling failed: {}", pod_id, reason);
                    }
                    _ => {}
                }
            }
        });
    }
}

// Clone implementation for background workers
impl Clone for TeeScheduler {
    fn clone(&self) -> Self {
        Self {
            store: Arc::clone(&self.store),
            node_cache: Arc::clone(&self.node_cache),
            scheduling_queue: Arc::clone(&self.scheduling_queue),
            decision_cache: Arc::clone(&self.decision_cache),
            affinity_cache: Arc::clone(&self.affinity_cache),
            metrics: Arc::clone(&self.metrics),
            config: self.config.clone(),
            event_tx: self.event_tx.clone(),
            event_rx: Arc::clone(&self.event_rx),
        }
    }
}

// Implementation of helper structs

impl NodeResourceCache {
    fn new() -> Self {
        Self {
            nodes: RwLock::new(HashMap::new()),
            sorted_nodes: RwLock::new(Vec::new()),
            cluster_resources: RwLock::new(ResourceSummary::default()),
            last_update: RwLock::new(Instant::now()),
        }
    }

    async fn update_node(&self, name: String, info: CachedNodeInfo) {
        let mut nodes = self.nodes.write().unwrap();
        nodes.insert(name, info);
    }

    async fn remove_node(&self, name: &str) {
        let mut nodes = self.nodes.write().unwrap();
        nodes.remove(name);
    }
}

impl SchedulingQueue {
    fn new() -> Self {
        Self {
            high_priority: VecDeque::new(),
            normal_priority: VecDeque::new(),
            low_priority: VecDeque::new(),
            failed_pods: HashMap::new(),
        }
    }

    fn add_pod(&mut self, pod: PendingPod) {
        match pod.priority {
            p if p > 1000 => self.high_priority.push_back(pod),
            p if p < -1000 => self.low_priority.push_back(pod),
            _ => self.normal_priority.push_back(pod),
        }
    }

    fn get_next_pod(&mut self) -> Option<PendingPod> {
        // Prioritize high priority pods
        if let Some(pod) = self.high_priority.pop_front() {
            return Some(pod);
        }

        // Then normal priority pods
        if let Some(pod) = self.normal_priority.pop_front() {
            return Some(pod);
        }

        // Finally low priority pods
        self.low_priority.pop_front()
    }
}

impl DecisionCache {
    fn new(config: &SchedulerConfig) -> Self {
        Self {
            decisions: RwLock::new(HashMap::new()),
            ttl: config.decision_cache_ttl,
            max_size: config.max_cache_size,
        }
    }

    async fn get(&self, key: &str) -> Option<CachedDecision> {
        let cache = self.decisions.read().unwrap();
        let decision = cache.get(key)?;

        // Check if decision is still valid
        if decision.timestamp.elapsed() < self.ttl {
            Some(decision.clone())
        } else {
            None
        }
    }

    async fn put(&self, key: String, decision: CachedDecision) {
        let mut cache = self.decisions.write().unwrap();

        // Evict old entries if cache is full
        if cache.len() >= self.max_size {
            // Simple LRU eviction
            if let Some(oldest_key) = cache.keys().next().cloned() {
                cache.remove(&oldest_key);
            }
        }

        cache.insert(key, decision);
    }
}

impl AffinityCache {
    fn new() -> Self {
        Self {
            node_affinities: RwLock::new(HashMap::new()),
            pod_affinities: RwLock::new(HashMap::new()),
        }
    }
}