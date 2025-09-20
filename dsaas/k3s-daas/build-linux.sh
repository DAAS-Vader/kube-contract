#!/bin/bash

echo "🏗️  Linux용 k3s-daas 빌드 중..."

# Linux용 크로스 컴파일
GOOS=linux GOARCH=amd64 go build -o k3s-daas-linux main.go

if [ $? -eq 0 ]; then
    echo "✅ Linux 빌드 성공: k3s-daas-linux"
    echo ""
    echo "EC2 업로드 방법:"
    echo "scp -i your-key.pem k3s-daas-linux ubuntu@your-ec2-ip:~/"
    echo "scp -i your-key.pem staker-config.json ubuntu@your-ec2-ip:~/"
    echo "scp -i your-key.pem ec2-setup.sh ubuntu@your-ec2-ip:~/"
else
    echo "❌ 빌드 실패"
    exit 1
fi