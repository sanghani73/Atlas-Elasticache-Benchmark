# --- SSH Key Pair ---

resource "tls_private_key" "ssh" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "aws_key_pair" "benchmark" {
  key_name   = var.key_name
  public_key = tls_private_key.ssh.public_key_openssh
}

resource "local_sensitive_file" "ssh_key" {
  content         = tls_private_key.ssh.private_key_pem
  filename        = "${abspath(path.module)}/bench-key.pem"
  file_permission = "0400"
}

# --- AMI Lookup (Amazon Linux 2023, x86_64) ---

data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "architecture"
    values = ["x86_64"]
  }
}

# --- EC2 Instance ---

resource "aws_instance" "benchmark" {
  ami                         = data.aws_ami.al2023.id
  instance_type               = var.instance_type
  key_name                    = aws_key_pair.benchmark.key_name
  subnet_id                   = aws_subnet.bench_a.id
  vpc_security_group_ids      = [aws_security_group.ec2.id]
  associate_public_ip_address = true

  user_data = file("${path.module}/userdata.sh")

  root_block_device {
    volume_size = 50
    volume_type = "gp3"
  }

  tags = { Name = "bench-client" }
}
