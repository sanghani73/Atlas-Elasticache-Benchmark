# --- VPC ---

output "vpc_id" {
  description = "ID of the benchmark VPC"
  value       = aws_vpc.benchmark.id
}

output "vpc_peering_status" {
  description = "Status of the Atlas VPC peering connection"
  value       = mongodbatlas_network_peering.benchmark.status_name
}

# --- EC2 ---

output "ec2_public_ip" {
  description = "Public IP of the benchmark EC2 instance"
  value       = aws_instance.benchmark.public_ip
}

output "ec2_instance_id" {
  description = "Instance ID of the benchmark EC2 instance"
  value       = aws_instance.benchmark.id
}

output "ssh_private_key_path" {
  description = "Path to the SSH private key file"
  value       = local_sensitive_file.ssh_key.filename
}

# --- Atlas ---

output "atlas_connection_string_private" {
  description = "Atlas private SRV connection string (via VPC peering)"
  value       = try(mongodbatlas_advanced_cluster.benchmark.connection_strings.private_srv, "")
  sensitive   = true
}

output "atlas_connection_string_public" {
  description = "Atlas standard SRV connection string (public)"
  value       = try(mongodbatlas_advanced_cluster.benchmark.connection_strings.standard_srv, "")
  sensitive   = true
}

output "atlas_mongodb_version" {
  description = "MongoDB version running on the Atlas cluster"
  value       = mongodbatlas_advanced_cluster.benchmark.mongo_db_version
}

# --- ElastiCache Redis ---

output "redis_endpoint" {
  description = "Primary endpoint of the Redis cluster"
  value       = aws_elasticache_replication_group.bench.primary_endpoint_address
}
