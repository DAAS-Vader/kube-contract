package security

import "time"

// 통합된 StakeInfo 구조체
type StakeInfo struct {
	WalletAddress string    `json:"wallet_address"`
	NodeID        string    `json:"node_id"`        // kubectl_auth.go 호환
	StakeAmount   uint64    `json:"stake_amount"`
	Status        string    `json:"status"`         // "active", "inactive", "suspended", "slashed"
	LastUpdate    int64     `json:"last_update"`
	ValidUntil    time.Time `json:"valid_until"`
}

// WorkerInfo represents comprehensive worker information
type WorkerInfo struct {
	WalletAddress    string `json:"wallet_address"`
	NodeName         string `json:"node_name"`
	StakeAmount      uint64 `json:"stake_amount"`
	PerformanceScore uint64 `json:"performance_score"`
	RegistrationTime int64  `json:"registration_time"`
	LastHeartbeat    int64  `json:"last_heartbeat"`
	Status           string `json:"status"` // "active", "inactive", "suspended"
}

// AuthCache stores validated authentication results
type AuthCache struct {
	Username    string
	Groups      []string
	ValidUntil  time.Time
	WalletAddr  string
	StakeAmount uint64
}

// AuthResult represents the result of authentication
type AuthResult struct {
	Authenticated bool
	Username      string
	Groups        []string
	WalletAddress string
	StakeAmount   uint64
}