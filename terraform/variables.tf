# --- AWS ---

variable "aws_profile" {
  description = "AWS SSO profile name"
  type        = string
}

variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "eu-west-2"
}

variable "my_ip" {
  description = "Your public IPv4 address for SSH access"
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for the benchmark client"
  type        = string
  default     = "c5.2xlarge"
}

variable "key_name" {
  description = "Name for the EC2 key pair"
  type        = string
  default     = "bench-key"
}

# --- AWS Tags ---

variable "tag_owner" {
  description = "Value for the 'owner' tag on all AWS resources"
  type        = string
}

variable "tag_purpose" {
  description = "Value for the 'purpose' tag on all AWS resources"
  type        = string
  default     = "benchmark"
}

variable "tag_expire_on" {
  description = "Value for the 'expire-on' tag (YYYY-MM-DD). If empty, defaults to tomorrow."
  type        = string
  default     = ""
}

# --- Atlas ---

variable "atlas_public_key" {
  description = "MongoDB Atlas API public key"
  type        = string
  sensitive   = true
}

variable "atlas_private_key" {
  description = "MongoDB Atlas API private key"
  type        = string
  sensitive   = true
}

variable "atlas_project_id" {
  description = "MongoDB Atlas project ID"
  type        = string
}

variable "atlas_cluster_name" {
  description = "Name for the Atlas benchmark cluster"
  type        = string
  default     = "bench-atlas-m30"
}

variable "atlas_instance_size" {
  description = "Atlas cluster instance size (e.g. M30, M40)"
  type        = string
  default     = "M30"
}

variable "atlas_region" {
  description = "Atlas region name (e.g. EU_WEST_1)"
  type        = string
  default     = "EU_WEST_2"
}

# --- Database ---

variable "db_username" {
  description = "Database username for benchmark access"
  type        = string
  default     = "benchuser"
}

variable "db_password" {
  description = "Database password for benchmark access"
  type        = string
  sensitive   = true
}
