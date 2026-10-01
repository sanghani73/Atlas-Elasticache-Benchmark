package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

func redisClient(cfg *BenchConfig) *redis.Client {
	poolSize := cfg.Threads * 2
	if poolSize < 100 {
		poolSize = 100
	}
	return redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		PoolSize:     poolSize,
		MinIdleConns: cfg.Threads,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})
}

// --- Point Lookup ---

type RedisPointLookup struct {
	cfg    *BenchConfig
	client *redis.Client
	zipf   *ScrambledZipfian
}

func (o *RedisPointLookup) Name() string { return "point_lookup" }

func (o *RedisPointLookup) Setup(ctx context.Context) error {
	o.client = redisClient(o.cfg)
	o.zipf = NewScrambledZipfian(o.cfg.RecordCount)
	return o.client.Ping(ctx).Err()
}

func (o *RedisPointLookup) Execute(ctx context.Context, rng *rand.Rand) error {
	id := RecordID(o.zipf.Next(rng))
	return o.client.Get(ctx, "record:"+id).Err()
}

func (o *RedisPointLookup) Teardown() {
	if o.client != nil {
		o.client.Close()
	}
}

// --- Secondary Lookup ---

type RedisSecondaryLookup struct {
	cfg    *BenchConfig
	client *redis.Client
}

func (o *RedisSecondaryLookup) Name() string { return "secondary_lookup" }

func (o *RedisSecondaryLookup) Setup(ctx context.Context) error {
	o.client = redisClient(o.cfg)
	return o.client.Ping(ctx).Err()
}

func (o *RedisSecondaryLookup) Execute(ctx context.Context, rng *rand.Rand) error {
	custIdx := rng.Int63n(NumCustomers)
	custID := fmt.Sprintf("cust_%06d", custIdx)
	members, err := o.client.SMembers(ctx, "idx:customer:"+custID).Result()
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	limit := 20
	if len(members) < limit {
		limit = len(members)
	}
	keys := make([]string, limit)
	for i := 0; i < limit; i++ {
		keys[i] = "record:" + members[i]
	}
	_, err = o.client.MGet(ctx, keys...).Result()
	return err
}

func (o *RedisSecondaryLookup) Teardown() {
	if o.client != nil {
		o.client.Close()
	}
}

// --- Filtered Query ---

type RedisFilteredQuery struct {
	cfg    *BenchConfig
	client *redis.Client
}

func (o *RedisFilteredQuery) Name() string { return "filtered_query" }

func (o *RedisFilteredQuery) Setup(ctx context.Context) error {
	o.client = redisClient(o.cfg)
	return o.client.Ping(ctx).Err()
}

func (o *RedisFilteredQuery) Execute(ctx context.Context, rng *rand.Rand) error {
	custIdx := rng.Int63n(NumCustomers)
	custID := fmt.Sprintf("cust_%06d", custIdx)
	status := Statuses[rng.Intn(len(Statuses))]
	priority := 1 + rng.Intn(5)
	members, err := o.client.SInter(ctx,
		"idx:customer:"+custID,
		"idx:status:"+status,
		fmt.Sprintf("idx:priority:%d", priority),
	).Result()
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	limit := 20
	if len(members) < limit {
		limit = len(members)
	}
	keys := make([]string, limit)
	for i := 0; i < limit; i++ {
		keys[i] = "record:" + members[i]
	}
	_, err = o.client.MGet(ctx, keys...).Result()
	return err
}

func (o *RedisFilteredQuery) Teardown() {
	if o.client != nil {
		o.client.Close()
	}
}

// --- Covered Query ---

type RedisCoveredQuery struct {
	cfg    *BenchConfig
	client *redis.Client
	zipf   *ScrambledZipfian
}

func (o *RedisCoveredQuery) Name() string { return "covered_query" }

func (o *RedisCoveredQuery) Setup(ctx context.Context) error {
	o.client = redisClient(o.cfg)
	o.zipf = NewScrambledZipfian(NumOwners)
	return o.client.Ping(ctx).Err()
}

func (o *RedisCoveredQuery) Execute(ctx context.Context, rng *rand.Rand) error {
	ownerIdx := o.zipf.Next(rng)
	ownerID := fmt.Sprintf("owner_%04d", ownerIdx)
	members, err := o.client.SMembers(ctx, "idx:owner:"+ownerID).Result()
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	limit := 20
	if len(members) < limit {
		limit = len(members)
	}
	keys := make([]string, limit)
	for i := 0; i < limit; i++ {
		keys[i] = "record:" + members[i]
	}
	_, err = o.client.MGet(ctx, keys...).Result()
	return err
}

func (o *RedisCoveredQuery) Teardown() {
	if o.client != nil {
		o.client.Close()
	}
}

// --- Range Scan ---

type RedisRangeScan struct {
	cfg    *BenchConfig
	client *redis.Client
}

func (o *RedisRangeScan) Name() string { return "range_scan" }

func (o *RedisRangeScan) Setup(ctx context.Context) error {
	o.client = redisClient(o.cfg)
	return o.client.Ping(ctx).Err()
}

func (o *RedisRangeScan) Execute(ctx context.Context, rng *rand.Rand) error {
	baseTime := int64(1687500000000)
	yearMs := int64(365 * 24 * 60 * 60 * 1000)
	hourMs := int64(60 * 60 * 1000)
	start := baseTime + rng.Int63n(yearMs-hourMs)
	end := start + hourMs

	members, err := o.client.ZRangeByScore(ctx, "idx:created", &redis.ZRangeBy{
		Min:    strconv.FormatInt(start, 10),
		Max:    strconv.FormatInt(end, 10),
		Count:  100,
		Offset: 0,
	}).Result()
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	keys := make([]string, len(members))
	for i, m := range members {
		keys[i] = "record:" + m
	}
	_, err = o.client.MGet(ctx, keys...).Result()
	return err
}

func (o *RedisRangeScan) Teardown() {
	if o.client != nil {
		o.client.Close()
	}
}

// --- Mixed Read/Write ---

type RedisMixed struct {
	cfg       *BenchConfig
	client    *redis.Client
	zipf      *ScrambledZipfian
	insertSeq atomic.Int64
}

func (o *RedisMixed) Name() string { return "mixed_readwrite" }

func (o *RedisMixed) Setup(ctx context.Context) error {
	o.client = redisClient(o.cfg)
	o.zipf = NewScrambledZipfian(o.cfg.RecordCount)
	o.insertSeq.Store(o.cfg.RecordCount)
	return o.client.Ping(ctx).Err()
}

func (o *RedisMixed) Execute(ctx context.Context, rng *rand.Rand) error {
	roll := rng.Float64()
	switch {
	case roll < 0.60:
		id := RecordID(o.zipf.Next(rng))
		err := o.client.Get(ctx, "record:"+id).Err()
		if err == redis.Nil {
			return nil
		}
		return err

	case roll < 0.85:
		id := RecordID(o.zipf.Next(rng))
		val, err := o.client.Get(ctx, "record:"+id).Result()
		if err == redis.Nil {
			return nil
		}
		if err != nil {
			return err
		}
		var rec Record
		if err := json.Unmarshal([]byte(val), &rec); err != nil {
			return err
		}
		oldStatus := rec.Status
		newStatus := Statuses[rng.Intn(len(Statuses))]
		rec.Status = newStatus
		rec.UpdatedAt = time.Now().UnixMilli()
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		pipe := o.client.Pipeline()
		pipe.Set(ctx, "record:"+id, data, 0)
		if oldStatus != newStatus {
			pipe.SRem(ctx, "idx:status:"+oldStatus, rec.ID)
			pipe.SAdd(ctx, "idx:status:"+newStatus, rec.ID)
		}
		_, err = pipe.Exec(ctx)
		return err

	default:
		seq := o.insertSeq.Add(1)
		rec := GenerateRecord(seq)
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		pipe := o.client.Pipeline()
		pipe.Set(ctx, "record:"+rec.ID, data, 0)
		pipe.SAdd(ctx, "idx:customer:"+rec.CustomerID, rec.ID)
		pipe.SAdd(ctx, "idx:status:"+rec.Status, rec.ID)
		pipe.SAdd(ctx, "idx:owner:"+rec.OwnerID, rec.ID)
		pipe.SAdd(ctx, fmt.Sprintf("idx:priority:%d", rec.Priority), rec.ID)
		pipe.ZAdd(ctx, "idx:created", redis.Z{Score: float64(rec.CreatedAt), Member: rec.ID})
		_, err = pipe.Exec(ctx)
		return err
	}
}

func (o *RedisMixed) Teardown() {
	if o.client != nil {
		o.client.Close()
	}
}
