package security

import (
	"context"
	"fmt"
	"time"
)

// TestSuiClientIntegration 통합 테스트 함수
func TestSuiClientIntegration() {
	fmt.Println("🧪 Sui Client 통합 테스트 시작")

	// 1. Mock 모드 테스트
	fmt.Println("\n📋 Mock 모드 테스트:")
	client := NewSuiClient("https://fullnode.testnet.sui.io:443")
	client.SetMockMode(true)

	ctx := context.Background()

	// 정상 케이스
	stakeInfo, err := client.ValidateStake(ctx, "0x123456789abcdef", 500000000)
	if err != nil {
		fmt.Printf("❌ Mock 테스트 실패: %v\n", err)
	} else {
		fmt.Printf("✅ Mock 테스트 성공: %s (stake: %d)\n", stakeInfo.Status, stakeInfo.StakeAmount)
	}

	// 부족한 스테이킹 케이스
	_, err = client.ValidateStake(ctx, "test_insufficient", 2000000000)
	if err != nil {
		fmt.Printf("✅ 부족한 스테이킹 테스트 성공: %v\n", err)
	} else {
		fmt.Printf("❌ 부족한 스테이킹 테스트 실패: 에러가 발생해야 함\n")
	}

	// 2. Seal Token 검증 테스트
	fmt.Println("\n🔐 Seal Token 검증 테스트:")
	testToken := &SealToken{
		WalletAddress: "0x123456789abcdef",
		Signature:     "test_signature",
		Challenge:     "test_challenge",
		Timestamp:     time.Now().Unix(),
	}

	err = client.ValidateSealToken(testToken, 500000000)
	if err != nil {
		fmt.Printf("❌ Seal Token 테스트 실패: %v\n", err)
	} else {
		fmt.Printf("✅ Seal Token 테스트 성공\n")
	}

	// 3. Worker Info 테스트
	fmt.Println("\n💼 Worker Info 테스트:")
	workerInfo, err := client.GetWorkerInfo(ctx, "0x123456789abcdef")
	if err != nil {
		fmt.Printf("❌ Worker Info 테스트 실패: %v\n", err)
	} else {
		fmt.Printf("✅ Worker Info 테스트 성공: %s (%s)\n", workerInfo.NodeName, workerInfo.Status)
	}

	fmt.Println("\n🎉 모든 테스트 완료!")
}

// TestKubectlAuthIntegration kubectl 인증 통합 테스트
func TestKubectlAuthIntegration() {
	fmt.Println("\n🔐 Kubectl Auth 통합 테스트 시작")

	client := NewSuiClient("https://fullnode.testnet.sui.io:443")
	client.SetMockMode(true)

	authHandler := NewKubectlAuthHandler(client, 1000000000)

	// 스테이킹 양에 따른 그룹 테스트
	testCases := []struct {
		stake    uint64
		expected string
	}{
		{10000000000, "daas:admin"}, // 10 SUI
		{5000000000, "daas:operator"}, // 5 SUI
		{1000000000, "daas:user"}, // 1 SUI
		{500000000, "system:authenticated"}, // 0.5 SUI
	}

	for _, tc := range testCases {
		groups := authHandler.determineUserGroups(tc.stake)
		found := false
		for _, group := range groups {
			if group == tc.expected {
				found = true
				break
			}
		}

		if found {
			fmt.Printf("✅ 스테이킹 %d -> %s 그룹 할당 성공\n", tc.stake, tc.expected)
		} else {
			fmt.Printf("❌ 스테이킹 %d -> %s 그룹 할당 실패, 실제: %v\n", tc.stake, tc.expected, groups)
		}
	}

	fmt.Println("🎉 Kubectl Auth 테스트 완료!")
}

// RunAllTests 모든 테스트 실행
func RunAllTests() {
	fmt.Println("🚀 K3s-DaaS Security 패키지 통합 테스트")
	fmt.Println("=" + fmt.Sprintf("%50s", "") + "=")

	TestSuiClientIntegration()
	TestKubectlAuthIntegration()

	fmt.Println("\n" + "=" + fmt.Sprintf("%50s", "") + "=")
	fmt.Println("✅ 모든 통합 테스트 완료!")
}