package main

import "time"

const (
	TargetMongoDB = "mongodb"
	TargetRedis   = "redis"

	CollectionName = "interactions"
	DatabaseName   = "benchmark"
)

type BenchConfig struct {
	MongoURI  string
	RedisAddr string
	Target    string
	Workload      string
	Threads       int
	Duration      time.Duration
	Warmup        time.Duration
	RecordCount   int64
	OutputDir     string
	Cooldown      time.Duration
	RedisOnly     bool
}
