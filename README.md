# MongoDB Atlas vs AWS ElastiCache Redis -- Performance Benchmark

A fully automated, end-to-end performance comparison between **MongoDB Atlas** and **AWS ElastiCache Redis**. All infrastructure is provisioned via Terraform, benchmarks are driven by a custom Go tool, and results are compiled into a markdown report -- ready to run against any AWS account and Atlas project.

## Why This Exists

Customers frequently ask how MongoDB Atlas compares to Redis for caching and low-latency workloads. This benchmark provides a fair, reproducible answer by running identical access patterns against both systems from the same EC2 client, in the same region, over the same network path.

The value lies in the **relative comparison**, not the absolute numbers. Both systems run the same workloads, same dataset, same concurrency levels, and same measurement methodology.

## Before You Add a Cache

It's common for teams to introduce an in-memory caching layer like Redis without first quantifying their actual latency or throughput requirements. The assumption is that "faster is better", but every additional data layer adds operational complexity: cache invalidation logic, consistency guarantees, extra infrastructure to provision and monitor, and a larger failure surface. All of this translates directly into engineering cost and cognitive overhead.

The purpose of this benchmark is to show the **relative** performance between MongoDB Atlas and ElastiCache Redis -- but the choice of technology should always be driven by **business requirements**, not benchmarks in isolation.

For example: your application may need low latency, but is 20ms good enough, or do you need to pay the additional tax -- in cost, complexity, and maintenance burden -- to bring it down to 5ms? If so, what is the business value that drives that requirement? A 15ms improvement may be critical for a real-time trading platform but irrelevant for a back-office dashboard. Understanding the answer to that question before reaching for another data layer can save significant complexity and cost.

The results here show that MongoDB Atlas delivers single-digit millisecond latency for many access patterns out of the box. If that meets your requirements, you may not need a separate caching tier at all.

## What It Tests

Six workloads cover common data access patterns:

| Workload | Description | MongoDB | Redis |
|----------|-------------|---------|-------|
| **Point Lookup** | Single-document fetch by primary key | `findOne({_id})` | `GET record:{id}` |
| **Secondary Lookup** | Fetch a customer's recent records via secondary index | `find({customerId}).sort().limit(20)` | `SMEMBERS` + `MGET` |
| **Filtered Query** | Customer-scoped compound filter (status + priority) | `find({customerId, status, priority}).limit(20)` | `SINTER` (3 sets) + `MGET` |
| **Covered Query** | Index-only query returning projected fields | `find().project({...})` | `SMEMBERS` + `MGET` (no equivalent optimisation) |
| **Range Scan** | Time-range query over a 1-hour window | `find({createdAt: {$gte, $lt}}).limit(100)` | `ZRANGEBYSCORE` + `MGET` |
| **Mixed Read/Write** | 60% reads, 25% updates, 15% inserts | Native operations | `GET`/`SET` + pipeline index maintenance |

Each workload runs at 3 concurrency levels (32, 128, 256 threads) across 2 targets:

- **MongoDB Atlas M30** -- 3-node replica set, 8 GB RAM (~4 GB WiredTiger cache), $0.59/hr
- **ElastiCache Redis** -- cache.r6g.large (Primary + Replica), 13.07 GB RAM, $0.48/hr

These targets provide both approximate cost parity ($0.59 vs $0.48/hr) and memory parity (MongoDB's ~4 GB working-set cache vs Redis's 13 GB).

Two dataset sizes exercise different memory regimes:

- **5M records** (~5 GB) -- fits entirely in memory for both targets
- **20M records** (~20 GB) -- exceeds RAM on both targets, forcing disk paging (MongoDB) or LRU eviction (Redis)

## Architecture

```
+------------------+       VPC Peering       +-------------------+
|  MongoDB Atlas   | <---------------------> |                   |
|  (M30 cluster)   |                         |   AWS VPC         |
+------------------+                         |   10.0.0.0/16     |
                                             |                   |
                                             | +---------------+ |
                                             | | EC2 c5.2xlarge| |
                                             | | (bench client)| |
                                             | +---------------+ |
                                             |        |          |
                                             | +---------------+ |
                                             | | ElastiCache   | |
                                             | | Redis 7.1     | |
                                             | | (r6g.large)   | |
                                             | +---------------+ |
                                             +-------------------+
```

All infrastructure lives in a single self-managed VPC. Atlas connects via VPC peering; ElastiCache runs in the same VPC. The benchmark client is a `c5.2xlarge` EC2 instance with enough CPU and network capacity to saturate both targets.

## Project Structure

```
.
├── orchestrate.sh              # Main entry point -- 12-phase automated pipeline
├── config.env.template         # Configuration template (copy to config.env)
├── terraform/
│   ├── main.tf                 # VPC, subnets, IGW, route tables, security groups
│   ├── atlas.tf                # Atlas cluster, DB user, IP access list
│   ├── peering.tf              # Atlas VPC peering + AWS accepter + routes
│   ├── elasticache.tf          # Redis replication group (cache.r6g.large)
│   ├── ec2.tf                  # Benchmark client instance + SSH key
│   ├── variables.tf            # Input variables
│   ├── outputs.tf              # Connection strings, IPs, endpoints
│   ├── versions.tf             # Provider version constraints
│   └── userdata.sh             # Minimal cloud-init (boot marker only)
├── benchmark/
│   ├── main.go                 # CLI: seed, run, run-all commands
│   ├── config.go               # Shared types and constants
│   ├── operations.go           # Operation interface + workload registry
│   ├── mongodb_ops.go          # MongoDB workload implementations
│   ├── redis_ops.go            # Redis workload implementations
│   ├── seed.go                 # Data seeding for MongoDB and Redis
│   ├── datagen.go              # Record generation, Zipfian distribution
│   ├── engine.go               # Benchmark engine (goroutine pool, HDR histograms)
│   ├── reporter.go             # CSV output and seed reports
│   └── go.mod                  # Go module definition
├── analysis/
│   └── generate_report.py      # Markdown report generator from CSV results
└── results/                    # Benchmark output (CSV data + generated report)
    ├── small/results.csv
    ├── overmemory/results.csv
    └── report.md
```

## Prerequisites

- **AWS CLI** configured with an SSO or IAM profile
- **Terraform** >= 1.0
- **MongoDB Atlas** project with an API key (Project Owner or Org-level with project access)
- **Go** >= 1.22 (installed on EC2 automatically)
- **Python 3** (installed on EC2 automatically; needed for report generation)

## Quick Start

1. **Clone and configure:**

   ```bash
   git clone <this-repo>
   cd Atlas-vs-ElastiCache-Benchmark
   cp config.env.template config.env
   # Edit config.env with your AWS profile, Atlas API keys, and project ID
   ```

2. **Run the full benchmark:**

   ```bash
   bash orchestrate.sh
   ```

   This single command executes the entire 12-phase pipeline:

   | Phase | Description | Duration |
   |-------|-------------|----------|
   | 1 | Validate configuration | ~5s |
   | 2 | Terraform apply (VPC, Atlas, ElastiCache, EC2) | ~15 min |
   | 3 | Wait for EC2 + install dependencies via SSH | ~3 min |
   | 4 | Upload benchmark code + compile | ~1 min |
   | 5 | Network RTT measurements | ~1 min |
   | 6 | Seed small dataset (5M records) | ~15 min |
   | 7 | Run benchmarks -- small dataset | ~2 hr |
   | 8 | Seed over-memory dataset (20M records, incremental) | ~45 min |
   | 9 | Run benchmarks -- over-memory dataset | ~2 hr |
   | 10 | Download results | ~10s |
   | 11 | Generate report | ~5s |
   | 12 | Summary + cleanup prompt | ~5s |

   **Total wall-clock time: ~5-6 hours**

3. **Review results:**

   ```bash
   cat results/report.md
   ```

## Configuration

All settings live in `config.env` (copied from the template). Key parameters:

| Variable | Default | Description |
|----------|---------|-------------|
| `AWS_PROFILE` | -- | AWS CLI profile name |
| `AWS_REGION` | `eu-west-2` | AWS region for all resources |
| `ATLAS_PUBLIC_KEY` | -- | Atlas API public key |
| `ATLAS_PRIVATE_KEY` | -- | Atlas API private key |
| `ATLAS_PROJECT_ID` | -- | Atlas project ID |
| `ATLAS_INSTANCE_SIZE` | `M30` | Atlas cluster tier |
| `SMALL_RECORD_COUNT` | `5000000` | In-memory dataset size |
| `LARGE_RECORD_COUNT` | `20000000` | Over-memory dataset size |
| `THREAD_COUNTS` | `32 128 256` | Concurrency levels to test |
| `RUN_DURATION_SECONDS` | `180` | Measurement window per run |
| `TAG_EXPIRE_ON` | tomorrow | Resource expiry tag (YYYY-MM-DD) |

### Pricing Variables

The report calculates cost-effectiveness ratios using hourly pricing from `config.env`. Update these if your region or reserved pricing differs:

```
ATLAS_HOURLY_COST_PER_NODE=0.59      # M30 Gen1 per cluster (3-node RS included)
REDIS_HOURLY_COST_PER_NODE=0.24      # cache.r6g.large per node (x2 nodes = $0.48/hr)
```

## Customisation

### Different Atlas Tier

Change `ATLAS_INSTANCE_SIZE` in `config.env` (e.g. `M40`, `M50`). Update `ATLAS_HOURLY_COST_PER_NODE` to match.

### Different Region

Update both `AWS_REGION` (e.g. `us-east-1`) and `ATLAS_REGION` (e.g. `US_EAST_1`) in `config.env`. If an Atlas network container already exists for the new region, you may need to import it into Terraform state:

```bash
terraform -chdir=terraform import mongodbatlas_network_container.benchmark <project_id>-<container_id>
```

### Different ElastiCache Node Types

Edit `terraform/elasticache.tf` to change `node_type` and update the corresponding cost variables in `config.env`.

## Cleanup

After the benchmark completes, tear down all infrastructure:

```bash
source config.env
terraform -chdir=terraform destroy -auto-approve \
  -var="atlas_public_key=${ATLAS_PUBLIC_KEY}" \
  -var="atlas_private_key=${ATLAS_PRIVATE_KEY}" \
  -var="atlas_project_id=${ATLAS_PROJECT_ID}" \
  -var="db_password=${DB_PASSWORD}" \
  -var="tag_owner=${TAG_OWNER}" \
  -var="tag_expire_on=${TAG_EXPIRE_ON:-$(date -v+1d +%Y-%m-%d)}"
```

## Data Model

The benchmark uses a generic customer-interaction record (~1 KB each) with fields designed to exercise different query patterns: primary key lookups, secondary indexes, compound filters, range queries, and write operations. See the report's Data Model section for the full schema.

### MongoDB Indexes

| Index | Used By |
|-------|---------|
| `{customerId: 1, createdAt: -1}` | Secondary Lookup |
| `{customerId: 1, status: 1, priority: 1}` | Filtered Query |
| `{status: 1, priority: 1, region: 1}` | (retained from initial design) |
| `{ownerId: 1, status: 1, updatedAt: -1}` | Covered Query |
| `{createdAt: 1}` | Range Scan |

### Redis Data Structures

| Key Pattern | Type | Used By |
|-------------|------|---------|
| `record:{id}` | String (JSON) | All workloads |
| `idx:customer:{id}` | Set | Secondary Lookup, Filtered Query |
| `idx:status:{status}` | Set | Filtered Query, Mixed R/W |
| `idx:owner:{id}` | Set | Covered Query |
| `idx:priority:{N}` | Set | Filtered Query |
| `idx:created` | Sorted Set | Range Scan |

## Measurement Methodology

- **HDR Histogram** for latency recording (microsecond precision, no coordination omission)
- **Scrambled Zipfian** key distribution (realistic hot-key skew, not uniform random)
- **Warmup phase** excluded from measurements (default 30s)
- **Per-run CSV** with throughput, avg/p50/p95/p99/p99.9/max latency, error count
- **Goroutine pool** sized to the thread count, each with its own RNG and operation instance

## Known Considerations

- **Network path difference**: Atlas connects via VPC peering (extra hop); ElastiCache is in the same VPC. The RTT measurements in the report quantify this.
- **Redis single-thread model**: Workloads that require server-side computation (large set intersections, sorted set range queries) are fundamentally limited by Redis's single-threaded architecture. The benchmark is designed to give Redis a fair shot -- e.g. the filtered query uses small customer-scoped sets rather than million-member global sets.
- **Atlas replication overhead**: MongoDB writes go through the replica set protocol (w:1 by default). Redis replication is asynchronous with lower overhead.
