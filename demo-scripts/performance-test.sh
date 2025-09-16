#!/bin/bash

# K3s-DaaS Performance Test Script
# Tests the <50ms response time target with 3-tier storage

set -e

echo "⚡ K3s-DaaS Performance Test"
echo "==========================="

# Configuration
ITERATIONS=50
TARGET_RESPONSE_TIME=50  # milliseconds
KUBECONFIG=${KUBECONFIG:-/k3s-data/k3s.yaml}

if [ ! -f "$KUBECONFIG" ]; then
    echo "❌ Kubeconfig not found at $KUBECONFIG"
    exit 1
fi

echo "📊 Running $ITERATIONS iterations to test response times..."
echo "🎯 Target: <${TARGET_RESPONSE_TIME}ms per request"
echo ""

# Arrays to store results
declare -a get_nodes_times=()
declare -a get_pods_times=()
declare -a get_configmap_times=()
declare -a create_resource_times=()

# Test 1: kubectl get nodes (Hot tier - TEE Memory)
echo "🔥 Test 1: Hot Tier - kubectl get nodes (Nautilus TEE Memory)"
echo "-----------------------------------------------------------"
total_time=0
failed_requests=0

for i in $(seq 1 $ITERATIONS); do
    start_time=$(date +%s%3N)
    if kubectl get nodes >/dev/null 2>&1; then
        end_time=$(date +%s%3N)
        response_time=$((end_time - start_time))
        get_nodes_times+=($response_time)
        total_time=$((total_time + response_time))
        printf "Request %2d: %3dms " $i $response_time
        if [ $response_time -le $TARGET_RESPONSE_TIME ]; then
            echo "✅"
        else
            echo "⚠️ "
        fi
    else
        failed_requests=$((failed_requests + 1))
        echo "Request $i: FAILED ❌"
    fi
done

avg_nodes_time=$((total_time / (ITERATIONS - failed_requests)))
echo "Average response time: ${avg_nodes_time}ms"
echo "Failed requests: $failed_requests"
echo ""

# Test 2: kubectl get pods (Warm tier - Sui Blockchain)
echo "🌡️  Test 2: Warm Tier - kubectl get pods (Sui Blockchain)"
echo "--------------------------------------------------------"
kubectl create namespace perf-test --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1 || true

total_time=0
failed_requests=0

for i in $(seq 1 $ITERATIONS); do
    start_time=$(date +%s%3N)
    if kubectl get pods -n perf-test >/dev/null 2>&1; then
        end_time=$(date +%s%3N)
        response_time=$((end_time - start_time))
        get_pods_times+=($response_time)
        total_time=$((total_time + response_time))
        printf "Request %2d: %3dms " $i $response_time
        if [ $response_time -le 3000 ]; then  # 3s target for warm tier
            echo "✅"
        else
            echo "⚠️ "
        fi
    else
        failed_requests=$((failed_requests + 1))
        echo "Request $i: FAILED ❌"
    fi
done

avg_pods_time=$((total_time / (ITERATIONS - failed_requests)))
echo "Average response time: ${avg_pods_time}ms"
echo "Failed requests: $failed_requests"
echo ""

# Test 3: kubectl get configmap (Cold tier - Walrus Storage simulation)
echo "🧊 Test 3: Cold Tier - kubectl get configmap (Walrus Storage)"
echo "------------------------------------------------------------"

# Create a test configmap if it doesn't exist
kubectl create configmap perf-test-config \
    --from-literal=data="performance-test-data" \
    -n perf-test >/dev/null 2>&1 || true

total_time=0
failed_requests=0

for i in $(seq 1 $ITERATIONS); do
    start_time=$(date +%s%3N)
    if kubectl get configmap perf-test-config -n perf-test >/dev/null 2>&1; then
        end_time=$(date +%s%3N)
        response_time=$((end_time - start_time))
        get_configmap_times+=($response_time)
        total_time=$((total_time + response_time))
        printf "Request %2d: %3dms " $i $response_time
        if [ $response_time -le 30000 ]; then  # 30s target for cold tier
            echo "✅"
        else
            echo "⚠️ "
        fi
    else
        failed_requests=$((failed_requests + 1))
        echo "Request $i: FAILED ❌"
    fi
done

avg_configmap_time=$((total_time / (ITERATIONS - failed_requests)))
echo "Average response time: ${avg_configmap_time}ms"
echo "Failed requests: $failed_requests"
echo ""

# Test 4: Resource creation performance
echo "📝 Test 4: Resource Creation Performance"
echo "--------------------------------------"
total_time=0
failed_requests=0

for i in $(seq 1 $ITERATIONS); do
    start_time=$(date +%s%3N)
    if kubectl create configmap temp-config-$i \
        --from-literal=test="data-$i" \
        -n perf-test >/dev/null 2>&1; then
        end_time=$(date +%s%3N)
        response_time=$((end_time - start_time))
        create_resource_times+=($response_time)
        total_time=$((total_time + response_time))
        printf "Create %2d: %3dms " $i $response_time
        if [ $response_time -le $TARGET_RESPONSE_TIME ]; then
            echo "✅"
        else
            echo "⚠️ "
        fi

        # Clean up
        kubectl delete configmap temp-config-$i -n perf-test >/dev/null 2>&1 || true
    else
        failed_requests=$((failed_requests + 1))
        echo "Create $i: FAILED ❌"
    fi
done

avg_create_time=$((total_time / (ITERATIONS - failed_requests)))
echo "Average response time: ${avg_create_time}ms"
echo "Failed requests: $failed_requests"
echo ""

# Calculate statistics
calculate_percentile() {
    local -n arr=$1
    local percentile=$2
    local sorted=($(printf '%s\n' "${arr[@]}" | sort -n))
    local index=$(( (${#sorted[@]} * percentile) / 100 ))
    echo ${sorted[$index]}
}

echo "📈 Performance Analysis"
echo "======================="
echo ""

# Hot Tier (TEE Memory) Analysis
if [ ${#get_nodes_times[@]} -gt 0 ]; then
    p50_nodes=$(calculate_percentile get_nodes_times 50)
    p95_nodes=$(calculate_percentile get_nodes_times 95)
    p99_nodes=$(calculate_percentile get_nodes_times 99)

    echo "🔥 Hot Tier (Nautilus TEE Memory - kubectl get nodes):"
    echo "   Average: ${avg_nodes_time}ms"
    echo "   P50: ${p50_nodes}ms"
    echo "   P95: ${p95_nodes}ms"
    echo "   P99: ${p99_nodes}ms"

    if [ $avg_nodes_time -le $TARGET_RESPONSE_TIME ]; then
        echo "   Status: ✅ PASSED (<${TARGET_RESPONSE_TIME}ms target)"
    else
        echo "   Status: ❌ FAILED (>${TARGET_RESPONSE_TIME}ms target)"
    fi
    echo ""
fi

# Warm Tier (Sui Blockchain) Analysis
if [ ${#get_pods_times[@]} -gt 0 ]; then
    p50_pods=$(calculate_percentile get_pods_times 50)
    p95_pods=$(calculate_percentile get_pods_times 95)
    p99_pods=$(calculate_percentile get_pods_times 99)

    echo "🌡️  Warm Tier (Sui Blockchain - kubectl get pods):"
    echo "   Average: ${avg_pods_time}ms"
    echo "   P50: ${p50_pods}ms"
    echo "   P95: ${p95_pods}ms"
    echo "   P99: ${p99_pods}ms"

    if [ $avg_pods_time -le 3000 ]; then
        echo "   Status: ✅ PASSED (<3s target)"
    else
        echo "   Status: ❌ FAILED (>3s target)"
    fi
    echo ""
fi

# Cold Tier (Walrus Storage) Analysis
if [ ${#get_configmap_times[@]} -gt 0 ]; then
    p50_configmap=$(calculate_percentile get_configmap_times 50)
    p95_configmap=$(calculate_percentile get_configmap_times 95)
    p99_configmap=$(calculate_percentile get_configmap_times 99)

    echo "🧊 Cold Tier (Walrus Storage - kubectl get configmap):"
    echo "   Average: ${avg_configmap_time}ms"
    echo "   P50: ${p50_configmap}ms"
    echo "   P95: ${p95_configmap}ms"
    echo "   P99: ${p99_configmap}ms"

    if [ $avg_configmap_time -le 30000 ]; then
        echo "   Status: ✅ PASSED (<30s target)"
    else
        echo "   Status: ❌ FAILED (>30s target)"
    fi
    echo ""
fi

# Resource Creation Analysis
if [ ${#create_resource_times[@]} -gt 0 ]; then
    p50_create=$(calculate_percentile create_resource_times 50)
    p95_create=$(calculate_percentile create_resource_times 95)
    p99_create=$(calculate_percentile create_resource_times 99)

    echo "📝 Resource Creation Performance:"
    echo "   Average: ${avg_create_time}ms"
    echo "   P50: ${p50_create}ms"
    echo "   P95: ${p95_create}ms"
    echo "   P99: ${p99_create}ms"

    if [ $avg_create_time -le $TARGET_RESPONSE_TIME ]; then
        echo "   Status: ✅ PASSED (<${TARGET_RESPONSE_TIME}ms target)"
    else
        echo "   Status: ❌ FAILED (>${TARGET_RESPONSE_TIME}ms target)"
    fi
    echo ""
fi

# Overall assessment
echo "🎯 Overall Performance Assessment"
echo "================================"
hot_tier_pass=false
warm_tier_pass=false
cold_tier_pass=false

if [ ${#get_nodes_times[@]} -gt 0 ] && [ $avg_nodes_time -le $TARGET_RESPONSE_TIME ]; then
    hot_tier_pass=true
fi

if [ ${#get_pods_times[@]} -gt 0 ] && [ $avg_pods_time -le 3000 ]; then
    warm_tier_pass=true
fi

if [ ${#get_configmap_times[@]} -gt 0 ] && [ $avg_configmap_time -le 30000 ]; then
    cold_tier_pass=true
fi

echo "Hot Tier (TEE Memory):     $([ "$hot_tier_pass" = true ] && echo "✅ PASSED" || echo "❌ FAILED")"
echo "Warm Tier (Sui Blockchain): $([ "$warm_tier_pass" = true ] && echo "✅ PASSED" || echo "❌ FAILED")"
echo "Cold Tier (Walrus Storage): $([ "$cold_tier_pass" = true ] && echo "✅ PASSED" || echo "❌ FAILED")"

if [ "$hot_tier_pass" = true ] && [ "$warm_tier_pass" = true ] && [ "$cold_tier_pass" = true ]; then
    echo ""
    echo "🎉 K3s-DaaS Performance Test: PASSED"
    echo "All tiers meet their performance targets!"
else
    echo ""
    echo "⚠️  K3s-DaaS Performance Test: PARTIALLY PASSED"
    echo "Some tiers may need optimization."
fi

echo ""
echo "Performance test completed! ⚡"

# Cleanup
kubectl delete namespace perf-test --ignore-not-found=true >/dev/null 2>&1 || true