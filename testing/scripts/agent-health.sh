#!/bin/bash
# Health check script for K3s DaaS Agent

set -e

# Check if k3s binary is running
if ! pgrep -f "k3s.*agent" > /dev/null; then
    echo "ERROR: K3s agent process not found"
    exit 1
fi

# Check if kubelet is responding
if ! kubectl get --raw /healthz > /dev/null 2>&1; then
    echo "ERROR: Kubelet health check failed"
    exit 1
fi

# Check DaaS components if enabled
if [ "${DAAS_ENABLED}" = "true" ]; then
    # Check Sui connectivity
    if ! curl -sf "${SUI_RPC_ENDPOINT:-http://sui-node:9000}/health" > /dev/null; then
        echo "WARNING: Sui node not reachable"
    fi

    # Check Walrus connectivity
    if ! curl -sf "${WALRUS_ENDPOINT:-http://walrus-simulator:31415}/v1/health" > /dev/null; then
        echo "WARNING: Walrus simulator not reachable"
    fi

    # Check if agent registered with DaaS authentication
    if [ -f "/var/lib/rancher/k3s/agent/daas-registered" ]; then
        echo "INFO: DaaS registration successful"
    else
        echo "WARNING: DaaS registration pending"
    fi
fi

echo "INFO: K3s DaaS agent health check passed"
exit 0