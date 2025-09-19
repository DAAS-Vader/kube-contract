# 🚀 EC2에서 K3s-DaaS 워커 노드 실행하기

## 1. EC2 인스턴스 준비

### 권장 사양:
- **인스턴스 타입**: `t3.medium` 이상 (2 vCPU, 4GB RAM)
- **OS**: Ubuntu 22.04 LTS
- **스토리지**: 20GB+ EBS
- **보안 그룹**: 다음 포트 오픈
  - 22 (SSH)
  - 6443 (K3s API)
  - 10250 (Kubelet)
  - 8472 (Flannel VXLAN)

## 2. 파일 업로드

### Windows에서 빌드 후 업로드:
```bash
# 1. Linux용 빌드
./build-linux.sh

# 2. EC2로 파일 업로드
scp -i your-key.pem k3s-daas-linux ubuntu@your-ec2-ip:~/
scp -i your-key.pem staker-config.json ubuntu@your-ec2-ip:~/
scp -i your-key.pem ec2-setup.sh ubuntu@your-ec2-ip:~/
```

## 3. EC2에서 설정

### SSH 접속:
```bash
ssh -i your-key.pem ubuntu@your-ec2-ip
```

### 시스템 설정:
```bash
# 1. 설정 스크립트 실행
chmod +x ec2-setup.sh
./ec2-setup.sh

# 2. 로그아웃 후 재접속 (Docker 그룹 권한 적용)
exit
ssh -i your-key.pem ubuntu@your-ec2-ip
```

## 4. 설정 파일 편집

```bash
# staker-config.json 편집
nano staker-config.json
```

### 필수 설정 항목:
```json
{
  "node_id": "my-ec2-worker-01",
  "sui_wallet_address": "0x실제_지갑_주소",
  "sui_private_key": "실제_프라이빗_키",
  "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
  "stake_amount": 1000,
  "contract_address": "배포된_컨트랙트_주소",
  "nautilus_endpoint": "http://nautilus-tee-ip:8080",
  "container_runtime": "docker",
  "min_stake_amount": 1000
}
```

## 5. 워커 노드 실행

```bash
# 1. 실행 권한 부여
chmod +x k3s-daas-linux

# 2. 백그라운드 실행
nohup ./k3s-daas-linux > k3s-daas.log 2>&1 &

# 3. 로그 확인
tail -f k3s-daas.log
```

## 6. 상태 확인

### 컨테이너 런타임 확인:
```bash
# Docker 사용 시
docker ps

# Containerd 사용 시
sudo ctr -n k8s.io containers list
```

### 로그 모니터링:
```bash
# 실시간 로그 확인
tail -f k3s-daas.log

# 특정 키워드 검색
grep "스테이킹" k3s-daas.log
grep "하트비트" k3s-daas.log
```

## 7. 트러블슈팅

### 일반적인 문제들:

1. **Docker 권한 문제**:
   ```bash
   sudo usermod -aG docker $USER
   # 재로그인 필요
   ```

2. **Containerd 소켓 문제**:
   ```bash
   sudo systemctl restart containerd
   ```

3. **방화벽 문제**:
   ```bash
   sudo ufw status
   sudo ufw allow [필요한_포트]
   ```

4. **메모리 부족**:
   ```bash
   free -h
   # 인스턴스 타입 업그레이드 고려
   ```

## 8. 자동 시작 설정

### Systemd 서비스 생성:
```bash
sudo tee /etc/systemd/system/k3s-daas.service > /dev/null <<EOF
[Unit]
Description=K3s-DaaS Worker Node
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu
ExecStart=/home/ubuntu/k3s-daas-linux
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable k3s-daas
sudo systemctl start k3s-daas
```

## 9. 모니터링

### 상태 확인:
```bash
# 서비스 상태
sudo systemctl status k3s-daas

# 리소스 사용량
htop
df -h
```

이제 EC2 인스턴스가 완전한 K3s 워커 노드로 작동합니다! 🎉