package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func SeedMongoDB(ctx context.Context, uri string, recordCount int64, startFrom int64) error {
	toInsert := recordCount - startFrom
	fmt.Printf("=== Seeding MongoDB (%d records, starting from %d) ===\n", toInsert, startFrom)
	start := time.Now()

	opts := options.Client().ApplyURI(uri).SetMinPoolSize(64).SetMaxPoolSize(128)
	client, err := mongo.Connect(opts)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Disconnect(ctx)

	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	coll := client.Database(DatabaseName).Collection(CollectionName)

	if startFrom == 0 {
		fmt.Println("  Dropping existing collection...")
		_ = coll.Drop(ctx)
	} else {
		fmt.Println("  Incremental seed — skipping collection drop")
	}

	const batchSize = 1000
	const workers = 64
	var inserted atomic.Int64

	batchCount := toInsert / batchSize
	batchCh := make(chan int64, batchCount)
	for i := int64(0); i < batchCount; i++ {
		batchCh <- i
	}
	close(batchCh)

	var wg sync.WaitGroup
	var firstErr atomic.Value

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batchIdx := range batchCh {
				if firstErr.Load() != nil {
					return
				}
				startIdx := startFrom + batchIdx*batchSize
				docs := make([]interface{}, batchSize)
				for i := int64(0); i < batchSize; i++ {
					docs[i] = GenerateRecord(startIdx + i)
				}
				_, err := coll.InsertMany(ctx, docs)
				if err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
				done := inserted.Add(batchSize)
				if done%(batchSize*100) == 0 {
					fmt.Printf("  MongoDB: %d / %d records (%.1f%%)\n", done, toInsert, float64(done)/float64(toInsert)*100)
				}
			}
		}()
	}
	wg.Wait()

	if v := firstErr.Load(); v != nil {
		return fmt.Errorf("insert: %v", v)
	}

	remainder := toInsert % batchSize
	if remainder > 0 {
		docs := make([]interface{}, remainder)
		baseIdx := startFrom + batchCount*batchSize
		for i := int64(0); i < remainder; i++ {
			docs[i] = GenerateRecord(baseIdx + i)
		}
		if _, err := coll.InsertMany(ctx, docs); err != nil {
			return fmt.Errorf("insert remainder: %w", err)
		}
		inserted.Add(remainder)
	}

	elapsed := time.Since(start)
	total := inserted.Load()
	fmt.Printf("  MongoDB: inserted %d records in %s (%.0f records/sec)\n", total, elapsed, float64(total)/elapsed.Seconds())

	if startFrom == 0 {
		fmt.Println("  Creating indexes...")
		indexes := []mongo.IndexModel{
			{Keys: bson.D{{Key: "customerId", Value: 1}, {Key: "createdAt", Value: -1}}},
			{Keys: bson.D{{Key: "customerId", Value: 1}, {Key: "status", Value: 1}, {Key: "priority", Value: 1}}},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "priority", Value: 1}, {Key: "region", Value: 1}}},
			{Keys: bson.D{{Key: "ownerId", Value: 1}, {Key: "status", Value: 1}, {Key: "updatedAt", Value: -1}}},
			{Keys: bson.D{{Key: "createdAt", Value: 1}}},
		}
		names, err := coll.Indexes().CreateMany(ctx, indexes)
		if err != nil {
			return fmt.Errorf("create indexes: %w", err)
		}
		fmt.Printf("  Created indexes: %v\n", names)
	}
	fmt.Printf("  MongoDB seeding complete (total: %s)\n", time.Since(start))
	return nil
}

func SeedRedis(ctx context.Context, addr string, recordCount int64, startFrom int64) error {
	toInsert := recordCount - startFrom
	fmt.Printf("=== Seeding Redis at %s (%d records, starting from %d) ===\n", addr, toInsert, startFrom)
	start := time.Now()

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		PoolSize:     128,
		MinIdleConns: 64,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	})
	defer client.Close()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	if startFrom == 0 {
		fmt.Println("  Flushing existing data...")
		if err := client.FlushAll(ctx).Err(); err != nil {
			return fmt.Errorf("flush: %w", err)
		}
	} else {
		fmt.Println("  Incremental seed — skipping flush")
	}

	const pipelineBatch = 500
	const workers = 64
	var inserted atomic.Int64

	chunkSize := toInsert / int64(workers)
	var wg sync.WaitGroup
	var firstErr atomic.Value

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			startIdx := startFrom + int64(workerID)*chunkSize
			endIdx := startIdx + chunkSize
			if workerID == workers-1 {
				endIdx = recordCount
			}

			pipe := client.Pipeline()
			pipeCount := 0

			for i := startIdx; i < endIdx; i++ {
				if firstErr.Load() != nil {
					return
				}
				rec := GenerateRecord(i)
				data, err := json.Marshal(rec)
				if err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}

				pipe.Set(ctx, "record:"+rec.ID, data, 0)
				pipe.SAdd(ctx, "idx:customer:"+rec.CustomerID, rec.ID)
				pipe.SAdd(ctx, "idx:status:"+rec.Status, rec.ID)
				pipe.SAdd(ctx, "idx:owner:"+rec.OwnerID, rec.ID)
				pipe.SAdd(ctx, fmt.Sprintf("idx:priority:%d", rec.Priority), rec.ID)
				pipe.ZAdd(ctx, "idx:created", redis.Z{Score: float64(rec.CreatedAt), Member: rec.ID})
				pipeCount += 6

				if pipeCount >= pipelineBatch {
					if _, err := pipe.Exec(ctx); err != nil {
						firstErr.CompareAndSwap(nil, err)
						return
					}
					pipe = client.Pipeline()
					pipeCount = 0
				}

				done := inserted.Add(1)
				if done%(100000) == 0 {
					fmt.Printf("  Redis (%s): %d / %d records (%.1f%%)\n", addr, done, toInsert, float64(done)/float64(toInsert)*100)
				}
			}

			if pipeCount > 0 {
				if _, err := pipe.Exec(ctx); err != nil {
					firstErr.CompareAndSwap(nil, err)
				}
			}
		}(w)
	}
	wg.Wait()

	if v := firstErr.Load(); v != nil {
		return fmt.Errorf("pipeline: %v", v)
	}

	elapsed := time.Since(start)
	total := inserted.Load()
	fmt.Printf("  Redis (%s): inserted %d records in %s (%.0f records/sec)\n", addr, total, elapsed, float64(total)/elapsed.Seconds())
	return nil
}
