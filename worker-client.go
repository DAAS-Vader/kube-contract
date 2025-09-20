package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// WorkerClient - 워커 노드 클라이언트
type WorkerClient struct {
	nodeID            string
	contractPackageID string
	workerRegistryID  string
	masterURL         string
	logger            *log.Logger
}

// ContractCall 응답 구조체
type ContractCallResponse struct {
	Result struct {
		ReturnValues []struct {
			Value string `json:"value"`
		} `json:"returnValues"`
	} `json:"result"`
}

// NewWorkerClient - 새로운 워커 클라이언트 생성
func NewWorkerClient(nodeID string) *WorkerClient {
	return &WorkerClient{
		nodeID:            nodeID,
		contractPackageID: getEnvOrDefault("CONTRACT_PACKAGE_ID", "0x029f3e4a78286e7534e2958c84c795cee3677c27f89dee56a29501b858e8892c"),
		workerRegistryID:  getEnvOrDefault("WORKER_REGISTRY_ID", "0x733fe1e93455271672bdccec650f466c835edcf77e7c1ab7ee37ec70666cdc24"),
		masterURL:         getEnvOrDefault("MASTER_URL", "https://nautilus-control:6443"),
		logger:            log.New(os.Stdout, fmt.Sprintf("[WORKER-%s] ", nodeID), log.LstdFlags),
	}
}

// GetJoinTokenFromContract - 컨트랙트에서 조인 토큰 조회
func (w *WorkerClient) GetJoinTokenFromContract() (string, error) {
	w.logger.Printf("🔍 Querying join token from contract for worker %s", w.nodeID)

	// SUI 클라이언트 명령어 구성 (view function call)
	cmd := exec.Command("sui", "client", "call",
		"--package", w.contractPackageID,
		"--module", "worker_registry",
		"--function", "get_worker_join_token",
		"--args", w.workerRegistryID, w.nodeID,
		"--gas-budget", "1000000",
	)

	w.logger.Printf("🔗 Executing: %s", strings.Join(cmd.Args, " "))

	// 명령 실행
	output, err := cmd.CombinedOutput()
	if err != nil {
		w.logger.Printf("❌ Failed to execute SUI command: %v", err)
		w.logger.Printf("❌ Command output: %s", string(output))
		return "", fmt.Errorf("failed to query join token: %v", err)
	}

	w.logger.Printf("✅ Raw SUI output: %s", string(output))

	// 출력에서 조인 토큰 추출 (간단한 파싱)
	outputStr := string(output)
	if strings.Contains(outputStr, "TransactionDigest:") {
		w.logger.Printf("✅ Contract call successful")
		// TODO: 실제 응답에서 조인 토큰 파싱
		// 지금은 간단하게 하드코딩
		return "K123456789abcdef::node-token", nil
	}

	return "", fmt.Errorf("failed to parse join token from contract response")
}

// JoinK3sCluster - 조인 토큰으로 K3s 클러스터 참여
func (w *WorkerClient) JoinK3sCluster(joinToken string) error {
	w.logger.Printf("🔗 Joining K3s cluster with token: %s...", joinToken[:20])

	// K3s agent 명령어 구성
	cmd := exec.Command("k3s", "agent",
		"--server", w.masterURL,
		"--token", joinToken,
		"--node-name", w.nodeID,
	)

	w.logger.Printf("🚀 Starting K3s agent: %s", strings.Join(cmd.Args, " "))

	// 백그라운드로 실행
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start K3s agent: %v", err)
	}

	w.logger.Printf("✅ K3s agent started successfully, PID: %d", cmd.Process.Pid)
	return nil
}

// Start - 워커 클라이언트 시작
func (w *WorkerClient) Start() error {
	w.logger.Printf("🚀 Starting worker client for node: %s", w.nodeID)

	// 1. 컨트랙트에서 조인 토큰 조회
	joinToken, err := w.GetJoinTokenFromContract()
	if err != nil {
		return fmt.Errorf("failed to get join token: %v", err)
	}

	if joinToken == "" {
		w.logger.Printf("⚠️ No join token found for worker %s, waiting...", w.nodeID)
		return fmt.Errorf("join token not available yet")
	}

	w.logger.Printf("✅ Retrieved join token: %s...", joinToken[:20])

	// 2. K3s 클러스터 참여
	if err := w.JoinK3sCluster(joinToken); err != nil {
		return fmt.Errorf("failed to join cluster: %v", err)
	}

	w.logger.Printf("🎉 Worker %s successfully joined the cluster!", w.nodeID)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: ./worker-client <node-id>")
	}

	nodeID := os.Args[1]
	client := NewWorkerClient(nodeID)

	// 시작 시도 (몇 번 재시도)
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		if err := client.Start(); err != nil {
			client.logger.Printf("❌ Attempt %d failed: %v", i+1, err)
			if i < maxRetries-1 {
				client.logger.Printf("⏳ Retrying in 30 seconds...")
				time.Sleep(30 * time.Second)
			}
		} else {
			client.logger.Printf("✅ Worker successfully started!")
			break
		}
	}

	// 무한 대기 (실제로는 K3s agent가 실행 중)
	select {}
}

// getEnvOrDefault - 환경변수 또는 기본값 반환
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}