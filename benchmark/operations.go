package main

import (
	"context"
	"math/rand"
)

type Operation interface {
	Name() string
	Setup(ctx context.Context) error
	Execute(ctx context.Context, rng *rand.Rand) error
	Teardown()
}

type OperationFactory func(cfg *BenchConfig) Operation

var WorkloadNames = []string{
	"point_lookup",
	"secondary_lookup",
	"filtered_query",
	"covered_query",
	"range_scan",
	"mixed_readwrite",
}

var MongoDBOps = map[string]OperationFactory{
	"point_lookup":     func(cfg *BenchConfig) Operation { return &MongoPointLookup{cfg: cfg} },
	"secondary_lookup": func(cfg *BenchConfig) Operation { return &MongoSecondaryLookup{cfg: cfg} },
	"filtered_query":   func(cfg *BenchConfig) Operation { return &MongoFilteredQuery{cfg: cfg} },
	"covered_query":    func(cfg *BenchConfig) Operation { return &MongoCoveredQuery{cfg: cfg} },
	"range_scan":       func(cfg *BenchConfig) Operation { return &MongoRangeScan{cfg: cfg} },
	"mixed_readwrite":  func(cfg *BenchConfig) Operation { return &MongoMixed{cfg: cfg} },
}

var RedisOps = map[string]OperationFactory{
	"point_lookup":     func(cfg *BenchConfig) Operation { return &RedisPointLookup{cfg: cfg} },
	"secondary_lookup": func(cfg *BenchConfig) Operation { return &RedisSecondaryLookup{cfg: cfg} },
	"filtered_query":   func(cfg *BenchConfig) Operation { return &RedisFilteredQuery{cfg: cfg} },
	"covered_query":    func(cfg *BenchConfig) Operation { return &RedisCoveredQuery{cfg: cfg} },
	"range_scan":       func(cfg *BenchConfig) Operation { return &RedisRangeScan{cfg: cfg} },
	"mixed_readwrite":  func(cfg *BenchConfig) Operation { return &RedisMixed{cfg: cfg} },
}
