#!/bin/bash
# AWS EC2 + Nitro Enclave Setup for K3s-DaaS Testing

set -e

echo "🚀 Setting up K3s-DaaS Test Environment on AWS"

# AWS EC2 Instance Requirements
echo "📋 AWS Instance Requirements:"
echo "   - Instance Type: m5.xlarge or larger (Nitro Enclave support)"
echo "   - OS: Amazon Linux 2 or Ubuntu 22.04"
echo "   - VPC: Public subnet with internet gateway"
echo "   - Security Groups: Ports 6443, 8080, 9000, 10250"

# 1. Enable Nitro Enclaves
echo "🔒 Enabling Nitro Enclaves..."
sudo yum update -y
sudo yum install -y aws-nitro-enclaves-cli aws-nitro-enclaves-cli-devel

# Enable Nitro Enclave allocator service
sudo systemctl enable --now nitro-enclaves-allocator.service

# Allocate resources for enclaves
echo "💾 Allocating resources for Nitro Enclaves..."
sudo sh -c 'echo "memory_mib=1024" >> /etc/nitro_enclaves/allocator.yaml'
sudo sh -c 'echo "cpu_count=2" >> /etc/nitro_enclaves/allocator.yaml'
sudo systemctl restart nitro-enclaves-allocator.service

# 2. Install Docker
echo "🐳 Installing Docker..."
sudo yum install -y docker
sudo systemctl enable --now docker
sudo usermod -aG docker $USER

# 3. Install Go
echo "🔧 Installing Go..."
cd /tmp
wget https://golang.org/dl/go1.21.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# 4. Install Sui CLI
echo "🌊 Installing Sui CLI..."
cargo install --git https://github.com/MystenLabs/sui.git --tag testnet sui

# 5. Setup Sui Testnet Wallet
echo "💰 Setting up Sui Testnet Wallet..."
sui client new-env --alias testnet --rpc https://fullnode.testnet.sui.io:443
sui client switch --env testnet
sui client new-address ed25519

echo "💰 Get testnet SUI from faucet:"
echo "   https://discord.com/channels/916379725201563759/1037811694564560966"
echo "   !faucet <your-sui-address>"

# 6. Clone K3s-DaaS
echo "📥 Cloning K3s-DaaS..."
git clone <your-repo-url> /opt/k3s-daas
cd /opt/k3s-daas

# 7. Build Nautilus TEE (Nitro Enclave)
echo "🔒 Building Nautilus TEE for Nitro Enclaves..."
cd nautilus-tee
cat > Dockerfile.nitro << 'EOF'
FROM amazonlinux:2

RUN yum update -y && yum install -y \
    go \
    gcc \
    openssl-devel

WORKDIR /app
COPY . .
RUN go build -o nautilus-tee main.go

EXPOSE 8080
CMD ["./nautilus-tee"]
EOF

# Build enclave image
docker build -f Dockerfile.nitro -t nautilus-tee .
nitro-cli build-enclave --docker-uri nautilus-tee --output-file nautilus-tee.eif

# 8. Setup Worker Node
echo "🖥️  Setting up K3s-DaaS Worker Node..."
cd ../k3s-daas

# Create staker config with actual values
cat > staker-config.json << EOF
{
  "node_id": "aws-worker-$(hostname)",
  "sui_wallet_address": "$(sui client active-address)",
  "sui_private_key": "$(cat ~/.sui/sui_config/sui.keystore/* | head -1)",
  "sui_rpc_endpoint": "https://fullnode.testnet.sui.io:443",
  "stake_amount": 1000,
  "contract_address": "REPLACE_WITH_DEPLOYED_CONTRACT_ADDRESS",
  "min_stake_amount": 1000
}
EOF

# Build worker node
go build -o k3s-daas main.go

echo "✅ AWS Setup Complete!"
echo ""
echo "🎯 Next Steps:"
echo "1. Deploy smart contracts to Sui testnet"
echo "2. Update CONTRACT_ADDRESS in staker-config.json"
echo "3. Get testnet SUI from Discord faucet"
echo "4. Start Nautilus TEE: nitro-cli run-enclave --eif-path nautilus-tee.eif --memory 1024 --cpu-count 2"
echo "5. Start worker node: ./k3s-daas"
echo ""
echo "🔍 Useful Commands:"
echo "   - Check enclave: nitro-cli describe-enclaves"
echo "   - Worker logs: journalctl -f -u k3s-daas"
echo "   - Sui balance: sui client gas"