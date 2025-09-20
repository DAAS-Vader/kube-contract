// User-friendly error handling for K3s-DaaS
package main

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

// 사용자 친화적 에러 구조체
type UserFriendlyError struct {
	Code         string `json:"code"`
	UserMessage  string `json:"user_message"`
	TechMessage  string `json:"technical_message"`
	Solution     string `json:"solution"`
	HelpURL      string `json:"help_url,omitempty"`
}

func (e *UserFriendlyError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.UserMessage)
}

// 기술적 세부사항을 포함한 완전한 에러 메시지
func (e *UserFriendlyError) FullError() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("🚫 %s", e.UserMessage))
	if e.TechMessage != "" {
		parts = append(parts, fmt.Sprintf("🔧 기술적 세부사항: %s", e.TechMessage))
	}
	if e.Solution != "" {
		parts = append(parts, fmt.Sprintf("💡 해결 방법: %s", e.Solution))
	}
	if e.HelpURL != "" {
		parts = append(parts, fmt.Sprintf("📖 도움말: %s", e.HelpURL))
	}
	return strings.Join(parts, "\n")
}

// 에러 코드 상수
const (
	ErrCodeConfigLoad       = "CONFIG_LOAD_FAILED"
	ErrCodeConfigValidation = "CONFIG_VALIDATION_FAILED"
	ErrCodeTEEInit          = "TEE_INIT_FAILED"
	ErrCodeK3sStart         = "K3S_START_FAILED"
	ErrCodeK3sBinary        = "K3S_BINARY_NOT_FOUND"
	ErrCodeSealToken        = "SEAL_TOKEN_INVALID"
	ErrCodeSuiConnection    = "SUI_CONNECTION_FAILED"
	ErrCodeNautilusAttest   = "NAUTILUS_ATTESTATION_FAILED"
	ErrCodeWorkerRegister   = "WORKER_REGISTRATION_FAILED"
	ErrCodeKubectl          = "KUBECTL_COMMAND_FAILED"
	ErrCodeHealthCheck      = "HEALTH_CHECK_FAILED"
	ErrCodeDataDir          = "DATA_DIR_ACCESS_FAILED"
)

// 사용자 친화적 에러 생성 함수들

func NewConfigLoadError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeConfigLoad,
		UserMessage: "설정 파일을 불러올 수 없습니다",
		TechMessage: techErr.Error(),
		Solution:    "설정 파일이 올바른 JSON 형식인지 확인하고, 파일 권한을 확인해주세요. 또는 환경변수로 설정하세요.",
		HelpURL:     "https://github.com/k3s-io/k3s-daas/wiki/configuration",
	}
}

func NewConfigValidationError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeConfigValidation,
		UserMessage: "설정값에 오류가 있습니다",
		TechMessage: techErr.Error(),
		Solution:    "필수 설정값 (포트, 디렉토리 경로, Sui 키 등)이 올바르게 설정되었는지 확인해주세요.",
		HelpURL:     "https://github.com/k3s-io/k3s-daas/wiki/configuration#validation",
	}
}

func NewTEEInitError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeTEEInit,
		UserMessage: "TEE 환경 초기화에 실패했습니다",
		TechMessage: techErr.Error(),
		Solution:    "TEE 모드를 'simulation'으로 설정하거나, AWS Nitro Enclaves가 활성화되어 있는지 확인해주세요.",
		HelpURL:     "https://github.com/k3s-io/k3s-daas/wiki/tee-setup",
	}
}

func NewK3sStartError(techErr error) *UserFriendlyError {
	solution := "K3s 바이너리가 설치되어 있는지 확인하고, 데이터 디렉토리에 쓰기 권한이 있는지 확인해주세요."

	// 일반적인 문제들에 대한 구체적 해결책 제공
	if strings.Contains(techErr.Error(), "permission denied") {
		solution = "데이터 디렉토리에 쓰기 권한이 없습니다. 'sudo chown -R $USER /var/lib/k3s-daas-tee' 명령어를 실행해주세요."
	} else if strings.Contains(techErr.Error(), "port already in use") {
		solution = "6443 포트가 이미 사용 중입니다. 다른 K3s 인스턴스를 종료하거나 설정에서 포트를 변경해주세요."
	}

	return &UserFriendlyError{
		Code:        ErrCodeK3sStart,
		UserMessage: "K3s 클러스터 시작에 실패했습니다",
		TechMessage: techErr.Error(),
		Solution:    solution,
		HelpURL:     "https://github.com/k3s-io/k3s-daas/wiki/troubleshooting#k3s-startup",
	}
}

func NewK3sBinaryError() *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeK3sBinary,
		UserMessage: "K3s 바이너리를 찾을 수 없습니다",
		TechMessage: "k3s binary not found in PATH or common locations",
		Solution:    "K3s를 설치하거나 K3S_BINARY_PATH 환경변수로 바이너리 경로를 지정해주세요. 설치 방법: 'curl -sfL https://get.k3s.io | sh -'",
		HelpURL:     "https://k3s.io/",
	}
}

func NewSealTokenError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeSealToken,
		UserMessage: "Seal 토큰 인증에 실패했습니다",
		TechMessage: techErr.Error(),
		Solution:    "워커 노드에서 올바른 스테이킹이 완료되었는지 확인하고, Sui 네트워크 연결을 확인해주세요.",
		HelpURL:     "https://github.com/k3s-io/k3s-daas/wiki/seal-tokens",
	}
}

func NewSuiConnectionError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeSuiConnection,
		UserMessage: "Sui 블록체인 네트워크 연결에 실패했습니다",
		TechMessage: techErr.Error(),
		Solution:    "인터넷 연결을 확인하고, Sui 네트워크 URL이 올바른지 확인해주세요. 현재 testnet 사용 시: https://fullnode.testnet.sui.io:443",
		HelpURL:     "https://docs.sui.io/build/sui-object",
	}
}

func NewNautilusAttestationError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeNautilusAttest,
		UserMessage: "Nautilus TEE 인증에 실패했습니다",
		TechMessage: techErr.Error(),
		Solution:    "TEE 모드를 'simulation'으로 설정하거나, Nautilus 서비스 상태를 확인해주세요. Mock 모드도 사용 가능합니다.",
		HelpURL:     "https://github.com/k3s-io/k3s-daas/wiki/nautilus-tee",
	}
}

func NewWorkerRegistrationError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeWorkerRegister,
		UserMessage: "워커 노드 등록에 실패했습니다",
		TechMessage: techErr.Error(),
		Solution:    "마스터 노드가 실행 중인지 확인하고, 네트워크 연결과 Seal 토큰을 확인해주세요.",
		HelpURL:     "https://github.com/k3s-io/k3s-daas/wiki/worker-setup",
	}
}

func NewKubectlError(techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeKubectl,
		UserMessage: "kubectl 명령어 실행에 실패했습니다",
		TechMessage: techErr.Error(),
		Solution:    "kubectl이 설치되어 있는지 확인하고, K3s 클러스터가 실행 중인지 확인해주세요. kubeconfig 설정도 확인하세요.",
		HelpURL:     "https://kubernetes.io/docs/tasks/tools/install-kubectl/",
	}
}

func NewHealthCheckError(component string, techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeHealthCheck,
		UserMessage: fmt.Sprintf("%s 상태 확인에 실패했습니다", component),
		TechMessage: techErr.Error(),
		Solution:    fmt.Sprintf("%s 서비스가 정상적으로 시작되었는지 확인하고, 네트워크 연결을 점검해주세요.", component),
	}
}

func NewDataDirError(path string, techErr error) *UserFriendlyError {
	return &UserFriendlyError{
		Code:        ErrCodeDataDir,
		UserMessage: "데이터 디렉토리에 접근할 수 없습니다",
		TechMessage: techErr.Error(),
		Solution:    fmt.Sprintf("디렉토리 '%s'에 읽기/쓰기 권한이 있는지 확인하거나, 다른 경로로 설정해주세요.", path),
	}
}

// 기존 에러를 사용자 친화적 에러로 변환
func WrapError(originalErr error, errorType string) *UserFriendlyError {
	switch errorType {
	case ErrCodeConfigLoad:
		return NewConfigLoadError(originalErr)
	case ErrCodeK3sStart:
		return NewK3sStartError(originalErr)
	case ErrCodeSealToken:
		return NewSealTokenError(originalErr)
	case ErrCodeSuiConnection:
		return NewSuiConnectionError(originalErr)
	default:
		return &UserFriendlyError{
			Code:        "UNKNOWN_ERROR",
			UserMessage: "예상치 못한 오류가 발생했습니다",
			TechMessage: originalErr.Error(),
			Solution:    "로그를 확인하고 문제가 지속되면 GitHub Issues에서 도움을 요청해주세요.",
			HelpURL:     "https://github.com/k3s-io/k3s-daas/issues",
		}
	}
}

// 에러 로깅 도우미
func LogUserFriendlyError(logger interface{}, err *UserFriendlyError) {
	// logrus 사용 가정
	if logrusLogger, ok := logger.(*logrus.Logger); ok {
		logrusLogger.WithFields(logrus.Fields{
			"error_code": err.Code,
			"tech_error": err.TechMessage,
		}).Error(err.UserMessage)

		// 해결책이 있으면 INFO 레벨로 추가 로깅
		if err.Solution != "" {
			logrusLogger.Infof("💡 해결 방법: %s", err.Solution)
		}
	}
}

// 개발자를 위한 상세 로깅
func LogDetailedError(logger interface{}, err *UserFriendlyError) {
	if logrusLogger, ok := logger.(*logrus.Logger); ok {
		logrusLogger.WithFields(logrus.Fields{
			"error_code":     err.Code,
			"user_message":   err.UserMessage,
			"tech_message":   err.TechMessage,
			"solution":       err.Solution,
			"help_url":       err.HelpURL,
		}).Debug("Detailed error information")
	}
}