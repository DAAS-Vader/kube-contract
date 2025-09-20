# Event-Driven K3s-DaaS Makefile
# 빌드, 테스트, 배포 자동화

.PHONY: build test clean deploy setup deps lint

# 기본 설정
BINARY_DIR=../bin
GATEWAY_BINARY=$(BINARY_DIR)/contract-api-gateway
LISTENER_BINARY=$(BINARY_DIR)/nautilus-event-listener

# Go 빌드 플래그
GO_BUILD_FLAGS=-v -ldflags "-s -w"
GO_TEST_FLAGS=-v -race -coverprofile=coverage.out

# 의존성 설치
deps:
	@echo "📦 Installing Go dependencies..."
	go mod tidy
	go mod download
	@echo "✅ Dependencies installed"

# 바이너리 빌드
build: deps
	@echo "🔨 Building binaries..."
	mkdir -p $(BINARY_DIR)
	go build $(GO_BUILD_FLAGS) -o $(GATEWAY_BINARY) contract_api_gateway.go
	go build $(GO_BUILD_FLAGS) -o $(LISTENER_BINARY) nautilus_event_listener.go
	@echo "✅ Build completed"
	@echo "   API Gateway: $(GATEWAY_BINARY)"
	@echo "   Event Listener: $(LISTENER_BINARY)"

# 개발용 빠른 빌드
build-dev:
	@echo "🚀 Quick development build..."
	go build -o api-gateway contract_api_gateway.go
	go build -o nautilus-listener nautilus_event_listener.go
	@echo "✅ Development build completed"

# 테스트 실행
test:
	@echo "🧪 Running tests..."
	go test $(GO_TEST_FLAGS) ./...
	@echo "📊 Test coverage:"
	go tool cover -func=coverage.out
	@echo "✅ Tests completed"

# 코드 린팅
lint:
	@echo "🔍 Running code linting..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "⚠️ golangci-lint not installed, using go vet"; \
		go vet ./...; \
	fi
	@echo "✅ Linting completed"

# Move Contract 테스트
test-contract:
	@echo "⛓️ Testing Move contracts..."
	cd ../contracts-release && sui move test
	@echo "✅ Contract tests completed"

# 전체 테스트 (Go + Move)
test-all: test test-contract
	@echo "🎯 All tests completed"

# 환경 설정 확인
setup:
	@echo "📋 Checking environment setup..."
	@echo "Go version: $$(go version)"
	@echo "Sui CLI: $$(sui --version 2>/dev/null || echo 'Not installed')"
	@echo "kubectl: $$(kubectl version --client --short 2>/dev/null || echo 'Not installed')"
	@echo "Docker: $$(docker --version 2>/dev/null || echo 'Not installed')"
	@echo "✅ Environment check completed"

# 로그 파일 정리
clean-logs:
	@echo "🧹 Cleaning log files..."
	rm -f *.log *.pid
	rm -f ../contracts-release/*.log
	rm -f ../nautilus-release/*.log
	rm -f ../worker-release/*.log
	@echo "✅ Log files cleaned"

# 바이너리 정리
clean-bin:
	@echo "🧹 Cleaning binaries..."
	rm -rf $(BINARY_DIR)
	rm -f api-gateway nautilus-listener
	@echo "✅ Binaries cleaned"

# 전체 정리
clean: clean-logs clean-bin
	@echo "🧹 Cleaning all generated files..."
	rm -f coverage.out
	rm -f *.json *.yaml *.txt
	go clean -cache
	@echo "✅ Clean completed"

# 통합 테스트 실행
integration-test:
	@echo "🚀 Running integration test..."
	chmod +x 5_STEP_INTEGRATION_TEST.sh
	./5_STEP_INTEGRATION_TEST.sh
	@echo "✅ Integration test completed"

# 개발 서버 시작 (백그라운드)
dev-start: build-dev
	@echo "🌟 Starting development servers..."
	@echo "Starting API Gateway..."
	./api-gateway > api-gateway.log 2>&1 &
	@echo $$! > api-gateway.pid
	@sleep 3
	@echo "Starting Nautilus Event Listener..."
	./nautilus-listener > nautilus-listener.log 2>&1 &
	@echo $$! > nautilus-listener.pid
	@sleep 3
	@echo "✅ Development servers started"
	@echo "   API Gateway PID: $$(cat api-gateway.pid)"
	@echo "   Event Listener PID: $$(cat nautilus-listener.pid)"
	@echo "   Logs: tail -f *.log"

# 개발 서버 중지
dev-stop:
	@echo "🛑 Stopping development servers..."
	@if [ -f api-gateway.pid ]; then \
		kill $$(cat api-gateway.pid) 2>/dev/null || true; \
		rm -f api-gateway.pid; \
		echo "API Gateway stopped"; \
	fi
	@if [ -f nautilus-listener.pid ]; then \
		kill $$(cat nautilus-listener.pid) 2>/dev/null || true; \
		rm -f nautilus-listener.pid; \
		echo "Event Listener stopped"; \
	fi
	@echo "✅ Development servers stopped"

# 개발 서버 재시작
dev-restart: dev-stop clean-logs dev-start
	@echo "🔄 Development servers restarted"

# 상태 확인
status:
	@echo "📊 System status:"
	@if [ -f api-gateway.pid ] && kill -0 $$(cat api-gateway.pid) 2>/dev/null; then \
		echo "✅ API Gateway: Running (PID: $$(cat api-gateway.pid))"; \
	else \
		echo "❌ API Gateway: Not running"; \
	fi
	@if [ -f nautilus-listener.pid ] && kill -0 $$(cat nautilus-listener.pid) 2>/dev/null; then \
		echo "✅ Event Listener: Running (PID: $$(cat nautilus-listener.pid))"; \
	else \
		echo "❌ Event Listener: Not running"; \
	fi
	@echo "Ports:"
	@echo "  - API Gateway: http://localhost:8080"
	@echo "  - Event Listener: http://localhost:10250"

# 로그 실시간 확인
logs:
	@echo "📄 Showing recent logs..."
	@echo "=== API Gateway Logs ==="
	@tail -20 api-gateway.log 2>/dev/null || echo "No API Gateway logs"
	@echo ""
	@echo "=== Event Listener Logs ==="
	@tail -20 nautilus-listener.log 2>/dev/null || echo "No Event Listener logs"

# 로그 실시간 스트림
logs-follow:
	@echo "📄 Following logs (Ctrl+C to stop)..."
	tail -f api-gateway.log nautilus-listener.log

# 헬스체크
health:
	@echo "🏥 Health check..."
	@echo "API Gateway:"
	@curl -s http://localhost:8080/healthz || echo "  ❌ API Gateway not responding"
	@echo ""
	@echo "Event Listener:"
	@curl -s http://localhost:10250/health || echo "  ❌ Event Listener not responding"

# kubectl 설정
setup-kubectl:
	@echo "⚙️ Setting up kubectl for K3s-DaaS..."
	kubectl config set-cluster k3s-daas \
		--server=http://localhost:8080 \
		--insecure-skip-tls-verify=true
	kubectl config set-credentials k3s-daas-user \
		--token="seal_test_token_$(shell date +%s)"
	kubectl config set-context k3s-daas \
		--cluster=k3s-daas \
		--user=k3s-daas-user
	kubectl config use-context k3s-daas
	@echo "✅ kubectl configured for K3s-DaaS"

# 빠른 테스트
quick-test: dev-start
	@echo "⚡ Quick functionality test..."
	@sleep 5
	@echo "Testing API Gateway..."
	@curl -s http://localhost:8080/healthz || echo "API Gateway test failed"
	@echo "Testing Event Listener..."
	@curl -s http://localhost:10250/health || echo "Event Listener test failed"
	@echo "✅ Quick test completed"

# 프로덕션 빌드
build-prod:
	@echo "🏭 Production build..."
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo $(GO_BUILD_FLAGS) -o $(GATEWAY_BINARY) contract_api_gateway.go
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo $(GO_BUILD_FLAGS) -o $(LISTENER_BINARY) nautilus_event_listener.go
	@echo "✅ Production build completed"

# Docker 이미지 빌드 (향후)
docker-build:
	@echo "🐳 Building Docker images..."
	@echo "TODO: Implement Docker build"

# 도움말
help:
	@echo "📋 Available commands:"
	@echo ""
	@echo "🔨 Build commands:"
	@echo "  make build          - Build production binaries"
	@echo "  make build-dev      - Quick development build"
	@echo "  make build-prod     - Production build with optimizations"
	@echo ""
	@echo "🧪 Test commands:"
	@echo "  make test           - Run Go unit tests"
	@echo "  make test-contract  - Run Move contract tests"
	@echo "  make test-all       - Run all tests"
	@echo "  make integration-test - Run full integration test"
	@echo ""
	@echo "🌟 Development commands:"
	@echo "  make dev-start      - Start development servers"
	@echo "  make dev-stop       - Stop development servers"
	@echo "  make dev-restart    - Restart development servers"
	@echo "  make quick-test     - Quick functionality test"
	@echo ""
	@echo "📊 Monitoring commands:"
	@echo "  make status         - Show system status"
	@echo "  make health         - Run health checks"
	@echo "  make logs           - Show recent logs"
	@echo "  make logs-follow    - Follow logs in real-time"
	@echo ""
	@echo "🧹 Maintenance commands:"
	@echo "  make clean          - Clean all generated files"
	@echo "  make clean-logs     - Clean log files only"
	@echo "  make clean-bin      - Clean binaries only"
	@echo ""
	@echo "⚙️ Setup commands:"
	@echo "  make setup          - Check environment setup"
	@echo "  make deps           - Install dependencies"
	@echo "  make setup-kubectl  - Configure kubectl"
	@echo "  make lint           - Run code linting"

# 기본 타겟
all: clean deps lint test build
	@echo "🎉 Full build completed successfully!"