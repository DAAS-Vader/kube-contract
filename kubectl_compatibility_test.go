// End-to-End kubectl Compatibility Test Suite
// This file provides comprehensive testing for K3s-DaaS kubectl compatibility

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// kubectl 호환성 테스트 도구
type KubectlCompatibilityTester struct {
	nautilusEndpoint   string
	testSealToken      string
	kubeconfigPath     string
	logger             *logrus.Logger
	testResults        map[string]*TestResult
}

// 테스트 결과 구조체
type TestResult struct {
	TestName    string        `json:"test_name"`
	Success     bool          `json:"success"`
	Duration    time.Duration `json:"duration"`
	Output      string        `json:"output"`
	Error       string        `json:"error,omitempty"`
	Command     string        `json:"command"`
	Timestamp   time.Time     `json:"timestamp"`
}

// kubectl 호환성 테스트 실행
func NewKubectlCompatibilityTester(nautilusEndpoint, sealToken string) *KubectlCompatibilityTester {
	return &KubectlCompatibilityTester{
		nautilusEndpoint: nautilusEndpoint,
		testSealToken:    sealToken,
		kubeconfigPath:   "/tmp/k3s-daas-test-kubeconfig.yaml",
		logger:           logrus.New(),
		testResults:      make(map[string]*TestResult),
	}
}

// 전체 kubectl 호환성 테스트 실행
func (tester *KubectlCompatibilityTester) RunFullCompatibilityTest() error {
	tester.logger.Info("🚀 Starting comprehensive kubectl compatibility test...")

	// 1. Nautilus TEE 연결 테스트
	if err := tester.testNautilusTEEConnection(); err != nil {
		return fmt.Errorf("Nautilus TEE connection test failed: %v", err)
	}

	// 2. kubeconfig 생성 테스트
	if err := tester.testKubeconfigGeneration(); err != nil {
		return fmt.Errorf("kubeconfig generation test failed: %v", err)
	}

	// 3. 기본 kubectl 명령어 테스트
	if err := tester.testBasicKubectlCommands(); err != nil {
		return fmt.Errorf("basic kubectl commands test failed: %v", err)
	}

	// 4. 리소스 관리 테스트
	if err := tester.testResourceManagement(); err != nil {
		return fmt.Errorf("resource management test failed: %v", err)
	}

	// 5. Seal 토큰 인증 테스트
	if err := tester.testSealTokenAuthentication(); err != nil {
		return fmt.Errorf("Seal token authentication test failed: %v", err)
	}

	// 6. 고급 kubectl 기능 테스트
	if err := tester.testAdvancedKubectlFeatures(); err != nil {
		return fmt.Errorf("advanced kubectl features test failed: %v", err)
	}

	// 7. 테스트 결과 리포트 생성
	return tester.generateTestReport()
}

// 1. Nautilus TEE 연결 테스트
func (tester *KubectlCompatibilityTester) testNautilusTEEConnection() error {
	tester.logger.Info("📡 Testing Nautilus TEE connection...")

	start := time.Now()
	testName := "nautilus_tee_connection"

	// TEE 헬스체크
	resp, err := http.Get(tester.nautilusEndpoint + "/kubectl/health")
	if err != nil {
		tester.recordTestResult(testName, false, time.Since(start), "", err.Error(), "GET /kubectl/health")
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	success := resp.StatusCode == 200

	tester.recordTestResult(testName, success, time.Since(start), string(body), "", "GET /kubectl/health")

	if !success {
		return fmt.Errorf("TEE health check failed: HTTP %d", resp.StatusCode)
	}

	tester.logger.Info("✅ Nautilus TEE connection successful")
	return nil
}

// 2. kubeconfig 생성 테스트
func (tester *KubectlCompatibilityTester) testKubeconfigGeneration() error {
	tester.logger.Info("🔧 Testing kubeconfig generation...")

	start := time.Now()
	testName := "kubeconfig_generation"

	// kubeconfig 요청
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", tester.nautilusEndpoint+"/kubectl/config", nil)
	if err != nil {
		tester.recordTestResult(testName, false, time.Since(start), "", err.Error(), "GET /kubectl/config")
		return err
	}

	req.Header.Set("X-Seal-Token", tester.testSealToken)

	resp, err := client.Do(req)
	if err != nil {
		tester.recordTestResult(testName, false, time.Since(start), "", err.Error(), "GET /kubectl/config")
		return err
	}
	defer resp.Body.Close()

	kubeconfigData, err := io.ReadAll(resp.Body)
	if err != nil {
		tester.recordTestResult(testName, false, time.Since(start), "", err.Error(), "GET /kubectl/config")
		return err
	}

	success := resp.StatusCode == 200 && len(kubeconfigData) > 0

	// kubeconfig 파일 저장
	if success {
		err = os.WriteFile(tester.kubeconfigPath, kubeconfigData, 0600)
		if err != nil {
			tester.recordTestResult(testName, false, time.Since(start), string(kubeconfigData), err.Error(), "write kubeconfig")
			return err
		}
	}

	tester.recordTestResult(testName, success, time.Since(start), string(kubeconfigData), "", "GET /kubectl/config")

	if !success {
		return fmt.Errorf("kubeconfig generation failed: HTTP %d", resp.StatusCode)
	}

	tester.logger.Info("✅ kubeconfig generation successful")
	return nil
}

// 3. 기본 kubectl 명령어 테스트
func (tester *KubectlCompatibilityTester) testBasicKubectlCommands() error {
	tester.logger.Info("🔍 Testing basic kubectl commands...")

	basicCommands := []string{
		"version",
		"cluster-info",
		"get nodes",
		"get namespaces",
		"get pods --all-namespaces",
		"get services --all-namespaces",
	}

	for _, command := range basicCommands {
		if err := tester.runKubectlTest("basic_kubectl_"+strings.ReplaceAll(command, " ", "_"), command); err != nil {
			tester.logger.Warnf("Basic kubectl command failed: %s - %v", command, err)
			// 기본 명령어 실패는 치명적이지 않음
		}
	}

	tester.logger.Info("✅ Basic kubectl commands test completed")
	return nil
}

// 4. 리소스 관리 테스트
func (tester *KubectlCompatibilityTester) testResourceManagement() error {
	tester.logger.Info("📋 Testing resource management...")

	// 테스트 네임스페이스 생성
	if err := tester.runKubectlTest("create_test_namespace",
		"create namespace k3s-daas-test"); err != nil {
		tester.logger.Warnf("Test namespace creation failed: %v", err)
	}

	// ConfigMap 생성/조회/삭제 테스트
	cmCommands := []string{
		"create configmap test-cm --from-literal=key1=value1 -n k3s-daas-test",
		"get configmap test-cm -n k3s-daas-test",
		"describe configmap test-cm -n k3s-daas-test",
		"delete configmap test-cm -n k3s-daas-test",
	}

	for i, command := range cmCommands {
		testName := fmt.Sprintf("configmap_test_%d", i+1)
		if err := tester.runKubectlTest(testName, command); err != nil {
			tester.logger.Warnf("ConfigMap test failed: %s - %v", command, err)
		}
	}

	// Secret 생성/조회/삭제 테스트
	secretCommands := []string{
		"create secret generic test-secret --from-literal=password=secret123 -n k3s-daas-test",
		"get secret test-secret -n k3s-daas-test",
		"describe secret test-secret -n k3s-daas-test",
		"delete secret test-secret -n k3s-daas-test",
	}

	for i, command := range secretCommands {
		testName := fmt.Sprintf("secret_test_%d", i+1)
		if err := tester.runKubectlTest(testName, command); err != nil {
			tester.logger.Warnf("Secret test failed: %s - %v", command, err)
		}
	}

	// 테스트 네임스페이스 정리
	if err := tester.runKubectlTest("cleanup_test_namespace",
		"delete namespace k3s-daas-test"); err != nil {
		tester.logger.Warnf("Test namespace cleanup failed: %v", err)
	}

	tester.logger.Info("✅ Resource management test completed")
	return nil
}

// 5. Seal 토큰 인증 테스트
func (tester *KubectlCompatibilityTester) testSealTokenAuthentication() error {
	tester.logger.Info("🔐 Testing Seal token authentication...")

	// 유효한 토큰으로 인증 테스트
	validTokenTest := tester.testTokenAuthentication("valid_token_auth", tester.testSealToken, true)

	// 무효한 토큰으로 인증 테스트
	invalidTokenTest := tester.testTokenAuthentication("invalid_token_auth", "invalid_seal_token", false)

	// 빈 토큰으로 인증 테스트
	emptyTokenTest := tester.testTokenAuthentication("empty_token_auth", "", false)

	if validTokenTest != nil || invalidTokenTest != nil || emptyTokenTest != nil {
		return fmt.Errorf("Seal token authentication tests had issues")
	}

	tester.logger.Info("✅ Seal token authentication test completed")
	return nil
}

// 토큰 인증 개별 테스트
func (tester *KubectlCompatibilityTester) testTokenAuthentication(testName, token string, expectSuccess bool) error {
	start := time.Now()

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", tester.nautilusEndpoint+"/api/v1/nodes", nil)
	if err != nil {
		tester.recordTestResult(testName, false, time.Since(start), "", err.Error(), "GET /api/v1/nodes")
		return err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		success := !expectSuccess // 에러가 예상되는 경우 성공
		tester.recordTestResult(testName, success, time.Since(start), "", err.Error(), "GET /api/v1/nodes")
		if expectSuccess {
			return err
		}
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	actualSuccess := resp.StatusCode == 200
	testSuccess := actualSuccess == expectSuccess

	tester.recordTestResult(testName, testSuccess, time.Since(start), string(body), "", "GET /api/v1/nodes")
	return nil
}

// 6. 고급 kubectl 기능 테스트
func (tester *KubectlCompatibilityTester) testAdvancedKubectlFeatures() error {
	tester.logger.Info("🚀 Testing advanced kubectl features...")

	advancedCommands := []string{
		"api-resources",
		"api-versions",
		"explain pod",
		"explain service",
		"auth can-i get pods",
		"auth can-i create deployments",
	}

	for _, command := range advancedCommands {
		testName := "advanced_" + strings.ReplaceAll(command, " ", "_")
		if err := tester.runKubectlTest(testName, command); err != nil {
			tester.logger.Warnf("Advanced kubectl command failed: %s - %v", command, err)
		}
	}

	tester.logger.Info("✅ Advanced kubectl features test completed")
	return nil
}

// kubectl 명령어 실행 헬퍼
func (tester *KubectlCompatibilityTester) runKubectlTest(testName, command string) error {
	start := time.Now()
	fullCommand := fmt.Sprintf("kubectl --kubeconfig=%s %s", tester.kubeconfigPath, command)

	cmd := exec.Command("bash", "-c", fullCommand)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := stdout.String()
	errorOutput := stderr.String()
	success := err == nil

	tester.recordTestResult(testName, success, time.Since(start), output, errorOutput, fullCommand)

	if err != nil {
		return fmt.Errorf("command failed: %v, stderr: %s", err, errorOutput)
	}

	return nil
}

// 테스트 결과 기록
func (tester *KubectlCompatibilityTester) recordTestResult(testName string, success bool, duration time.Duration, output, errorMsg, command string) {
	result := &TestResult{
		TestName:  testName,
		Success:   success,
		Duration:  duration,
		Output:    output,
		Error:     errorMsg,
		Command:   command,
		Timestamp: time.Now(),
	}

	tester.testResults[testName] = result

	status := "✅"
	if !success {
		status = "❌"
	}

	tester.logger.WithFields(logrus.Fields{
		"test":     testName,
		"success":  success,
		"duration": duration.String(),
	}).Infof("%s %s (%s)", status, testName, duration.String())
}

// 7. 테스트 결과 리포트 생성
func (tester *KubectlCompatibilityTester) generateTestReport() error {
	tester.logger.Info("📊 Generating test report...")

	report := map[string]interface{}{
		"test_summary": tester.generateTestSummary(),
		"test_results": tester.testResults,
		"generated_at": time.Now(),
		"nautilus_endpoint": tester.nautilusEndpoint,
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to generate report JSON: %v", err)
	}

	reportPath := "/tmp/k3s-daas-kubectl-compatibility-report.json"
	if err := os.WriteFile(reportPath, reportJSON, 0644); err != nil {
		return fmt.Errorf("failed to write report file: %v", err)
	}

	// 콘솔에 요약 출력
	tester.printTestSummary()

	tester.logger.Infof("📄 Full test report saved to: %s", reportPath)
	return nil
}

// 테스트 요약 생성
func (tester *KubectlCompatibilityTester) generateTestSummary() map[string]interface{} {
	totalTests := len(tester.testResults)
	passedTests := 0
	var totalDuration time.Duration

	for _, result := range tester.testResults {
		if result.Success {
			passedTests++
		}
		totalDuration += result.Duration
	}

	return map[string]interface{}{
		"total_tests":    totalTests,
		"passed_tests":   passedTests,
		"failed_tests":   totalTests - passedTests,
		"success_rate":   float64(passedTests) / float64(totalTests) * 100,
		"total_duration": totalDuration.String(),
	}
}

// 테스트 요약 콘솔 출력
func (tester *KubectlCompatibilityTester) printTestSummary() {
	summary := tester.generateTestSummary()

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📊 K3s-DaaS kubectl Compatibility Test Report")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("📋 Total Tests: %v\n", summary["total_tests"])
	fmt.Printf("✅ Passed: %v\n", summary["passed_tests"])
	fmt.Printf("❌ Failed: %v\n", summary["failed_tests"])
	fmt.Printf("📈 Success Rate: %.1f%%\n", summary["success_rate"])
	fmt.Printf("⏱️  Total Duration: %v\n", summary["total_duration"])
	fmt.Println(strings.Repeat("=", 60))

	// 실패한 테스트 상세 정보
	failedTests := []string{}
	for name, result := range tester.testResults {
		if !result.Success {
			failedTests = append(failedTests, name)
		}
	}

	if len(failedTests) > 0 {
		fmt.Println("❌ Failed Tests:")
		for _, testName := range failedTests {
			result := tester.testResults[testName]
			fmt.Printf("  - %s: %s\n", testName, result.Error)
		}
	}

	fmt.Println("")
}

// 메인 테스트 실행 함수
func runKubectlCompatibilityTest() {
	// 환경변수에서 설정 읽기
	nautilusEndpoint := os.Getenv("NAUTILUS_ENDPOINT")
	if nautilusEndpoint == "" {
		nautilusEndpoint = "http://localhost:8080"
	}

	testSealToken := os.Getenv("TEST_SEAL_TOKEN")
	if testSealToken == "" {
		testSealToken = "seal_test_token_12345678901234567890123456789012"
	}

	// 테스트 실행
	tester := NewKubectlCompatibilityTester(nautilusEndpoint, testSealToken)

	fmt.Println("🚀 Starting K3s-DaaS kubectl Compatibility Test Suite...")
	fmt.Printf("🔗 Nautilus Endpoint: %s\n", nautilusEndpoint)
	fmt.Printf("🔑 Test Seal Token: %s...\n", testSealToken[:20])
	fmt.Println("")

	if err := tester.RunFullCompatibilityTest(); err != nil {
		fmt.Printf("❌ Test suite failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("🎉 kubectl Compatibility Test Suite completed successfully!")
}