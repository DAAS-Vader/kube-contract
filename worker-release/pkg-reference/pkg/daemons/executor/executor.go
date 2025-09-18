package executor

import (
	"context"
	"github.com/k3s-io/k3s/pkg/daemons/config"
)

// Executor interface for control plane execution
type Executor interface {
	APIServer(ctx context.Context, config *config.Control) error
	Scheduler(ctx context.Context, config *config.Control) error
	ControllerManager(ctx context.Context, config *config.Control) error
}

// DefaultExecutor provides default implementation
type DefaultExecutor struct{}

func (e *DefaultExecutor) APIServer(ctx context.Context, config *config.Control) error {
	// Mock implementation
	return nil
}

func (e *DefaultExecutor) Scheduler(ctx context.Context, config *config.Control) error {
	// Mock implementation
	return nil
}

func (e *DefaultExecutor) ControllerManager(ctx context.Context, config *config.Control) error {
	// Mock implementation
	return nil
}

// Embedded returns an embedded executor
func Embedded(ctx context.Context) (Executor, error) {
	return &DefaultExecutor{}, nil
}