# --- Atlas Cluster ---

resource "mongodbatlas_advanced_cluster" "benchmark" {
  project_id             = var.atlas_project_id
  name                   = var.atlas_cluster_name
  cluster_type           = "REPLICASET"
  mongo_db_major_version = "8.0"

  replication_specs = [
    {
      region_configs = [
        {
          provider_name = "AWS"
          region_name   = var.atlas_region
          priority      = 7
          electable_specs = {
            instance_size = var.atlas_instance_size
            node_count    = 3
          }
        }
      ]
    }
  ]
}

# --- Database User ---

resource "mongodbatlas_database_user" "benchmark" {
  project_id         = var.atlas_project_id
  auth_database_name = "admin"
  username           = var.db_username
  password           = var.db_password

  roles {
    role_name     = "readWriteAnyDatabase"
    database_name = "admin"
  }

  scopes {
    name = mongodbatlas_advanced_cluster.benchmark.name
    type = "CLUSTER"
  }
}

# --- IP Access List ---

resource "mongodbatlas_project_ip_access_list" "ec2_ip" {
  project_id = var.atlas_project_id
  ip_address = aws_instance.benchmark.public_ip
  comment    = "Benchmark EC2 public IP (fallback)"
}

resource "mongodbatlas_project_ip_access_list" "vpc_cidr" {
  project_id = var.atlas_project_id
  cidr_block = aws_vpc.benchmark.cidr_block
  comment    = "AWS VPC CIDR via peering"
}
