# --- Atlas Network Container ---
# If this errors with OVERLAPPING_ATLAS_CIDR_BLOCK, import the existing container:
#   terraform import mongodbatlas_network_container.benchmark <project_id>-<container_id>

resource "mongodbatlas_network_container" "benchmark" {
  project_id       = var.atlas_project_id
  provider_name    = "AWS"
  region_name      = var.atlas_region
  atlas_cidr_block = "192.168.248.0/21"
}

# --- Atlas VPC Peering ---

resource "mongodbatlas_network_peering" "benchmark" {
  project_id             = var.atlas_project_id
  container_id           = mongodbatlas_network_container.benchmark.id
  provider_name          = "AWS"
  accepter_region_name   = var.aws_region
  aws_account_id         = data.aws_caller_identity.current.account_id
  vpc_id                 = aws_vpc.benchmark.id
  route_table_cidr_block = aws_vpc.benchmark.cidr_block
}

# --- AWS Peering Accepter ---

resource "aws_vpc_peering_connection_accepter" "atlas" {
  vpc_peering_connection_id = mongodbatlas_network_peering.benchmark.connection_id
  auto_accept               = true

  tags = { Name = "atlas-peering" }
}

# --- Route to Atlas CIDR via peering ---

resource "aws_route" "atlas_cidr" {
  route_table_id            = aws_route_table.bench.id
  destination_cidr_block    = mongodbatlas_network_container.benchmark.atlas_cidr_block
  vpc_peering_connection_id = aws_vpc_peering_connection_accepter.atlas.vpc_peering_connection_id
}
