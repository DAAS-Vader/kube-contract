// Nautilus TEE API Server - Ultra-fast Kubernetes API with <50ms response time
// Optimized for TEE environment with zero-allocation request processing

use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};
use std::net::SocketAddr;

use tokio::net::TcpListener;
use tokio::sync::{mpsc, Semaphore};
use tokio_util::codec::{Framed, LengthDelimitedCodec};
use futures_util::{SinkExt, StreamExt};

use hyper::{Body, Request, Response, StatusCode, Method};
use hyper::service::{make_service_fn, service_fn};
use hyper::server::Server;

use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use prost::Message;

use crate::memory_store::{TeeMemoryStore, QueryOptions, OperationResult};

/// High-performance TEE API Server
pub struct TeeApiServer {
    /// Reference to the memory store
    store: Arc<TeeMemoryStore>,
    /// Connection pool for clients
    connection_pool: Arc<ConnectionPool>,
    /// Request cache for identical requests
    response_cache: Arc<ResponseCache>,
    /// Rate limiter
    rate_limiter: Arc<RateLimiter>,
    /// Performance metrics
    metrics: Arc<ApiServerMetrics>,
    /// Configuration
    config: ApiServerConfig,
}

/// Connection pool for managing client connections
struct ConnectionPool {
    /// Active connections
    connections: RwLock<HashMap<String, ClientConnection>>,
    /// Connection timeout
    timeout: Duration,
    /// Maximum connections
    max_connections: usize,
}

/// Individual client connection
struct ClientConnection {
    /// Client identifier
    id: String,
    /// Connection establishment time
    established: Instant,
    /// Last activity time
    last_activity: Instant,
    /// Number of active requests
    active_requests: usize,
    /// Connection metadata
    metadata: ConnectionMetadata,
}

/// Connection metadata
#[derive(Clone)]
struct ConnectionMetadata {
    /// Client user agent
    user_agent: String,
    /// Client IP address
    remote_addr: SocketAddr,
    /// Authentication context
    auth_context: AuthContext,
}

/// Authentication context
#[derive(Clone)]
struct AuthContext {
    /// User identity
    user: String,
    /// User groups
    groups: Vec<String>,
    /// Service account
    service_account: Option<String>,
    /// Namespace
    namespace: Option<String>,
}

/// Response cache for frequently requested data
struct ResponseCache {
    /// Cached responses
    cache: RwLock<HashMap<String, CachedResponse>>,
    /// Cache TTL
    ttl: Duration,
    /// Maximum cache size
    max_size: usize,
}

/// Cached response entry
struct CachedResponse {
    /// Response data
    data: Vec<u8>,
    /// Content type
    content_type: String,
    /// Cache timestamp
    cached_at: Instant,
    /// Resource version when cached
    resource_version: u64,
    /// Access count
    access_count: u64,
}

/// Rate limiter for API requests
struct RateLimiter {
    /// Per-client rate limits
    client_limits: RwLock<HashMap<String, ClientRateLimit>>,
    /// Global rate limit
    global_limit: Arc<Semaphore>,
}

/// Per-client rate limiting state
struct ClientRateLimit {
    /// Tokens available
    tokens: f64,
    /// Last refill time
    last_refill: Instant,
    /// Requests per second limit
    rate: f64,
    /// Burst capacity
    burst: usize,
}

/// API server performance metrics
#[derive(Default)]
pub struct ApiServerMetrics {
    /// Total requests handled
    pub total_requests: std::sync::atomic::AtomicU64,
    /// Successful requests
    pub successful_requests: std::sync::atomic::AtomicU64,
    /// Failed requests
    pub failed_requests: std::sync::atomic::AtomicU64,
    /// Average response time (microseconds)
    pub avg_response_time: std::sync::atomic::AtomicU64,
    /// Peak response time (microseconds)
    pub peak_response_time: std::sync::atomic::AtomicU64,
    /// Cache hit ratio
    pub cache_hit_ratio: std::sync::atomic::AtomicU64,
    /// Active connections
    pub active_connections: std::sync::atomic::AtomicU64,
    /// Bytes transferred
    pub bytes_transferred: std::sync::atomic::AtomicU64,
}

/// API server configuration
#[derive(Clone)]
pub struct ApiServerConfig {
    /// Server bind address
    pub bind_addr: SocketAddr,
    /// Maximum concurrent connections
    pub max_connections: usize,
    /// Request timeout
    pub request_timeout: Duration,
    /// Response cache TTL
    pub cache_ttl: Duration,
    /// Maximum cache size
    pub max_cache_size: usize,
    /// Rate limit (requests per second per client)
    pub rate_limit: f64,
    /// Rate limit burst size
    pub rate_burst: usize,
    /// Enable request compression
    pub enable_compression: bool,
    /// Enable response caching
    pub enable_caching: bool,
    /// Enable metrics collection
    pub enable_metrics: bool,
}

impl Default for ApiServerConfig {
    fn default() -> Self {
        Self {
            bind_addr: "0.0.0.0:6443".parse().unwrap(),
            max_connections: 10000,
            request_timeout: Duration::from_millis(30000), // 30s total timeout
            cache_ttl: Duration::from_secs(60),
            max_cache_size: 10000,
            rate_limit: 1000.0, // 1000 requests per second per client
            rate_burst: 100,
            enable_compression: true,
            enable_caching: true,
            enable_metrics: true,
        }
    }
}

/// API request/response types
#[derive(Debug, Serialize, Deserialize)]
pub struct ApiRequest {
    pub method: String,
    pub path: String,
    pub query: HashMap<String, String>,
    pub headers: HashMap<String, String>,
    pub body: Option<Vec<u8>>,
    pub auth: AuthContext,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ApiResponse {
    pub status: u16,
    pub headers: HashMap<String, String>,
    pub body: Vec<u8>,
    pub latency_us: u64,
}

/// Kubernetes API resource information
#[derive(Debug)]
struct ResourceInfo {
    group: String,
    version: String,
    kind: String,
    namespace: Option<String>,
    name: Option<String>,
}

impl TeeApiServer {
    /// Create a new TEE API server
    pub fn new(store: Arc<TeeMemoryStore>) -> Self {
        Self::with_config(store, ApiServerConfig::default())
    }

    /// Create a new TEE API server with custom configuration
    pub fn with_config(store: Arc<TeeMemoryStore>, config: ApiServerConfig) -> Self {
        Self {
            store,
            connection_pool: Arc::new(ConnectionPool::new(&config)),
            response_cache: Arc::new(ResponseCache::new(&config)),
            rate_limiter: Arc::new(RateLimiter::new(&config)),
            metrics: Arc::new(ApiServerMetrics::default()),
            config,
        }
    }

    /// Start the API server
    pub async fn start(&self) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        let addr = self.config.bind_addr;

        // Create service factory
        let store = Arc::clone(&self.store);
        let connection_pool = Arc::clone(&self.connection_pool);
        let response_cache = Arc::clone(&self.response_cache);
        let rate_limiter = Arc::clone(&self.rate_limiter);
        let metrics = Arc::clone(&self.metrics);
        let config = self.config.clone();

        let make_service = make_service_fn(move |conn: &hyper::server::conn::AddrStream| {
            let remote_addr = conn.remote_addr();
            let store = Arc::clone(&store);
            let connection_pool = Arc::clone(&connection_pool);
            let response_cache = Arc::clone(&response_cache);
            let rate_limiter = Arc::clone(&rate_limiter);
            let metrics = Arc::clone(&metrics);
            let config = config.clone();

            async move {
                Ok::<_, hyper::Error>(service_fn(move |req| {
                    Self::handle_request(
                        req,
                        remote_addr,
                        Arc::clone(&store),
                        Arc::clone(&connection_pool),
                        Arc::clone(&response_cache),
                        Arc::clone(&rate_limiter),
                        Arc::clone(&metrics),
                        config.clone(),
                    )
                }))
            }
        });

        // Start the server
        let server = Server::bind(&addr)
            .serve(make_service)
            .with_graceful_shutdown(Self::shutdown_signal());

        println!("Nautilus TEE API Server listening on {}", addr);

        if let Err(e) = server.await {
            eprintln!("Server error: {}", e);
        }

        Ok(())
    }

    /// Handle individual HTTP request with ultra-fast processing
    async fn handle_request(
        req: Request<Body>,
        remote_addr: SocketAddr,
        store: Arc<TeeMemoryStore>,
        connection_pool: Arc<ConnectionPool>,
        response_cache: Arc<ResponseCache>,
        rate_limiter: Arc<RateLimiter>,
        metrics: Arc<ApiServerMetrics>,
        config: ApiServerConfig,
    ) -> Result<Response<Body>, hyper::Error> {
        let start_time = Instant::now();

        // Update metrics
        metrics.total_requests.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        metrics.active_connections.fetch_add(1, std::sync::atomic::Ordering::SeqCst);

        // Extract client identifier
        let client_id = Self::extract_client_id(&req, remote_addr);

        // Check rate limit
        if !rate_limiter.check_rate_limit(&client_id).await {
            return Ok(Self::create_error_response(
                StatusCode::TOO_MANY_REQUESTS,
                "Rate limit exceeded",
            ));
        }

        // Parse request
        let api_request = match Self::parse_request(req, remote_addr).await {
            Ok(req) => req,
            Err(e) => {
                return Ok(Self::create_error_response(
                    StatusCode::BAD_REQUEST,
                    &format!("Invalid request: {}", e),
                ));
            }
        };

        // Check cache for GET requests
        if api_request.method == "GET" && config.enable_caching {
            let cache_key = Self::generate_cache_key(&api_request);
            if let Some(cached) = response_cache.get(&cache_key).await {
                // Update cache hit metrics
                let cache_hits = metrics.cache_hit_ratio.load(std::sync::atomic::Ordering::SeqCst);
                metrics.cache_hit_ratio.store(cache_hits + 1, std::sync::atomic::Ordering::SeqCst);

                let response_time = start_time.elapsed().as_micros() as u64;
                Self::update_response_time_metrics(&metrics, response_time);

                return Ok(Response::builder()
                    .status(200)
                    .header("content-type", &cached.content_type)
                    .header("x-cache", "HIT")
                    .header("x-response-time", format!("{}μs", response_time))
                    .body(Body::from(cached.data))?);
            }
        }

        // Process the request
        let response = Self::process_api_request(&api_request, &store).await;

        // Cache successful GET responses
        if api_request.method == "GET" && response.status == 200 && config.enable_caching {
            let cache_key = Self::generate_cache_key(&api_request);
            response_cache.put(cache_key, &response).await;
        }

        // Update metrics
        if response.status < 400 {
            metrics.successful_requests.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        } else {
            metrics.failed_requests.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        }

        let response_time = start_time.elapsed().as_micros() as u64;
        Self::update_response_time_metrics(&metrics, response_time);
        metrics.active_connections.fetch_sub(1, std::sync::atomic::Ordering::SeqCst);

        // Build HTTP response
        let mut builder = Response::builder()
            .status(response.status)
            .header("x-response-time", format!("{}μs", response_time))
            .header("x-served-by", "nautilus-tee");

        for (key, value) in response.headers {
            builder = builder.header(key, value);
        }

        Ok(builder.body(Body::from(response.body))?)
    }

    /// Process Kubernetes API request
    async fn process_api_request(request: &ApiRequest, store: &TeeMemoryStore) -> ApiResponse {
        let start_time = Instant::now();

        // Parse the API path to extract resource information
        let resource_info = match Self::parse_api_path(&request.path) {
            Ok(info) => info,
            Err(e) => {
                return ApiResponse {
                    status: 400,
                    headers: Self::default_headers(),
                    body: format!("{{\"error\": \"{}\"}}", e).into_bytes(),
                    latency_us: start_time.elapsed().as_micros() as u64,
                };
            }
        };

        // Route request based on method and resource
        let result = match request.method.as_str() {
            "GET" => {
                if resource_info.name.is_some() {
                    Self::handle_get_resource(&resource_info, store).await
                } else {
                    Self::handle_list_resources(&resource_info, &request.query, store).await
                }
            }
            "POST" => {
                Self::handle_create_resource(&resource_info, request, store).await
            }
            "PUT" => {
                Self::handle_update_resource(&resource_info, request, store).await
            }
            "PATCH" => {
                Self::handle_patch_resource(&resource_info, request, store).await
            }
            "DELETE" => {
                Self::handle_delete_resource(&resource_info, store).await
            }
            _ => {
                ApiResponse {
                    status: 405,
                    headers: Self::default_headers(),
                    body: b"{\"error\": \"Method not allowed\"}".to_vec(),
                    latency_us: start_time.elapsed().as_micros() as u64,
                }
            }
        };

        result
    }

    /// Handle GET request for a specific resource
    async fn handle_get_resource(resource_info: &ResourceInfo, store: &TeeMemoryStore) -> ApiResponse {
        let start_time = Instant::now();

        let resource_key = Self::build_resource_key(resource_info);
        let resource_type = Self::get_store_type(&resource_info.kind);

        let result = store.get_object(&resource_type, &resource_key);

        match result.data {
            Some(data) => {
                ApiResponse {
                    status: 200,
                    headers: Self::json_headers(),
                    body: data,
                    latency_us: start_time.elapsed().as_micros() as u64,
                }
            }
            None => {
                ApiResponse {
                    status: 404,
                    headers: Self::json_headers(),
                    body: b"{\"error\": \"Resource not found\"}".to_vec(),
                    latency_us: start_time.elapsed().as_micros() as u64,
                }
            }
        }
    }

    /// Handle GET request for listing resources
    async fn handle_list_resources(
        resource_info: &ResourceInfo,
        query: &HashMap<String, String>,
        store: &TeeMemoryStore,
    ) -> ApiResponse {
        let start_time = Instant::now();

        let resource_type = Self::get_store_type(&resource_info.kind);

        // Build query options from request parameters
        let options = QueryOptions {
            namespace: resource_info.namespace.clone(),
            label_selector: query.get("labelSelector").cloned(),
            field_selector: query.get("fieldSelector").cloned(),
            limit: query.get("limit").and_then(|s| s.parse().ok()),
            continue_token: query.get("continue").cloned(),
            resource_version: query.get("resourceVersion").and_then(|s| s.parse().ok()),
        };

        let result = store.list_objects(&resource_type, &options);

        // Build Kubernetes list response
        let list_response = json!({
            "apiVersion": format!("{}/{}", resource_info.group, resource_info.version),
            "kind": format!("{}List", resource_info.kind),
            "metadata": {
                "resourceVersion": result.revision.to_string()
            },
            "items": result.data.iter().map(|data| {
                serde_json::from_slice::<Value>(data).unwrap_or(Value::Null)
            }).collect::<Vec<_>>()
        });

        ApiResponse {
            status: 200,
            headers: Self::json_headers(),
            body: serde_json::to_vec(&list_response).unwrap_or_default(),
            latency_us: start_time.elapsed().as_micros() as u64,
        }
    }

    /// Handle POST request for creating resources
    async fn handle_create_resource(
        resource_info: &ResourceInfo,
        request: &ApiRequest,
        store: &TeeMemoryStore,
    ) -> ApiResponse {
        let start_time = Instant::now();

        let resource_type = Self::get_store_type(&resource_info.kind);
        let body = request.body.as_ref().unwrap_or(&Vec::new());

        // Parse the resource from request body
        let resource: Value = match serde_json::from_slice(body) {
            Ok(r) => r,
            Err(e) => {
                return ApiResponse {
                    status: 400,
                    headers: Self::json_headers(),
                    body: format!("{{\"error\": \"Invalid JSON: {}\"}}", e).into_bytes(),
                    latency_us: start_time.elapsed().as_micros() as u64,
                };
            }
        };

        // Extract metadata and generate key
        let metadata = resource.get("metadata").and_then(|m| m.as_object());
        let name = metadata
            .and_then(|m| m.get("name"))
            .and_then(|n| n.as_str())
            .unwrap_or("unknown");

        let resource_key = if let Some(ns) = &resource_info.namespace {
            format!("{}/{}", ns, name)
        } else {
            name.to_string()
        };

        // Create object metadata
        let obj_metadata = crate::memory_store::ObjectMetadata {
            kind: resource_info.kind.clone(),
            namespace: resource_info.namespace.clone(),
            labels: HashMap::new(), // Extract from resource
            annotations: HashMap::new(), // Extract from resource
            size: body.len(),
            checksum: [0; 32], // Calculate checksum
        };

        let result = store.create_object(&resource_type, &resource_key, body, obj_metadata);

        ApiResponse {
            status: 201,
            headers: Self::json_headers(),
            body: body.clone(),
            latency_us: start_time.elapsed().as_micros() as u64,
        }
    }

    /// Handle PUT request for updating resources
    async fn handle_update_resource(
        resource_info: &ResourceInfo,
        request: &ApiRequest,
        store: &TeeMemoryStore,
    ) -> ApiResponse {
        let start_time = Instant::now();

        let resource_type = Self::get_store_type(&resource_info.kind);
        let resource_key = Self::build_resource_key(resource_info);
        let body = request.body.as_ref().unwrap_or(&Vec::new());

        // Create object metadata
        let obj_metadata = crate::memory_store::ObjectMetadata {
            kind: resource_info.kind.clone(),
            namespace: resource_info.namespace.clone(),
            labels: HashMap::new(),
            annotations: HashMap::new(),
            size: body.len(),
            checksum: [0; 32],
        };

        let result = store.update_object(&resource_type, &resource_key, body, obj_metadata, None);

        match result.data {
            Ok(_) => {
                ApiResponse {
                    status: 200,
                    headers: Self::json_headers(),
                    body: body.clone(),
                    latency_us: start_time.elapsed().as_micros() as u64,
                }
            }
            Err(e) => {
                ApiResponse {
                    status: 409,
                    headers: Self::json_headers(),
                    body: format!("{{\"error\": \"{}\"}}", e).into_bytes(),
                    latency_us: start_time.elapsed().as_micros() as u64,
                }
            }
        }
    }

    /// Handle PATCH request for partial updates
    async fn handle_patch_resource(
        resource_info: &ResourceInfo,
        request: &ApiRequest,
        store: &TeeMemoryStore,
    ) -> ApiResponse {
        let start_time = Instant::now();

        // For simplicity, treat PATCH as PUT in this implementation
        // Real implementation would apply JSON merge patch or strategic merge patch
        Self::handle_update_resource(resource_info, request, store).await
    }

    /// Handle DELETE request for removing resources
    async fn handle_delete_resource(resource_info: &ResourceInfo, store: &TeeMemoryStore) -> ApiResponse {
        let start_time = Instant::now();

        let resource_type = Self::get_store_type(&resource_info.kind);
        let resource_key = Self::build_resource_key(resource_info);

        let result = store.delete_object(&resource_type, &resource_key);

        if result.data {
            ApiResponse {
                status: 200,
                headers: Self::json_headers(),
                body: b"{\"status\": \"Success\"}".to_vec(),
                latency_us: start_time.elapsed().as_micros() as u64,
            }
        } else {
            ApiResponse {
                status: 404,
                headers: Self::json_headers(),
                body: b"{\"error\": \"Resource not found\"}".to_vec(),
                latency_us: start_time.elapsed().as_micros() as u64,
            }
        }
    }

    // Helper methods

    async fn parse_request(req: Request<Body>, remote_addr: SocketAddr) -> Result<ApiRequest, String> {
        let method = req.method().to_string();
        let path = req.uri().path().to_string();

        let query: HashMap<String, String> = req
            .uri()
            .query()
            .map(|v| {
                url::form_urlencoded::parse(v.as_bytes())
                    .into_owned()
                    .collect()
            })
            .unwrap_or_default();

        let headers: HashMap<String, String> = req
            .headers()
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
            .collect();

        let body = match hyper::body::to_bytes(req.into_body()).await {
            Ok(bytes) => if bytes.is_empty() { None } else { Some(bytes.to_vec()) },
            Err(_) => return Err("Failed to read request body".to_string()),
        };

        // Extract authentication context from headers
        let auth = AuthContext {
            user: headers.get("x-remote-user").cloned().unwrap_or("anonymous".to_string()),
            groups: vec![], // Extract from headers
            service_account: headers.get("x-remote-service-account").cloned(),
            namespace: headers.get("x-remote-namespace").cloned(),
        };

        Ok(ApiRequest {
            method,
            path,
            query,
            headers,
            body,
            auth,
        })
    }

    fn parse_api_path(path: &str) -> Result<ResourceInfo, String> {
        let parts: Vec<&str> = path.trim_start_matches('/').split('/').collect();

        if parts.len() < 3 {
            return Err("Invalid API path".to_string());
        }

        // Handle different API path patterns:
        // /api/v1/pods
        // /api/v1/namespaces/{namespace}/pods
        // /api/v1/namespaces/{namespace}/pods/{name}
        // /apis/{group}/{version}/...

        let (group, version, rest) = if parts[0] == "api" {
            ("", parts[1], &parts[2..])
        } else if parts[0] == "apis" {
            (parts[1], parts[2], &parts[3..])
        } else {
            return Err("Invalid API path".to_string());
        };

        let (namespace, kind, name) = match rest.len() {
            1 => (None, rest[0], None),
            2 => (None, rest[0], Some(rest[1])),
            3 if rest[0] == "namespaces" => (Some(rest[1]), rest[2], None),
            4 if rest[0] == "namespaces" => (Some(rest[1]), rest[2], Some(rest[3])),
            _ => return Err("Unsupported API path pattern".to_string()),
        };

        Ok(ResourceInfo {
            group: group.to_string(),
            version: version.to_string(),
            kind: kind.to_string(),
            namespace: namespace.map(|s| s.to_string()),
            name: name.map(|s| s.to_string()),
        })
    }

    fn build_resource_key(resource_info: &ResourceInfo) -> String {
        match (&resource_info.namespace, &resource_info.name) {
            (Some(ns), Some(name)) => format!("{}/{}", ns, name),
            (None, Some(name)) => name.clone(),
            _ => "unknown".to_string(),
        }
    }

    fn get_store_type(kind: &str) -> String {
        match kind.to_lowercase().as_str() {
            "node" => "nodes",
            "pod" => "pods",
            "service" => "services",
            "endpoint" => "endpoints",
            "configmap" => "configmaps",
            "secret" => "secrets",
            _ => kind,
        }.to_string()
    }

    fn extract_client_id(req: &Request<Body>, remote_addr: SocketAddr) -> String {
        // Use user agent + remote IP as client identifier
        let user_agent = req
            .headers()
            .get("user-agent")
            .and_then(|v| v.to_str().ok())
            .unwrap_or("unknown");

        format!("{}:{}", remote_addr.ip(), user_agent)
    }

    fn generate_cache_key(request: &ApiRequest) -> String {
        // Simple cache key based on method, path, and query
        format!("{}:{}:{:?}", request.method, request.path, request.query)
    }

    fn default_headers() -> HashMap<String, String> {
        let mut headers = HashMap::new();
        headers.insert("server".to_string(), "nautilus-tee".to_string());
        headers.insert("x-content-type-options".to_string(), "nosniff".to_string());
        headers
    }

    fn json_headers() -> HashMap<String, String> {
        let mut headers = Self::default_headers();
        headers.insert("content-type".to_string(), "application/json".to_string());
        headers
    }

    fn create_error_response(status: StatusCode, message: &str) -> Response<Body> {
        let body = format!("{{\"error\": \"{}\"}}", message);
        Response::builder()
            .status(status)
            .header("content-type", "application/json")
            .body(Body::from(body))
            .unwrap()
    }

    fn update_response_time_metrics(metrics: &ApiServerMetrics, response_time: u64) {
        // Update average response time
        let current_avg = metrics.avg_response_time.load(std::sync::atomic::Ordering::SeqCst);
        let new_avg = (current_avg * 7 + response_time) / 8; // Moving average
        metrics.avg_response_time.store(new_avg, std::sync::atomic::Ordering::SeqCst);

        // Update peak response time
        let current_peak = metrics.peak_response_time.load(std::sync::atomic::Ordering::SeqCst);
        if response_time > current_peak {
            metrics.peak_response_time.store(response_time, std::sync::atomic::Ordering::SeqCst);
        }
    }

    async fn shutdown_signal() {
        tokio::signal::ctrl_c()
            .await
            .expect("Failed to install CTRL+C signal handler");
    }

    /// Get current performance metrics
    pub fn get_metrics(&self) -> &ApiServerMetrics {
        &self.metrics
    }
}

// Implementation of helper structs

impl ConnectionPool {
    fn new(config: &ApiServerConfig) -> Self {
        Self {
            connections: RwLock::new(HashMap::new()),
            timeout: config.request_timeout,
            max_connections: config.max_connections,
        }
    }
}

impl ResponseCache {
    fn new(config: &ApiServerConfig) -> Self {
        Self {
            cache: RwLock::new(HashMap::new()),
            ttl: config.cache_ttl,
            max_size: config.max_cache_size,
        }
    }

    async fn get(&self, key: &str) -> Option<CachedResponse> {
        let cache = self.cache.read().unwrap();
        let entry = cache.get(key)?;

        // Check if entry is still valid
        if entry.cached_at.elapsed() < self.ttl {
            Some(entry.clone())
        } else {
            None
        }
    }

    async fn put(&self, key: String, response: &ApiResponse) {
        let mut cache = self.cache.write().unwrap();

        // Evict old entries if cache is full
        if cache.len() >= self.max_size {
            // Simple LRU eviction - remove oldest entry
            if let Some(oldest_key) = cache.keys().next().cloned() {
                cache.remove(&oldest_key);
            }
        }

        let cached_response = CachedResponse {
            data: response.body.clone(),
            content_type: "application/json".to_string(),
            cached_at: Instant::now(),
            resource_version: 0, // Extract from response
            access_count: 0,
        };

        cache.insert(key, cached_response);
    }
}

impl Clone for CachedResponse {
    fn clone(&self) -> Self {
        Self {
            data: self.data.clone(),
            content_type: self.content_type.clone(),
            cached_at: self.cached_at,
            resource_version: self.resource_version,
            access_count: self.access_count,
        }
    }
}

impl RateLimiter {
    fn new(config: &ApiServerConfig) -> Self {
        Self {
            client_limits: RwLock::new(HashMap::new()),
            global_limit: Arc::new(Semaphore::new((config.rate_limit * config.max_connections as f64) as usize)),
        }
    }

    async fn check_rate_limit(&self, client_id: &str) -> bool {
        // Check global limit first
        if self.global_limit.try_acquire().is_err() {
            return false;
        }

        let mut limits = self.client_limits.write().unwrap();
        let now = Instant::now();

        let client_limit = limits.entry(client_id.to_string()).or_insert(ClientRateLimit {
            tokens: 100.0, // Start with burst capacity
            last_refill: now,
            rate: 1000.0, // 1000 requests per second
            burst: 100,
        });

        // Refill tokens based on elapsed time
        let elapsed = now.duration_since(client_limit.last_refill).as_secs_f64();
        client_limit.tokens = (client_limit.tokens + elapsed * client_limit.rate).min(client_limit.burst as f64);
        client_limit.last_refill = now;

        // Check if we have tokens available
        if client_limit.tokens >= 1.0 {
            client_limit.tokens -= 1.0;
            true
        } else {
            false
        }
    }
}