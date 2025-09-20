package types

import (
	"encoding/json"
	"time"
)

// SuiTransactionResult - 통일된 Sui 트랜잭션 결과
type SuiTransactionResult struct {
	Result struct {
		Digest  string                 `json:"digest"`
		Effects map[string]interface{} `json:"effects"`
		Events  []interface{}          `json:"events"`
	} `json:"result"`
	Error interface{} `json:"error"`
}

// K8sResponse - Contract에서 받는 응답
type K8sResponse struct {
	StatusCode  int               `json:"status_code"`
	Headers     map[string]string `json:"headers"`
	Body        json.RawMessage   `json:"body"`
	ProcessedAt time.Time         `json:"processed_at"`
}

// KubectlRequest - kubectl 요청 구조체
type KubectlRequest struct {
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Namespace    string            `json:"namespace"`
	ResourceType string            `json:"resource_type"`
	Payload      []byte            `json:"payload"`
	SealToken    string            `json:"seal_token"`
	Headers      map[string]string `json:"headers"`
	UserAgent    string            `json:"user_agent"`
}

// ContractEvent - Move Contract에서 발생하는 이벤트
type ContractEvent struct {
	Type      string    `json:"type"`
	PackageID string    `json:"packageId"`
	Module    string    `json:"module"`
	Sender    string    `json:"sender"`
	EventData EventData `json:"parsedJson"`
	TxDigest  string    `json:"transactionDigest"`
	Timestamp time.Time `json:"timestampMs"`
}

// EventData - K8s API 요청 이벤트 데이터
type EventData struct {
	RequestID    string `json:"request_id"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	Namespace    string `json:"namespace"`
	ResourceType string `json:"resource_type"`
	Payload      []int  `json:"payload"` // vector<u8> from Move
	SealToken    string `json:"seal_token"`
	Requester    string `json:"requester"`
	Priority     int    `json:"priority"`
	Timestamp    uint64 `json:"timestamp"`
}

// K8sExecutionResult - K8s 실행 결과
type K8sExecutionResult struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       json.RawMessage   `json:"body"`
	Success    bool              `json:"success"`
	Error      string            `json:"error,omitempty"`
}