#!/bin/sh

echo "🔧 Starting K3s Worker Node..."
echo "Master URL: $MASTER_URL"
echo "Node ID: $NODE_ID"
echo "Seal Token: $SEAL_TOKEN"

# Wait for master to be ready
echo "⏳ Waiting for master node..."
while ! curl -k -s "$MASTER_URL/readyz" > /dev/null 2>&1; do
    echo "Waiting for master at $MASTER_URL..."
    sleep 5
done

# Get join token from master
echo "🔑 Getting join token from master..."
TOKEN_RESPONSE=$(curl -s http://nautilus-control:8080/api/v1/nodes/token)
JOIN_TOKEN=$(echo $TOKEN_RESPONSE | grep -o '"join_token":"[^"]*"' | cut -d'"' -f4)

if [ -z "$JOIN_TOKEN" ]; then
    echo "❌ Failed to get join token"
    exit 1
fi

echo "✅ Got join token: ${JOIN_TOKEN:0:20}..."

# Start K3s agent
echo "🚀 Starting K3s agent..."
exec k3s agent \
    --server "$MASTER_URL" \
    --token "$JOIN_TOKEN" \
    --node-name "$NODE_ID" \
    --kubelet-arg "--hostname-override=$NODE_ID"
