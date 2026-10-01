package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	hdr "github.com/HdrHistogram/hdrhistogram-go"
)

type Engine struct {
	cfg     *BenchConfig
	factory OperationFactory
}

func NewEngine(cfg *BenchConfig, factory OperationFactory) *Engine {
	return &Engine{cfg: cfg, factory: factory}
}

type RunResult struct {
	Target     string  `json:"target"`
	Workload   string  `json:"workload"`
	Threads    int     `json:"threads"`
	DurationS  float64 `json:"duration_s"`
	TotalOps   int64   `json:"total_ops"`
	Throughput float64 `json:"throughput_ops_sec"`
	AvgUs      int64   `json:"avg_us"`
	P50Us      int64   `json:"p50_us"`
	P95Us      int64   `json:"p95_us"`
	P99Us      int64   `json:"p99_us"`
	P999Us     int64   `json:"p999_us"`
	MaxUs      int64   `json:"max_us"`
	Errors     int64   `json:"errors"`
}

func (e *Engine) Run(ctx context.Context) (*RunResult, error) {
	op := e.factory(e.cfg)
	if err := op.Setup(ctx); err != nil {
		return nil, fmt.Errorf("operation setup: %w", err)
	}
	defer op.Teardown()

	fmt.Printf("  Warming up (%s)...\n", e.cfg.Warmup)
	e.runPhase(ctx, op, e.cfg.Warmup)

	fmt.Printf("  Measuring (%s, %d threads)...\n", e.cfg.Duration, e.cfg.Threads)
	measStart := time.Now()
	hist, totalOps, errors := e.runPhase(ctx, op, e.cfg.Duration)
	elapsed := time.Since(measStart).Seconds()

	throughput := float64(totalOps) / elapsed
	var avgUs int64
	if totalOps > 0 {
		avgUs = int64(hist.Mean())
	}

	return &RunResult{
		Target:     e.cfg.Target,
		Workload:   e.cfg.Workload,
		Threads:    e.cfg.Threads,
		DurationS:  elapsed,
		TotalOps:   totalOps,
		Throughput: throughput,
		AvgUs:      avgUs,
		P50Us:      hist.ValueAtQuantile(50),
		P95Us:      hist.ValueAtQuantile(95),
		P99Us:      hist.ValueAtQuantile(99),
		P999Us:     hist.ValueAtQuantile(99.9),
		MaxUs:      hist.Max(),
		Errors:     errors,
	}, nil
}

func (e *Engine) runPhase(ctx context.Context, op Operation, duration time.Duration) (*hdr.Histogram, int64, int64) {
	merged := hdr.New(1, 30_000_000, 3)
	var mu sync.Mutex
	var totalOps atomic.Int64
	var totalErrors atomic.Int64
	var wg sync.WaitGroup

	phaseCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	for i := 0; i < e.cfg.Threads; i++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed + time.Now().UnixNano()))
			localHist := hdr.New(1, 30_000_000, 3)
			var localOps int64
			var localErrors int64

			for phaseCtx.Err() == nil {
				start := time.Now()
				err := op.Execute(phaseCtx, rng)
				us := time.Since(start).Microseconds()

				if err != nil {
					if phaseCtx.Err() != nil {
						break
					}
					localErrors++
					continue
				}
				if us > 30_000_000 {
					us = 30_000_000
				}
				localHist.RecordValue(us)
				localOps++
			}

			totalOps.Add(localOps)
			totalErrors.Add(localErrors)

			mu.Lock()
			merged.Merge(localHist)
			mu.Unlock()
		}(int64(i))
	}

	wg.Wait()
	return merged, totalOps.Load(), totalErrors.Load()
}
