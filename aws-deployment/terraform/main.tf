# AWS Infrastructure for K3s-DaaS Testing
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# Variables
variable "aws_region" {
  description = "AWS region for deployment"
  default     = "us-west-2"
}

variable "key_pair_name" {
  description = "EC2 Key Pair name"
  type        = string
}

# VPC and Networking
resource "aws_vpc" "k3s_daas_vpc" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = "k3s-daas-vpc"
    Project = "K3s-DaaS-Testing"
  }
}

resource "aws_internet_gateway" "k3s_daas_igw" {
  vpc_id = aws_vpc.k3s_daas_vpc.id

  tags = {
    Name = "k3s-daas-igw"
  }
}

resource "aws_subnet" "k3s_daas_subnet" {
  vpc_id                  = aws_vpc.k3s_daas_vpc.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = "${var.aws_region}a"
  map_public_ip_on_launch = true

  tags = {
    Name = "k3s-daas-subnet"
  }
}

resource "aws_route_table" "k3s_daas_rt" {
  vpc_id = aws_vpc.k3s_daas_vpc.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.k3s_daas_igw.id
  }

  tags = {
    Name = "k3s-daas-rt"
  }
}

resource "aws_route_table_association" "k3s_daas_rta" {
  subnet_id      = aws_subnet.k3s_daas_subnet.id
  route_table_id = aws_route_table.k3s_daas_rt.id
}

# Security Groups
resource "aws_security_group" "nautilus_tee_sg" {
  name_prefix = "nautilus-tee-"
  vpc_id      = aws_vpc.k3s_daas_vpc.id

  # Nautilus TEE API
  ingress {
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Nautilus TEE API"
  }

  # K8s API Server
  ingress {
    from_port   = 6443
    to_port     = 6443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Kubernetes API Server"
  }

  # SSH
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SSH"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "nautilus-tee-sg"
  }
}

resource "aws_security_group" "staker_host_sg" {
  name_prefix = "staker-host-"
  vpc_id      = aws_vpc.k3s_daas_vpc.id

  # Kubelet
  ingress {
    from_port   = 10250
    to_port     = 10250
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
    description = "Kubelet API"
  }

  # Status Server
  ingress {
    from_port   = 10250
    to_port     = 10250
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Staker Status API"
  }

  # SSH
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SSH"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "staker-host-sg"
  }
}

# EC2 Instances
resource "aws_instance" "nautilus_tee" {
  ami                    = data.aws_ami.amazon_linux.id
  instance_type          = "m5.xlarge"  # Nitro Enclave support
  key_name              = var.key_pair_name
  vpc_security_group_ids = [aws_security_group.nautilus_tee_sg.id]
  subnet_id             = aws_subnet.k3s_daas_subnet.id

  # Enable Nitro Enclaves
  enclave_options {
    enabled = true
  }

  user_data = file("${path.module}/../aws-setup.sh")

  tags = {
    Name = "k3s-daas-nautilus-tee"
    Role = "nautilus-tee"
    Project = "K3s-DaaS-Testing"
  }
}

resource "aws_instance" "staker_host" {
  ami                    = data.aws_ami.amazon_linux.id
  instance_type          = "t3.medium"
  key_name              = var.key_pair_name
  vpc_security_group_ids = [aws_security_group.staker_host_sg.id]
  subnet_id             = aws_subnet.k3s_daas_subnet.id

  user_data = file("${path.module}/../aws-setup.sh")

  tags = {
    Name = "k3s-daas-staker-host"
    Role = "staker-host"
    Project = "K3s-DaaS-Testing"
  }
}

# Data Sources
data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["amzn2-ami-hvm-*-x86_64-gp2"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# Outputs
output "nautilus_tee_public_ip" {
  value = aws_instance.nautilus_tee.public_ip
  description = "Public IP of Nautilus TEE instance"
}

output "staker_host_public_ip" {
  value = aws_instance.staker_host.public_ip
  description = "Public IP of Staker Host instance"
}

output "nautilus_tee_url" {
  value = "https://${aws_instance.nautilus_tee.public_ip}:6443"
  description = "Nautilus TEE Kubernetes API URL"
}

output "setup_commands" {
  value = <<EOF
# SSH to instances:
ssh -i your-key.pem ec2-user@${aws_instance.nautilus_tee.public_ip}
ssh -i your-key.pem ec2-user@${aws_instance.staker_host.public_ip}

# Check Nautilus TEE:
curl http://${aws_instance.nautilus_tee.public_ip}:8080/health

# Check Staker Host:
curl http://${aws_instance.staker_host.public_ip}:10250/health
EOF
}