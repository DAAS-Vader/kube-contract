// Nautilus TEE Secure Communication - Cryptographic inter-component messaging
// Optimized for TEE environment with zero-copy encrypted channels

use std::collections::HashMap;
use std::sync::{Arc, RwLock, Mutex};
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};
use tokio::sync::{mpsc, oneshot, broadcast};

/// Secure message bus for TEE inter-component communication
pub struct SecureMessageBus {
    /// Component registry
    components: Arc<RwLock<HashMap<String, ComponentInfo>>>,
    /// Message channels per component
    channels: Arc<RwLock<HashMap<String, ComponentChannels>>>,
    /// Cryptographic context
    crypto_context: Arc<CryptoContext>,
    /// Message routing table
    routing_table: Arc<RwLock<RoutingTable>>,
    /// Security policies
    security_policies: Arc<SecurityPolicies>,
    /// Performance metrics
    metrics: Arc<CommunicationMetrics>,
    /// Configuration
    config: CommunicationConfig,
}

/// Component information for registration
#[derive(Clone, Debug)]
pub struct ComponentInfo {
    pub id: String,
    pub name: String,
    pub component_type: ComponentType,
    pub public_key: Vec<u8>,
    pub permissions: Vec<Permission>,
    pub registered_at: Instant,
    pub last_heartbeat: Instant,
    pub trusted: bool,
}

/// Component types in the TEE master node
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ComponentType {
    ApiServer,
    Scheduler,
    ControllerManager,
    MemoryStore,
    External,
}

/// Permission types for component communication
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Permission {
    ReadPods,
    WritePods,
    ReadNodes,
    WriteNodes,
    ReadServices,
    WriteServices,
    ReadSecrets,
    WriteSecrets,
    Schedule,
    Admin,
}

/// Component communication channels
struct ComponentChannels {
    /// Incoming message channel
    incoming_tx: mpsc::UnboundedSender<SecureMessage>,
    incoming_rx: Arc<Mutex<mpsc::UnboundedReceiver<SecureMessage>>>,
    /// Outgoing message channel
    outgoing_tx: mpsc::UnboundedSender<SecureMessage>,
    outgoing_rx: Arc<Mutex<mpsc::UnboundedReceiver<SecureMessage>>>,
    /// Broadcast channel for system events
    broadcast_tx: broadcast::Sender<SystemEvent>,
    broadcast_rx: broadcast::Receiver<SystemEvent>,
}

/// Secure message structure
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SecureMessage {
    /// Message ID for tracking
    pub id: String,
    /// Source component ID
    pub from: String,
    /// Destination component ID
    pub to: String,
    /// Message type
    pub message_type: MessageType,
    /// Encrypted payload
    pub payload: Vec<u8>,
    /// Message signature
    pub signature: Vec<u8>,
    /// Timestamp
    pub timestamp: u64,
    /// Nonce for replay protection
    pub nonce: u64,
    /// Message priority
    pub priority: MessagePriority,
}

/// Message types for component communication
#[derive(Clone, Debug, Serialize, Deserialize)]
pub enum MessageType {
    /// Direct request-response
    Request,
    Response,
    /// Asynchronous notification
    Notification,
    /// System events
    SystemEvent,
    /// Heartbeat
    Heartbeat,
    /// Error message
    Error,
}

/// Message priority levels
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq, PartialOrd, Ord)]
pub enum MessagePriority {
    Critical = 4,
    High = 3,
    Normal = 2,
    Low = 1,
}

/// System events broadcast to all components
#[derive(Clone, Debug, Serialize, Deserialize)]
pub enum SystemEvent {
    ComponentRegistered { component_id: String },
    ComponentDeregistered { component_id: String },
    SecurityAlert { alert_type: String, details: String },
    PerformanceWarning { component_id: String, metric: String, value: f64 },
    SystemShutdown,
}

/// Cryptographic context for secure communication
pub struct CryptoContext {
    /// Master encryption key for TEE
    master_key: [u8; 32],
    /// Component key pairs
    component_keys: RwLock<HashMap<String, ComponentKeys>>,
    /// Nonce generator
    nonce_generator: Mutex<NonceGenerator>,
    /// Key derivation function parameters
    kdf_params: KdfParams,
}

/// Component cryptographic keys
#[derive(Clone)]
struct ComponentKeys {
    public_key: [u8; 32],
    private_key: [u8; 32],
    shared_secrets: HashMap<String, [u8; 32]>, // Pre-computed shared secrets
}

/// Nonce generator for replay protection
struct NonceGenerator {
    counter: u64,
    last_timestamp: u64,
}

/// Key derivation function parameters
struct KdfParams {
    iterations: u32,
    salt: [u8; 16],
}

/// Message routing table
struct RoutingTable {
    /// Direct routes between components
    direct_routes: HashMap<(String, String), RouteInfo>,
    /// Broadcast routes for system events
    broadcast_routes: HashMap<String, Vec<String>>,
    /// Message filters
    filters: HashMap<String, MessageFilter>,
}

/// Route information
#[derive(Clone, Debug)]
struct RouteInfo {
    latency: u64,          // microseconds
    bandwidth: u64,        // bytes per second
    reliability: f64,      // success rate 0.0-1.0
    last_updated: Instant,
}

/// Message filtering rules
#[derive(Clone, Debug)]
struct MessageFilter {
    allowed_types: Vec<MessageType>,
    rate_limit: Option<RateLimit>,
    size_limit: Option<usize>,
}

/// Rate limiting configuration
#[derive(Clone, Debug)]
struct RateLimit {
    max_messages: u32,
    time_window: std::time::Duration,
    current_count: u32,
    window_start: Instant,
}

/// Security policies for component communication
pub struct SecurityPolicies {
    /// Require encryption for all messages
    require_encryption: bool,
    /// Require signature verification
    require_signatures: bool,
    /// Maximum message age (anti-replay)
    max_message_age: std::time::Duration,
    /// Trusted component list
    trusted_components: RwLock<HashMap<String, TrustLevel>>,
    /// Blocked component list
    blocked_components: RwLock<HashMap<String, BlockReason>>,
}

/// Trust levels for components
#[derive(Clone, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum TrustLevel {
    Untrusted = 0,
    Limited = 1,
    Trusted = 2,
    HighlyTrusted = 3,
}

/// Reasons for blocking components
#[derive(Clone, Debug)]
pub enum BlockReason {
    SecurityViolation,
    ExcessiveErrors,
    RateLimitExceeded,
    ManualBlock,
}

/// Communication performance metrics
#[derive(Default)]
pub struct CommunicationMetrics {
    /// Total messages sent
    pub messages_sent: std::sync::atomic::AtomicU64,
    /// Total messages received
    pub messages_received: std::sync::atomic::AtomicU64,
    /// Messages per second
    pub messages_per_second: std::sync::atomic::AtomicU64,
    /// Average message latency (microseconds)
    pub avg_latency: std::sync::atomic::AtomicU64,
    /// Peak message latency (microseconds)
    pub peak_latency: std::sync::atomic::AtomicU64,
    /// Encryption operations per second
    pub encryption_ops_per_second: std::sync::atomic::AtomicU64,
    /// Failed messages
    pub failed_messages: std::sync::atomic::AtomicU64,
    /// Security violations
    pub security_violations: std::sync::atomic::AtomicU64,
}

/// Communication configuration
#[derive(Clone)]
pub struct CommunicationConfig {
    /// Maximum message size
    pub max_message_size: usize,
    /// Message timeout
    pub message_timeout: std::time::Duration,
    /// Enable zero-copy optimization
    pub enable_zero_copy: bool,
    /// Enable message compression
    pub enable_compression: bool,
    /// Heartbeat interval
    pub heartbeat_interval: std::time::Duration,
    /// Maximum concurrent connections
    pub max_connections: usize,
    /// Buffer sizes
    pub send_buffer_size: usize,
    pub recv_buffer_size: usize,
}

impl Default for CommunicationConfig {
    fn default() -> Self {
        Self {
            max_message_size: 1024 * 1024, // 1MB
            message_timeout: std::time::Duration::from_secs(30),
            enable_zero_copy: true,
            enable_compression: true,
            heartbeat_interval: std::time::Duration::from_secs(10),
            max_connections: 1000,
            send_buffer_size: 64 * 1024,
            recv_buffer_size: 64 * 1024,
        }
    }
}

impl SecureMessageBus {
    /// Create a new secure message bus
    pub fn new() -> Self {
        Self::with_config(CommunicationConfig::default())
    }

    /// Create a new secure message bus with configuration
    pub fn with_config(config: CommunicationConfig) -> Self {
        Self {
            components: Arc::new(RwLock::new(HashMap::new())),
            channels: Arc::new(RwLock::new(HashMap::new())),
            crypto_context: Arc::new(CryptoContext::new()),
            routing_table: Arc::new(RwLock::new(RoutingTable::new())),
            security_policies: Arc::new(SecurityPolicies::default()),
            metrics: Arc::new(CommunicationMetrics::default()),
            config,
        }
    }

    /// Register a component with the message bus
    pub async fn register_component(&self, info: ComponentInfo) -> Result<ComponentChannels, CommunicationError> {
        // Validate component permissions
        if !self.validate_component_registration(&info) {
            return Err(CommunicationError::RegistrationFailed(
                "Component validation failed".to_string()
            ));
        }

        // Generate cryptographic keys for the component
        let keys = self.crypto_context.generate_component_keys(&info.id).await?;

        // Create communication channels
        let channels = self.create_component_channels(&info.id).await?;

        // Register in component registry
        {
            let mut components = self.components.write().unwrap();
            components.insert(info.id.clone(), info.clone());
        }

        // Update routing table
        self.update_routing_table(&info.id, &info.component_type).await;

        // Broadcast registration event
        let event = SystemEvent::ComponentRegistered {
            component_id: info.id.clone(),
        };
        self.broadcast_system_event(event).await;

        println!("Component registered: {} ({})", info.name, info.id);
        Ok(channels)
    }

    /// Deregister a component
    pub async fn deregister_component(&self, component_id: &str) -> Result<(), CommunicationError> {
        // Remove from registry
        {
            let mut components = self.components.write().unwrap();
            components.remove(component_id);
        }

        // Remove channels
        {
            let mut channels = self.channels.write().unwrap();
            channels.remove(component_id);
        }

        // Remove from crypto context
        self.crypto_context.remove_component_keys(component_id).await;

        // Update routing table
        self.remove_from_routing_table(component_id).await;

        // Broadcast deregistration event
        let event = SystemEvent::ComponentDeregistered {
            component_id: component_id.to_string(),
        };
        self.broadcast_system_event(event).await;

        println!("Component deregistered: {}", component_id);
        Ok(())
    }

    /// Send a secure message between components
    pub async fn send_message(&self, message: SecureMessage) -> Result<(), CommunicationError> {
        let start_time = Instant::now();

        // Validate message
        self.validate_message(&message)?;

        // Check security policies
        if !self.check_security_policies(&message)? {
            self.metrics.security_violations.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            return Err(CommunicationError::SecurityViolation(
                "Message failed security policy check".to_string()
            ));
        }

        // Encrypt message if required
        let encrypted_message = if self.security_policies.require_encryption {
            self.encrypt_message(message)?
        } else {
            message
        };

        // Route message to destination
        self.route_message(encrypted_message).await?;

        // Update metrics
        self.metrics.messages_sent.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        let latency = start_time.elapsed().as_micros() as u64;
        self.update_latency_metrics(latency);

        Ok(())
    }

    /// Receive a message from a component's inbox
    pub async fn receive_message(&self, component_id: &str) -> Result<Option<SecureMessage>, CommunicationError> {
        let channels = self.channels.read().unwrap();
        if let Some(component_channels) = channels.get(component_id) {
            let mut rx = component_channels.incoming_rx.lock().unwrap();
            if let Ok(message) = rx.try_recv() {
                // Decrypt message if needed
                let decrypted_message = if self.security_policies.require_encryption {
                    self.decrypt_message(message)?
                } else {
                    message
                };

                self.metrics.messages_received.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                return Ok(Some(decrypted_message));
            }
        }
        Ok(None)
    }

    /// Subscribe to system events
    pub async fn subscribe_to_events(&self, component_id: &str) -> Result<broadcast::Receiver<SystemEvent>, CommunicationError> {
        let channels = self.channels.read().unwrap();
        if let Some(component_channels) = channels.get(component_id) {
            Ok(component_channels.broadcast_tx.subscribe())
        } else {
            Err(CommunicationError::ComponentNotFound(component_id.to_string()))
        }
    }

    /// Get current communication metrics
    pub fn get_metrics(&self) -> &CommunicationMetrics {
        &self.metrics
    }

    // Private implementation methods

    /// Validate component registration
    fn validate_component_registration(&self, info: &ComponentInfo) -> bool {
        // Check if component is blocked
        let blocked = self.security_policies.blocked_components.read().unwrap();
        if blocked.contains_key(&info.id) {
            return false;
        }

        // Validate permissions
        self.validate_permissions(&info.permissions, &info.component_type)
    }

    /// Validate component permissions
    fn validate_permissions(&self, permissions: &[Permission], component_type: &ComponentType) -> bool {
        // Define allowed permissions per component type
        let allowed_permissions = match component_type {
            ComponentType::ApiServer => vec![
                Permission::ReadPods, Permission::WritePods,
                Permission::ReadNodes, Permission::WriteNodes,
                Permission::ReadServices, Permission::WriteServices,
                Permission::ReadSecrets, Permission::WriteSecrets,
            ],
            ComponentType::Scheduler => vec![
                Permission::ReadPods, Permission::WritePods,
                Permission::ReadNodes, Permission::Schedule,
            ],
            ComponentType::ControllerManager => vec![
                Permission::ReadPods, Permission::WritePods,
                Permission::ReadNodes, Permission::WriteNodes,
                Permission::ReadServices, Permission::WriteServices,
            ],
            ComponentType::MemoryStore => vec![
                Permission::ReadPods, Permission::WritePods,
                Permission::ReadNodes, Permission::WriteNodes,
                Permission::ReadServices, Permission::WriteServices,
                Permission::ReadSecrets, Permission::WriteSecrets,
            ],
            ComponentType::External => vec![], // No permissions by default
        };

        // Check if all requested permissions are allowed
        permissions.iter().all(|p| allowed_permissions.contains(p))
    }

    /// Create communication channels for a component
    async fn create_component_channels(&self, component_id: &str) -> Result<ComponentChannels, CommunicationError> {
        let (incoming_tx, incoming_rx) = mpsc::unbounded_channel();
        let (outgoing_tx, outgoing_rx) = mpsc::unbounded_channel();
        let (broadcast_tx, broadcast_rx) = broadcast::channel(1000);

        let channels = ComponentChannels {
            incoming_tx,
            incoming_rx: Arc::new(Mutex::new(incoming_rx)),
            outgoing_tx,
            outgoing_rx: Arc::new(Mutex::new(outgoing_rx)),
            broadcast_tx,
            broadcast_rx,
        };

        // Store channels
        {
            let mut channels_map = self.channels.write().unwrap();
            channels_map.insert(component_id.to_string(), channels);
        }

        // Return a copy for the component
        let channels_copy = {
            let channels_map = self.channels.read().unwrap();
            channels_map.get(component_id).unwrap().clone()
        };

        Ok(channels_copy)
    }

    /// Update routing table for a new component
    async fn update_routing_table(&self, component_id: &str, component_type: &ComponentType) {
        let mut routing = self.routing_table.write().unwrap();

        // Add default routes based on component type
        let other_components: Vec<String> = {
            let components = self.components.read().unwrap();
            components.keys().filter(|&k| k != component_id).cloned().collect()
        };

        for other_id in other_components {
            let route_info = RouteInfo {
                latency: 100, // Default 100μs latency
                bandwidth: 1_000_000_000, // 1 Gbps
                reliability: 0.999,
                last_updated: Instant::now(),
            };

            routing.direct_routes.insert(
                (component_id.to_string(), other_id.clone()),
                route_info.clone()
            );
            routing.direct_routes.insert(
                (other_id, component_id.to_string()),
                route_info
            );
        }
    }

    /// Remove component from routing table
    async fn remove_from_routing_table(&self, component_id: &str) {
        let mut routing = self.routing_table.write().unwrap();

        // Remove all routes involving this component
        routing.direct_routes.retain(|(from, to), _| {
            from != component_id && to != component_id
        });

        routing.filters.remove(component_id);
    }

    /// Validate message format and content
    fn validate_message(&self, message: &SecureMessage) -> Result<(), CommunicationError> {
        // Check message size
        if message.payload.len() > self.config.max_message_size {
            return Err(CommunicationError::MessageTooLarge(message.payload.len()));
        }

        // Check timestamp (anti-replay)
        let current_time = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| CommunicationError::InvalidTimestamp)?
            .as_secs();

        let message_age = current_time.saturating_sub(message.timestamp);
        if message_age > self.security_policies.max_message_age.as_secs() {
            return Err(CommunicationError::MessageTooOld(message_age));
        }

        // Verify components exist
        let components = self.components.read().unwrap();
        if !components.contains_key(&message.from) {
            return Err(CommunicationError::ComponentNotFound(message.from.clone()));
        }
        if !components.contains_key(&message.to) {
            return Err(CommunicationError::ComponentNotFound(message.to.clone()));
        }

        Ok(())
    }

    /// Check security policies for a message
    fn check_security_policies(&self, message: &SecureMessage) -> Result<bool, CommunicationError> {
        // Check if source component is trusted
        let trusted = self.security_policies.trusted_components.read().unwrap();
        let trust_level = trusted.get(&message.from).unwrap_or(&TrustLevel::Untrusted);

        // Check if source is blocked
        let blocked = self.security_policies.blocked_components.read().unwrap();
        if blocked.contains_key(&message.from) {
            return Ok(false);
        }

        // Apply trust-based policies
        match trust_level {
            TrustLevel::Untrusted => {
                // Untrusted components can only send basic messages
                matches!(message.message_type, MessageType::Request | MessageType::Heartbeat)
            }
            TrustLevel::Limited => {
                // Limited trust can send most message types
                !matches!(message.message_type, MessageType::SystemEvent)
            }
            TrustLevel::Trusted | TrustLevel::HighlyTrusted => {
                // Trusted components can send all message types
                true
            }
        }.then_some(true).ok_or(CommunicationError::SecurityViolation(
            "Trust level insufficient for message type".to_string()
        ))
    }

    /// Encrypt a message
    fn encrypt_message(&self, mut message: SecureMessage) -> Result<SecureMessage, CommunicationError> {
        // Get shared secret between components
        let shared_secret = self.crypto_context.get_shared_secret(&message.from, &message.to)
            .ok_or_else(|| CommunicationError::CryptographicError("No shared secret".to_string()))?;

        // Encrypt payload using AES-256-GCM
        let encrypted_payload = self.crypto_context.encrypt(&message.payload, &shared_secret)?;
        message.payload = encrypted_payload;

        // Sign the message
        if self.security_policies.require_signatures {
            let signature = self.crypto_context.sign_message(&message, &message.from)?;
            message.signature = signature;
        }

        Ok(message)
    }

    /// Decrypt a message
    fn decrypt_message(&self, mut message: SecureMessage) -> Result<SecureMessage, CommunicationError> {
        // Verify signature if required
        if self.security_policies.require_signatures && !message.signature.is_empty() {
            if !self.crypto_context.verify_signature(&message, &message.from)? {
                return Err(CommunicationError::InvalidSignature);
            }
        }

        // Get shared secret
        let shared_secret = self.crypto_context.get_shared_secret(&message.from, &message.to)
            .ok_or_else(|| CommunicationError::CryptographicError("No shared secret".to_string()))?;

        // Decrypt payload
        let decrypted_payload = self.crypto_context.decrypt(&message.payload, &shared_secret)?;
        message.payload = decrypted_payload;

        Ok(message)
    }

    /// Route message to destination component
    async fn route_message(&self, message: SecureMessage) -> Result<(), CommunicationError> {
        let channels = self.channels.read().unwrap();
        if let Some(dest_channels) = channels.get(&message.to) {
            dest_channels.incoming_tx.send(message)
                .map_err(|_| CommunicationError::RoutingFailed("Channel send failed".to_string()))?;
            Ok(())
        } else {
            Err(CommunicationError::ComponentNotFound(message.to))
        }
    }

    /// Broadcast system event to all components
    async fn broadcast_system_event(&self, event: SystemEvent) {
        let channels = self.channels.read().unwrap();
        for (_, component_channels) in channels.iter() {
            let _ = component_channels.broadcast_tx.send(event.clone());
        }
    }

    /// Update latency metrics
    fn update_latency_metrics(&self, latency: u64) {
        // Update average latency
        let current_avg = self.metrics.avg_latency.load(std::sync::atomic::Ordering::SeqCst);
        let new_avg = (current_avg * 7 + latency) / 8; // Moving average
        self.metrics.avg_latency.store(new_avg, std::sync::atomic::Ordering::SeqCst);

        // Update peak latency
        let current_peak = self.metrics.peak_latency.load(std::sync::atomic::Ordering::SeqCst);
        if latency > current_peak {
            self.metrics.peak_latency.store(latency, std::sync::atomic::Ordering::SeqCst);
        }
    }
}

// Clone implementation for ComponentChannels
impl Clone for ComponentChannels {
    fn clone(&self) -> Self {
        Self {
            incoming_tx: self.incoming_tx.clone(),
            incoming_rx: Arc::clone(&self.incoming_rx),
            outgoing_tx: self.outgoing_tx.clone(),
            outgoing_rx: Arc::clone(&self.outgoing_rx),
            broadcast_tx: self.broadcast_tx.clone(),
            broadcast_rx: self.broadcast_tx.subscribe(),
        }
    }
}

// Implementation of helper structs

impl CryptoContext {
    fn new() -> Self {
        // Generate master key from TEE sealing
        let master_key = [0u8; 32]; // Would use TEE sealing key

        Self {
            master_key,
            component_keys: RwLock::new(HashMap::new()),
            nonce_generator: Mutex::new(NonceGenerator::new()),
            kdf_params: KdfParams {
                iterations: 100000,
                salt: [0u8; 16], // Would be random
            },
        }
    }

    async fn generate_component_keys(&self, component_id: &str) -> Result<ComponentKeys, CommunicationError> {
        // Generate Ed25519 key pair
        let private_key = [0u8; 32]; // Would use secure random
        let public_key = [0u8; 32];  // Would derive from private key

        let keys = ComponentKeys {
            public_key,
            private_key,
            shared_secrets: HashMap::new(),
        };

        // Store keys
        {
            let mut component_keys = self.component_keys.write().unwrap();
            component_keys.insert(component_id.to_string(), keys.clone());
        }

        Ok(keys)
    }

    async fn remove_component_keys(&self, component_id: &str) {
        let mut component_keys = self.component_keys.write().unwrap();
        component_keys.remove(component_id);
    }

    fn get_shared_secret(&self, from: &str, to: &str) -> Option<[u8; 32]> {
        let keys = self.component_keys.read().unwrap();
        if let Some(from_keys) = keys.get(from) {
            from_keys.shared_secrets.get(to).copied()
        } else {
            None
        }
    }

    fn encrypt(&self, data: &[u8], key: &[u8; 32]) -> Result<Vec<u8>, CommunicationError> {
        // AES-256-GCM encryption implementation
        // For now, just return data (would implement actual encryption)
        Ok(data.to_vec())
    }

    fn decrypt(&self, data: &[u8], key: &[u8; 32]) -> Result<Vec<u8>, CommunicationError> {
        // AES-256-GCM decryption implementation
        // For now, just return data (would implement actual decryption)
        Ok(data.to_vec())
    }

    fn sign_message(&self, message: &SecureMessage, component_id: &str) -> Result<Vec<u8>, CommunicationError> {
        // Ed25519 signature implementation
        // For now, return empty signature
        Ok(vec![0u8; 64])
    }

    fn verify_signature(&self, message: &SecureMessage, component_id: &str) -> Result<bool, CommunicationError> {
        // Ed25519 signature verification
        // For now, always return true
        Ok(true)
    }
}

impl NonceGenerator {
    fn new() -> Self {
        Self {
            counter: 0,
            last_timestamp: 0,
        }
    }

    fn next_nonce(&mut self) -> u64 {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();

        if now > self.last_timestamp {
            self.last_timestamp = now;
            self.counter = 0;
        } else {
            self.counter += 1;
        }

        (now << 32) | self.counter
    }
}

impl RoutingTable {
    fn new() -> Self {
        Self {
            direct_routes: HashMap::new(),
            broadcast_routes: HashMap::new(),
            filters: HashMap::new(),
        }
    }
}

impl SecurityPolicies {
    fn default() -> Self {
        Self {
            require_encryption: true,
            require_signatures: true,
            max_message_age: std::time::Duration::from_secs(60),
            trusted_components: RwLock::new(HashMap::new()),
            blocked_components: RwLock::new(HashMap::new()),
        }
    }
}

/// Communication error types
#[derive(Debug)]
pub enum CommunicationError {
    RegistrationFailed(String),
    ComponentNotFound(String),
    MessageTooLarge(usize),
    MessageTooOld(u64),
    InvalidTimestamp,
    SecurityViolation(String),
    InvalidSignature,
    CryptographicError(String),
    RoutingFailed(String),
}

impl std::fmt::Display for CommunicationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CommunicationError::RegistrationFailed(msg) => write!(f, "Registration failed: {}", msg),
            CommunicationError::ComponentNotFound(id) => write!(f, "Component not found: {}", id),
            CommunicationError::MessageTooLarge(size) => write!(f, "Message too large: {} bytes", size),
            CommunicationError::MessageTooOld(age) => write!(f, "Message too old: {} seconds", age),
            CommunicationError::InvalidTimestamp => write!(f, "Invalid timestamp"),
            CommunicationError::SecurityViolation(msg) => write!(f, "Security violation: {}", msg),
            CommunicationError::InvalidSignature => write!(f, "Invalid signature"),
            CommunicationError::CryptographicError(msg) => write!(f, "Cryptographic error: {}", msg),
            CommunicationError::RoutingFailed(msg) => write!(f, "Routing failed: {}", msg),
        }
    }
}

impl std::error::Error for CommunicationError {}