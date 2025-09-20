package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// WorkerNode represents a single worker node in the pool
type WorkerNode struct {
	NodeID        string    `json:"node_id"`
	SealToken     string    `json:"seal_token"`
	Status        string    `json:"status"` // "pending", "active", "busy", "offline"
	StakeAmount   uint64    `json:"stake_amount"`
	JoinToken     string    `json:"join_token"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	WorkerAddress string    `json:"worker_address"`
	RegisteredAt  time.Time `json:"registered_at"`
}

// WorkerPool manages all worker nodes
type WorkerPool struct {
	workers map[string]*WorkerNode
	mutex   sync.RWMutex
	logger  *logrus.Logger
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(logger *logrus.Logger) *WorkerPool {
	return &WorkerPool{
		workers: make(map[string]*WorkerNode),
		logger:  logger,
	}
}

// AddWorker adds a new worker to the pool
func (wp *WorkerPool) AddWorker(worker *WorkerNode) error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	if _, exists := wp.workers[worker.NodeID]; exists {
		return fmt.Errorf("worker %s already exists", worker.NodeID)
	}

	worker.RegisteredAt = time.Now()
	worker.LastHeartbeat = time.Now()
	wp.workers[worker.NodeID] = worker

	wp.logger.Infof("👥 Worker added to pool: %s (stake: %d)", worker.NodeID, worker.StakeAmount)
	return nil
}

// GetWorker retrieves a worker by ID
func (wp *WorkerPool) GetWorker(nodeID string) (*WorkerNode, bool) {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	worker, exists := wp.workers[nodeID]
	return worker, exists
}

// GetAvailableWorker returns an available worker for scheduling
func (wp *WorkerPool) GetAvailableWorker() *WorkerNode {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	for _, worker := range wp.workers {
		if worker.Status == "active" {
			return worker
		}
	}
	return nil
}

// GetAvailableWorkers returns all available workers
func (wp *WorkerPool) GetAvailableWorkers() []*WorkerNode {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	var available []*WorkerNode
	for _, worker := range wp.workers {
		if worker.Status == "active" {
			available = append(available, worker)
		}
	}
	return available
}

// UpdateWorkerStatus updates worker status
func (wp *WorkerPool) UpdateWorkerStatus(nodeID, status string) error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	worker, exists := wp.workers[nodeID]
	if !exists {
		return fmt.Errorf("worker %s not found", nodeID)
	}

	oldStatus := worker.Status
	worker.Status = status
	worker.LastHeartbeat = time.Now()

	wp.logger.Infof("🔄 Worker %s status: %s → %s", nodeID, oldStatus, status)
	return nil
}

// SetWorkerJoinToken sets the K3s join token for a worker
func (wp *WorkerPool) SetWorkerJoinToken(nodeID, joinToken string) error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	worker, exists := wp.workers[nodeID]
	if !exists {
		return fmt.Errorf("worker %s not found", nodeID)
	}

	worker.JoinToken = joinToken
	wp.logger.Infof("🔑 Join token set for worker %s: %s...", nodeID, joinToken[:20])
	return nil
}

// GetWorkerStats returns worker pool statistics
func (wp *WorkerPool) GetWorkerStats() map[string]int {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	stats := map[string]int{
		"total":    0,
		"pending":  0,
		"active":   0,
		"busy":     0,
		"offline":  0,
	}

	for _, worker := range wp.workers {
		stats["total"]++
		stats[worker.Status]++
	}

	return stats
}

// ListWorkers returns all workers
func (wp *WorkerPool) ListWorkers() []*WorkerNode {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	var workers []*WorkerNode
	for _, worker := range wp.workers {
		workers = append(workers, worker)
	}
	return workers
}

// RemoveWorker removes a worker from the pool
func (wp *WorkerPool) RemoveWorker(nodeID string) error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	if _, exists := wp.workers[nodeID]; !exists {
		return fmt.Errorf("worker %s not found", nodeID)
	}

	delete(wp.workers, nodeID)
	wp.logger.Infof("❌ Worker removed from pool: %s", nodeID)
	return nil
}

// CheckHeartbeats checks for offline workers
func (wp *WorkerPool) CheckHeartbeats() {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	timeout := 5 * time.Minute
	now := time.Now()

	for nodeID, worker := range wp.workers {
		if now.Sub(worker.LastHeartbeat) > timeout && worker.Status != "offline" {
			worker.Status = "offline"
			wp.logger.Warnf("💀 Worker %s marked offline (no heartbeat)", nodeID)
		}
	}
}