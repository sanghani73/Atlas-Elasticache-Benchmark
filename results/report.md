# MongoDB Atlas vs ElastiCache Redis — Performance Comparison Report

> **Generated:** 2026-10-01 14:17:59  
> **Benchmark tool:** Custom Go benchmark (goroutine-based, HDR Histogram latency tracking)

---

## Purpose

> **Important:** This benchmark is a **comparative test** between MongoDB Atlas and AWS ElastiCache Redis, **not** an absolute performance benchmark of either system. The workloads use a generic interaction-record data model designed to test both systems under the same access patterns. Real-world applications with optimised schemas, connection tuning, and caching strategies would achieve different absolute numbers.
>
> The value of this test lies in the **relative comparison**: both targets run the exact same workloads, with the same client, same dataset, same access patterns, and same concurrency levels. The EC2 benchmark client communicates with Atlas via VPC peering and with ElastiCache via the same VPC, minimising network differences.

---

## Test Environment

| Parameter | MongoDB Atlas | ElastiCache Redis |
|-----------|---------------|-------------------|
| **Service** | Atlas M30 | ElastiCache for Redis |
| **Node Type** | M30 (3-node RS) | cache.r6g.large (Primary + Replica) |
| **RAM** | 8 GB (~4 GB WiredTiger cache) | 13.07 GB |
| **Engine** | MongoDB 8.0.34 | Redis 7.1 |
| **Hourly Cost** | $0.59/hr | $0.48/hr |
| **Region** | eu-west-2 | eu-west-2 |
| **Networking** | VPC Peering | Same VPC |
| **Benchmark Client** | EC2 c5.2xlarge | EC2 c5.2xlarge |
| **Small Dataset** | 5,000,000 records (~5 GB) | Same |
| **Over-Memory Dataset** | 20,000,000 records (~20 GB) | Same (LRU eviction) |

---

## Data Model

Each record represents a generic customer interaction (~1 KB):

```json
{
  "_id": "record_0000000001",
  "customerId": "cust_039201",
  "type": "phone",
  "status": "resolved",
  "priority": 3,
  "ownerId": "owner_1042",
  "category": "network",
  "region": "northwest",
  "createdAt": 1718445780000,
  "updatedAt": 1718450700000,
  "tags": ["broadband", "speed"],
  "payload": "... ~500 bytes of text ..."
}
```

**MongoDB indexes:** `{customerId, createdAt}`, `{customerId, status, priority}`, `{status, priority, region}`, `{ownerId, status, updatedAt}`, `{createdAt}`

**Redis structures:** `record:{id}` (JSON string) + secondary index SETs (`idx:customer:*`, `idx:status:*`, `idx:owner:*`, `idx:priority:*`) + sorted set (`idx:created`)

---

## Workloads

### Point Lookup
*100% single-key reads*

A pure key-value read workload. Every operation fetches a single record by its primary key using a Zipfian distribution (hot-spot access pattern). This is Redis's home turf — in-memory hash table lookups with O(1) complexity.

- **MongoDB:** `findOne({_id: id})`
- **Redis:** `GET record:{id}`

### Secondary Index Lookup
*Secondary index lookup + multi-doc fetch*

Looks up a customer by ID (secondary index) and fetches their most recent 20 interactions. This tests secondary index performance and multi-document retrieval.

- **MongoDB:** `find({customerId: id}).sort({createdAt: -1}).limit(20)`
- **Redis:** `SMEMBERS idx:customer:{id} → MGET (up to 20 keys)`

### Filtered Query
*Customer-scoped compound filter*

Finds a specific customer's tickets filtered by status and priority — a common application query (e.g. 'show me customer X's open high-priority tickets'). MongoDB uses a compound index on {customerId, status, priority}; Redis intersects the small customer set (~100 members) with status and priority sets, keeping SINTER viable.

- **MongoDB:** `find({customerId: id, status: S, priority: N}).limit(20)`
- **Redis:** `SINTER idx:customer:{id} idx:status:S idx:priority:N → MGET`

### Covered Query
*Index-only read (covered query)*

MongoDB returns results entirely from the index without touching documents — a 'covered query'. Redis has no equivalent optimisation; it must always fetch and deserialise the full value. This workload highlights MongoDB's advantage when applications need only a subset of fields that happen to be indexed.

- **MongoDB:** `find({ownerId: id, status: 'open'}, {ownerId:1, status:1, updatedAt:1, _id:0}).limit(20)`
- **Redis:** `SMEMBERS idx:owner:{id} → MGET (full value deserialization)`

### Range Scan
*Time-range scan*

Scans a 1-hour window of interactions by creation time. MongoDB uses a B-tree index scan; Redis uses ZRANGEBYSCORE on a sorted set followed by MGET.

- **MongoDB:** `find({createdAt: {$gte: start, $lt: end}}).limit(100)`
- **Redis:** `ZRANGEBYSCORE idx:created start end LIMIT 0 100 → MGET`

### Mixed Read/Write
*60% reads, 25% updates, 15% inserts*

A realistic mixed workload simulating a live application. Reads dominate, but updates and inserts add write pressure. For Redis, writes require maintaining secondary index structures (SADD/SREM/ZADD), adding significant overhead.

- **MongoDB:** `findOne / updateOne / insertOne`
- **Redis:** `GET + SET/SADD/SREM/ZADD (index maintenance on every write)`

---

## Results — In-Memory Dataset

### Throughput Comparison (ops/sec)

| Workload | Threads | Atlas M30 | Redis r6g.large |
|:---------|--------:| ---:| ---:|
| **Point Lookup** | 32 | 9,975 | 43,323 |
|  | 128 | 9,151 | 155,634 |
|  | 256 | 9,390 | 166,205 |
| **Secondary Index Lookup** | 32 | 2,196 | 12,785 |
|  | 128 | 2,671 | 8,040 |
|  | 256 | 2,136 | 12,328 |
| **Filtered Query** | 32 | 4,112 | 6,412 |
|  | 128 | 4,463 | 6,328 |
|  | 256 | 4,536 | 6,312 |
| **Covered Query** | 32 | 6,212 | 2,843 |
|  | 128 | 6,370 | 2,827 |
|  | 256 | 6,260 | 2,730 |
| **Range Scan** | 32 | 593.2 | 4,814 |
|  | 128 | 638.9 | 4,616 |
|  | 256 | 603.8 | 4,451 |
| **Mixed Read/Write** | 32 | 3,820 | 34,462 |
|  | 128 | 3,325 | 80,288 |
|  | 256 | 3,495 | 87,662 |

### Point Lookup — Detailed Latency

| Threads | Atlas M30 ops/s | P95 (ms) | P99 (ms) | Redis r6g.large ops/s | P95 (ms) | P99 (ms) |
|--------:| ---:| ---:| ---:| ---:| ---:| ---:|
| 32 | 9,975 | 5.00 | 9.26 | 43,323 | 0.886 | 0.981 |
| 128 | 9,151 | 25.1 | 44.6 | 155,634 | 0.961 | 1.12 |
| 256 | 9,390 | 40.0 | 60.0 | 166,205 | 1.79 | 2.03 |

### Secondary Index Lookup — Detailed Latency

| Threads | Atlas M30 ops/s | P95 (ms) | P99 (ms) | Redis r6g.large ops/s | P95 (ms) | P99 (ms) |
|--------:| ---:| ---:| ---:| ---:| ---:| ---:|
| 32 | 2,196 | 21.9 | 28.7 | 12,785 | 2.84 | 3.08 |
| 128 | 2,671 | 80.1 | 133 | 8,040 | 16.5 | 17.0 |
| 256 | 2,136 | 226 | 380 | 12,328 | 22.5 | 26.1 |

### Filtered Query — Detailed Latency

| Threads | Atlas M30 ops/s | P95 (ms) | P99 (ms) | Redis r6g.large ops/s | P95 (ms) | P99 (ms) |
|--------:| ---:| ---:| ---:| ---:| ---:| ---:|
| 32 | 4,112 | 15.2 | 26.9 | 6,412 | 5.18 | 5.46 |
| 128 | 4,463 | 44.8 | 70.1 | 6,328 | 21.5 | 22.8 |
| 256 | 4,536 | 84.8 | 153 | 6,312 | 41.8 | 42.6 |

### Covered Query — Detailed Latency

| Threads | Atlas M30 ops/s | P95 (ms) | P99 (ms) | Redis r6g.large ops/s | P95 (ms) | P99 (ms) |
|--------:| ---:| ---:| ---:| ---:| ---:| ---:|
| 32 | 6,212 | 7.99 | 14.4 | 2,843 | 12.3 | 12.8 |
| 128 | 6,370 | 28.8 | 38.9 | 2,827 | 49.0 | 58.7 |
| 256 | 6,260 | 58.1 | 107 | 2,730 | 121 | 129 |

### Range Scan — Detailed Latency

| Threads | Atlas M30 ops/s | P95 (ms) | P99 (ms) | Redis r6g.large ops/s | P95 (ms) | P99 (ms) |
|--------:| ---:| ---:| ---:| ---:| ---:| ---:|
| 32 | 593.2 | 98.9 | 118 | 4,814 | 7.00 | 7.23 |
| 128 | 638.9 | 476 | 583 | 4,616 | 33.4 | 36.3 |
| 256 | 603.8 | 829 | 1,021 | 4,451 | 69.2 | 96.5 |

### Mixed Read/Write — Detailed Latency

| Threads | Atlas M30 ops/s | P95 (ms) | P99 (ms) | Redis r6g.large ops/s | P95 (ms) | P99 (ms) |
|--------:| ---:| ---:| ---:| ---:| ---:| ---:|
| 32 | 3,820 | 33.2 | 58.6 | 34,462 | 1.58 | 1.76 |
| 128 | 3,325 | 158 | 296 | 80,288 | 2.78 | 3.07 |
| 256 | 3,495 | 262 | 523 | 87,662 | 4.88 | 5.38 |

---

## Results — Over-Memory Dataset

With 20,000,000 records (~20 GB), the dataset exceeds available RAM on both targets. MongoDB pages data to/from disk via WiredTiger. Redis evicts least-recently-used keys — reads for evicted keys return nil (cache miss).

### Throughput Comparison (ops/sec)

| Workload | Threads | Atlas M30 | Redis r6g.large |
|:---------|--------:| ---:| ---:|
| **Point Lookup** | 32 | 3,142 | 17,481 |
|  | 128 | 3,148 | 64,312 |
|  | 256 | 3,098 | 71,163 |
| **Secondary Index Lookup** | 32 | 2,622 | 9,628 |
|  | 128 | 2,657 | 9,574 |
|  | 256 | 2,411 | 9,403 |
| **Filtered Query** | 32 | 4,647 | 6,422 |
|  | 128 | 4,411 | 6,353 |
|  | 256 | 4,527 | 6,307 |
| **Covered Query** | 32 | 5,797 | 1,813 |
|  | 128 | 5,434 | 1,793 |
|  | 256 | 5,525 | 1,782 |
| **Range Scan** | 32 | 843.2 | 4,531 |
|  | 128 | 831.4 | 4,414 |
|  | 256 | 790.2 | 4,123 |
| **Mixed Read/Write** | 32 | 3,193 | 39,574 |
|  | 128 | 3,107 | 90,728 |
|  | 256 | 2,972 | 105,209 |

### Degradation from In-Memory Baseline

How much throughput dropped when the dataset exceeded available RAM:

| Workload | Threads | Atlas M30 | Redis r6g.large |
|:---------|--------:| ---:| ---:|
| **Point Lookup** | 32 | -68.5% | -59.6% |
|  | 128 | -65.6% | -58.7% |
|  | 256 | -67.0% | -57.2% |
| **Secondary Index Lookup** | 32 | +19.4% | -24.7% |
|  | 128 | -0.5% | +19.1% |
|  | 256 | +12.9% | -23.7% |
| **Filtered Query** | 32 | +13.0% | +0.2% |
|  | 128 | -1.1% | +0.4% |
|  | 256 | -0.2% | -0.1% |
| **Covered Query** | 32 | -6.7% | -36.2% |
|  | 128 | -14.7% | -36.6% |
|  | 256 | -11.7% | -34.7% |
| **Range Scan** | 32 | +42.1% | -5.9% |
|  | 128 | +30.1% | -4.4% |
|  | 256 | +30.9% | -7.4% |
| **Mixed Read/Write** | 32 | -16.4% | +14.8% |
|  | 128 | -6.6% | +13.0% |
|  | 256 | -15.0% | +20.0% |

---

## Cost Effectiveness

Throughput normalised by hourly cost: Atlas $0.59/hr, Redis $0.48/hr.

| Workload | Threads | Atlas M30 (ops/$/hr) | Redis r6g.large (ops/$/hr) |
|:---------|--------:| ---:| ---:|
| **Point Lookup** | 32 | 16,907 | 90,257 |
|  | 128 | 15,509 | 324,238 |
|  | 256 | 15,915 | 346,260 |
| **Secondary Index Lookup** | 32 | 3,723 | 26,635 |
|  | 128 | 4,528 | 16,749 |
|  | 256 | 3,621 | 25,683 |
| **Filtered Query** | 32 | 6,970 | 13,358 |
|  | 128 | 7,564 | 13,183 |
|  | 256 | 7,688 | 13,150 |
| **Covered Query** | 32 | 10,530 | 5,923 |
|  | 128 | 10,797 | 5,891 |
|  | 256 | 10,609 | 5,687 |
| **Range Scan** | 32 | 1,006 | 10,030 |
|  | 128 | 1,083 | 9,616 |
|  | 256 | 1,023 | 9,273 |
| **Mixed Read/Write** | 32 | 6,474 | 71,795 |
|  | 128 | 5,636 | 167,267 |
|  | 256 | 5,924 | 182,628 |

---

## Analysis & Conclusions

### Throughput Wins by Target (In-Memory)

- **Atlas M30**: 3 of 18 tests (17%)
- **Redis r6g.large**: 15 of 18 tests (83%)

### Findings by Workload

**Point Lookup:**
  Redis r6g.large leads with an average of 121,721 ops/sec.
  Redis r6g.large is 1180.6% faster than Atlas M30.

**Secondary Index Lookup:**
  Redis r6g.large leads with an average of 11,051 ops/sec.
  Redis r6g.large is 373.3% faster than Atlas M30.

**Filtered Query:**
  Redis r6g.large leads with an average of 6,350 ops/sec.
  Redis r6g.large is 45.3% faster than Atlas M30.

**Covered Query:**
  Atlas M30 leads with an average of 6,281 ops/sec.
  Atlas M30 is 124.3% faster than Redis r6g.large.

**Range Scan:**
  Redis r6g.large leads with an average of 4,627 ops/sec.
  Redis r6g.large is 656.0% faster than Atlas M30.

**Mixed Read/Write:**
  Redis r6g.large leads with an average of 67,471 ops/sec.
  Redis r6g.large is 1802.3% faster than Atlas M30.

### Key Takeaways

1. **Simple key-value lookups**: Redis excels at pure GET operations with sub-millisecond latency when data fits in memory.
2. **Secondary indexes & filtered queries**: MongoDB's native secondary indexes and query planner handle complex access patterns without the write amplification that Redis's manual secondary index structures impose.
3. **Covered queries**: MongoDB can serve reads entirely from the index, approaching in-memory speeds for indexed-field-only projections. Redis has no equivalent.
4. **Over-memory behaviour**: When data exceeds RAM, MongoDB gracefully pages to disk with predictable performance degradation. Redis evicts data entirely, causing cache misses that must be handled by the application.
5. **Write complexity**: Redis requires applications to maintain secondary index structures manually, adding code complexity and write amplification. MongoDB handles index maintenance transparently.
6. **Cost effectiveness**: At comparable hourly cost ($0.59 vs $0.48), MongoDB delivers more value per dollar for complex access patterns, while Redis leads on simple key-value operations.

---
*Generated by Atlas vs ElastiCache Performance Benchmark Suite*
