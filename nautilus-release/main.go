// Nautilus Control - K3s 마스터 노드 (핵심 기능만)
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
)

func main() {
	// 로거 초기화
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Context 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Info("🚀 Nautilus Control starting...")

	// K3s Manager 초기화
	k3sMgr := NewK3sManager(logger)

	// API Server 초기화
	apiServer := NewAPIServer(logger, k3sMgr)

	// Sui Integration 초기화
	suiIntegration := NewSuiIntegration(logger, k3sMgr)

	// 컴포넌트 시작
	go k3sMgr.Start(ctx)
	go apiServer.Start(ctx)
	go suiIntegration.Start(ctx)

	logger.Info("✅ All components started")

	// 우아한 종료 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Infof("🛑 Received signal %v, shutting down...", sig)

	cancel()
	logger.Info("✅ Nautilus Control stopped")
}