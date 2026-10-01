package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "seed":
		cmdSeed(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "run-all":
		cmdRunAll(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: benchmark <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  seed      Populate MongoDB and/or Redis with test data")
	fmt.Fprintln(os.Stderr, "  run       Run a single workload against a single target")
	fmt.Fprintln(os.Stderr, "  run-all   Run all workloads against all targets")
}

func cmdSeed(args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	mongoURI := fs.String("mongodb-uri", "", "MongoDB connection URI")
	redisAddr := fs.String("redis-addr", "", "Redis address (host:port)")
	records := fs.Int64("records", 5000000, "Number of records to seed")
	startFrom := fs.Int64("start-from", 0, "Start seeding from this record index (skip drop/flush when > 0)")
	redisOnly := fs.Bool("redis-only", false, "Only seed Redis (skip MongoDB)")
	outputDir := fs.String("output-dir", "./results", "Output directory for seed report")
	fs.Parse(args)

	ctx := context.Background()

	if !*redisOnly {
		if *mongoURI == "" {
			fmt.Fprintln(os.Stderr, "Error: --mongodb-uri is required")
			os.Exit(1)
		}
		start := time.Now()
		if err := SeedMongoDB(ctx, *mongoURI, *records, *startFrom); err != nil {
			fmt.Fprintf(os.Stderr, "MongoDB seed failed: %v\n", err)
			os.Exit(1)
		}
		seeded := *records - *startFrom
		WriteSeedReport("mongodb", seeded, time.Since(start), *outputDir)
	}

	if *redisAddr != "" {
		start := time.Now()
		if err := SeedRedis(ctx, *redisAddr, *records, *startFrom); err != nil {
			fmt.Fprintf(os.Stderr, "Redis seed failed: %v\n", err)
			os.Exit(1)
		}
		seeded := *records - *startFrom
		WriteSeedReport("redis", seeded, time.Since(start), *outputDir)
	}
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	target := fs.String("target", "", "Target: mongodb, redis")
	targetLabel := fs.String("target-label", "", "Label for CSV output (defaults to --target)")
	mongoURI := fs.String("mongodb-uri", "", "MongoDB connection URI")
	redisAddr := fs.String("redis-addr", "", "Redis address (host:port)")
	workload := fs.String("workload", "", "Workload name")
	threads := fs.Int("threads", 64, "Number of concurrent workers")
	duration := fs.Int("duration", 180, "Measurement duration in seconds")
	warmup := fs.Int("warmup", 30, "Warmup duration in seconds")
	records := fs.Int64("records", 5000000, "Total record count")
	outputDir := fs.String("output-dir", "./results", "Output directory")
	fs.Parse(args)

	if *target == "" || *workload == "" {
		fmt.Fprintln(os.Stderr, "Error: --target and --workload are required")
		os.Exit(1)
	}

	label := *targetLabel
	if label == "" {
		label = *target
	}

	cfg := &BenchConfig{
		MongoURI:    *mongoURI,
		RedisAddr:   *redisAddr,
		Target:      *target,
		Workload:    *workload,
		Threads:     *threads,
		Duration:    time.Duration(*duration) * time.Second,
		Warmup:      time.Duration(*warmup) * time.Second,
		RecordCount: *records,
		OutputDir:   *outputDir,
	}

	result, err := runSingle(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Benchmark failed: %v\n", err)
		os.Exit(1)
	}

	result.Target = label
	printResult(result)
	if err := WriteCSV([]RunResult{*result}, *outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write CSV: %v\n", err)
	}
}

func cmdRunAll(args []string) {
	fs := flag.NewFlagSet("run-all", flag.ExitOnError)
	mongoURI := fs.String("mongodb-uri", "", "MongoDB connection URI")
	redisAddr := fs.String("redis-addr", "", "Redis address (host:port)")
	threadsStr := fs.String("threads", "32,128,256", "Comma-separated thread counts")
	duration := fs.Int("duration", 180, "Measurement duration in seconds")
	warmup := fs.Int("warmup", 30, "Warmup duration in seconds")
	records := fs.Int64("records", 5000000, "Total record count")
	outputDir := fs.String("output-dir", "./results", "Output directory")
	cooldown := fs.Int("cooldown", 30, "Cooldown between runs in seconds")
	fs.Parse(args)

	threadCounts := parseThreadCounts(*threadsStr)
	if len(threadCounts) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no valid thread counts")
		os.Exit(1)
	}

	type targetDef struct {
		name string
		addr string
	}
	var targets []targetDef
	if *mongoURI != "" {
		targets = append(targets, targetDef{TargetMongoDB, *mongoURI})
	}
	if *redisAddr != "" {
		targets = append(targets, targetDef{TargetRedis, *redisAddr})
	}

	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "Error: at least one target URI/address is required")
		os.Exit(1)
	}

	totalRuns := len(WorkloadNames) * len(threadCounts) * len(targets)
	currentRun := 0

	fmt.Printf("=== Benchmark Run-All ===\n")
	fmt.Printf("Workloads:     %v\n", WorkloadNames)
	fmt.Printf("Thread counts: %v\n", threadCounts)
	fmt.Printf("Targets:       %d\n", len(targets))
	fmt.Printf("Duration:      %ds per run\n", *duration)
	fmt.Printf("Total runs:    %d\n", totalRuns)
	fmt.Println()

	for _, wl := range WorkloadNames {
		for _, tc := range threadCounts {
			for _, t := range targets {
				currentRun++
				fmt.Printf("\n--- Run %d/%d: %s | %s | %d threads ---\n", currentRun, totalRuns, t.name, wl, tc)

				cfg := &BenchConfig{
					Workload:    wl,
					Threads:     tc,
					Duration:    time.Duration(*duration) * time.Second,
					Warmup:      time.Duration(*warmup) * time.Second,
					RecordCount: *records,
					OutputDir:   *outputDir,
				}

				if t.name == TargetMongoDB {
					cfg.Target = TargetMongoDB
					cfg.MongoURI = t.addr
				} else {
					cfg.Target = t.name
					cfg.RedisAddr = t.addr
				}

				result, err := runSingle(cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  ERROR: %v\n", err)
					continue
				}

				printResult(result)
				if err := WriteCSV([]RunResult{*result}, *outputDir); err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: CSV write error: %v\n", err)
				}

				if currentRun < totalRuns {
					fmt.Printf("  Cooldown: %ds\n", *cooldown)
					time.Sleep(time.Duration(*cooldown) * time.Second)
				}
			}
		}
	}

	fmt.Printf("\n=== All %d runs complete ===\n", totalRuns)
	fmt.Printf("Results: %s/results.csv\n", *outputDir)
}

func runSingle(cfg *BenchConfig) (*RunResult, error) {
	var factory OperationFactory
	switch {
	case cfg.Target == TargetMongoDB:
		f, ok := MongoDBOps[cfg.Workload]
		if !ok {
			return nil, fmt.Errorf("unknown MongoDB workload: %s", cfg.Workload)
		}
		factory = f
	case strings.HasPrefix(cfg.Target, "redis"):
		f, ok := RedisOps[cfg.Workload]
		if !ok {
			return nil, fmt.Errorf("unknown Redis workload: %s", cfg.Workload)
		}
		factory = f
	default:
		return nil, fmt.Errorf("unknown target: %s", cfg.Target)
	}

	engine := NewEngine(cfg, factory)
	ctx := context.Background()
	return engine.Run(ctx)
}

func printResult(r *RunResult) {
	fmt.Printf("  Target:     %s\n", r.Target)
	fmt.Printf("  Workload:   %s\n", r.Workload)
	fmt.Printf("  Threads:    %d\n", r.Threads)
	fmt.Printf("  Duration:   %.1fs\n", r.DurationS)
	fmt.Printf("  Throughput: %.0f ops/sec\n", r.Throughput)
	fmt.Printf("  Latency:    avg=%.2fms  p50=%.2fms  p95=%.2fms  p99=%.2fms  p99.9=%.2fms\n",
		float64(r.AvgUs)/1000, float64(r.P50Us)/1000, float64(r.P95Us)/1000,
		float64(r.P99Us)/1000, float64(r.P999Us)/1000)
	fmt.Printf("  Total ops:  %d  errors: %d\n", r.TotalOps, r.Errors)
}

func parseThreadCounts(s string) []int {
	parts := strings.Split(s, ",")
	var result []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			result = append(result, n)
		}
	}
	return result
}
