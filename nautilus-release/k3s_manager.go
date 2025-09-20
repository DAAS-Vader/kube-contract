// K3s Manager - K3s 바이너리 실행 관리
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

// K3sManager - K3s 마스터 노드 관리
type K3sManager struct {
	logger     *logrus.Logger
	dataDir    string
	configFile string
	process    *exec.Cmd
	running    bool
}

// NewK3sManager - 새 K3s Manager 생성
func NewK3sManager(logger *logrus.Logger) *K3sManager {
	return &K3sManager{
		logger:     logger,
		dataDir:    "/var/lib/rancher/k3s",
		configFile: "/etc/rancher/k3s/k3s.yaml",
		running:    false,
	}
}

// Start - K3s 마스터 시작
func (k *K3sManager) Start(ctx context.Context) {
	k.logger.Info("🔧 Starting K3s Manager...")

	// 데이터 디렉토리 생성
	if err := k.setupDirectories(); err != nil {
		k.logger.Errorf("❌ Failed to setup directories: %v", err)
		return
	}

	// K3s 바이너리 확인 및 다운로드
	if err := k.ensureK3sBinary(); err != nil {
		k.logger.Errorf("❌ Failed to ensure K3s binary: %v", err)
		return
	}

	// K3s 서버 시작
	if err := k.startK3sServer(ctx); err != nil {
		k.logger.Errorf("❌ Failed to start K3s server: %v", err)
		return
	}

	k.logger.Info("✅ K3s Manager started successfully")
}

// setupDirectories - 필요한 디렉토리 생성
func (k *K3sManager) setupDirectories() error {
	dirs := []string{
		"/var/lib/rancher/k3s",
		"/etc/rancher/k3s",
		"/var/lib/rancher/k3s/server",
		"/var/lib/rancher/k3s/agent",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	k.logger.Info("📁 Directories created successfully")
	return nil
}

// ensureK3sBinary - K3s 바이너리 확인 및 다운로드
func (k *K3sManager) ensureK3sBinary() error {
	k3sPath := "/usr/local/bin/k3s"

	// 이미 존재하는지 확인
	if _, err := os.Stat(k3sPath); err == nil {
		k.logger.Info("📦 K3s binary already exists")
		return nil
	}

	k.logger.Info("📥 Downloading K3s binary...")

	// K3s 다운로드 명령
	downloadCmd := exec.Command("sh", "-c", `
		curl -sfL https://get.k3s.io | INSTALL_K3S_SKIP_START=true sh -
	`)

	downloadCmd.Stdout = os.Stdout
	downloadCmd.Stderr = os.Stderr

	if err := downloadCmd.Run(); err != nil {
		return fmt.Errorf("failed to download K3s: %v", err)
	}

	k.logger.Info("✅ K3s binary downloaded successfully")
	return nil
}

// startK3sServer - K3s 서버 시작
func (k *K3sManager) startK3sServer(ctx context.Context) error {
	k.logger.Info("🚀 Starting K3s server...")

	// K3s 서버 명령 구성
	args := []string{
		"server",
		"--data-dir", k.dataDir,
		"--bind-address", "0.0.0.0",
		"--https-listen-port", "6443",
		"--disable", "traefik",    // Traefik 비활성화 (우리가 API Gateway 사용)
		"--disable", "servicelb",  // 기본 LoadBalancer 비활성화
		"--write-kubeconfig", k.configFile,
		"--write-kubeconfig-mode", "644",
		"--node-name", "nautilus-master",
		"--cluster-init", // 단일 노드 클러스터로 시작
	}

	k.process = exec.CommandContext(ctx, "/usr/local/bin/k3s", args...)
	k.process.Stdout = os.Stdout
	k.process.Stderr = os.Stderr

	// 환경 변수 설정
	k.process.Env = append(os.Environ(),
		"K3S_KUBECONFIG_OUTPUT="+k.configFile,
		"K3S_KUBECONFIG_MODE=644",
	)

	if err := k.process.Start(); err != nil {
		return fmt.Errorf("failed to start K3s server: %v", err)
	}

	k.running = true
	k.logger.Info("🎯 K3s server started on port 6443")

	// K3s가 준비될 때까지 대기
	go k.waitForReady()

	// 프로세스 종료 감시
	go func() {
		err := k.process.Wait()
		k.running = false
		if err != nil {
			k.logger.Errorf("❌ K3s process exited with error: %v", err)
		} else {
			k.logger.Info("🛑 K3s process exited normally")
		}
	}()

	return nil
}

// waitForReady - K3s가 준비될 때까지 대기
func (k *K3sManager) waitForReady() {
	k.logger.Info("⏳ Waiting for K3s to be ready...")

	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		if k.checkK3sReady() {
			k.logger.Info("✅ K3s is ready!")
			return
		}

		time.Sleep(2 * time.Second)
		k.logger.Debugf("🔄 Checking K3s readiness... (%d/%d)", i+1, maxRetries)
	}

	k.logger.Error("❌ K3s failed to become ready within timeout")
}

// checkK3sReady - K3s 준비 상태 확인
func (k *K3sManager) checkK3sReady() bool {
	// kubeconfig 파일 존재 확인
	if _, err := os.Stat(k.configFile); err != nil {
		return false
	}

	// kubectl 명령으로 API 서버 상태 확인
	cmd := exec.Command("kubectl", "get", "nodes", "--kubeconfig", k.configFile)
	err := cmd.Run()
	return err == nil
}

// IsRunning - K3s 실행 상태 확인
func (k *K3sManager) IsRunning() bool {
	// 프로세스 상태 체크
	if !k.running || k.process == nil {
		return false
	}

	// 프로세스가 실제로 실행 중인지 확인
	if k.process.ProcessState != nil && k.process.ProcessState.Exited() {
		k.running = false
		return false
	}

	// kubeconfig 파일 존재 확인
	if _, err := os.Stat(k.configFile); err != nil {
		return false
	}

	// kubectl로 실제 API 서버 상태 확인
	return k.checkK3sReady()
}

// GetKubeconfig - kubeconfig 파일 경로 반환
func (k *K3sManager) GetKubeconfig() string {
	return k.configFile
}

// GetJoinToken - 워커 노드 join token 생성
func (k *K3sManager) GetJoinToken() (string, error) {
	if !k.running {
		return "", fmt.Errorf("K3s is not running")
	}

	// Node token 파일에서 읽기
	tokenFile := filepath.Join(k.dataDir, "server", "node-token")
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("failed to read node token: %v", err)
	}

	token := string(tokenBytes)
	k.logger.Infof("🎟️ Generated join token: %s...", token[:20])
	return token, nil
}