#!/bin/bash

# Fresh E2E Test Script for K8s-DaaS Staker Workflow
echo "🚀 Starting Fresh K8s-DaaS Staker E2E Test"
echo "==========================================="

# Configuration
CONTRACT_PACKAGE="0x7cec09084d43c2b8b9dd217b3bf69316b04e924e455b80b8813873e5c52cadd6"
STAKER_PRIVATE_KEY="suiprivkey1qqd74wmst3u3ar3kenngevpnayu0n4kvdklu9ses22p7pfev7x53yugm7aw"
SUI_RPC_URL="https://fullnode.testnet.sui.io:443"

echo "📋 Test Configuration:"
echo "   Contract Package: $CONTRACT_PACKAGE"
echo "   Staker Key: ${STAKER_PRIVATE_KEY:0:20}..."
echo "   RPC URL: $SUI_RPC_URL"
echo ""

# Step 1: Deploy Fresh K8s-DaaS System
echo "📦 Step 1: Deploying fresh K8s-DaaS system..."
cd /mnt/c/Users/ahwls/daasVader
docker-compose up -d
echo "✅ Docker Compose deployment initiated"
echo ""

# Wait for services to be ready
echo "⏳ Waiting for services to initialize..."
sleep 30

# Check service status
echo "🔍 Checking service status:"
docker-compose ps
echo ""

# Step 2: Monitor Logs
echo "📊 Step 2: Monitoring service logs..."
echo "   (Logs will be shown for 60 seconds to verify initialization)"
timeout 60 docker-compose logs -f --tail=50
echo ""

echo "🎯 Fresh deployment completed!"
echo "Next steps will be performed manually:"
echo "1. Register worker node via contract staking"
echo "2. Deploy nginx pod via contract"
echo "3. Query pod status via contract"
echo "4. Verify complete E2E workflow"