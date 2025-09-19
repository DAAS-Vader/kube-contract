// K3s Agent Integration for Worker Nodes
// This file integrates actual K3s agent components into K3s-DaaS worker nodes

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"encoding/json"

	// K3s Agent 컴포넌트들
	"github.com/k3s-io/k3s/pkg/daemons/config"
	"github.com/k3s-io/k3s/pkg/agent/proxy"

	// HTTP 클라이언트
	"github.com/go-resty/resty/v2"

	// Kubernetes 클라이언트
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K3s Agent Manager - 워커 노드에서 실제 K3s Agent 실행
type K3sAgentManager struct {
	stakerHost   *StakerHost
	nodeConfig   *config.Node
	agentProxy   proxy.Proxy
	ctx          context.Context
	cancel       context.CancelFunc
	k8sClient    kubernetes.Interface
	configPath   string
	k3sProcess   *exec.Cmd
}

// 실제 K3s Agent 시작 (기존 시뮬레이션 kubelet 대체)
func (s *StakerHost) startRealK3sAgent() error {
	log.Printf("🚀 Starting real K3s Agent with Seal token integration...")

	// Context 생성
	ctx, cancel := context.WithCancel(context.Background())

	// K3s Agent Manager 생성
	manager := &K3sAgentManager{
		stakerHost: s,
		ctx:        ctx,
		cancel:     cancel,
	}

	// 1. K3s Agent 설정 구성
	if err := manager.setupK3sAgentConfig(); err != nil {
		return fmt.Errorf("K3s Agent 설정 실패: %v", err)
	}

	// 2. Proxy 설정 (Nautilus TEE 연결)
	if err := manager.setupAgentProxy(); err != nil {
		return fmt.Errorf("Agent Proxy 설정 실패: %v", err)
	}

	// 3. K3s Agent 시작
	if err := manager.startAgent(); err != nil {
		return fmt.Errorf("K3s Agent 시작 실패: %v", err)
	}

	// 4. Kubernetes 클라이언트 설정
	if err := manager.setupKubernetesClient(); err != nil {
		return fmt.Errorf("Kubernetes 클라이언트 설정 실패: %v", err)
	}

	log.Printf("✅ K3s Agent가 Seal 토큰으로 성공적으로 시작됨")
	return nil
}

// K3s Agent 설정 구성
func (manager *K3sAgentManager) setupK3sAgentConfig() error {
	log.Printf("🔧 Configuring K3s Agent...")

	dataDir := "/var/lib/k3s-daas-agent"

	// 데이터 디렉토리 생성
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %v", err)
	}

	// K3s Agent 설정 생성
	config := &K3sAgentWorkerConfig{
		ServerURL:                manager.stakerHost.config.NautilusEndpoint,
		Token:                    manager.stakerHost.stakingStatus.SealToken,
		DataDir:                  dataDir,
		NodeName:                 manager.stakerHost.config.NodeID,
		NodeIP:                   "0.0.0.0",
		ContainerRuntimeEndpoint: manager.getContainerRuntimeEndpoint(),
		NodeLabels: []string{
			"k3s-daas.io/worker=true",
			"k3s-daas.io/seal-auth=enabled",
			fmt.Sprintf("k3s-daas.io/stake-amount=%d", manager.stakerHost.stakingStatus.StakeAmount),
		},
		KubeletArgs: []string{
			"--container-runtime=remote",
			"--container-runtime-endpoint=" + manager.getContainerRuntimeEndpoint(),
			"--fail-swap-on=false",
			"--cgroup-driver=systemd",
		},
		LogLevel: "info",
	}

	// 설정 파일 저장
	configPath := filepath.Join(dataDir, "agent-config.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, configData, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	manager.configPath = configPath

	log.Printf("✅ K3s Agent 설정 완료 - Node: %s, Token: %s...",
		config.NodeName, config.Token[:10])

	return nil
}

// 컨테이너 런타임 엔드포인트 결정
func (manager *K3sAgentManager) getContainerRuntimeEndpoint() string {
	switch manager.stakerHost.config.ContainerRuntime {
	case "containerd":
		return "unix:///run/containerd/containerd.sock"
	case "docker":
		return "unix:///var/run/docker.sock"
	default:
		return "unix:///run/containerd/containerd.sock"
	}
}

// Agent Proxy 설정 (실제로는 K3s 프로세스에서 자동 처리)
func (manager *K3sAgentManager) setupAgentProxy() error {
	log.Printf("🔗 Setting up agent proxy to Nautilus TEE...")

	// 프로세스 기반에서는 K3s agent가 자동으로 서버와 연결
	// 여기서는 연결 가능성만 확인
	endpoint := manager.stakerHost.config.NautilusEndpoint
	if _, err := manager.stakerHost.makeHealthCheck(endpoint + "/kubectl/health"); err != nil {
		log.Printf("⚠️ Nautilus TEE 연결 확인 실패, 계속 진행: %v", err)
	}

	log.Printf("✅ Agent proxy 설정 완료 - Target: %s", endpoint)
	return nil
}

// K3s Agent 시작
func (manager *K3sAgentManager) startAgent() error {
	log.Printf("🚀 Starting K3s Agent process...")

	// K3s 바이너리 경로 확인
	k3sBinary, err := manager.findK3sBinary()
	if err != nil {
		log.Printf("❌ K3s 바이너리를 찾을 수 없습니다")
		log.Printf("💡 해결 방법: K3s를 설치하거나 K3S_BINARY_PATH 환경변수로 바이너리 경로를 지정해주세요")
		log.Printf("📖 설치 방법: curl -sfL https://get.k3s.io | sh -")
		return fmt.Errorf("K3s 바이너리를 찾을 수 없음: %v", err)
	}

	// K3s agent 명령어 구성
	args := []string{
		"agent",
		"--server", manager.stakerHost.config.NautilusEndpoint,
		"--token", manager.stakerHost.stakingStatus.SealToken,
		"--data-dir", "/var/lib/k3s-daas-agent",
		"--node-name", manager.stakerHost.config.NodeID,
		"--node-ip", "0.0.0.0",
		"--container-runtime-endpoint", manager.getContainerRuntimeEndpoint(),
		"--log", "info",
	}

	// 노드 라벨 추가
	for _, label := range []string{
		"k3s-daas.io/worker=true",
		"k3s-daas.io/seal-auth=enabled",
		fmt.Sprintf("k3s-daas.io/stake-amount=%d", manager.stakerHost.stakingStatus.StakeAmount),
	} {
		args = append(args, "--node-label", label)
	}

	// kubelet args 추가
	kubeletArgs := []string{
		"--container-runtime=remote",
		"--container-runtime-endpoint=" + manager.getContainerRuntimeEndpoint(),
		"--fail-swap-on=false",
		"--cgroup-driver=systemd",
	}
	for _, arg := range kubeletArgs {
		args = append(args, "--kubelet-arg", arg)
	}

	log.Printf("K3s 프로세스 시작 중: %s %v", k3sBinary, args)

	// K3s 프로세스 시작
	manager.k3sProcess = exec.CommandContext(manager.ctx, k3sBinary, args...)
	manager.k3sProcess.Env = append(os.Environ(),
		"K3S_NODE_NAME="+manager.stakerHost.config.NodeID,
	)

	// 표준 출력/에러를 로그로 연결
	manager.k3sProcess.Stdout = &AgentLogWriter{prefix: "K3s-Agent"}
	manager.k3sProcess.Stderr = &AgentLogWriter{prefix: "K3s-Agent-Error"}

	// 프로세스 시작
	if err := manager.k3sProcess.Start(); err != nil {
		log.Printf("❌ K3s Agent 프로세스 시작에 실패했습니다")
		log.Printf("🔧 기술적 세부사항: %v", err)
		log.Printf("💡 해결 방법: 마스터 노드가 실행 중인지 확인하고, 네트워크 연결과 Seal 토큰을 확인해주세요")
		return fmt.Errorf("K3s Agent 프로세스 시작 실패: %v", err)
	}

	log.Printf("K3s Agent 프로세스 시작됨 (PID: %d)", manager.k3sProcess.Process.Pid)

	// 백그라운드에서 프로세스 종료 대기
	go func() {
		if err := manager.k3sProcess.Wait(); err != nil && manager.ctx.Err() == nil {
			log.Printf("❌ K3s Agent 프로세스 예상치 못한 종료: %v", err)
		}
	}()

	// Agent 시작 대기
	log.Printf("⏳ Waiting for K3s Agent to be ready...")
	if err := manager.waitForAgentReady(); err != nil {
		return fmt.Errorf("K3s Agent 준비 대기 실패: %v", err)
	}

	log.Printf("✅ K3s Agent 시작 완료")
	return nil
}

// K3s 바이너리 찾기
func (manager *K3sAgentManager) findK3sBinary() (string, error) {
	// 1. 환경변수에서 확인
	if k3sPath := os.Getenv("K3S_BINARY_PATH"); k3sPath != "" {
		if _, err := os.Stat(k3sPath); err == nil {
			return k3sPath, nil
		}
	}

	// 2. PATH에서 k3s 검색
	if k3sPath, err := exec.LookPath("k3s"); err == nil {
		return k3sPath, nil
	}

	// 3. 일반적인 위치에서 검색
	commonPaths := []string{
		"/usr/local/bin/k3s",
		"/usr/bin/k3s",
		"/opt/k3s/bin/k3s",
		"./k3s",
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("k3s binary not found in PATH or common locations")
}

// Agent 로그 출력용 Writer
type AgentLogWriter struct {
	prefix string
}

func (w *AgentLogWriter) Write(p []byte) (n int, err error) {
	message := string(p)
	log.Printf("[%s] %s", w.prefix, message)
	return len(p), nil
}

// Agent 준비 상태 대기
func (manager *K3sAgentManager) waitForAgentReady() error {
	timeout := time.After(120 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("K3s Agent 시작 타임아웃 (120초)")
		case <-ticker.C:
			if manager.isAgentReady() {
				return nil
			}
			log.Printf("⏳ K3s Agent 아직 준비되지 않음, 대기 중...")
		}
	}
}

// Agent 준비 상태 확인
func (manager *K3sAgentManager) isAgentReady() bool {
	// kubelet 상태 확인
	if !manager.isKubeletReady() {
		return false
	}

	// 컨테이너 런타임 확인
	if !manager.isContainerRuntimeReady() {
		return false
	}

	// 마스터 노드 연결 확인
	if !manager.isMasterConnectionReady() {
		return false
	}

	return true
}

// kubelet 준비 상태 확인
func (manager *K3sAgentManager) isKubeletReady() bool {
	// kubelet 헬스체크 포트 (10248) 확인
	_, err := manager.stakerHost.makeHealthCheck("http://localhost:10248/healthz")
	return err == nil
}

// 컨테이너 런타임 준비 상태 확인
func (manager *K3sAgentManager) isContainerRuntimeReady() bool {
	// 컨테이너 런타임 소켓 존재 확인
	endpoint := manager.getContainerRuntimeEndpoint()
	if len(endpoint) > 7 && endpoint[:7] == "unix://" {
		socketPath := endpoint[7:]
		_, err := os.Stat(socketPath)
		return err == nil
	}
	return false
}

// 마스터 노드 연결 확인
func (manager *K3sAgentManager) isMasterConnectionReady() bool {
	// Nautilus TEE API 서버 연결 확인
	_, err := manager.stakerHost.makeHealthCheck(manager.stakerHost.config.NautilusEndpoint + "/kubectl/health")
	return err == nil
}

// Kubernetes 클라이언트 설정
func (manager *K3sAgentManager) setupKubernetesClient() error {
	log.Printf("🔧 Setting up Kubernetes client...")

	// kubeconfig 파일 경로
	kubeConfigPath := filepath.Join(manager.nodeConfig.AgentConfig.DataDir, "kubeconfig.yaml")

	// kubeconfig 생성 또는 로드
	config, err := manager.getKubeConfig(kubeConfigPath)
	if err != nil {
		return fmt.Errorf("kubeconfig 설정 실패: %v", err)
	}

	// Kubernetes 클라이언트 생성
	manager.k8sClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("Kubernetes 클라이언트 생성 실패: %v", err)
	}

	// 연결 테스트
	if err := manager.testKubernetesConnection(); err != nil {
		return fmt.Errorf("Kubernetes 연결 테스트 실패: %v", err)
	}

	log.Printf("✅ Kubernetes 클라이언트 설정 완료")
	return nil
}

// kubeconfig 설정 가져오기
func (manager *K3sAgentManager) getKubeConfig(configPath string) (*rest.Config, error) {
	// 파일이 존재하면 로드
	if _, err := os.Stat(configPath); err == nil {
		return clientcmd.BuildConfigFromFlags("", configPath)
	}

	// 없으면 생성
	return manager.createKubeConfig(configPath)
}

// kubeconfig 생성
func (manager *K3sAgentManager) createKubeConfig(configPath string) (*rest.Config, error) {
	// Nautilus TEE에서 kubeconfig 요청
	kubeconfigData, err := manager.stakerHost.requestKubeconfigFromTEE()
	if err != nil {
		return nil, fmt.Errorf("TEE에서 kubeconfig 요청 실패: %v", err)
	}

	// 파일로 저장
	if err := os.WriteFile(configPath, kubeconfigData, 0600); err != nil {
		return nil, fmt.Errorf("kubeconfig 파일 저장 실패: %v", err)
	}

	// 설정 로드
	return clientcmd.BuildConfigFromFlags("", configPath)
}

// Kubernetes 연결 테스트
func (manager *K3sAgentManager) testKubernetesConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 서버 버전 확인
	version, err := manager.k8sClient.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("서버 버전 확인 실패: %v", err)
	}

	// context timeout 체크
	select {
	case <-ctx.Done():
		return fmt.Errorf("연결 테스트 타임아웃")
	default:
		// 계속 진행
	}

	log.Printf("🔗 Kubernetes 서버 연결 성공 - Version: %s", version.String())
	return nil
}

// TEE에서 kubeconfig 요청
func (s *StakerHost) requestKubeconfigFromTEE() ([]byte, error) {
	log.Printf("📄 Requesting kubeconfig from Nautilus TEE...")

	// Nautilus TEE에 kubeconfig 요청
	resp, err := resty.New().R().
		SetHeader("X-Seal-Token", s.stakingStatus.SealToken).
		Get(s.config.NautilusEndpoint + "/kubectl/config")

	if err != nil {
		return nil, fmt.Errorf("kubeconfig 요청 실패: %v", err)
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("kubeconfig 요청 거부됨 (HTTP %d): %s",
			resp.StatusCode(), resp.String())
	}

	log.Printf("✅ kubeconfig 수신 완료")
	return resp.Body(), nil
}

// Agent 헬스체크 (기존 함수 확장)
func (s *StakerHost) makeHealthCheck(url string) (string, error) {
	client := resty.New().SetTimeout(5 * time.Second)

	resp, err := client.R().Get(url)
	if err != nil {
		return "", err
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode())
	}

	return resp.String(), nil
}

// kubectl 명령어 실행 도우미 (워커 노드에서 직접 kubectl 사용 가능)
func (s *StakerHost) executeKubectl(args []string) (string, error) {
	// kubectl 명령어를 Seal 토큰으로 인증하여 실행
	kubeconfigPath := filepath.Join("/var/lib/k3s-daas-agent", "kubeconfig.yaml")

	fullArgs := append([]string{"--kubeconfig", kubeconfigPath}, args...)

	cmd := exec.Command("kubectl", fullArgs...)
	output, err := cmd.CombinedOutput()

	return string(output), err
}