// K3s Control Plane Integration for Nautilus EC2
// This file manages K3s as external binary process

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
	"path/filepath"
	"io/ioutil"
	"strings"

	"github.com/sirupsen/logrus"
)

// K3s Control Plane Manager - 외부 K3s 바이너리 관리
type K3sControlPlaneManager struct {
	nautilusMaster   *NautilusMaster
	k3sBinaryPath    string
	dataDir          string
	configFile       string
	logger           *logrus.Logger
	ctx              context.Context
	cancel           context.CancelFunc
	k3sProcess       *exec.Cmd
}

// K3s Control Plane 초기화 및 시작 (외부 바이너리)
func (n *NautilusMaster) startK3sControlPlane() error {
	n.logger.Info("EC2: Starting K3s Control Plane as external binary...")

	// Context 생성
	ctx, cancel := context.WithCancel(context.Background())

	// K3s Control Plane Manager 생성
	manager := &K3sControlPlaneManager{
		nautilusMaster: n,
		k3sBinaryPath:  "/usr/local/bin/k3s",
		dataDir:        "/var/lib/k3s-daas",
		configFile:     "/etc/k3s-daas/config.yaml",
		logger:         n.logger,
		ctx:            ctx,
		cancel:         cancel,
	}

	// 1. K3s 바이너리 확인 및 다운로드
	if err := manager.ensureK3sBinary(); err != nil {
		return fmt.Errorf("K3s 바이너리 준비 실패: %v", err)
	}

	// 2. K3s 설정 파일 생성
	if err := manager.generateK3sConfig(); err != nil {
		return fmt.Errorf("K3s 설정 생성 실패: %v", err)
	}

	// 3. K3s 서버 프로세스 시작
	if err := manager.startK3sServer(); err != nil {
		return fmt.Errorf("K3s 서버 시작 실패: %v", err)
	}

	n.logger.Info("✅ K3s Control Plane이 외부 프로세스로 성공적으로 시작됨")
	return nil
}

// K3s 바이너리 확인 및 다운로드
func (manager *K3sControlPlaneManager) ensureK3sBinary() error {
	manager.logger.Info("K3s 바이너리 확인 중...")

	// K3s 바이너리 존재 확인
	if _, err := os.Stat(manager.k3sBinaryPath); os.IsNotExist(err) {
		manager.logger.Info("K3s 바이너리가 없습니다. 다운로드 중...")
		if err := manager.downloadK3sBinary(); err != nil {
			return fmt.Errorf("K3s 바이너리 다운로드 실패: %v", err)
		}
	}

	// 실행 권한 확인
	if err := os.Chmod(manager.k3sBinaryPath, 0755); err != nil {
		return fmt.Errorf("K3s 바이너리 권한 설정 실패: %v", err)
	}

	manager.logger.Info("✅ K3s 바이너리 준비 완료")
	return nil
}

// K3s 바이너리 다운로드
func (manager *K3sControlPlaneManager) downloadK3sBinary() error {
	// 디렉토리 생성
	dir := filepath.Dir(manager.k3sBinaryPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("디렉토리 생성 실패: %v", err)
	}

	// K3s 바이너리 다운로드 명령
	cmd := exec.Command("curl", "-L", "-o", manager.k3sBinaryPath, 
		"https://github.com/k3s-io/k3s/releases/latest/download/k3s")
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("다운로드 실패: %v, output: %s", err, output)
	}

	manager.logger.Info("K3s 바이너리 다운로드 완료")
	return nil
}

// K3s 설정 파일 생성
func (manager *K3sControlPlaneManager) generateK3sConfig() error {
	manager.logger.Info("K3s 설정 파일 생성 중...")

	// 설정 디렉토리 생성
	configDir := filepath.Dir(manager.configFile)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("설정 디렉토리 생성 실패: %v", err)
	}

	// 데이터 디렉토리 생성
	if err := os.MkdirAll(manager.dataDir, 0755); err != nil {
		return fmt.Errorf("데이터 디렉토리 생성 실패: %v", err)
	}

	// K3s 설정 내용
	configContent := `# K3s-DaaS Configuration
# Generated automatically

cluster-cidr: "10.42.0.0/16"
service-cidr: "10.43.0.0/16"
cluster-dns: "10.43.0.10"
data-dir: "` + manager.dataDir + `"
bind-address: "0.0.0.0"
https-listen-port: 6443
write-kubeconfig-mode: "0644"
tls-san:
  - "localhost"
  - "127.0.0.1"
  - "0.0.0.0"
disable:
  - "traefik"  # 나중에 istio 사용 예정
kube-apiserver-arg:
  - "enable-admission-plugins=NodeRestriction,ResourceQuota"
  - "audit-log-maxage=30"
  - "audit-log-maxbackup=3"
  - "audit-log-maxsize=100"
`

	// 설정 파일 쓰기
	if err := ioutil.WriteFile(manager.configFile, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("설정 파일 쓰기 실패: %v", err)
	}

	manager.logger.WithField("config_file", manager.configFile).Info("✅ K3s 설정 파일 생성 완료")
	return nil
}

// K3s 서버 시작 (외부 프로세스)
func (manager *K3sControlPlaneManager) startK3sServer() error {
	manager.logger.Info("K3s 서버 프로세스 시작 중...")

	// K3s 서버 명령 준비
	cmd := exec.CommandContext(manager.ctx, manager.k3sBinaryPath, "server",
		"--config", manager.configFile,
		"--token", "k3s-daas-bootstrap-token",
		"--disable", "traefik", // 나중에 istio 사용
		"--write-kubeconfig-mode", "0644",
		"--kube-apiserver-arg", "enable-admission-plugins=NodeRestriction,ResourceQuota",
	)

	// 환경 변수 설정
	cmd.Env = append(os.Environ(),
		"K3S_TOKEN=k3s-daas-bootstrap-token",
		"K3S_DATA_DIR="+manager.dataDir,
	)

	// 로그 출력 설정
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 프로세스 시작
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("K3s 서버 시작 실패: %v", err)
	}

	// 프로세스 참조 저장
	manager.k3sProcess = cmd

	// 프로세스 상태 모니터링
	go manager.monitorK3sProcess()

	// K3s API 서버 준비 대기
	if err := manager.waitForK3sReady(); err != nil {
		return fmt.Errorf("K3s API 서버 준비 대기 실패: %v", err)
	}

	manager.logger.WithField("pid", cmd.Process.Pid).Info("✅ K3s 서버 성공적으로 시작")
	return nil
}

// K3s 프로세스 모니터링
func (manager *K3sControlPlaneManager) monitorK3sProcess() {
	if manager.k3sProcess == nil {
		return
	}

	// 프로세스 종료 대기
	err := manager.k3sProcess.Wait()
	if err != nil {
		manager.logger.WithError(err).Error("⚠️ K3s 프로세스가 비정상 종료")
	} else {
		manager.logger.Info("K3s 프로세스가 정상 종료")
	}

	// 종료 이벤트 처리
	select {
	case <-manager.ctx.Done():
		// 정상 종료
	default:
		// 비정상 종료 - 재시작 시도
		manager.logger.Warn("K3s 서버 재시작 시도...")
		time.Sleep(5 * time.Second)
		if err := manager.startK3sServer(); err != nil {
			manager.logger.WithError(err).Error("K3s 서버 재시작 실패")
		}
	}
}

// K3s API 서버 준비 대기
func (manager *K3sControlPlaneManager) waitForK3sReady() error {
	manager.logger.Info("K3s API 서버 준비 대기 중...")

	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		// kubectl 을 사용하여 API 서버 상태 확인
		cmd := exec.Command("curl", "-k", "-s", "https://localhost:6443/healthz")
		output, err := cmd.Output()
		if err == nil && strings.Contains(string(output), "ok") {
			manager.logger.Info("✅ K3s API 서버 준비 완료")
			return nil
		}

		manager.logger.WithField("attempt", i+1).Debug("K3s API 서버 준비 대기...")
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("K3s API 서버 준비 시간 초과 (%d초)", maxRetries*2)
}

// K3s 서버 중지
func (manager *K3sControlPlaneManager) stopK3sServer() error {
	manager.logger.Info("K3s 서버 중지 중...")

	if manager.k3sProcess != nil {
		// 정상 종료 시그널 전송
		if err := manager.k3sProcess.Process.Signal(os.Interrupt); err != nil {
			manager.logger.WithError(err).Warn("정상 종료 시그널 전송 실패, 강제 종료")
			manager.k3sProcess.Process.Kill()
		}

		// 프로세스 종료 대기
		manager.k3sProcess.Wait()
		manager.k3sProcess = nil
	}

	manager.logger.Info("✅ K3s 서버 중지 완료")
	return nil
}

// K3s kubeconfig 파일 경로 반환
func (manager *K3sControlPlaneManager) getKubeconfigPath() string {
	return filepath.Join(manager.dataDir, "server", "cred", "admin.kubeconfig")
}

// SealTokenAuthenticator - K3s 인증을 위한 Seal Token 검증기
type SealTokenAuthenticator struct {
	validator *SealTokenValidator
	logger    *logrus.Logger
}

// AuthenticateToken K3s 인증 인터페이스 구현
func (auth *SealTokenAuthenticator) AuthenticateToken(ctx context.Context, token string) (*authenticator.Response, bool, error) {
	auth.logger.WithField("token_prefix", token[:min(len(token), 10)]).Debug("Authenticating Seal token")

	// 1. 토큰 포맷 검증
	if !auth.isValidTokenFormat(token) {
		auth.logger.Debug("Invalid token format")
		return nil, false, nil
	}

	// 2. 블록체인 기반 토큰 검증 (실제 Sui RPC 호출)
	tokenInfo, err := auth.validateTokenWithBlockchain(token)
	if err != nil {
		auth.logger.WithError(err).Warn("Blockchain token validation failed")
		return nil, false, nil
	}

	if tokenInfo == nil {
		auth.logger.Debug("Token not found or invalid")
		return nil, false, nil
	}

	// 3. K3s 인증 응답 생성
	return auth.createAuthResponse(tokenInfo), true, nil
}

// 최소값 함수
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 토큰 포맷 검증
func (auth *SealTokenAuthenticator) isValidTokenFormat(token string) bool {
	// Seal 토큰은 64자 hex 문자열
	if len(token) != 64 {
		return false
	}

	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}

	return true
}

// 블록체인 기반 토큰 검증
func (auth *SealTokenAuthenticator) validateTokenWithBlockchain(token string) (*SealTokenInfo, error) {
	// 실제 Sui RPC 호출로 토큰 검증
	return auth.validator.ValidateToken(token)
}

// K3s 인증 응답 생성
func (auth *SealTokenAuthenticator) createAuthResponse(tokenInfo *SealTokenInfo) *authenticator.Response {
	return &authenticator.Response{
		User: &user.DefaultInfo{
			Name: tokenInfo.UserID,
			Groups: []string{
				"system:authenticated",
				"system:seal-authenticated",
			},
		},
	}
}

// SealTokenInfo Seal 토큰 정보
type SealTokenInfo struct {
	Token       string
	UserID      string
	StakeAmount uint64
	NodeID      string
	Permissions []string
}