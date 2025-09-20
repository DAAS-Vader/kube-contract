#!/bin/bash

# 워커 노드 연결 테스트 스크립트 for K3s-DaaS

echo "🔧 K3s-DaaS 워커 노드 연결 테스트"
echo "================================="

# 1. Nautilus TEE 마스터 노드 확인
echo "1. Nautilus TEE 마스터 노드 상태 확인..."
if curl -s http://localhost:8080/health > /dev/null; then
    echo "   ✅ Nautilus TEE 마스터 노드 정상 작동"
else
    echo "   ❌ Nautilus TEE 마스터 노드 연결 실패"
    echo "   먼저 ./nautilus-tee/nautilus-tee.exe를 실행하세요"
    exit 1
fi

# 2. k3s-daas 워커 노드 빌드 확인
echo "2. k3s-daas 워커 노드 빌드 확인..."
cd k3s-daas
if [ ! -f "./k3s-daas" ] && [ ! -f "./k3s-daas.exe" ]; then
    echo "   ⚠️  워커 노드 실행파일이 없습니다. 빌드 중..."
    go build -o k3s-daas . || go build -o k3s-daas.exe .
    if [ $? -eq 0 ]; then
        echo "   ✅ 워커 노드 빌드 성공"
    else
        echo "   ❌ 워커 노드 빌드 실패"
        exit 1
    fi
else
    echo "   ✅ 워커 노드 실행파일 존재"
fi

# 3. 설정 파일 확인
echo "3. 워커 노드 설정 파일 확인..."
if [ -f "staker-config.json" ]; then
    echo "   ✅ staker-config.json 파일 존재"
    echo "   설정 내용:"
    cat staker-config.json | jq . 2>/dev/null || cat staker-config.json
else
    echo "   ❌ staker-config.json 파일이 없습니다"
    exit 1
fi

# 4. 워커 노드 연결 테스트 (5초간)
echo "4. 워커 노드 연결 테스트 (5초간)..."
echo "   워커 노드 시작 중..."

if [ -f "./k3s-daas.exe" ]; then
    timeout 5s ./k3s-daas.exe &
else
    timeout 5s ./k3s-daas &
fi

WORKER_PID=$!
sleep 2

# 5. 마스터 노드에서 워커 등록 확인
echo "5. 마스터 노드에서 워커 등록 상태 확인..."
WORKER_STATUS=$(curl -s http://localhost:8080/api/v1/nodes 2>/dev/null || echo "API 호출 실패")
echo "   워커 노드 상태: $WORKER_STATUS"

# 6. 하트비트 테스트
echo "6. 워커 노드 하트비트 테스트..."
HEARTBEAT_STATUS=$(curl -s http://localhost:8080/api/v1/nodes/heartbeat 2>/dev/null || echo "하트비트 API 호출 실패")
echo "   하트비트 상태: $HEARTBEAT_STATUS"

# 정리
kill $WORKER_PID 2>/dev/null
cd ..

echo ""
echo "🎯 워커 노드 연결 방법:"
echo "   1. Nautilus TEE 마스터 시작: ./nautilus-tee/nautilus-tee.exe"
echo "   2. 새 터미널에서 워커 시작: cd k3s-daas && ./k3s-daas"
echo ""
echo "🌊 워커 노드 로그 확인:"
echo "   tail -f /tmp/k3s-daas-worker.log"