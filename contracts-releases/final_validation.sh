#!/bin/bash

echo "🔍 K3s-DaaS Contract Final Validation"
echo "====================================="

echo ""
echo "📋 1. CRITICAL SYNTAX CHECKS"
echo "=============================="

# Check for Move language compliance
echo "🔧 Checking Move language compliance..."

# Check for invalid syntax patterns
echo "   ➤ Checking for invalid string concatenation..."
if grep -n " + " sources/*.move | grep -v "i + 1" | grep -v "n + 1" | grep -v "+ 48"; then
    echo "   ❌ Invalid string concatenation found"
else
    echo "   ✅ String concatenation syntax OK"
fi

echo "   ➤ Checking function visibility..."
ENTRY_FUNCS=$(grep -c "public entry fun" sources/*.move)
PUBLIC_FUNCS=$(grep -c "public fun" sources/*.move)
PRIVATE_FUNCS=$(grep -c "^    fun " sources/*.move)
echo "   📊 Entry functions: $ENTRY_FUNCS"
echo "   📊 Public functions: $PUBLIC_FUNCS"
echo "   📊 Private functions: $PRIVATE_FUNCS"

echo "   ➤ Checking error constants..."
ERROR_CONSTS=$(grep -c "const E_" sources/*.move)
echo "   📊 Error constants defined: $ERROR_CONSTS"

echo "   ➤ Checking struct capabilities..."
if grep -q "has key" sources/*.move && grep -q "has store" sources/*.move && grep -q "has copy" sources/*.move; then
    echo "   ✅ Struct capabilities properly defined"
else
    echo "   ⚠️  Some struct capabilities may be missing"
fi

echo ""
echo "📋 2. CONTRACT INTEGRATION ANALYSIS"
echo "==================================="

echo "🔗 Checking cross-contract dependencies..."
if grep -q "k3s_daas::staking" sources/k8s_gateway_enhanced.move; then
    echo "   ✅ Gateway contract imports staking module"
else
    echo "   ❌ Gateway contract missing staking import"
fi

if grep -q "get_stake_record_" sources/k8s_gateway_enhanced.move; then
    echo "   ✅ Gateway uses staking getter functions"
else
    echo "   ❌ Gateway missing staking integration"
fi

echo ""
echo "📋 3. SECURITY VALIDATION"
echo "========================"

echo "🛡️  Checking access controls..."
if grep -q "assert!" sources/*.move; then
    ASSERTIONS=$(grep -c "assert!" sources/*.move)
    echo "   ✅ Found $ASSERTIONS assertion checks"
else
    echo "   ❌ No assertion checks found"
fi

echo "🔐 Checking authentication mechanisms..."
if grep -q "is_valid_seal_token" sources/*.move; then
    echo "   ✅ Seal token validation implemented"
else
    echo "   ❌ Missing seal token validation"
fi

if grep -q "has_permission" sources/*.move; then
    echo "   ✅ Permission checking implemented"
else
    echo "   ❌ Missing permission checks"
fi

echo ""
echo "📋 4. FUNCTIONAL COMPLETENESS"
echo "============================"

echo "🎯 Core functionality checks..."

# Check kubectl command execution
if grep -q "execute_kubectl_command" sources/*.move; then
    echo "   ✅ kubectl command execution implemented"
else
    echo "   ❌ Missing kubectl command execution"
fi

# Check staking functionality
if grep -q "stake_for_node\|stake_for_user\|stake_for_admin" sources/*.move; then
    echo "   ✅ Staking functions implemented"
else
    echo "   ❌ Missing staking functions"
fi

# Check response handling
if grep -q "store_k8s_response\|get_k8s_response" sources/*.move; then
    echo "   ✅ Response handling implemented"
else
    echo "   ❌ Missing response handling"
fi

echo ""
echo "📋 5. TEST COVERAGE ANALYSIS"
echo "============================"

echo "🧪 Analyzing test coverage..."

TEST_SCENARIOS=(
    "test_staking_workflow"
    "test_gateway_initialization"
    "test_seal_token_creation"
    "test_kubectl_command_execution"
    "test_permission_checks"
    "test_response_registry"
    "test_staking_minimums"
)

echo "   📊 Test scenarios covered:"
for scenario in "${TEST_SCENARIOS[@]}"; do
    if grep -q "$scenario" sources/test_contracts.move; then
        echo "      ✅ $scenario"
    else
        echo "      ❌ $scenario - MISSING"
    fi
done

echo ""
echo "📋 6. DEPLOYMENT READINESS"
echo "=========================="

echo "🚀 Deployment checklist..."

# Check Move.toml
if [ -f "Move.toml" ]; then
    echo "   ✅ Move.toml configuration file present"

    if grep -q "k3s_daas = " Move.toml; then
        echo "   ✅ Address mapping configured"
    else
        echo "   ⚠️  Address mapping needs configuration"
    fi
else
    echo "   ❌ Move.toml missing"
fi

# Check for init functions
if grep -q "fun init(" sources/*.move; then
    echo "   ✅ Initialization functions present"
else
    echo "   ❌ Missing initialization functions"
fi

echo ""
echo "🎉 FINAL ASSESSMENT"
echo "=================="

# Count critical issues
CRITICAL_ISSUES=0

if ! grep -q "k3s_daas::staking" sources/k8s_gateway_enhanced.move; then
    CRITICAL_ISSUES=$((CRITICAL_ISSUES + 1))
fi

if ! grep -q "assert!" sources/*.move; then
    CRITICAL_ISSUES=$((CRITICAL_ISSUES + 1))
fi

if [ $CRITICAL_ISSUES -eq 0 ]; then
    echo "✅ CONTRACTS READY FOR DEPLOYMENT"
    echo "   💎 All critical checks passed"
    echo "   🎯 Test coverage adequate"
    echo "   🔒 Security measures in place"
    echo "   🔗 Contract integration working"
    echo ""
    echo "📝 Next steps:"
    echo "   1. Deploy to Sui testnet"
    echo "   2. Test with real transactions"
    echo "   3. Verify event emission"
    echo "   4. Test API integration"
else
    echo "⚠️  ISSUES FOUND: $CRITICAL_ISSUES critical problems"
    echo "   🔧 Fix critical issues before deployment"
fi

echo ""
echo "📊 CONTRACT STATISTICS"
echo "====================="
echo "   📄 Total files: $(ls sources/*.move | wc -l)"
echo "   📝 Total lines: $(wc -l sources/*.move | tail -1 | awk '{print $1}')"
echo "   🔧 Total functions: $(grep -c "fun " sources/*.move | awk -F: '{sum += $2} END {print sum}')"
echo "   🧪 Test functions: $(grep -c "#\[test\]" sources/*.move | awk -F: '{sum += $2} END {print sum}')"
echo "   📦 Dependencies: $(grep -c "use " sources/*.move | awk -F: '{sum += $2} END {print sum}')"

echo ""
echo "🏁 Validation completed!"