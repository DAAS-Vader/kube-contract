package main

import (
	"fmt"
	"log"
	"time"
)

/*
validateConfig - K3s Agent 설정 검증 함수
kubelet이 시작하기 전에 필수 설정들이 올바르게 구성되었는지 확인합니다.
*/
func (k *Kubelet) validateConfig() error {
	if k.nodeID == "" {
		return fmt.Errorf("nodeID가 설정되지 않았습니다")
	}

	if k.masterURL == "" {
		return fmt.Errorf("master URL이 설정되지 않았습니다")
	}

	if k.token == "" {
		return fmt.Errorf("K3s join token이 설정되지 않았습니다")
	}

	if k.dataDir == "" {
		return fmt.Errorf("데이터 디렉토리가 설정되지 않았습니다")
	}

	return nil
}

/*
Stop - Kubelet 중지 함수
실행 중인 K3s agent 프로세스를 안전하게 중지합니다.
*/
func (k *Kubelet) Stop() error {
	log.Printf("🛑 K3s Agent 중지 중...")

	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.running {
		return nil
	}

	// context 취소로 프로세스 종료
	if k.cancel != nil {
		k.cancel()
	}

	// K3s agent 프로세스가 있다면 종료 대기
	if k.cmd != nil && k.cmd.Process != nil {
		log.Printf("K3s Agent 프로세스 종료 대기 중... PID: %d", k.cmd.Process.Pid)

		// 5초 대기 후 강제 종료
		done := make(chan error, 1)
		go func() {
			done <- k.cmd.Wait()
		}()

		select {
		case err := <-done:
			if err != nil {
				log.Printf("K3s Agent 프로세스 종료: %v", err)
			}
		case <-time.After(5 * time.Second):
			log.Printf("⚠️ K3s Agent 프로세스 강제 종료")
			k.cmd.Process.Kill()
		}
	}

	k.running = false
	log.Printf("✅ K3s Agent 중지 완료")
	return nil
}

/*
healthCheck - K3s Agent 상태 확인 함수
agent 프로세스가 정상적으로 실행되고 있는지 확인합니다.
*/
func (k *Kubelet) healthCheck() error {
	k.mu.RLock()
	defer k.mu.RUnlock()

	if !k.running {
		return fmt.Errorf("K3s Agent가 실행되고 있지 않습니다")
	}

	if k.cmd == nil {
		return fmt.Errorf("K3s Agent 프로세스가 설정되지 않았습니다")
	}

	if k.cmd.Process == nil {
		return fmt.Errorf("K3s Agent 프로세스가 시작되지 않았습니다")
	}

	// 프로세스 상태 확인 (Unix에서만 가능)
	if k.ctx != nil {
		select {
		case <-k.ctx.Done():
			return fmt.Errorf("K3s Agent 컨텍스트가 취소되었습니다")
		default:
			// 정상 실행 중
		}
	}

	return nil
}

/*
restart - K3s Agent 재시작 함수
오류 발생 시 agent를 안전하게 재시작합니다.
*/
func (k *Kubelet) restart() error {
	log.Printf("🔄 K3s Agent 재시작 중...")

	// 기존 agent 중지
	if err := k.Stop(); err != nil {
		log.Printf("⚠️ Agent 중지 중 오류: %v", err)
	}

	// 잠시 대기
	time.Sleep(5 * time.Second)

	// agent 재시작
	if err := k.Start(); err != nil {
		return fmt.Errorf("Agent 재시작 실패: %v", err)
	}

	log.Printf("✅ K3s Agent 재시작 완료")
	return nil
}