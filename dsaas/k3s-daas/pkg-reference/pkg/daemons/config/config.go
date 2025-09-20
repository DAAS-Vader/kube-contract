package config

import (
	"context"
)

// Control contains control plane configuration
type Control struct {
	DataDir     string
	Token       string
	BindAddress string
	HTTPSBindAddress string
	APIPort     int
	HTTPSPort   int
	Context     context.Context
	ClusterIPRange []string
	ServiceIPRange []string
	ClusterDNS     []string
	DisableAPIServer bool
	DisableScheduler bool
	DisableControllerManager bool
	DisableETCD bool
	EncryptSecrets bool
	LogFormat   string
	LogLevel    string
	Runtime     string
	TLSMinVersion string
	CipherSuites  []string
	Authenticator interface{}
}

// Node contains node configuration
type Node struct {
	AgentConfig Agent
}

// Agent contains agent configuration
type Agent struct {
	NodeName     string
	ServerURL    string
	Token        string
	DataDir      string
	ContainerRuntimeEndpoint string
	NodeIP       string
	NodeExternalIP string
	KubeletArgs  []string
	ProtectKernelDefaults bool
	LogLevel     string
	PauseImage   string
	CNIPlugin    string
	NodeLabels   []string
}