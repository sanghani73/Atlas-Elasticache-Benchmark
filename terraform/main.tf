locals {
  expire_on = var.tag_expire_on != "" ? var.tag_expire_on : formatdate("YYYY-MM-DD", timeadd(timestamp(), "24h"))
}

data "aws_caller_identity" "current" {}

data "aws_availability_zones" "available" {
  state = "available"
}

# --- Providers ---

provider "aws" {
  region  = var.aws_region
  profile = var.aws_profile

  default_tags {
    tags = {
      owner     = var.tag_owner
      purpose   = var.tag_purpose
      expire-on = local.expire_on
    }
  }
}

provider "mongodbatlas" {
  public_key  = var.atlas_public_key
  private_key = var.atlas_private_key
}

# --- VPC ---

resource "aws_vpc" "benchmark" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "bench-vpc" }
}

# --- Subnets (2 AZs required for ElastiCache subnet group) ---

resource "aws_subnet" "bench_a" {
  vpc_id                  = aws_vpc.benchmark.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "bench-subnet-a" }
}

resource "aws_subnet" "bench_b" {
  vpc_id                  = aws_vpc.benchmark.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.available.names[1]
  map_public_ip_on_launch = true

  tags = { Name = "bench-subnet-b" }
}

# --- Internet Gateway ---

resource "aws_internet_gateway" "bench" {
  vpc_id = aws_vpc.benchmark.id

  tags = { Name = "bench-igw" }
}

# --- Route Table ---

resource "aws_route_table" "bench" {
  vpc_id = aws_vpc.benchmark.id

  tags = { Name = "bench-rt" }
}

resource "aws_route" "internet" {
  route_table_id         = aws_route_table.bench.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.bench.id
}

resource "aws_route_table_association" "bench_a" {
  subnet_id      = aws_subnet.bench_a.id
  route_table_id = aws_route_table.bench.id
}

resource "aws_route_table_association" "bench_b" {
  subnet_id      = aws_subnet.bench_b.id
  route_table_id = aws_route_table.bench.id
}

# --- Security Groups ---

resource "aws_security_group" "ec2" {
  name        = "bench-ec2-sg"
  description = "SSH inbound and all outbound for benchmark EC2"
  vpc_id      = aws_vpc.benchmark.id

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["${var.my_ip}/32"]
  }

  egress {
    description = "All outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "bench-ec2-sg" }
}

resource "aws_security_group" "elasticache" {
  name        = "bench-redis-sg"
  description = "Redis inbound from EC2 benchmark client"
  vpc_id      = aws_vpc.benchmark.id

  ingress {
    description     = "Redis from EC2"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.ec2.id]
  }

  egress {
    description = "All outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "bench-redis-sg" }
}
