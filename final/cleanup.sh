#!/bin/bash
# K3s-DaaS 테스트 환경 정리 스크립트

echo "🧹 K3s-DaaS 테스트 환경 정리 중..."

# 실행 중인 프로세스 종료
if [ -f nautilus-release/nautilus.pid ]; then
    NAUTILUS_PID=$(cat nautilus-release/nautilus.pid)
    if kill -0 $NAUTILUS_PID 2>/dev/null; then
        echo "🛑 Nautilus 프로세스 종료 중... (PID: $NAUTILUS_PID)"
        kill $NAUTILUS_PID
        sleep 2
        if kill -0 $NAUTILUS_PID 2>/dev/null; then
            kill -9 $NAUTILUS_PID
        fi
    fi
    rm -f nautilus-release/nautilus.pid
fi

if [ -f worker-release/worker.pid ]; then
    WORKER_PID=$(cat worker-release/worker.pid)
    if kill -0 $WORKER_PID 2>/dev/null; then
        echo "🛑 워커 프로세스 종료 중... (PID: $WORKER_PID)"
        kill $WORKER_PID
        sleep 2
        if kill -0 $WORKER_PID 2>/dev/null; then
            kill -9 $WORKER_PID
        fi
    fi
    rm -f worker-release/worker.pid
fi

# 로그 파일 정리
echo "📄 로그 파일 정리 중..."
rm -f nautilus-release/nautilus.log
rm -f worker-release/worker.log
rm -f *.json
rm -f contract_info.env

# 설정 파일 복원
if [ -f contracts-release/Move.toml.backup ]; then
    mv contracts-release/Move.toml.backup contracts-release/Move.toml
fi

# kubectl 설정 복원 (선택사항)
read -p "kubectl 설정을 이전 상태로 복원하시겠습니까? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    if kubectl config get-contexts | grep -q "docker-desktop"; then
        kubectl config use-context docker-desktop
        echo "✅ kubectl 컨텍스트를 docker-desktop으로 복원"
    elif kubectl config get-contexts | grep -q "minikube"; then
        kubectl config use-context minikube
        echo "✅ kubectl 컨텍스트를 minikube로 복원"
    else
        echo "⚠️ 기본 컨텍스트를 찾을 수 없습니다. 수동으로 설정해주세요."
    fi
fi

echo "✅ 정리 완료!"