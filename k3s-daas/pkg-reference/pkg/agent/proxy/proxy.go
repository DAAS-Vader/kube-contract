package proxy

import "context"

// Proxy interface for agent proxy
type Proxy interface {
	Start(ctx context.Context) error
	Stop() error
}

// SupervisorProxy implements Proxy interface
type SupervisorProxy struct {
	ctx         context.Context
	useWebsocket bool
	dataDirPrefix string
	serverURL   string
}

// NewSupervisorProxy creates a new supervisor proxy
func NewSupervisorProxy(ctx context.Context, useWebsocket bool, dataDirPrefix, serverURL string) Proxy {
	return &SupervisorProxy{
		ctx:           ctx,
		useWebsocket:  useWebsocket,
		dataDirPrefix: dataDirPrefix,
		serverURL:     serverURL,
	}
}

func (p *SupervisorProxy) Start(ctx context.Context) error {
	// Mock implementation
	return nil
}

func (p *SupervisorProxy) Stop() error {
	// Mock implementation
	return nil
}