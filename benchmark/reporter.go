package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func WriteCSV(results []RunResult, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	csvPath := filepath.Join(outputDir, "results.csv")

	// Check if file exists to decide about headers
	writeHeader := true
	if _, err := os.Stat(csvPath); err == nil {
		writeHeader = false
	}

	f, err := os.OpenFile(csvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if writeHeader {
		header := []string{
			"target", "workload", "threads", "duration_s",
			"total_ops", "throughput_ops_sec",
			"avg_us", "p50_us", "p95_us", "p99_us", "p999_us", "max_us",
			"errors", "timestamp",
		}
		if err := w.Write(header); err != nil {
			return err
		}
	}

	for _, r := range results {
		row := []string{
			r.Target,
			r.Workload,
			strconv.Itoa(r.Threads),
			fmt.Sprintf("%.1f", r.DurationS),
			strconv.FormatInt(r.TotalOps, 10),
			fmt.Sprintf("%.2f", r.Throughput),
			strconv.FormatInt(r.AvgUs, 10),
			strconv.FormatInt(r.P50Us, 10),
			strconv.FormatInt(r.P95Us, 10),
			strconv.FormatInt(r.P99Us, 10),
			strconv.FormatInt(r.P999Us, 10),
			strconv.FormatInt(r.MaxUs, 10),
			strconv.FormatInt(r.Errors, 10),
			time.Now().UTC().Format(time.RFC3339),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}

	return nil
}

func WriteSeedReport(target string, records int64, elapsed time.Duration, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	csvPath := filepath.Join(outputDir, "seed_results.csv")
	writeHeader := true
	if _, err := os.Stat(csvPath); err == nil {
		writeHeader = false
	}

	f, err := os.OpenFile(csvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if writeHeader {
		if err := w.Write([]string{"target", "records", "elapsed_s", "records_per_sec", "timestamp"}); err != nil {
			return err
		}
	}

	rps := float64(records) / elapsed.Seconds()
	row := []string{
		target,
		strconv.FormatInt(records, 10),
		fmt.Sprintf("%.1f", elapsed.Seconds()),
		fmt.Sprintf("%.0f", rps),
		time.Now().UTC().Format(time.RFC3339),
	}
	return w.Write(row)
}
