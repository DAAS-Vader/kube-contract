# K3s Authentication and Security Analysis

## Overview
This document analyzes K3s authentication mechanisms, security policies, and certificate management to identify integration points for Seal identity-based authentication and Sui wallet integration.

## Current Authentication Architecture

### 1. Node Password System (`pkg/nodepassword/nodepassword.go`)

#### Purpose
Provides secure password-based authentication for node registration and ongoing communication.

#### Implementation Details
```go
// Core components
var Hasher = hash.NewSCrypt()  // SCrypt hashing algorithm

// Password secret naming convention
func getSecretName(nodeName string) string {
    return strings.ToLower(nodeName + ".node-password." + version.Program)
}
```

#### Key Functions

**A. Password Generation and Storage**
```go
func Ensure(secretClient coreclient.SecretController, nodeName, pass string) error {
    // 1. Verify existing password hash
    err := verifyHash(secretClient, nodeName, pass)

    // 2. If not found, create new password hash
    if apierrors.IsNotFound(err) {
        hash, err = Hasher.CreateHash(pass)  // SCrypt hashing
        // Store in Kubernetes Secret with immutable flag
        secretClient.Create(&v1.Secret{
            ObjectMeta: metav1.ObjectMeta{
                Name:      getSecretName(nodeName),
                Namespace: metav1.NamespaceSystem,
            },
            Immutable: ptr.To(true),
            Data:      map[string][]byte{"hash": []byte(hash)},
        })
    }
}
```

**B. Password Verification**
```go
func verifyHash(secretClient coreclient.SecretController, nodeName, pass string) error {
    // 1. Retrieve password secret from Kubernetes
    secret, err := secretClient.Cache().Get(metav1.NamespaceSystem, name)

    // 2. Verify password against stored SCrypt hash
    if hash, ok := secret.Data["hash"]; ok {
        return Hasher.VerifyHash(string(hash), pass)
    }
}
```

**Security Features:**
- **SCrypt hashing**: Computationally expensive, memory-hard
- **Immutable secrets**: Cannot be modified after creation
- **Kubernetes-native storage**: Leverages cluster RBAC
- **Per-node isolation**: Each node has unique password

### 2. Token Handling System (`pkg/agent/config/config.go`)

#### Token Generation Process
```go
func ensureNodePassword(nodePasswordFile string) (string, error) {
    // 1. Check for existing password file
    if _, err := os.Stat(nodePasswordFile); err == nil {
        password, err := os.ReadFile(nodePasswordFile)
        return strings.TrimSpace(string(password)), err
    }

    // 2. Generate new random password (16 bytes -> 32 hex chars)
    password := make([]byte, 16, 16)
    _, err := cryptorand.Read(password)
    nodePassword := hex.EncodeToString(password)

    // 3. Store with secure permissions (0600)
    err = os.WriteFile(nodePasswordFile, []byte(nodePassword+"\n"), 0600)

    // 4. Configure ACLs for additional security
    return nodePassword, configureACL(nodePasswordFile)
}
```

#### Token-to-Certificate Flow
```go
// Agent requests certificates using token
func getKubeletClientCert(certFile, keyFile, nodeName string, nodeIPs []net.IP,
                          nodePasswordFile string, info *clientaccess.Info) error {
    // 1. Generate CSR with local private key
    csr, err := getCSRBytes(keyFile)

    // 2. Submit CSR to server with node credentials
    body, err := Request("/v1-"+version.Program+"/"+basename, info,
                        getNodeNamedCrt(nodeName, nodeIPs, nodePasswordFile, csr))

    // 3. Server signs CSR or provides new cert+key pair
    certBytes, keyBytes := splitCertKeyPEM(body)

    // 4. Store certificates for future use
    os.WriteFile(certFile, certBytes, 0600)
    if len(keyBytes) > 0 {
        os.WriteFile(keyFile, keyBytes, 0600)
    }
}
```

### 3. Certificate Management (`pkg/server/auth/auth.go`)

#### Authentication Middleware System
```go
// Role-based authentication middleware
func HasRole(serverConfig *config.Control, roles ...string) mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
            doAuth(roles, serverConfig, next, rw, req)
        })
    }
}
```

#### Core Authentication Process
```go
func doAuth(roles []string, serverConfig *config.Control, next http.Handler,
           rw http.ResponseWriter, req *http.Request) {
    // 1. Validate server configuration
    if serverConfig.Runtime.Authenticator == nil {
        util.SendError(errors.New("not authorized"), rw, req, http.StatusUnauthorized)
        return
    }

    // 2. Authenticate request using configured authenticator
    resp, ok, err := serverConfig.Runtime.Authenticator.AuthenticateRequest(req)

    // 3. Check if user has required roles
    if !ok || !hasRole(roles, resp.User.GetGroups()) {
        util.SendError(errors.New("forbidden"), rw, req, http.StatusForbidden)
        return
    }

    // 4. Add user to request context and continue
    ctx := apirequest.WithUser(req.Context(), resp.User)
    req = req.WithContext(ctx)
    next.ServeHTTP(rw, req)
}
```

#### Delegated Authentication (Kubernetes-native)
```go
func Delegated(clientCA, kubeConfig string, config *server.Config) mux.MiddlewareFunc {
    // 1. Configure client certificate authentication
    authn.ClientCert = options.ClientCertAuthenticationOptions{
        ClientCA: clientCA,
    }

    // 2. Configure remote kubeconfig for authentication
    authn.RemoteKubeConfigFile = kubeConfig

    // 3. Set up authorization with SubjectAccessReview
    authz.RemoteKubeConfigFile = kubeConfig

    // 4. Create middleware chain
    return func(handler http.Handler) http.Handler {
        handler = genericapifilters.WithAuthorization(handler, config.Authorization.Authorizer, scheme.Codecs)
        handler = genericapifilters.WithAuthentication(handler, config.Authentication.Authenticator, failedHandler, nil, nil)
        return handler
    }
}
```

### 4. Authenticator Framework (`pkg/authenticator/authenticator.go`)

#### Multi-Method Authentication Support
```go
func FromArgs(args []string) (authenticator.Request, error) {
    var authenticators []authenticator.Request

    // 1. Basic auth file support
    basicFile := getArg("--basic-auth-file", args)
    if basicFile != "" {
        basicAuthenticator, err := passwordfile.NewCSV(basicFile)
        authenticators = append(authenticators, basicauth.New(basicAuthenticator))
    }

    // 2. Client certificate authentication
    clientCA := getArg("--client-ca-file", args)
    if clientCA != "" {
        ca, err := dynamiccertificates.NewDynamicCAContentFromFile("client-ca", clientCA)
        authenticators = append(authenticators, x509.NewDynamic(ca.VerifyOptions, x509.CommonNameUserConversion))
    }

    return Combine(authenticators...), nil
}
```

#### Combiner Pattern
```go
func Combine(auths ...authenticator.Request) authenticator.Request {
    // Union authenticator tries each method in sequence
    return group.NewAuthenticatedGroupAdder(union.New(authenticators...))
}
```

## Token Validation Process

### 1. Initial Token Format (`pkg/clientaccess/token.go`)
```
Format: K10<CA_HASH>::<CREDENTIALS>
Example: K10a1b2c3d4e5f6789abcdef0123456789::node-token-here
```

### 2. Token Parsing Logic
```go
func parseToken(token string) (*Info, error) {
    // 1. Add K10 prefix if missing
    if !strings.HasPrefix(token, tokenPrefix) {
        token = tokenPrefix + ":::" + token  // Basic auth format
    }

    // 2. Strip prefix and split CA hash from credentials
    token = token[len(tokenPrefix):]
    parts := strings.SplitN(token, "::", 2)

    // 3. Validate CA hash length (if present)
    if hashLen := len(parts[0]); hashLen > 0 && hashLen != caHashLength {
        return nil, errors.New("invalid token CA hash length")
    }

    // 4. Parse credentials as bootstrap token or basic auth
    bts, err := kubeadm.NewBootstrapTokenString(token)
    if err != nil {
        // Fall back to username:password format
        parts := strings.SplitN(token, ":", 2)
        info.Username = parts[0]
        info.Password = parts[1]
    }
}
```

### 3. Certificate Authority Validation
```go
func hashCA(b []byte) (string, error) {
    certs, err := certutil.ParseCertsPEM(b)

    if len(certs) > 1 {
        // For certificate chains, hash the root CA
        roots := x509.NewCertPool()
        // Build certificate chain and hash root
        chain := chains[0]
        b = chain[len(chain)-1].Raw
    }

    // SHA256 hash of CA certificate
    digest := sha256.Sum256(b)
    return hex.EncodeToString(digest[:]), nil
}
```

## Security Policies and Access Control

### 1. Local vs Remote Access (`pkg/server/auth/auth.go`)
```go
func IsLocalOrHasRole(serverConfig *config.Control, roles ...string) mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
            client, _, _ := net.SplitHostPort(req.RemoteAddr)
            // Allow local connections without authentication
            if client == "127.0.0.1" || client == "::1" {
                next.ServeHTTP(rw, req)
            } else {
                // Require role-based authentication for remote access
                doAuth(roles, serverConfig, next, rw, req)
            }
        })
    }
}
```

### 2. Password File Authentication (`pkg/authenticator/passwordfile/passwordfile.go`)
```go
func (auth *PasswordAuthenticator) AuthenticatePassword(ctx context.Context, username, password string) (*authenticator.Response, bool, error) {
    // Support for node password verification using SCrypt
    if strings.HasPrefix(username, "node:") {
        if user, ok := auth.users[username]; ok {
            // Use nodepassword.Hasher for verification
            if err := nodepassword.Hasher.VerifyHash(user.hash, password); err == nil {
                return &authenticator.Response{User: user.info}, true, nil
            }
        }
    }
}
```

## Seal Integration Points for DaaS

### 1. Replace Node Password System

#### A. Sui Wallet-Based Node Identity
```go
// MODIFICATION POINT: pkg/nodepassword/nodepassword.go
// Replace SCrypt password system with Sui signature verification

type SuiNodeIdentity struct {
    WalletAddress string
    PublicKey     string
    StakeAmount   string
}

func VerifySuiSignature(secretClient coreclient.SecretController, nodeName string,
                       signature, message string, walletAddress string) error {
    // 1. Retrieve Sui identity secret
    secret, err := secretClient.Cache().Get(metav1.NamespaceSystem, getSuiSecretName(nodeName))

    // 2. Verify signature using Sui cryptography
    pubKey := secret.Data["public_key"]
    valid := sui.VerifySignature(signature, message, pubKey)

    // 3. Verify stake requirements
    return verifyStakeRequirement(walletAddress, secret.Data["min_stake"])
}

func EnsureSuiIdentity(secretClient coreclient.SecretController, nodeName string,
                      identity *SuiNodeIdentity) error {
    // Store Sui wallet address, public key, and stake requirements
    _, err := secretClient.Create(&v1.Secret{
        ObjectMeta: metav1.ObjectMeta{
            Name:      getSuiSecretName(nodeName),
            Namespace: metav1.NamespaceSystem,
            Labels: map[string]string{
                "daas.sui.network/node": nodeName,
                "daas.sui.network/type": "node-identity",
            },
        },
        Data: map[string][]byte{
            "wallet_address": []byte(identity.WalletAddress),
            "public_key":     []byte(identity.PublicKey),
            "stake_amount":   []byte(identity.StakeAmount),
        },
    })
    return err
}
```

#### B. Token Generation with Seal Authentication
```go
// MODIFICATION POINT: pkg/agent/config/config.go:ensureNodePassword
func ensureSuiNodeToken(walletPath string) (string, error) {
    // 1. Load Sui wallet
    wallet, err := sui.LoadWallet(walletPath)
    if err != nil {
        return "", err
    }

    // 2. Generate challenge message
    challenge := generateChallenge()

    // 3. Sign challenge with Sui wallet
    signature, err := wallet.Sign(challenge)
    if err != nil {
        return "", err
    }

    // 4. Create Seal token format: SEAL<SUI_ADDRESS>::<SIGNATURE>::<CHALLENGE>
    token := fmt.Sprintf("SEAL%s::%s::%s", wallet.Address(), signature, challenge)

    return token, nil
}

func generateChallenge() string {
    // Create timestamped challenge to prevent replay attacks
    timestamp := time.Now().Unix()
    nonce := make([]byte, 16)
    cryptorand.Read(nonce)
    return fmt.Sprintf("%d:%s", timestamp, hex.EncodeToString(nonce))
}
```

### 2. Enhance Token Validation System

#### A. Seal Token Parser
```go
// MODIFICATION POINT: pkg/clientaccess/token.go:parseToken
func parseSealToken(token string) (*SealInfo, error) {
    // SEAL<SUI_ADDRESS>::<SIGNATURE>::<CHALLENGE>
    if !strings.HasPrefix(token, "SEAL") {
        return nil, errors.New("invalid Seal token format")
    }

    token = token[4:] // Remove "SEAL" prefix
    parts := strings.Split(token, "::")

    if len(parts) != 3 {
        return nil, errors.New("invalid Seal token structure")
    }

    return &SealInfo{
        WalletAddress: parts[0],
        Signature:     parts[1],
        Challenge:     parts[2],
    }, nil
}

type SealInfo struct {
    WalletAddress string
    Signature     string
    Challenge     string
    StakeAmount   *big.Int
    Verified      bool
}

func (s *SealInfo) Verify() error {
    // 1. Verify signature against challenge
    valid := sui.VerifySignature(s.Signature, s.Challenge, s.WalletAddress)
    if !valid {
        return errors.New("invalid Sui signature")
    }

    // 2. Check challenge freshness (prevent replay)
    if err := validateChallengeFreshness(s.Challenge); err != nil {
        return err
    }

    // 3. Verify stake requirements
    stake, err := sui.GetStakedAmount(s.WalletAddress)
    if err != nil {
        return err
    }

    s.StakeAmount = stake
    s.Verified = true
    return nil
}
```

### 3. Certificate Management Integration

#### A. Sui-Based Certificate Requests
```go
// MODIFICATION POINT: pkg/agent/config/config.go:getKubeletClientCert
func getSuiKubeletClientCert(certFile, keyFile, nodeName string, nodeIPs []net.IP,
                            walletPath string, info *clientaccess.Info) error {
    // 1. Generate CSR with local private key
    csr, err := getCSRBytes(keyFile)

    // 2. Create Sui-authenticated request
    request := &SuiCertRequest{
        CSR:         csr,
        NodeName:    nodeName,
        NodeIPs:     nodeIPs,
        WalletAddr:  info.SuiWallet.Address(),
        Signature:   info.SuiWallet.SignCSR(csr),
        Timestamp:   time.Now().Unix(),
    }

    // 3. Submit to DaaS-enabled server
    body, err := Request("/v1-daas/cert-request", info, request)

    // 4. Process response with additional validation
    return processSuiCertResponse(certFile, keyFile, body)
}

type SuiCertRequest struct {
    CSR         []byte    `json:"csr"`
    NodeName    string    `json:"node_name"`
    NodeIPs     []net.IP  `json:"node_ips"`
    WalletAddr  string    `json:"wallet_address"`
    Signature   string    `json:"signature"`
    Timestamp   int64     `json:"timestamp"`
}
```

### 4. Enhanced Authentication Middleware

#### A. Sui Authenticator Implementation
```go
// NEW: pkg/authenticator/sui/sui.go
type SuiAuthenticator struct {
    stakeValidator StakeValidator
    nodeRegistry   NodeRegistry
}

func (s *SuiAuthenticator) AuthenticateRequest(req *http.Request) (*authenticator.Response, bool, error) {
    // 1. Extract Sui signature from headers
    walletAddr := req.Header.Get("X-Sui-Wallet")
    signature := req.Header.Get("X-Sui-Signature")
    challenge := req.Header.Get("X-Sui-Challenge")

    // 2. Verify Sui signature
    valid := sui.VerifySignature(signature, challenge, walletAddr)
    if !valid {
        return nil, false, errors.New("invalid Sui signature")
    }

    // 3. Verify stake requirements
    if err := s.stakeValidator.ValidateStake(walletAddr); err != nil {
        return nil, false, err
    }

    // 4. Create authenticated user
    user := &user.DefaultInfo{
        Name:   fmt.Sprintf("sui:%s", walletAddr),
        Groups: []string{"daas:nodes", "system:authenticated"},
    }

    return &authenticator.Response{User: user}, true, nil
}
```

#### B. DaaS Middleware Integration
```go
// MODIFICATION POINT: pkg/server/auth/auth.go
func DaaSAuth(serverConfig *config.Control) mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
            // 1. Check for Sui authentication headers
            if walletAddr := req.Header.Get("X-Sui-Wallet"); walletAddr != "" {
                if err := validateSuiAuth(req); err != nil {
                    util.SendError(err, rw, req, http.StatusUnauthorized)
                    return
                }
            }

            // 2. Check for Nautilus attestation
            if attestation := req.Header.Get("X-Nautilus-Attestation"); attestation != "" {
                if err := validateNautilusAttestation(attestation); err != nil {
                    util.SendError(err, rw, req, http.StatusForbidden)
                    return
                }
            }

            // 3. Continue with standard authentication
            next.ServeHTTP(rw, req)
        })
    }
}
```

## Sui Wallet Integration Architecture

### 1. Wallet Management Service
```go
// NEW: pkg/sui/wallet.go
type WalletManager struct {
    walletPath    string
    privateKey    *sui.PrivateKey
    publicKey     *sui.PublicKey
    address       string
    stakeAmount   *big.Int
}

func NewWalletManager(walletPath string) (*WalletManager, error) {
    // 1. Load wallet from file system
    wallet, err := sui.LoadWallet(walletPath)

    // 2. Verify wallet integrity
    if err := wallet.Verify(); err != nil {
        return nil, err
    }

    // 3. Check current stake
    stake, err := sui.GetStakedAmount(wallet.Address())

    return &WalletManager{
        walletPath:  walletPath,
        privateKey:  wallet.PrivateKey(),
        publicKey:   wallet.PublicKey(),
        address:     wallet.Address(),
        stakeAmount: stake,
    }, nil
}

func (w *WalletManager) SignChallenge(challenge string) (string, error) {
    return w.privateKey.Sign([]byte(challenge))
}

func (w *WalletManager) VerifyStakeRequirement(minStake *big.Int) error {
    if w.stakeAmount.Cmp(minStake) < 0 {
        return fmt.Errorf("insufficient stake: have %s, need %s",
                         w.stakeAmount.String(), minStake.String())
    }
    return nil
}
```

### 2. Stake Validation Service
```go
// NEW: pkg/daas/stake.go
type StakeValidator struct {
    suiClient     *sui.Client
    minStake      *big.Int
    stakeCache    map[string]*StakeInfo
    cacheTTL      time.Duration
}

type StakeInfo struct {
    Amount    *big.Int
    ValidUntil time.Time
    LastCheck time.Time
}

func (sv *StakeValidator) ValidateStake(walletAddress string) error {
    // 1. Check cache first
    if info, exists := sv.stakeCache[walletAddress]; exists {
        if time.Now().Before(info.ValidUntil) {
            return sv.checkStakeAmount(info.Amount)
        }
    }

    // 2. Query Sui network for current stake
    stake, err := sv.suiClient.GetStakedAmount(walletAddress)
    if err != nil {
        return err
    }

    // 3. Update cache
    sv.stakeCache[walletAddress] = &StakeInfo{
        Amount:     stake,
        ValidUntil: time.Now().Add(sv.cacheTTL),
        LastCheck:  time.Now(),
    }

    return sv.checkStakeAmount(stake)
}
```

## Migration Strategy

### Phase 1: Parallel Authentication
1. **Dual Support**: Run both K3s native and Seal auth simultaneously
2. **Gradual Migration**: Flag-controlled transition per node
3. **Fallback Support**: Maintain K3s auth as backup

### Phase 2: Seal Integration
1. **Replace Token System**: Migrate from K10 to SEAL tokens
2. **Certificate Integration**: Sui-signed certificate requests
3. **Middleware Enhancement**: Add DaaS authentication layers

### Phase 3: Full DaaS Integration
1. **Remove Legacy Auth**: Phase out K3s native authentication
2. **Stake-Based Access**: Full stake requirement enforcement
3. **Nautilus Integration**: Performance-based authentication

## Security Considerations

### Current K3s Security Model
- **Node isolation**: Per-node password/certificate pairs
- **Role-based access**: Groups and permissions
- **Certificate rotation**: Automatic certificate management
- **Secure storage**: Kubernetes secrets with RBAC

### Enhanced DaaS Security
- **Economic security**: Stake-based participation
- **Identity verification**: Cryptographic proof of ownership
- **Performance attestation**: Nautilus hardware verification
- **Replay protection**: Challenge-based authentication

### Risk Mitigation
- **Key compromise**: Stake slashing mechanisms
- **Network attacks**: Cryptographic signatures
- **Performance degradation**: Attestation-based penalties
- **Availability issues**: Redundant authentication methods

This analysis provides the foundation for replacing K3s authentication with Seal's identity-based system while maintaining security and operational requirements.