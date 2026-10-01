# --- Parameter Group (LRU eviction for over-memory tests) ---

resource "aws_elasticache_parameter_group" "bench" {
  name   = "bench-redis-params"
  family = "redis7"

  parameter {
    name  = "maxmemory-policy"
    value = "allkeys-lru"
  }

  tags = { Name = "bench-redis-params" }
}

# --- Subnet Group ---

resource "aws_elasticache_subnet_group" "bench" {
  name       = "bench-redis-subnets"
  subnet_ids = [aws_subnet.bench_a.id, aws_subnet.bench_b.id]

  tags = { Name = "bench-redis-subnets" }
}

# --- Redis (cache.r6g.large, 13.07 GB, 2 vCPU) ---

resource "aws_elasticache_replication_group" "bench" {
  replication_group_id = "bench-redis"
  description          = "Benchmark Redis (cache.r6g.large, 13GB)"
  node_type            = "cache.r6g.large"
  num_cache_clusters   = 2
  engine               = "redis"
  engine_version       = "7.1"
  port                 = 6379

  subnet_group_name    = aws_elasticache_subnet_group.bench.name
  security_group_ids   = [aws_security_group.elasticache.id]
  parameter_group_name = aws_elasticache_parameter_group.bench.name

  automatic_failover_enabled = false
  multi_az_enabled           = false
  at_rest_encryption_enabled = false
  transit_encryption_enabled = false

  tags = { Name = "bench-redis" }
}
