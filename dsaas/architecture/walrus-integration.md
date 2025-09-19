# Walrus Storage Integration for K3s DaaS

## Overview

This document provides a comprehensive architecture for integrating Walrus decentralized storage into K3s agents, enabling container images and application code to be fetched from the Walrus network instead of traditional container registries. This integration transforms K3s into a fully decentralized platform where both compute and storage are distributed.

## Architectural Goals

1. **Decentralized Storage**: Replace traditional container registries with Walrus network storage
2. **Seamless Integration**: Maintain containerd compatibility while adding Walrus capabilities
3. **Performance Optimization**: Implement intelligent caching and parallel downloads
4. **Security**: Integrate with Seal for encrypted storage and authentication
5. **Backward Compatibility**: Support both traditional registries and Walrus simultaneously

## Current K3s Container Handling Analysis

### Existing Flow Architecture

Based on analysis of `pkg/agent/containerd/`, K3s currently handles container images through:

1. **containerd.go**: Main orchestration
   - `Run()`: Starts containerd process and preloads images
   - `PreloadImages()`: Imports from local files or pulls from registries
   - `prePullImages()`: Uses CRI API for registry pulls
   - Image labeling and pinning system

2. **config.go**: Registry configuration
   - Registry mirror/endpoint configuration
   - Host configuration template generation
   - Authentication and TLS settings

3. **watcher.go**: File system monitoring
   - Watches agent images directory for new files
   - Processes tarball imports and text-based pull lists
   - Caching system for file states

### Key Integration Points

1. **Image Pull Flow**: `prePullImages()` function in containerd.go:361
2. **Registry Configuration**: `getHostConfigs()` in config.go:145
3. **File Import System**: `preloadFile()` via watcher.go:166
4. **CRI Integration**: Uses `runtimeapi.ImageServiceClient` for pulls

## Walrus Integration Architecture

### 1. Walrus Client Integration

#### A. Client Initialization Architecture

```go
// NEW: pkg/walrus/client.go
type WalrusClient struct {
    httpClient       *http.Client
    aggregatorURLs   []string
    publisherNodes   []string
    storageNodes     []string
    cacheDir         string
    sealAuth         *seal.AuthClient
    blobCache        *BlobCache
    downloadManager  *DownloadManager
    retryConfig      *RetryConfig
    circuitBreaker   *CircuitBreaker
}

type WalrusConfig struct {
    Enabled             bool          `yaml:"enabled" env:"WALRUS_ENABLED"`
    AggregatorURLs      []string      `yaml:"aggregator_urls" env:"WALRUS_AGGREGATOR_URLS"`
    PublisherNodes      []string      `yaml:"publisher_nodes" env:"WALRUS_PUBLISHER_NODES"`
    StorageNodes        []string      `yaml:"storage_nodes" env:"WALRUS_STORAGE_NODES"`
    CacheDir            string        `yaml:"cache_dir" env:"WALRUS_CACHE_DIR"`
    MaxCacheSize        int64         `yaml:"max_cache_size" env:"WALRUS_MAX_CACHE_SIZE"`
    ParallelDownloads   int           `yaml:"parallel_downloads" env:"WALRUS_PARALLEL_DOWNLOADS"`
    ChunkSize           int64         `yaml:"chunk_size" env:"WALRUS_CHUNK_SIZE"`
    RetryAttempts       int           `yaml:"retry_attempts" env:"WALRUS_RETRY_ATTEMPTS"`
    TimeoutDuration     time.Duration `yaml:"timeout_duration" env:"WALRUS_TIMEOUT_DURATION"`
    SealEncryption      bool          `yaml:"seal_encryption" env:"WALRUS_SEAL_ENCRYPTION"`
    CompressionEnabled  bool          `yaml:"compression_enabled" env:"WALRUS_COMPRESSION_ENABLED"`
}

func NewWalrusClient(config *WalrusConfig, sealAuth *seal.AuthClient) (*WalrusClient, error) {
    // 1. Setup HTTP client with connection pooling
    transport := &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 30,
        IdleConnTimeout:     90 * time.Second,
        TLSHandshakeTimeout: 10 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
    }

    httpClient := &http.Client{
        Transport: transport,
        Timeout:   config.TimeoutDuration,
    }

    // 2. Initialize blob cache
    blobCache, err := NewBlobCache(&BlobCacheConfig{
        CacheDir:     config.CacheDir,
        MaxSize:      config.MaxCacheSize,
        Compression:  config.CompressionEnabled,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize blob cache: %w", err)
    }

    // 3. Setup download manager
    downloadManager := NewDownloadManager(&DownloadConfig{
        ParallelDownloads: config.ParallelDownloads,
        ChunkSize:        config.ChunkSize,
        RetryAttempts:    config.RetryAttempts,
    })

    // 4. Initialize circuit breaker
    circuitBreaker := NewCircuitBreaker(&CircuitBreakerConfig{
        Threshold:   5,
        Timeout:     60 * time.Second,
        MaxRequests: 3,
    })

    return &WalrusClient{
        httpClient:      httpClient,
        aggregatorURLs:  config.AggregatorURLs,
        publisherNodes:  config.PublisherNodes,
        storageNodes:    config.StorageNodes,
        cacheDir:        config.CacheDir,
        sealAuth:        sealAuth,
        blobCache:       blobCache,
        downloadManager: downloadManager,
        circuitBreaker:  circuitBreaker,
    }, nil
}
```

#### B. Blob Resolution and Download

```go
type BlobID struct {
    Hash     string `json:"hash"`
    Size     int64  `json:"size"`
    Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type BlobLocation struct {
    NodeURL     string    `json:"node_url"`
    BlobID      *BlobID   `json:"blob_id"`
    ValidUntil  time.Time `json:"valid_until"`
    Replicas    []string  `json:"replicas"`
}

func (wc *WalrusClient) ResolveBlobLocation(ctx context.Context, blobID *BlobID) (*BlobLocation, error) {
    return wc.circuitBreaker.Execute(func() (*BlobLocation, error) {
        // 1. Query aggregator nodes for blob location
        for _, aggregatorURL := range wc.aggregatorURLs {
            location, err := wc.queryAggregator(ctx, aggregatorURL, blobID)
            if err == nil {
                return location, nil
            }
            logrus.Warnf("Failed to resolve blob from aggregator %s: %v", aggregatorURL, err)
        }

        // 2. Fallback to direct storage node queries
        return wc.queryStorageNodes(ctx, blobID)
    })
}

func (wc *WalrusClient) DownloadBlob(ctx context.Context, blobID *BlobID) (io.ReadCloser, error) {
    // 1. Check local cache first
    if cached, err := wc.blobCache.Get(blobID.Hash); err == nil {
        return cached, nil
    }

    // 2. Resolve blob location
    location, err := wc.ResolveBlobLocation(ctx, blobID)
    if err != nil {
        return nil, fmt.Errorf("failed to resolve blob location: %w", err)
    }

    // 3. Download using download manager
    reader, err := wc.downloadManager.Download(ctx, location)
    if err != nil {
        return nil, fmt.Errorf("failed to download blob: %w", err)
    }

    // 4. Cache the blob and return tee reader
    return wc.blobCache.Store(blobID.Hash, reader), nil
}

func (wc *WalrusClient) queryAggregator(ctx context.Context, aggregatorURL string, blobID *BlobID) (*BlobLocation, error) {
    url := fmt.Sprintf("%s/v1/blobs/%s/location", aggregatorURL, blobID.Hash)

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }

    // Add Seal authentication if enabled
    if wc.sealAuth != nil {
        if err := wc.sealAuth.SignRequest(req); err != nil {
            return nil, fmt.Errorf("failed to sign request: %w", err)
        }
    }

    resp, err := wc.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("aggregator returned status %d", resp.StatusCode)
    }

    var location BlobLocation
    if err := json.NewDecoder(resp.Body).Decode(&location); err != nil {
        return nil, err
    }

    return &location, nil
}
```

### 2. Modified Container Pull Flow

#### A. Enhanced Image Service Integration

```go
// MODIFICATION POINT: pkg/agent/containerd/containerd.go
// Replace prePullImages function with Walrus-aware version

func prePullImagesWithWalrus(ctx context.Context, client *containerd.Client,
                            imageClient runtimeapi.ImageServiceClient,
                            walrusClient *walrus.WalrusClient,
                            imageList io.Reader) ([]images.Image, error) {

    errs := []error{}
    images := []images.Image{}
    imageService := client.ImageService()
    scanner := bufio.NewScanner(imageList)

    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }

        // Parse line format: [walrus://blob_id] or [registry_url]
        if strings.HasPrefix(line, "walrus://") {
            // Handle Walrus blob reference
            blobRef := strings.TrimPrefix(line, "walrus://")
            image, err := wc.pullImageFromWalrus(ctx, client, imageClient, blobRef)
            if err != nil {
                errs = append(errs, fmt.Errorf("failed to pull from Walrus %s: %w", blobRef, err))
                continue
            }
            images = append(images, *image)
        } else {
            // Handle traditional registry pull
            image, err := pullImageFromRegistry(ctx, client, imageClient, line)
            if err != nil {
                errs = append(errs, fmt.Errorf("failed to pull from registry %s: %w", line, err))
                continue
            }
            images = append(images, *image)
        }
    }

    return images, merr.NewErrors(errs...)
}

func (wc *WalrusClient) pullImageFromWalrus(ctx context.Context,
                                          client *containerd.Client,
                                          imageClient runtimeapi.ImageServiceClient,
                                          blobRef string) (*images.Image, error) {

    // 1. Parse blob reference (format: blob_id[:image_name[:tag]])
    parts := strings.Split(blobRef, ":")
    if len(parts) < 1 {
        return nil, fmt.Errorf("invalid blob reference: %s", blobRef)
    }

    blobID := &BlobID{Hash: parts[0]}
    imageName := "walrus/" + parts[0]
    imageTag := "latest"

    if len(parts) > 1 {
        imageName = parts[1]
    }
    if len(parts) > 2 {
        imageTag = parts[2]
    }

    fullImageName := fmt.Sprintf("%s:%s", imageName, imageTag)

    // 2. Check if image already exists
    if status, err := imageClient.ImageStatus(ctx, &runtimeapi.ImageStatusRequest{
        Image: &runtimeapi.ImageSpec{Image: fullImageName},
    }); err == nil && status.Image != nil {
        logrus.Infof("Walrus image %s already exists", fullImageName)
        return client.ImageService().Get(ctx, fullImageName)
    }

    // 3. Download blob from Walrus
    logrus.Infof("Pulling image %s from Walrus blob %s", fullImageName, blobID.Hash)
    reader, err := wc.DownloadBlob(ctx, blobID)
    if err != nil {
        return nil, fmt.Errorf("failed to download blob: %w", err)
    }
    defer reader.Close()

    // 4. Import into containerd
    importResults, err := client.Import(ctx, reader,
        containerd.WithAllPlatforms(true),
        containerd.WithSkipMissing(),
        containerd.WithImageRefTranslator(func(ref string) string {
            return fullImageName
        }))
    if err != nil {
        return nil, fmt.Errorf("failed to import image: %w", err)
    }

    if len(importResults) == 0 {
        return nil, fmt.Errorf("no images imported from blob %s", blobID.Hash)
    }

    return &importResults[0], nil
}
```

#### B. Registry Configuration Enhancement

```go
// MODIFICATION POINT: pkg/agent/containerd/config.go
// Enhance getHostConfigs to include Walrus endpoints

func getHostConfigsWithWalrus(registry *registries.Registry, noDefaultEndpoint bool,
                             mirrorAddr string, walrusConfig *walrus.WalrusConfig) HostConfigs {

    hosts := getHostConfigs(registry, noDefaultEndpoint, mirrorAddr)

    // Add Walrus pseudo-registry configuration
    if walrusConfig != nil && walrusConfig.Enabled {
        walrusHostConfig := templates.HostConfig{
            Program: version.Program + "-walrus",
            Default: &templates.RegistryEndpoint{
                URL: &url.URL{
                    Scheme: "walrus",
                    Host:   "storage.walrus.network",
                    Path:   "/v1",
                },
                Config: registries.RegistryConfig{
                    Auth: nil, // Handled by Seal authentication
                    TLS:  nil, // HTTPS handled by client
                },
            },
        }
        hosts["walrus.network"] = walrusHostConfig
    }

    return hosts
}

// Enhanced template for Walrus registry endpoint
const WalrusHostsTemplate = `# Generated by {{.Program}}
[host]
  [host."walrus://"]
    capabilities = ["pull"]
    client = [[
      ca_cert = ""
      cert = ""
      key = ""
      skip_verify = false
    ]]
{{range .Endpoints}}
  [host."{{.URL}}"]
    capabilities = ["pull"]
{{if .OverridePath}}
    override_path = true
{{end}}
{{if .Config.Auth}}
    [host."{{.URL}}".header]
      authorization = "{{.Config.Auth.Auth}}"
{{end}}
{{if .Config.TLS}}
    ca = "{{.Config.TLS.CAFile}}"
    cert = "{{.Config.TLS.CertFile}}"
    key = "{{.Config.TLS.KeyFile}}"
    skip_verify = {{.Config.TLS.InsecureSkipVerify}}
{{end}}
{{end}}
`
```

### 3. Code Deployment Flow from Walrus

#### A. Application Code Storage Structure

```go
// NEW: pkg/walrus/deployment.go
type ApplicationBundle struct {
    Metadata *BundleMetadata `json:"metadata"`
    Images   []*ImageRef     `json:"images"`
    Configs  []*ConfigRef    `json:"configs"`
    Secrets  []*SecretRef    `json:"secrets"`
    Code     []*CodeRef      `json:"code"`
}

type BundleMetadata struct {
    Name        string            `json:"name"`
    Version     string            `json:"version"`
    Description string            `json:"description"`
    Labels      map[string]string `json:"labels"`
    Checksum    string            `json:"checksum"`
    CreatedAt   time.Time         `json:"created_at"`
    SealConfig  *SealConfig       `json:"seal_config,omitempty"`
}

type ImageRef struct {
    Name     string  `json:"name"`
    Tag      string  `json:"tag"`
    BlobID   *BlobID `json:"blob_id"`
    Platform string  `json:"platform,omitempty"`
}

type ConfigRef struct {
    Name       string            `json:"name"`
    Type       string            `json:"type"` // configmap, secret
    BlobID     *BlobID          `json:"blob_id"`
    Encrypted  bool             `json:"encrypted"`
    SealKey    string           `json:"seal_key,omitempty"`
    Metadata   map[string]string `json:"metadata"`
}

type CodeRef struct {
    Path       string  `json:"path"`
    BlobID     *BlobID `json:"blob_id"`
    Executable bool    `json:"executable"`
    Compressed bool    `json:"compressed"`
    Encrypted  bool    `json:"encrypted"`
    SealKey    string  `json:"seal_key,omitempty"`
}

type SealConfig struct {
    EncryptionEnabled bool     `json:"encryption_enabled"`
    AllowedWallets   []string `json:"allowed_wallets"`
    RequiredStake    string   `json:"required_stake"`
}
```

#### B. Deployment Manager

```go
type DeploymentManager struct {
    walrusClient   *WalrusClient
    sealAuth       *seal.AuthClient
    containerdAddr string
    workDir        string
    deployments    sync.Map // map[string]*ActiveDeployment
}

type ActiveDeployment struct {
    Bundle     *ApplicationBundle `json:"bundle"`
    Status     DeploymentStatus   `json:"status"`
    StartedAt  time.Time         `json:"started_at"`
    UpdatedAt  time.Time         `json:"updated_at"`
    WorkDir    string            `json:"work_dir"`
    ImagePaths map[string]string  `json:"image_paths"`
    CodePaths  map[string]string  `json:"code_paths"`
}

type DeploymentStatus string

const (
    StatusPending    DeploymentStatus = "pending"
    StatusDownloading DeploymentStatus = "downloading"
    StatusExtracting  DeploymentStatus = "extracting"
    StatusReady      DeploymentStatus = "ready"
    StatusFailed     DeploymentStatus = "failed"
)

func NewDeploymentManager(walrusClient *WalrusClient, sealAuth *seal.AuthClient,
                         containerdAddr, workDir string) *DeploymentManager {
    return &DeploymentManager{
        walrusClient:   walrusClient,
        sealAuth:       sealAuth,
        containerdAddr: containerdAddr,
        workDir:        workDir,
        deployments:    sync.Map{},
    }
}

func (dm *DeploymentManager) DeployBundle(ctx context.Context, bundleBlobID *BlobID) (*ActiveDeployment, error) {
    // 1. Download bundle manifest
    reader, err := dm.walrusClient.DownloadBlob(ctx, bundleBlobID)
    if err != nil {
        return nil, fmt.Errorf("failed to download bundle manifest: %w", err)
    }
    defer reader.Close()

    var bundle ApplicationBundle
    if err := json.NewDecoder(reader).Decode(&bundle); err != nil {
        return nil, fmt.Errorf("failed to parse bundle manifest: %w", err)
    }

    // 2. Validate Seal permissions
    if bundle.Metadata.SealConfig != nil {
        if err := dm.validateSealPermissions(&bundle); err != nil {
            return nil, fmt.Errorf("seal permission validation failed: %w", err)
        }
    }

    // 3. Create deployment workspace
    deploymentID := fmt.Sprintf("%s-%s-%d", bundle.Metadata.Name, bundle.Metadata.Version, time.Now().Unix())
    workDir := filepath.Join(dm.workDir, deploymentID)

    if err := os.MkdirAll(workDir, 0755); err != nil {
        return nil, fmt.Errorf("failed to create work directory: %w", err)
    }

    deployment := &ActiveDeployment{
        Bundle:     &bundle,
        Status:     StatusPending,
        StartedAt:  time.Now(),
        UpdatedAt:  time.Now(),
        WorkDir:    workDir,
        ImagePaths: make(map[string]string),
        CodePaths:  make(map[string]string),
    }

    dm.deployments.Store(deploymentID, deployment)

    // 4. Start async deployment process
    go dm.processDeployment(ctx, deploymentID, deployment)

    return deployment, nil
}

func (dm *DeploymentManager) processDeployment(ctx context.Context, deploymentID string, deployment *ActiveDeployment) {
    deployment.Status = StatusDownloading
    deployment.UpdatedAt = time.Now()

    // 1. Download all images
    if err := dm.downloadImages(ctx, deployment); err != nil {
        logrus.Errorf("Failed to download images for deployment %s: %v", deploymentID, err)
        deployment.Status = StatusFailed
        return
    }

    // 2. Download code and configs
    if err := dm.downloadCode(ctx, deployment); err != nil {
        logrus.Errorf("Failed to download code for deployment %s: %v", deploymentID, err)
        deployment.Status = StatusFailed
        return
    }

    deployment.Status = StatusExtracting

    // 3. Extract and prepare files
    if err := dm.extractDeployment(ctx, deployment); err != nil {
        logrus.Errorf("Failed to extract deployment %s: %v", deploymentID, err)
        deployment.Status = StatusFailed
        return
    }

    deployment.Status = StatusReady
    deployment.UpdatedAt = time.Now()

    logrus.Infof("Deployment %s is ready", deploymentID)
}

func (dm *DeploymentManager) downloadImages(ctx context.Context, deployment *ActiveDeployment) error {
    client, err := containerd.New(dm.containerdAddr)
    if err != nil {
        return err
    }
    defer client.Close()

    for _, imageRef := range deployment.Bundle.Images {
        imageName := fmt.Sprintf("%s:%s", imageRef.Name, imageRef.Tag)

        // Download blob
        reader, err := dm.walrusClient.DownloadBlob(ctx, imageRef.BlobID)
        if err != nil {
            return fmt.Errorf("failed to download image blob %s: %w", imageRef.BlobID.Hash, err)
        }

        // Import into containerd
        importResults, err := client.Import(ctx, reader,
            containerd.WithAllPlatforms(true),
            containerd.WithImageRefTranslator(func(ref string) string {
                return imageName
            }))
        reader.Close()

        if err != nil {
            return fmt.Errorf("failed to import image %s: %w", imageName, err)
        }

        if len(importResults) > 0 {
            deployment.ImagePaths[imageName] = importResults[0].Name
        }
    }

    return nil
}

func (dm *DeploymentManager) downloadCode(ctx context.Context, deployment *ActiveDeployment) error {
    for _, codeRef := range deployment.Bundle.Code {
        // Download code blob
        reader, err := dm.walrusClient.DownloadBlob(ctx, codeRef.BlobID)
        if err != nil {
            return fmt.Errorf("failed to download code blob %s: %w", codeRef.BlobID.Hash, err)
        }

        // Decrypt if necessary
        if codeRef.Encrypted && codeRef.SealKey != "" {
            decryptedReader, err := dm.sealAuth.DecryptStream(reader, codeRef.SealKey)
            if err != nil {
                reader.Close()
                return fmt.Errorf("failed to decrypt code: %w", err)
            }
            reader = decryptedReader
        }

        // Save to work directory
        codePath := filepath.Join(deployment.WorkDir, codeRef.Path)
        if err := os.MkdirAll(filepath.Dir(codePath), 0755); err != nil {
            reader.Close()
            return fmt.Errorf("failed to create code directory: %w", err)
        }

        file, err := os.Create(codePath)
        if err != nil {
            reader.Close()
            return fmt.Errorf("failed to create code file: %w", err)
        }

        if codeRef.Compressed {
            gzReader, err := gzip.NewReader(reader)
            if err != nil {
                reader.Close()
                file.Close()
                return fmt.Errorf("failed to create gzip reader: %w", err)
            }
            _, err = io.Copy(file, gzReader)
            gzReader.Close()
        } else {
            _, err = io.Copy(file, reader)
        }

        reader.Close()
        file.Close()

        if err != nil {
            return fmt.Errorf("failed to write code file: %w", err)
        }

        if codeRef.Executable {
            if err := os.Chmod(codePath, 0755); err != nil {
                return fmt.Errorf("failed to set executable permissions: %w", err)
            }
        }

        deployment.CodePaths[codeRef.Path] = codePath
    }

    return nil
}
```

### 4. Caching Strategy for Walrus Blobs

#### A. Multi-Level Cache Architecture

```go
// NEW: pkg/walrus/cache.go
type BlobCache struct {
    l1Cache     *MemoryCache    // In-memory LRU cache
    l2Cache     *DiskCache      // Persistent disk cache
    l3Cache     *NetworkCache   // Distributed cache across nodes
    maxSize     int64
    compression bool
    encryption  bool
    sealAuth    *seal.AuthClient
    metrics     *CacheMetrics
}

type CacheMetrics struct {
    L1Hits      int64 `json:"l1_hits"`
    L2Hits      int64 `json:"l2_hits"`
    L3Hits      int64 `json:"l3_hits"`
    Misses      int64 `json:"misses"`
    Evictions   int64 `json:"evictions"`
    TotalSize   int64 `json:"total_size"`
    LastCleanup time.Time `json:"last_cleanup"`
}

func NewBlobCache(config *BlobCacheConfig) (*BlobCache, error) {
    // 1. Initialize L1 memory cache (LRU)
    l1Cache, err := NewMemoryCache(&MemoryCacheConfig{
        MaxEntries: 1000,
        MaxSize:    config.MaxSize / 10, // 10% of total cache for memory
        TTL:        30 * time.Minute,
    })
    if err != nil {
        return nil, err
    }

    // 2. Initialize L2 disk cache
    l2Cache, err := NewDiskCache(&DiskCacheConfig{
        CacheDir:     config.CacheDir,
        MaxSize:      config.MaxSize,
        Compression:  config.Compression,
        ShardCount:   256, // Distribute across shards to avoid filesystem limits
    })
    if err != nil {
        return nil, err
    }

    // 3. Initialize L3 network cache (peer-to-peer)
    l3Cache, err := NewNetworkCache(&NetworkCacheConfig{
        PeerNodes:    config.PeerNodes,
        DownloadTimeout: 30 * time.Second,
        MaxConcurrency: 5,
    })
    if err != nil {
        return nil, err
    }

    return &BlobCache{
        l1Cache:     l1Cache,
        l2Cache:     l2Cache,
        l3Cache:     l3Cache,
        maxSize:     config.MaxSize,
        compression: config.Compression,
        encryption:  config.Encryption,
        sealAuth:    config.SealAuth,
        metrics:     &CacheMetrics{},
    }, nil
}

func (bc *BlobCache) Get(blobHash string) (io.ReadCloser, error) {
    start := time.Now()
    defer func() {
        logrus.Debugf("Cache lookup for %s took %v", blobHash[:8], time.Since(start))
    }()

    // 1. Try L1 memory cache first
    if reader, err := bc.l1Cache.Get(blobHash); err == nil {
        atomic.AddInt64(&bc.metrics.L1Hits, 1)
        logrus.Debugf("L1 cache hit for blob %s", blobHash[:8])
        return reader, nil
    }

    // 2. Try L2 disk cache
    if reader, err := bc.l2Cache.Get(blobHash); err == nil {
        atomic.AddInt64(&bc.metrics.L2Hits, 1)
        logrus.Debugf("L2 cache hit for blob %s", blobHash[:8])

        // Promote to L1 cache
        go bc.promoteToL1(blobHash, reader)

        return reader, nil
    }

    // 3. Try L3 network cache (peer nodes)
    if reader, err := bc.l3Cache.Get(blobHash); err == nil {
        atomic.AddInt64(&bc.metrics.L3Hits, 1)
        logrus.Debugf("L3 cache hit for blob %s", blobHash[:8])

        // Store in local caches
        go bc.storeInLocalCaches(blobHash, reader)

        return reader, nil
    }

    // 4. Cache miss
    atomic.AddInt64(&bc.metrics.Misses, 1)
    return nil, fmt.Errorf("blob %s not found in cache", blobHash)
}

func (bc *BlobCache) Store(blobHash string, reader io.Reader) io.ReadCloser {
    // Create tee readers for parallel storage in multiple cache levels
    l1Buffer := &bytes.Buffer{}
    l2Buffer := &bytes.Buffer{}

    // Limit memory buffer size
    limitedL1 := &io.LimitedReader{R: reader, N: 50 * 1024 * 1024} // 50MB limit for L1
    teeReader := io.TeeReader(limitedL1, l1Buffer)

    mainReader := io.TeeReader(teeReader, l2Buffer)

    // Store in L1 if small enough
    go func() {
        if l1Buffer.Len() > 0 && limitedL1.N > 0 {
            bc.l1Cache.Store(blobHash, io.NopCloser(bytes.NewReader(l1Buffer.Bytes())))
        }
    }()

    // Store in L2 disk cache
    go func() {
        if l2Buffer.Len() > 0 {
            bc.l2Cache.Store(blobHash, io.NopCloser(bytes.NewReader(l2Buffer.Bytes())))
        }
    }()

    return io.NopCloser(mainReader)
}
```

#### B. Disk Cache Implementation

```go
type DiskCache struct {
    cacheDir    string
    maxSize     int64
    currentSize int64
    shardCount  int
    compression bool
    indexDB     *bolt.DB
    sizeMutex   sync.RWMutex
    gcInterval  time.Duration
    lastGC      time.Time
}

type CacheEntry struct {
    Hash        string    `json:"hash"`
    Size        int64     `json:"size"`
    AccessTime  time.Time `json:"access_time"`
    CreateTime  time.Time `json:"create_time"`
    Compressed  bool      `json:"compressed"`
    ShardIndex  int       `json:"shard_index"`
    FilePath    string    `json:"file_path"`
}

func NewDiskCache(config *DiskCacheConfig) (*DiskCache, error) {
    if err := os.MkdirAll(config.CacheDir, 0755); err != nil {
        return nil, err
    }

    // Open metadata database
    dbPath := filepath.Join(config.CacheDir, "cache.db")
    db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 5 * time.Second})
    if err != nil {
        return nil, err
    }

    // Create buckets
    if err := db.Update(func(tx *bolt.Tx) error {
        _, err := tx.CreateBucketIfNotExists([]byte("entries"))
        return err
    }); err != nil {
        return nil, err
    }

    cache := &DiskCache{
        cacheDir:    config.CacheDir,
        maxSize:     config.MaxSize,
        shardCount:  config.ShardCount,
        compression: config.Compression,
        indexDB:     db,
        gcInterval:  30 * time.Minute,
    }

    // Calculate current cache size
    cache.calculateCurrentSize()

    // Start garbage collection routine
    go cache.gcRoutine()

    return cache, nil
}

func (dc *DiskCache) Get(blobHash string) (io.ReadCloser, error) {
    // 1. Look up entry in index
    var entry CacheEntry
    err := dc.indexDB.View(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte("entries"))
        data := bucket.Get([]byte(blobHash))
        if data == nil {
            return fmt.Errorf("entry not found")
        }
        return json.Unmarshal(data, &entry)
    })
    if err != nil {
        return nil, err
    }

    // 2. Check if file exists
    if _, err := os.Stat(entry.FilePath); os.IsNotExist(err) {
        // Remove stale entry
        dc.removeEntry(blobHash)
        return nil, fmt.Errorf("cached file not found")
    }

    // 3. Open file
    file, err := os.Open(entry.FilePath)
    if err != nil {
        return nil, err
    }

    // 4. Update access time
    go dc.updateAccessTime(blobHash)

    // 5. Return decompressed reader if necessary
    if entry.Compressed {
        gzReader, err := gzip.NewReader(file)
        if err != nil {
            file.Close()
            return nil, err
        }
        return &compoundReadCloser{gzReader, file}, nil
    }

    return file, nil
}

func (dc *DiskCache) Store(blobHash string, reader io.Reader) error {
    // 1. Determine shard
    shardIndex := dc.getShardIndex(blobHash)
    shardDir := filepath.Join(dc.cacheDir, fmt.Sprintf("shard_%03d", shardIndex))

    if err := os.MkdirAll(shardDir, 0755); err != nil {
        return err
    }

    // 2. Create temporary file
    tempFile, err := os.CreateTemp(shardDir, "blob_*.tmp")
    if err != nil {
        return err
    }
    defer os.Remove(tempFile.Name())

    // 3. Write data (with optional compression)
    var size int64
    if dc.compression {
        gzWriter := gzip.NewWriter(tempFile)
        size, err = io.Copy(gzWriter, reader)
        gzWriter.Close()
    } else {
        size, err = io.Copy(tempFile, reader)
    }

    if err != nil {
        tempFile.Close()
        return err
    }

    tempFile.Close()

    // 4. Check if we have space
    dc.sizeMutex.Lock()
    if dc.currentSize+size > dc.maxSize {
        dc.sizeMutex.Unlock()
        // Trigger garbage collection
        dc.performGC()
        dc.sizeMutex.Lock()
        if dc.currentSize+size > dc.maxSize {
            dc.sizeMutex.Unlock()
            os.Remove(tempFile.Name())
            return fmt.Errorf("insufficient cache space")
        }
    }
    dc.currentSize += size
    dc.sizeMutex.Unlock()

    // 5. Move to final location
    finalPath := filepath.Join(shardDir, blobHash)
    if err := os.Rename(tempFile.Name(), finalPath); err != nil {
        return err
    }

    // 6. Update index
    entry := CacheEntry{
        Hash:       blobHash,
        Size:       size,
        AccessTime: time.Now(),
        CreateTime: time.Now(),
        Compressed: dc.compression,
        ShardIndex: shardIndex,
        FilePath:   finalPath,
    }

    return dc.indexDB.Update(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte("entries"))
        data, err := json.Marshal(entry)
        if err != nil {
            return err
        }
        return bucket.Put([]byte(blobHash), data)
    })
}

func (dc *DiskCache) getShardIndex(blobHash string) int {
    hasher := fnv.New32a()
    hasher.Write([]byte(blobHash))
    return int(hasher.Sum32()) % dc.shardCount
}

type compoundReadCloser struct {
    io.Reader
    io.Closer
}

func (c *compoundReadCloser) Close() error {
    return c.Closer.Close()
}
```

### 5. Secret Management with Seal Integration

#### A. Encrypted Secret Storage

```go
// NEW: pkg/walrus/secrets.go
type SecretManager struct {
    walrusClient *WalrusClient
    sealAuth     *seal.AuthClient
    secretCache  sync.Map // map[string]*CachedSecret
    keyDerivation *KeyDerivation
}

type CachedSecret struct {
    Data      []byte    `json:"data"`
    ExpiresAt time.Time `json:"expires_at"`
    Checksum  string    `json:"checksum"`
}

type EncryptedSecret struct {
    EncryptedData   []byte            `json:"encrypted_data"`
    KeyID          string            `json:"key_id"`
    AuthorWallet   string            `json:"author_wallet"`
    AllowedWallets []string          `json:"allowed_wallets"`
    RequiredStake  string            `json:"required_stake"`
    Metadata       map[string]string `json:"metadata"`
    CreatedAt      time.Time         `json:"created_at"`
    ExpiresAt      time.Time         `json:"expires_at"`
}

type KeyDerivation struct {
    sealAuth *seal.AuthClient
    keyCache sync.Map // map[string]*DerivedKey
}

type DerivedKey struct {
    Key       []byte    `json:"key"`
    CreatedAt time.Time `json:"created_at"`
    ExpiresAt time.Time `json:"expires_at"`
}

func NewSecretManager(walrusClient *WalrusClient, sealAuth *seal.AuthClient) *SecretManager {
    return &SecretManager{
        walrusClient: walrusClient,
        sealAuth:     sealAuth,
        secretCache:  sync.Map{},
        keyDerivation: &KeyDerivation{
            sealAuth: sealAuth,
            keyCache: sync.Map{},
        },
    }
}

func (sm *SecretManager) StoreSecret(ctx context.Context, secretData []byte,
                                   allowedWallets []string, requiredStake string,
                                   metadata map[string]string, ttl time.Duration) (*BlobID, error) {

    // 1. Generate encryption key using Seal wallet
    keyID := generateKeyID()
    encKey, err := sm.keyDerivation.DeriveKey(keyID, ttl)
    if err != nil {
        return nil, fmt.Errorf("failed to derive encryption key: %w", err)
    }

    // 2. Encrypt secret data
    encryptedData, err := sm.encryptData(secretData, encKey.Key)
    if err != nil {
        return nil, fmt.Errorf("failed to encrypt secret: %w", err)
    }

    // 3. Create encrypted secret structure
    encryptedSecret := &EncryptedSecret{
        EncryptedData:  encryptedData,
        KeyID:         keyID,
        AuthorWallet:  sm.sealAuth.GetWalletAddress(),
        AllowedWallets: allowedWallets,
        RequiredStake: requiredStake,
        Metadata:      metadata,
        CreatedAt:     time.Now(),
        ExpiresAt:     time.Now().Add(ttl),
    }

    // 4. Serialize and upload to Walrus
    secretBytes, err := json.Marshal(encryptedSecret)
    if err != nil {
        return nil, fmt.Errorf("failed to serialize encrypted secret: %w", err)
    }

    blobID, err := sm.walrusClient.StoreBlob(ctx, bytes.NewReader(secretBytes))
    if err != nil {
        return nil, fmt.Errorf("failed to store secret in Walrus: %w", err)
    }

    return blobID, nil
}

func (sm *SecretManager) GetSecret(ctx context.Context, blobID *BlobID) ([]byte, error) {
    // 1. Check cache first
    cacheKey := blobID.Hash
    if cached, exists := sm.secretCache.Load(cacheKey); exists {
        cachedSecret := cached.(*CachedSecret)
        if time.Now().Before(cachedSecret.ExpiresAt) {
            return cachedSecret.Data, nil
        }
        sm.secretCache.Delete(cacheKey)
    }

    // 2. Download encrypted secret from Walrus
    reader, err := sm.walrusClient.DownloadBlob(ctx, blobID)
    if err != nil {
        return nil, fmt.Errorf("failed to download secret: %w", err)
    }
    defer reader.Close()

    var encryptedSecret EncryptedSecret
    if err := json.NewDecoder(reader).Decode(&encryptedSecret); err != nil {
        return nil, fmt.Errorf("failed to parse encrypted secret: %w", err)
    }

    // 3. Validate access permissions
    if err := sm.validateAccess(&encryptedSecret); err != nil {
        return nil, fmt.Errorf("access denied: %w", err)
    }

    // 4. Check expiration
    if time.Now().After(encryptedSecret.ExpiresAt) {
        return nil, fmt.Errorf("secret has expired")
    }

    // 5. Derive decryption key
    derivedKey, err := sm.keyDerivation.GetKey(encryptedSecret.KeyID)
    if err != nil {
        return nil, fmt.Errorf("failed to derive decryption key: %w", err)
    }

    // 6. Decrypt secret data
    decryptedData, err := sm.decryptData(encryptedSecret.EncryptedData, derivedKey.Key)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt secret: %w", err)
    }

    // 7. Cache decrypted secret
    cachedSecret := &CachedSecret{
        Data:      decryptedData,
        ExpiresAt: encryptedSecret.ExpiresAt,
        Checksum:  sha256Hash(decryptedData),
    }
    sm.secretCache.Store(cacheKey, cachedSecret)

    return decryptedData, nil
}

func (sm *SecretManager) validateAccess(secret *EncryptedSecret) error {
    currentWallet := sm.sealAuth.GetWalletAddress()

    // 1. Check if current wallet is in allowed list
    allowed := false
    for _, wallet := range secret.AllowedWallets {
        if wallet == currentWallet || wallet == "*" {
            allowed = true
            break
        }
    }

    if !allowed {
        return fmt.Errorf("wallet %s not in allowed list", currentWallet)
    }

    // 2. Check stake requirement if specified
    if secret.RequiredStake != "" {
        hasRequiredStake, err := sm.sealAuth.ValidateStakeRequirement(secret.RequiredStake)
        if err != nil {
            return fmt.Errorf("failed to validate stake: %w", err)
        }
        if !hasRequiredStake {
            return fmt.Errorf("insufficient stake for secret access")
        }
    }

    return nil
}

func (kd *KeyDerivation) DeriveKey(keyID string, ttl time.Duration) (*DerivedKey, error) {
    // 1. Check cache first
    if cached, exists := kd.keyCache.Load(keyID); exists {
        derivedKey := cached.(*DerivedKey)
        if time.Now().Before(derivedKey.ExpiresAt) {
            return derivedKey, nil
        }
        kd.keyCache.Delete(keyID)
    }

    // 2. Derive key using Seal wallet signature
    message := fmt.Sprintf("derive-key:%s:%d", keyID, time.Now().Unix())
    signature, err := kd.sealAuth.SignMessage(message)
    if err != nil {
        return nil, err
    }

    // 3. Create deterministic key from signature
    hasher := sha256.New()
    hasher.Write([]byte(signature))
    hasher.Write([]byte(keyID))
    key := hasher.Sum(nil)

    derivedKey := &DerivedKey{
        Key:       key,
        CreatedAt: time.Now(),
        ExpiresAt: time.Now().Add(ttl),
    }

    // 4. Cache the derived key
    kd.keyCache.Store(keyID, derivedKey)

    return derivedKey, nil
}

func (sm *SecretManager) encryptData(data, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    nonce := make([]byte, gcm.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return nil, err
    }

    encrypted := gcm.Seal(nonce, nonce, data, nil)
    return encrypted, nil
}

func (sm *SecretManager) decryptData(encryptedData, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    if len(encryptedData) < gcm.NonceSize() {
        return nil, fmt.Errorf("encrypted data too short")
    }

    nonce := encryptedData[:gcm.NonceSize()]
    ciphertext := encryptedData[gcm.NonceSize():]

    decrypted, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, err
    }

    return decrypted, nil
}

func generateKeyID() string {
    randomBytes := make([]byte, 16)
    rand.Read(randomBytes)
    return hex.EncodeToString(randomBytes)
}

func sha256Hash(data []byte) string {
    hasher := sha256.New()
    hasher.Write(data)
    return hex.EncodeToString(hasher.Sum(nil))
}
```

### 6. Performance Optimization Plan

#### A. Parallel Download Architecture

```go
// NEW: pkg/walrus/download.go
type DownloadManager struct {
    maxConcurrency   int
    chunkSize        int64
    retryAttempts    int
    workers          chan *DownloadWorker
    activeDownloads  sync.Map
    bandwidthLimiter *rate.Limiter
    metrics          *DownloadMetrics
}

type DownloadWorker struct {
    id       int
    client   *http.Client
    metrics  *WorkerMetrics
    active   bool
}

type DownloadTask struct {
    BlobID      *BlobID
    Location    *BlobLocation
    Chunks      []*ChunkInfo
    Progress    *DownloadProgress
    Callbacks   DownloadCallbacks
    CreatedAt   time.Time
}

type ChunkInfo struct {
    Index     int    `json:"index"`
    Start     int64  `json:"start"`
    End       int64  `json:"end"`
    Size      int64  `json:"size"`
    Hash      string `json:"hash"`
    URL       string `json:"url"`
    Downloaded bool  `json:"downloaded"`
    Attempts  int    `json:"attempts"`
}

type DownloadProgress struct {
    TotalSize      int64     `json:"total_size"`
    DownloadedSize int64     `json:"downloaded_size"`
    ChunksTotal    int       `json:"chunks_total"`
    ChunksComplete int       `json:"chunks_complete"`
    StartTime      time.Time `json:"start_time"`
    EstimatedTime  time.Duration `json:"estimated_time"`
    Speed          int64     `json:"speed"` // bytes per second
}

type DownloadCallbacks struct {
    OnProgress func(*DownloadProgress)
    OnComplete func(io.Reader, error)
    OnChunkComplete func(int, []byte)
}

func NewDownloadManager(config *DownloadConfig) *DownloadManager {
    workers := make(chan *DownloadWorker, config.ParallelDownloads)

    // Create worker pool
    for i := 0; i < config.ParallelDownloads; i++ {
        worker := &DownloadWorker{
            id: i,
            client: &http.Client{
                Timeout: 30 * time.Second,
                Transport: &http.Transport{
                    MaxIdleConns:        100,
                    MaxIdleConnsPerHost: 10,
                },
            },
            metrics: &WorkerMetrics{},
        }
        workers <- worker
    }

    return &DownloadManager{
        maxConcurrency:   config.ParallelDownloads,
        chunkSize:        config.ChunkSize,
        retryAttempts:    config.RetryAttempts,
        workers:          workers,
        activeDownloads:  sync.Map{},
        bandwidthLimiter: rate.NewLimiter(rate.Limit(config.MaxBandwidth), int(config.MaxBandwidth/10)),
        metrics:          &DownloadMetrics{},
    }
}

func (dm *DownloadManager) Download(ctx context.Context, location *BlobLocation) (io.ReadCloser, error) {
    // 1. Create download task
    task := &DownloadTask{
        BlobID:    location.BlobID,
        Location:  location,
        CreatedAt: time.Now(),
        Progress: &DownloadProgress{
            TotalSize: location.BlobID.Size,
            StartTime: time.Now(),
        },
    }

    // 2. Determine if we should use chunked download
    if location.BlobID.Size > dm.chunkSize {
        return dm.downloadChunked(ctx, task)
    } else {
        return dm.downloadSingle(ctx, task)
    }
}

func (dm *DownloadManager) downloadChunked(ctx context.Context, task *DownloadTask) (io.ReadCloser, error) {
    // 1. Calculate chunks
    chunks := dm.calculateChunks(task.Location.BlobID.Size)
    task.Chunks = chunks
    task.Progress.ChunksTotal = len(chunks)

    // 2. Create result buffer
    resultBuffer := make([][]byte, len(chunks))
    errorChan := make(chan error, len(chunks))
    completeChan := make(chan int, len(chunks))

    // 3. Start chunk downloads
    for i, chunk := range chunks {
        go dm.downloadChunk(ctx, task, i, chunk, resultBuffer, completeChan, errorChan)
    }

    // 4. Wait for completion
    completed := 0
    var lastErr error

    for completed < len(chunks) {
        select {
        case chunkIndex := <-completeChan:
            completed++
            task.Progress.ChunksComplete = completed

            // Calculate progress
            downloadedSize := int64(0)
            for _, data := range resultBuffer {
                if data != nil {
                    downloadedSize += int64(len(data))
                }
            }
            task.Progress.DownloadedSize = downloadedSize

            logrus.Debugf("Chunk %d completed, progress: %d/%d", chunkIndex, completed, len(chunks))

        case err := <-errorChan:
            if err != nil {
                lastErr = err
                logrus.Errorf("Chunk download error: %v", err)
            }

        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }

    if lastErr != nil && completed < len(chunks) {
        return nil, fmt.Errorf("chunked download failed: %w", lastErr)
    }

    // 5. Combine chunks
    var totalSize int64
    for _, data := range resultBuffer {
        totalSize += int64(len(data))
    }

    combinedData := make([]byte, 0, totalSize)
    for _, data := range resultBuffer {
        combinedData = append(combinedData, data...)
    }

    return io.NopCloser(bytes.NewReader(combinedData)), nil
}

func (dm *DownloadManager) downloadChunk(ctx context.Context, task *DownloadTask,
                                       chunkIndex int, chunk *ChunkInfo,
                                       resultBuffer [][]byte,
                                       completeChan chan int, errorChan chan error) {

    // Get worker from pool
    worker := <-dm.workers
    defer func() { dm.workers <- worker }()

    for attempt := 0; attempt < dm.retryAttempts; attempt++ {
        // Rate limiting
        if err := dm.bandwidthLimiter.WaitN(ctx, int(chunk.Size)); err != nil {
            errorChan <- err
            return
        }

        // Create range request
        req, err := http.NewRequestWithContext(ctx, "GET", chunk.URL, nil)
        if err != nil {
            errorChan <- err
            return
        }

        req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End))

        // Execute request
        resp, err := worker.client.Do(req)
        if err != nil {
            if attempt == dm.retryAttempts-1 {
                errorChan <- err
                return
            }
            time.Sleep(time.Duration(attempt+1) * time.Second)
            continue
        }

        if resp.StatusCode != http.StatusPartialContent {
            resp.Body.Close()
            if attempt == dm.retryAttempts-1 {
                errorChan <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
                return
            }
            time.Sleep(time.Duration(attempt+1) * time.Second)
            continue
        }

        // Read chunk data
        chunkData, err := io.ReadAll(resp.Body)
        resp.Body.Close()

        if err != nil {
            if attempt == dm.retryAttempts-1 {
                errorChan <- err
                return
            }
            time.Sleep(time.Duration(attempt+1) * time.Second)
            continue
        }

        // Verify chunk hash if available
        if chunk.Hash != "" {
            hasher := sha256.New()
            hasher.Write(chunkData)
            actualHash := hex.EncodeToString(hasher.Sum(nil))
            if actualHash != chunk.Hash {
                if attempt == dm.retryAttempts-1 {
                    errorChan <- fmt.Errorf("chunk hash mismatch")
                    return
                }
                time.Sleep(time.Duration(attempt+1) * time.Second)
                continue
            }
        }

        // Store result
        resultBuffer[chunkIndex] = chunkData
        completeChan <- chunkIndex
        return
    }
}

func (dm *DownloadManager) calculateChunks(totalSize int64) []*ChunkInfo {
    if totalSize <= dm.chunkSize {
        return []*ChunkInfo{{
            Index: 0,
            Start: 0,
            End:   totalSize - 1,
            Size:  totalSize,
        }}
    }

    chunkCount := int((totalSize + dm.chunkSize - 1) / dm.chunkSize)
    chunks := make([]*ChunkInfo, chunkCount)

    for i := 0; i < chunkCount; i++ {
        start := int64(i) * dm.chunkSize
        end := start + dm.chunkSize - 1
        if end >= totalSize {
            end = totalSize - 1
        }

        chunks[i] = &ChunkInfo{
            Index: i,
            Start: start,
            End:   end,
            Size:  end - start + 1,
        }
    }

    return chunks
}
```

#### B. Prefetching and Predictive Caching

```go
type PrefetchManager struct {
    walrusClient    *WalrusClient
    cache          *BlobCache
    predictionModel *PredictionModel
    prefetchQueue   chan *PrefetchTask
    workers         []*PrefetchWorker
    metrics         *PrefetchMetrics
}

type PredictionModel struct {
    imagePatterns   sync.Map // map[string]*AccessPattern
    deploymentChains sync.Map // map[string][]string
    timeBasedAccess sync.Map // map[string]*TimePattern
    learningEnabled bool
    modelPath      string
}

type AccessPattern struct {
    BlobID        string    `json:"blob_id"`
    AccessCount   int64     `json:"access_count"`
    LastAccess    time.Time `json:"last_access"`
    RelatedBlobs  []string  `json:"related_blobs"`
    Probability   float64   `json:"probability"`
}

func NewPrefetchManager(walrusClient *WalrusClient, cache *BlobCache) *PrefetchManager {
    pm := &PrefetchManager{
        walrusClient:    walrusClient,
        cache:          cache,
        predictionModel: NewPredictionModel(),
        prefetchQueue:   make(chan *PrefetchTask, 1000),
        metrics:        &PrefetchMetrics{},
    }

    // Start prefetch workers
    for i := 0; i < 3; i++ {
        worker := &PrefetchWorker{
            id: i,
            manager: pm,
        }
        pm.workers = append(pm.workers, worker)
        go worker.run()
    }

    return pm
}

func (pm *PrefetchManager) OnBlobAccess(blobID string, metadata map[string]interface{}) {
    // 1. Update access patterns
    pm.predictionModel.RecordAccess(blobID, metadata)

    // 2. Predict related blobs
    relatedBlobs := pm.predictionModel.PredictRelatedBlobs(blobID)

    // 3. Queue prefetch tasks
    for _, relatedBlobID := range relatedBlobs {
        task := &PrefetchTask{
            BlobID:   relatedBlobID,
            Priority: PriorityMedium,
            Source:   "prediction",
            CreatedAt: time.Now(),
        }

        select {
        case pm.prefetchQueue <- task:
        default:
            // Queue full, drop low priority tasks
        }
    }
}

func (pm *PredictionModel) PredictRelatedBlobs(blobID string) []string {
    // 1. Check deployment chains
    if chain, exists := pm.deploymentChains.Load(blobID); exists {
        return chain.([]string)
    }

    // 2. Check access patterns
    if pattern, exists := pm.imagePatterns.Load(blobID); exists {
        accessPattern := pattern.(*AccessPattern)
        return accessPattern.RelatedBlobs
    }

    // 3. Time-based prediction
    if timePattern, exists := pm.timeBasedAccess.Load(blobID); exists {
        pattern := timePattern.(*TimePattern)
        if pattern.ShouldPrefetch(time.Now()) {
            return pattern.NextBlobs
        }
    }

    return nil
}
```

## Integration Points Summary

### Key Modification Points

1. **pkg/agent/containerd/containerd.go:326** - Replace `prePullImages` with Walrus-aware version
2. **pkg/agent/containerd/config.go:145** - Enhance `getHostConfigs` for Walrus endpoints
3. **pkg/agent/containerd/watcher.go:166** - Add Walrus blob support to `preloadFile`
4. **pkg/daemons/config/types.go** - Add WalrusConfig to Agent configuration

### Configuration Integration

```yaml
# Example K3s configuration with Walrus
daas:
  enabled: true
  walrus:
    enabled: true
    aggregator_urls:
      - "https://aggregator1.walrus.network"
      - "https://aggregator2.walrus.network"
    publisher_nodes:
      - "https://publisher1.walrus.network"
      - "https://publisher2.walrus.network"
    cache_dir: "/var/lib/walrus/cache"
    max_cache_size: 10737418240  # 10GB
    parallel_downloads: 8
    chunk_size: 1048576  # 1MB
    seal_encryption: true
    compression_enabled: true
```

### Performance Optimizations

1. **Multi-level Caching**: L1 (memory) → L2 (disk) → L3 (network peers)
2. **Parallel Downloads**: Chunked downloads with configurable concurrency
3. **Predictive Prefetching**: ML-based prediction of related blobs
4. **Compression**: Optional blob compression in cache
5. **Rate Limiting**: Bandwidth control for downloads
6. **Circuit Breakers**: Resilience against network failures

This comprehensive Walrus integration transforms K3s into a fully decentralized platform while maintaining compatibility with existing container workflows and adding advanced features like encrypted secret management and intelligent caching.