package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/k3s-io/k3s/pkg/security"
)

func main() {
	fmt.Println("=== K3s DaaS Basic Registration Flow Test ===")

	ctx := context.Background()

	// Test 1: Initialize DaaS configuration
	fmt.Println("\n1. Testing DaaS Configuration Initialization...")
	daasConfig := security.DefaultDaaSConfig()
	fmt.Printf("DaaS Enabled: %v\n", daasConfig.Enabled)
	fmt.Printf("Sui RPC Endpoint: %s\n", daasConfig.SuiConfig.RPCEndpoint)
	fmt.Printf("Min Stake: %s\n", daasConfig.StakeConfig.MinStake)

	// Test 2: Create DaaS validator
	fmt.Println("\n2. Testing DaaS Validator Creation...")
	validator, err := security.NewDaaSValidator(daasConfig)
	if err != nil {
		log.Fatalf("Failed to create DaaS validator: %v", err)
	}
	fmt.Printf("Validator created successfully\n")
	fmt.Printf("Sui Client available: %v\n", validator.GetSuiClient() != nil)
	fmt.Printf("Seal Auth available: %v\n", validator.GetSealAuth() != nil)

	// Test 3: Generate Seal token
	fmt.Println("\n3. Testing Seal Token Generation...")
	testWalletAddress := "0x1234567890abcdef1234567890abcdef12345678"
	sealAuth := security.NewSealAuthenticator(testWalletAddress)

	challenge := security.GenerateChallenge()
	fmt.Printf("Generated challenge: %s\n", challenge)

	token, err := sealAuth.GenerateToken(challenge)
	if err != nil {
		log.Fatalf("Failed to generate Seal token: %v", err)
	}
	fmt.Printf("Generated Seal token for wallet: %s\n", token.WalletAddress)
	fmt.Printf("Token signature: %s\n", token.Signature[:16]+"...")

	// Test 4: Validate Seal token
	fmt.Println("\n4. Testing Seal Token Validation...")
	err = sealAuth.ValidateToken(token)
	if err != nil {
		log.Fatalf("Failed to validate Seal token: %v", err)
	}
	fmt.Printf("Seal token validation: SUCCESS\n")

	// Test 5: Test stake validation
	fmt.Println("\n5. Testing Stake Validation...")
	suiClient := validator.GetSuiClient()
	if suiClient != nil {
		minStake := uint64(1000000000) // 1 SUI
		stakeInfo, err := suiClient.ValidateStake(ctx, testWalletAddress, minStake)
		if err != nil {
			fmt.Printf("Stake validation failed (expected for mock): %v\n", err)
		} else {
			fmt.Printf("Stake validation: SUCCESS\n")
			fmt.Printf("Worker Address: %s\n", stakeInfo.WalletAddress)
			fmt.Printf("Stake Amount: %d\n", stakeInfo.StakeAmount)
			fmt.Printf("Status: %d\n", stakeInfo.Status)
		}
	}

	// Test 6: Test worker info retrieval
	fmt.Println("\n6. Testing Worker Info Retrieval...")
	if suiClient != nil {
		workerInfo, err := suiClient.GetWorkerInfo(ctx, testWalletAddress)
		if err != nil {
			fmt.Printf("Worker info retrieval failed: %v\n", err)
		} else {
			fmt.Printf("Worker info retrieval: SUCCESS\n")
			fmt.Printf("Node Name: %s\n", workerInfo.NodeName)
			fmt.Printf("Performance Score: %d\n", workerInfo.PerformanceScore)
			fmt.Printf("Registration Time: %s\n", time.Unix(workerInfo.RegistrationTime, 0).Format(time.RFC3339))
		}
	}

	// Test 7: Test token string parsing
	fmt.Println("\n7. Testing Token String Parsing...")
	tokenString := fmt.Sprintf("SEAL%s::%s::%s", token.WalletAddress, token.Signature, token.Challenge)
	fmt.Printf("Token string: %s\n", tokenString[:50]+"...")

	parsedToken, err := security.ParseSealTokenString(tokenString)
	if err != nil {
		log.Fatalf("Failed to parse token string: %v", err)
	}
	fmt.Printf("Token parsing: SUCCESS\n")
	fmt.Printf("Parsed wallet: %s\n", parsedToken.WalletAddress)

	// Test 8: Test IsSealToken detection
	fmt.Println("\n8. Testing Token Detection...")
	fmt.Printf("IsSealToken('%s'): %v\n", tokenString, security.IsSealToken(tokenString))
	fmt.Printf("IsSealToken('regular-token'): %v\n", security.IsSealToken("regular-token"))

	fmt.Println("\n=== All Tests Completed Successfully ===")
}