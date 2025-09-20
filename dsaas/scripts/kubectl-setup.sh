#!/bin/bash

# kubectl 설정 스크립트 for K3s-DaaS Nautilus

echo "🔧 K3s-DaaS kubectl 설정 스크립트"
echo "=================================="

# 1. Nautilus TEE 서버 확인
echo "1. Nautilus TEE 서버 상태 확인..."
if curl -s http://localhost:8080/health > /dev/null; then
    echo "   ✅ Nautilus TEE 서버 정상 작동"
else
    echo "   ❌ Nautilus TEE 서버 연결 실패"
    echo "   먼저 ./nautilus-tee.exe를 실행하세요"
    exit 1
fi

# 2. kubectl 설정 파일 생성
echo "2. kubectl 설정 파일 생성..."
KUBECONFIG_DIR="$HOME/.kube"
mkdir -p "$KUBECONFIG_DIR"

# 데모용 Seal Token 생성 (실제로는 Sui 월렛에서 생성)
DEMO_SEAL_TOKEN="demo-seal-token-sui-hackathon-$(date +%s)"

# kubeconfig 파일 생성
cat > "$KUBECONFIG_DIR/config-k3s-daas" << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: http://localhost:8080
    insecure-skip-tls-verify: true
  name: k3s-daas-nautilus
contexts:
- context:
    cluster: k3s-daas-nautilus
    user: k3s-daas-user
  name: k3s-daas-context
current-context: k3s-daas-context
users:
- name: k3s-daas-user
  user:
    token: $DEMO_SEAL_TOKEN
EOF

echo "   ✅ kubectl 설정 파일 생성됨: $KUBECONFIG_DIR/config-k3s-daas"

# 3. kubectl 명령어 테스트
echo "3. kubectl 명령어 테스트..."
export KUBECONFIG="$KUBECONFIG_DIR/config-k3s-daas"

echo "   테스트 명령어:"
echo "   kubectl get nodes"
echo "   kubectl get pods --all-namespaces"
echo "   kubectl get services"

# 4. 사용 안내
echo ""
echo "🎯 kubectl 사용 방법:"
echo "   export KUBECONFIG=$KUBECONFIG_DIR/config-k3s-daas"
echo "   kubectl get nodes"
echo ""
echo "또는 직접 서버 지정:"
echo "   kubectl --server=http://localhost:8080 get nodes"
echo ""
echo "🌊 Seal Token 헤더 사용:"
echo "   curl -H 'X-Seal-Token: $DEMO_SEAL_TOKEN' http://localhost:8080/api/v1/nodes"