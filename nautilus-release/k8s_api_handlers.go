// K8s API Handlers for Nautilus TEE Master Node
// Handles kubectl requests directly without API-Proxy
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// handleKubernetesAPIProxy - kubectl 요청의 메인 진입점
func (n *NautilusMaster) handleKubernetesAPIProxy(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	n.logger.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
		"agent":  r.UserAgent(),
	}).Info("🎯 K8s API Request received")

	// 1. Bearer Token (Seal Token) 추출
	token := extractBearerToken(r)
	if token == "" {
		n.logger.Warn("Missing Authorization header")
		n.returnK8sError(w, "Unauthorized", "Missing Authorization header", 401)
		return
	}

	// 2. Seal 토큰 검증 (캐시 활용)
	if !n.validateSealTokenQuick(token) {
		n.logger.WithField("token_prefix", token[:min(len(token), 10)]).Warn("Invalid Seal token")
		n.returnK8sError(w, "Unauthorized", "Invalid Seal token", 401)
		return
	}

	// 3. 요청 종류에 따른 처리 분기
	switch r.Method {
	case "GET":
		n.handleK8sGet(w, r, token)
	case "POST":
		n.handleK8sPost(w, r, token)
	case "PUT":
		n.handleK8sPut(w, r, token)
	case "DELETE":
		n.handleK8sDelete(w, r, token)
	case "PATCH":
		n.handleK8sPatch(w, r, token)
	default:
		n.returnK8sError(w, "MethodNotAllowed", fmt.Sprintf("Method %s not allowed", r.Method), 405)
	}

	// 4. 처리 시간 로깅
	duration := time.Since(startTime)
	n.logger.WithFields(logrus.Fields{
		"method":   r.Method,
		"path":     r.URL.Path,
		"duration": duration,
	}).Info("✅ K8s API Request completed")
}

// handleK8sGet - GET 요청 처리 (읽기 작업)
func (n *NautilusMaster) handleK8sGet(w http.ResponseWriter, r *http.Request, token string) {
	n.logger.WithField("path", r.URL.Path).Info("📖 Processing GET request")

	resource := parseK8sResource(r.URL.Path)

	// 권한 확인 (읽기 권한)
	if !n.checkReadPermission(token, resource) {
		n.returnK8sError(w, "Forbidden", "Insufficient permissions for read operation", 403)
		return
	}

	// etcd에서 리소스 조회
	if resource.Name != "" {
		// 특정 리소스 조회
		n.getSpecificResource(w, resource)
	} else {
		// 리소스 목록 조회
		n.getResourceList(w, resource)
	}
}

// handleK8sPost - POST 요청 처리 (생성 작업)
func (n *NautilusMaster) handleK8sPost(w http.ResponseWriter, r *http.Request, token string) {
	n.logger.WithField("path", r.URL.Path).Info("🆕 Processing POST request")

	// 요청 본문 읽기
	body, err := io.ReadAll(r.Body)
	if err != nil {
		n.returnK8sError(w, "BadRequest", "Cannot read request body", 400)
		return
	}
	defer r.Body.Close()

	resource := parseK8sResource(r.URL.Path)

	// 권한 확인 (쓰기 권한) - Move Contract 검증
	if !n.checkWritePermission(token, resource, "CREATE") {
		n.returnK8sError(w, "Forbidden", "Insufficient permissions for create operation", 403)
		return
	}

	// 리소스 생성
	result := n.createK8sResource(resource, body)

	// 성공 응답
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(result)
}

// handleK8sDelete - DELETE 요청 처리 (삭제 작업)
func (n *NautilusMaster) handleK8sDelete(w http.ResponseWriter, r *http.Request, token string) {
	n.logger.WithField("path", r.URL.Path).Info("🗑️ Processing DELETE request")

	resource := parseK8sResource(r.URL.Path)

	// 권한 확인 (삭제 권한) - Move Contract 검증
	if !n.checkWritePermission(token, resource, "DELETE") {
		n.returnK8sError(w, "Forbidden", "Insufficient permissions for delete operation", 403)
		return
	}

	// 리소스 삭제
	if n.deleteK8sResource(resource) {
		// 삭제 성공
		status := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Status",
			"status":     "Success",
			"metadata":   map[string]interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	} else {
		n.returnK8sError(w, "NotFound", fmt.Sprintf("%s \"%s\" not found", resource.Type, resource.Name), 404)
	}
}

// handleK8sPut - PUT 요청 처리 (업데이트 작업)
func (n *NautilusMaster) handleK8sPut(w http.ResponseWriter, r *http.Request, token string) {
	n.logger.WithField("path", r.URL.Path).Info("✏️ Processing PUT request")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		n.returnK8sError(w, "BadRequest", "Cannot read request body", 400)
		return
	}
	defer r.Body.Close()

	resource := parseK8sResource(r.URL.Path)

	// 권한 확인 (수정 권한)
	if !n.checkWritePermission(token, resource, "UPDATE") {
		n.returnK8sError(w, "Forbidden", "Insufficient permissions for update operation", 403)
		return
	}

	// 리소스 업데이트
	result := n.updateK8sResource(resource, body)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleK8sPatch - PATCH 요청 처리 (부분 업데이트)
func (n *NautilusMaster) handleK8sPatch(w http.ResponseWriter, r *http.Request, token string) {
	n.logger.WithField("path", r.URL.Path).Info("🩹 Processing PATCH request")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		n.returnK8sError(w, "BadRequest", "Cannot read request body", 400)
		return
	}
	defer r.Body.Close()

	resource := parseK8sResource(r.URL.Path)

	// 권한 확인 (수정 권한)
	if !n.checkWritePermission(token, resource, "PATCH") {
		n.returnK8sError(w, "Forbidden", "Insufficient permissions for patch operation", 403)
		return
	}

	// 리소스 패치
	result := n.patchK8sResource(resource, body)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// K8s 리소스 구조체
type K8sResource struct {
	APIVersion string
	Kind       string
	Type       string
	Namespace  string
	Name       string
	Subresource string
}

// parseK8sResource - URL 경로에서 K8s 리소스 정보 추출
func parseK8sResource(path string) K8sResource {
	// 예시 경로들:
	// /api/v1/pods
	// /api/v1/namespaces/default/pods
	// /api/v1/namespaces/default/pods/nginx
	// /apis/apps/v1/namespaces/default/deployments/nginx
	// /api/v1/namespaces/default/pods/nginx/status

	parts := strings.Split(strings.Trim(path, "/"), "/")
	resource := K8sResource{
		Namespace: "default", // 기본 네임스페이스
	}

	for i, part := range parts {
		switch part {
		case "api":
			if i+1 < len(parts) {
				resource.APIVersion = parts[i+1]
			}
		case "apis":
			if i+2 < len(parts) {
				resource.APIVersion = parts[i+1] + "/" + parts[i+2]
			}
		case "namespaces":
			if i+1 < len(parts) {
				resource.Namespace = parts[i+1]
			}
		case "pods":
			resource.Type = "pods"
			resource.Kind = "Pod"
			if i+1 < len(parts) && parts[i+1] != "status" && parts[i+1] != "log" {
				resource.Name = parts[i+1]
			}
			if i+2 < len(parts) {
				resource.Subresource = parts[i+2]
			}
		case "services":
			resource.Type = "services"
			resource.Kind = "Service"
			if i+1 < len(parts) {
				resource.Name = parts[i+1]
			}
		case "deployments":
			resource.Type = "deployments"
			resource.Kind = "Deployment"
			if i+1 < len(parts) {
				resource.Name = parts[i+1]
			}
		case "configmaps":
			resource.Type = "configmaps"
			resource.Kind = "ConfigMap"
			if i+1 < len(parts) {
				resource.Name = parts[i+1]
			}
		}
	}

	return resource
}

// validateSealTokenQuick - 빠른 Seal 토큰 검증 (캐시 활용)
func (n *NautilusMaster) validateSealTokenQuick(token string) bool {
	// 1. 기본 형식 검증
	if len(token) < 10 || !strings.HasPrefix(token, "seal_") {
		return false
	}

	// 2. 캐시된 토큰 확인
	if n.enhancedSealValidator != nil {
		return n.enhancedSealValidator.ValidateSealToken(token)
	}

	// 3. fallback으로 기본 검증기 사용
	return n.sealTokenValidator.ValidateSealToken(token)
}

// checkReadPermission - 읽기 권한 확인
func (n *NautilusMaster) checkReadPermission(token string, resource K8sResource) bool {
	// 읽기 권한은 로컬 캐시로 빠르게 검증
	// TODO: Move Contract와 동기화된 권한 캐시 사용
	return true // 임시로 모든 읽기 허용
}

// checkWritePermission - 쓰기 권한 확인 (Move Contract 검증)
func (n *NautilusMaster) checkWritePermission(token string, resource K8sResource, action string) bool {
	// 중요한 쓰기 작업은 Move Contract로 검증
	n.logger.WithFields(logrus.Fields{
		"action":   action,
		"resource": resource.Type,
		"name":     resource.Name,
	}).Info("🔐 Checking write permission with Move Contract")

	// TODO: 실제 Move Contract 호출 구현
	// 현재는 기본 Seal 토큰 검증만 수행
	return n.validateSealTokenQuick(token)
}

// getSpecificResource - 특정 리소스 조회
func (n *NautilusMaster) getSpecificResource(w http.ResponseWriter, resource K8sResource) {
	key := n.buildEtcdKey(resource)

	data, err := n.etcdStore.Get(key)
	if err != nil {
		n.returnK8sError(w, "NotFound", fmt.Sprintf("%s \"%s\" not found", resource.Kind, resource.Name), 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// getResourceList - 리소스 목록 조회
func (n *NautilusMaster) getResourceList(w http.ResponseWriter, resource K8sResource) {
	// 네임스페이스 내 모든 리소스 조회
	prefix := fmt.Sprintf("/%s/%s/", resource.Namespace, resource.Type)

	// etcd에서 prefix로 시작하는 모든 키 조회
	items := []interface{}{}

	// TODO: etcd prefix 스캔 구현
	// 현재는 빈 목록 반환

	list := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       resource.Kind + "List",
		"metadata": map[string]interface{}{
			"resourceVersion": "1",
		},
		"items": items,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// createK8sResource - K8s 리소스 생성
func (n *NautilusMaster) createK8sResource(resource K8sResource, body []byte) map[string]interface{} {
	// 1. 리소스 JSON 파싱
	var resourceObj map[string]interface{}
	json.Unmarshal(body, &resourceObj)

	// 2. 메타데이터 설정
	if metadata, ok := resourceObj["metadata"].(map[string]interface{}); ok {
		if metadata["name"] == nil {
			metadata["name"] = generateResourceName(resource.Type)
		}
		if metadata["namespace"] == nil {
			metadata["namespace"] = resource.Namespace
		}
		metadata["creationTimestamp"] = time.Now().Format(time.RFC3339)
		metadata["uid"] = generateUID()
	}

	// 3. etcd에 저장
	resourceName := extractResourceName(resourceObj)
	resource.Name = resourceName
	key := n.buildEtcdKey(resource)

	updatedBody, _ := json.Marshal(resourceObj)
	n.etcdStore.Put(key, updatedBody)

	// 4. Controller Manager에 알림
	n.notifyControllerManager(K8sAPIRequest{
		Method:       "POST",
		Path:         fmt.Sprintf("/api/v1/namespaces/%s/%s", resource.Namespace, resource.Type),
		Namespace:    resource.Namespace,
		ResourceType: resource.Type,
		Payload:      updatedBody,
		Sender:       "kubectl-user", // TODO: Seal token에서 추출
		Timestamp:    uint64(time.Now().UnixMilli()),
	})

	n.logger.WithFields(logrus.Fields{
		"type": resource.Type,
		"name": resourceName,
		"ns":   resource.Namespace,
	}).Info("✅ K8s resource created")

	return resourceObj
}

// deleteK8sResource - K8s 리소스 삭제
func (n *NautilusMaster) deleteK8sResource(resource K8sResource) bool {
	key := n.buildEtcdKey(resource)

	// 리소스 존재 확인
	_, err := n.etcdStore.Get(key)
	if err != nil {
		return false
	}

	// etcd에서 삭제
	n.etcdStore.Delete(key)

	// Controller Manager에 알림
	n.notifyControllerManager(K8sAPIRequest{
		Method:       "DELETE",
		Path:         fmt.Sprintf("/api/v1/namespaces/%s/%s/%s", resource.Namespace, resource.Type, resource.Name),
		Namespace:    resource.Namespace,
		ResourceType: resource.Type,
		Sender:       "kubectl-user",
		Timestamp:    uint64(time.Now().UnixMilli()),
	})

	n.logger.WithFields(logrus.Fields{
		"type": resource.Type,
		"name": resource.Name,
		"ns":   resource.Namespace,
	}).Info("🗑️ K8s resource deleted")

	return true
}

// updateK8sResource - K8s 리소스 업데이트
func (n *NautilusMaster) updateK8sResource(resource K8sResource, body []byte) map[string]interface{} {
	// 기존 리소스와 병합하여 업데이트
	var resourceObj map[string]interface{}
	json.Unmarshal(body, &resourceObj)

	// 메타데이터 업데이트
	if metadata, ok := resourceObj["metadata"].(map[string]interface{}); ok {
		metadata["namespace"] = resource.Namespace
		if metadata["name"] == nil {
			metadata["name"] = resource.Name
		}
	}

	key := n.buildEtcdKey(resource)
	updatedBody, _ := json.Marshal(resourceObj)
	n.etcdStore.Put(key, updatedBody)

	n.logger.WithFields(logrus.Fields{
		"type": resource.Type,
		"name": resource.Name,
		"ns":   resource.Namespace,
	}).Info("✏️ K8s resource updated")

	return resourceObj
}

// patchK8sResource - K8s 리소스 패치
func (n *NautilusMaster) patchK8sResource(resource K8sResource, patch []byte) map[string]interface{} {
	// 현재는 PUT과 동일하게 처리
	return n.updateK8sResource(resource, patch)
}

// 유틸리티 함수들
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (n *NautilusMaster) buildEtcdKey(resource K8sResource) string {
	return fmt.Sprintf("/%s/%s/%s", resource.Namespace, resource.Type, resource.Name)
}

func extractResourceName(resourceObj map[string]interface{}) string {
	if metadata, ok := resourceObj["metadata"].(map[string]interface{}); ok {
		if name, ok := metadata["name"].(string); ok {
			return name
		}
	}
	return generateResourceName("resource")
}

func generateResourceName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%100000)
}

func generateUID() string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		time.Now().Unix(),
		time.Now().UnixNano()&0xffff,
		time.Now().UnixNano()&0xffff,
		time.Now().UnixNano()&0xffff,
		time.Now().UnixNano())
}

// returnK8sError - K8s API 표준 에러 응답
func (n *NautilusMaster) returnK8sError(w http.ResponseWriter, reason, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	errorResponse := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Status",
		"status":     "Failure",
		"message":    message,
		"reason":     reason,
		"code":       code,
	}

	json.NewEncoder(w).Encode(errorResponse)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}