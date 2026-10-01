package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func mongoClient(cfg *BenchConfig) (*mongo.Client, error) {
	poolMin := uint64(cfg.Threads)
	poolMax := uint64(cfg.Threads * 2)
	if poolMax < 100 {
		poolMax = 100
	}
	opts := options.Client().
		ApplyURI(cfg.MongoURI).
		SetMinPoolSize(poolMin).
		SetMaxPoolSize(poolMax)
	return mongo.Connect(opts)
}

func mongoColl(client *mongo.Client) *mongo.Collection {
	return client.Database(DatabaseName).Collection(CollectionName)
}

// --- Point Lookup ---

type MongoPointLookup struct {
	cfg    *BenchConfig
	client *mongo.Client
	coll   *mongo.Collection
	zipf   *ScrambledZipfian
}

func (o *MongoPointLookup) Name() string { return "point_lookup" }

func (o *MongoPointLookup) Setup(ctx context.Context) error {
	var err error
	o.client, err = mongoClient(o.cfg)
	if err != nil {
		return err
	}
	o.coll = mongoColl(o.client)
	o.zipf = NewScrambledZipfian(o.cfg.RecordCount)
	return o.client.Ping(ctx, nil)
}

func (o *MongoPointLookup) Execute(ctx context.Context, rng *rand.Rand) error {
	id := RecordID(o.zipf.Next(rng))
	return o.coll.FindOne(ctx, bson.M{"_id": id}).Err()
}

func (o *MongoPointLookup) Teardown() {
	if o.client != nil {
		o.client.Disconnect(context.Background())
	}
}

// --- Secondary Lookup ---

type MongoSecondaryLookup struct {
	cfg    *BenchConfig
	client *mongo.Client
	coll   *mongo.Collection
}

func (o *MongoSecondaryLookup) Name() string { return "secondary_lookup" }

func (o *MongoSecondaryLookup) Setup(ctx context.Context) error {
	var err error
	o.client, err = mongoClient(o.cfg)
	if err != nil {
		return err
	}
	o.coll = mongoColl(o.client)
	return o.client.Ping(ctx, nil)
}

func (o *MongoSecondaryLookup) Execute(ctx context.Context, rng *rand.Rand) error {
	custIdx := rng.Int63n(NumCustomers)
	custID := fmt.Sprintf("cust_%06d", custIdx)
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(20)
	cursor, err := o.coll.Find(ctx, bson.M{"customerId": custID}, opts)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var results []bson.Raw
	return cursor.All(ctx, &results)
}

func (o *MongoSecondaryLookup) Teardown() {
	if o.client != nil {
		o.client.Disconnect(context.Background())
	}
}

// --- Filtered Query ---

type MongoFilteredQuery struct {
	cfg    *BenchConfig
	client *mongo.Client
	coll   *mongo.Collection
}

func (o *MongoFilteredQuery) Name() string { return "filtered_query" }

func (o *MongoFilteredQuery) Setup(ctx context.Context) error {
	var err error
	o.client, err = mongoClient(o.cfg)
	if err != nil {
		return err
	}
	o.coll = mongoColl(o.client)
	return o.client.Ping(ctx, nil)
}

func (o *MongoFilteredQuery) Execute(ctx context.Context, rng *rand.Rand) error {
	custIdx := rng.Int63n(NumCustomers)
	custID := fmt.Sprintf("cust_%06d", custIdx)
	status := Statuses[rng.Intn(len(Statuses))]
	priority := 1 + rng.Intn(5)
	opts := options.Find().SetLimit(20)
	cursor, err := o.coll.Find(ctx, bson.M{
		"customerId": custID,
		"status":     status,
		"priority":   priority,
	}, opts)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var results []bson.Raw
	return cursor.All(ctx, &results)
}

func (o *MongoFilteredQuery) Teardown() {
	if o.client != nil {
		o.client.Disconnect(context.Background())
	}
}

// --- Covered Query ---

type MongoCoveredQuery struct {
	cfg    *BenchConfig
	client *mongo.Client
	coll   *mongo.Collection
	zipf   *ScrambledZipfian
}

func (o *MongoCoveredQuery) Name() string { return "covered_query" }

func (o *MongoCoveredQuery) Setup(ctx context.Context) error {
	var err error
	o.client, err = mongoClient(o.cfg)
	if err != nil {
		return err
	}
	o.coll = mongoColl(o.client)
	o.zipf = NewScrambledZipfian(NumOwners)
	return o.client.Ping(ctx, nil)
}

func (o *MongoCoveredQuery) Execute(ctx context.Context, rng *rand.Rand) error {
	ownerIdx := o.zipf.Next(rng)
	ownerID := fmt.Sprintf("owner_%04d", ownerIdx)
	opts := options.Find().
		SetProjection(bson.M{"ownerId": 1, "status": 1, "updatedAt": 1, "_id": 0}).
		SetLimit(20)
	cursor, err := o.coll.Find(ctx, bson.M{
		"ownerId": ownerID,
		"status":  "open",
	}, opts)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var results []bson.Raw
	return cursor.All(ctx, &results)
}

func (o *MongoCoveredQuery) Teardown() {
	if o.client != nil {
		o.client.Disconnect(context.Background())
	}
}

// --- Range Scan ---

type MongoRangeScan struct {
	cfg    *BenchConfig
	client *mongo.Client
	coll   *mongo.Collection
}

func (o *MongoRangeScan) Name() string { return "range_scan" }

func (o *MongoRangeScan) Setup(ctx context.Context) error {
	var err error
	o.client, err = mongoClient(o.cfg)
	if err != nil {
		return err
	}
	o.coll = mongoColl(o.client)
	return o.client.Ping(ctx, nil)
}

func (o *MongoRangeScan) Execute(ctx context.Context, rng *rand.Rand) error {
	baseTime := int64(1687500000000)
	yearMs := int64(365 * 24 * 60 * 60 * 1000)
	hourMs := int64(60 * 60 * 1000)
	start := baseTime + rng.Int63n(yearMs-hourMs)
	end := start + hourMs
	opts := options.Find().SetLimit(100)
	cursor, err := o.coll.Find(ctx, bson.M{
		"createdAt": bson.M{"$gte": start, "$lt": end},
	}, opts)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var results []bson.Raw
	return cursor.All(ctx, &results)
}

func (o *MongoRangeScan) Teardown() {
	if o.client != nil {
		o.client.Disconnect(context.Background())
	}
}

// --- Mixed Read/Write ---

type MongoMixed struct {
	cfg       *BenchConfig
	client    *mongo.Client
	coll      *mongo.Collection
	zipf      *ScrambledZipfian
	insertSeq atomic.Int64
}

func (o *MongoMixed) Name() string { return "mixed_readwrite" }

func (o *MongoMixed) Setup(ctx context.Context) error {
	var err error
	o.client, err = mongoClient(o.cfg)
	if err != nil {
		return err
	}
	o.coll = mongoColl(o.client)
	o.zipf = NewScrambledZipfian(o.cfg.RecordCount)
	o.insertSeq.Store(o.cfg.RecordCount)
	return o.client.Ping(ctx, nil)
}

func (o *MongoMixed) Execute(ctx context.Context, rng *rand.Rand) error {
	roll := rng.Float64()
	switch {
	case roll < 0.60:
		id := RecordID(o.zipf.Next(rng))
		return o.coll.FindOne(ctx, bson.M{"_id": id}).Err()
	case roll < 0.85:
		id := RecordID(o.zipf.Next(rng))
		newStatus := Statuses[rng.Intn(len(Statuses))]
		_, err := o.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
			"$set": bson.M{
				"status":    newStatus,
				"updatedAt": time.Now().UnixMilli(),
			},
		})
		return err
	default:
		seq := o.insertSeq.Add(1)
		rec := GenerateRecord(seq)
		_, err := o.coll.InsertOne(ctx, rec)
		return err
	}
}

func (o *MongoMixed) Teardown() {
	if o.client != nil {
		o.client.Disconnect(context.Background())
	}
}
