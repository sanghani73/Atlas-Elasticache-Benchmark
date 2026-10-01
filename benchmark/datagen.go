package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"
	"sync"
)

var (
	Types      = []string{"phone", "chat", "email", "sms"}
	Statuses   = []string{"open", "in_progress", "resolved", "escalated"}
	Categories = []string{"network", "billing", "hardware", "software", "account", "delivery", "returns", "general"}
	Regions    = []string{
		"london", "southeast", "southwest", "east_anglia", "east_midlands",
		"west_midlands", "northwest", "northeast", "yorkshire", "wales",
		"scotland", "northern_ireland",
	}
	TagsPool = []string{
		"broadband", "speed", "fault", "billing", "refund", "upgrade",
		"installation", "cancellation", "complaint", "feedback",
		"mobile", "landline", "fibre", "router", "outage",
		"account", "password", "payment", "contract", "support",
	}
	NumCustomers int64 = 50000
	NumOwners    int64 = 5000
)

type Record struct {
	ID         string   `bson:"_id" json:"_id"`
	CustomerID string   `bson:"customerId" json:"customerId"`
	Type       string   `bson:"type" json:"type"`
	Status     string   `bson:"status" json:"status"`
	Priority   int      `bson:"priority" json:"priority"`
	OwnerID    string   `bson:"ownerId" json:"ownerId"`
	Category   string   `bson:"category" json:"category"`
	Region     string   `bson:"region" json:"region"`
	CreatedAt  int64    `bson:"createdAt" json:"createdAt"`
	UpdatedAt  int64    `bson:"updatedAt" json:"updatedAt"`
	Tags       []string `bson:"tags" json:"tags"`
	Payload    string   `bson:"payload" json:"payload"`
}

func RecordID(index int64) string {
	return fmt.Sprintf("record_%010d", index)
}

func CustomerID(index int64) string {
	return fmt.Sprintf("cust_%06d", index%NumCustomers)
}

func OwnerID(index int64) string {
	return fmt.Sprintf("owner_%04d", index%NumOwners)
}

func GenerateRecord(index int64) Record {
	rng := rand.New(rand.NewSource(index))

	baseTime := int64(1687500000000)
	yearMs := int64(365 * 24 * 60 * 60 * 1000)
	createdAt := baseTime + rng.Int63n(yearMs)
	weekMs := int64(7 * 24 * 60 * 60 * 1000)
	updatedAt := createdAt + rng.Int63n(weekMs)

	numTags := 1 + rng.Intn(4)
	tags := make([]string, numTags)
	for i := range tags {
		tags[i] = TagsPool[rng.Intn(len(TagsPool))]
	}

	return Record{
		ID:         RecordID(index),
		CustomerID: CustomerID(index),
		Type:       Types[rng.Intn(len(Types))],
		Status:     Statuses[rng.Intn(len(Statuses))],
		Priority:   1 + rng.Intn(5),
		OwnerID:    OwnerID(index),
		Category:   Categories[rng.Intn(len(Categories))],
		Region:     Regions[rng.Intn(len(Regions))],
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Tags:       tags,
		Payload:    generatePayload(rng, 500),
	}
}

func generatePayload(rng *rand.Rand, length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .,;:!?"
	var sb strings.Builder
	sb.Grow(length)
	for i := 0; i < length; i++ {
		sb.WriteByte(chars[rng.Intn(len(chars))])
	}
	return sb.String()
}

// ScrambledZipfian implements a scrambled Zipfian distribution following the YCSB approach.
// It maps items to a Zipfian distribution but scrambles the mapping so hot items
// are spread across the keyspace rather than clustered at the beginning.
type ScrambledZipfian struct {
	mu       sync.Mutex
	items    int64
	base     int64
	zipf     *ZipfianGenerator
	itemsMod int64
}

func NewScrambledZipfian(items int64) *ScrambledZipfian {
	return &ScrambledZipfian{
		items:    items,
		base:     0,
		zipf:     NewZipfianGenerator(items),
		itemsMod: items,
	}
}

func (s *ScrambledZipfian) Next(rng *rand.Rand) int64 {
	v := s.zipf.Next(rng)
	return s.base + scramble(v, s.itemsMod)
}

func scramble(value, items int64) int64 {
	h := fnv.New64a()
	b := make([]byte, 8)
	b[0] = byte(value)
	b[1] = byte(value >> 8)
	b[2] = byte(value >> 16)
	b[3] = byte(value >> 24)
	b[4] = byte(value >> 32)
	b[5] = byte(value >> 40)
	b[6] = byte(value >> 48)
	b[7] = byte(value >> 56)
	h.Write(b)
	return int64(h.Sum64()%uint64(items)) // nolint: gosec
}

// ZipfianGenerator produces Zipfian-distributed values in [0, items).
type ZipfianGenerator struct {
	items    int64
	theta    float64
	zeta2    float64
	zetaN    float64
	alpha    float64
	eta      float64
	countFzn float64
}

func NewZipfianGenerator(items int64) *ZipfianGenerator {
	const theta = 0.99
	zeta2 := zetaStatic(2, theta)
	zetaN := zetaStatic(items, theta)
	alpha := 1.0 / (1.0 - theta)
	eta := (1 - math.Pow(2.0/float64(items), 1.0-theta)) / (1 - zeta2/zetaN)
	return &ZipfianGenerator{
		items:    items,
		theta:    theta,
		zeta2:    zeta2,
		zetaN:    zetaN,
		alpha:    alpha,
		eta:      eta,
		countFzn: float64(items),
	}
}

func (z *ZipfianGenerator) Next(rng *rand.Rand) int64 {
	u := rng.Float64()
	uz := u * z.zetaN
	if uz < 1.0 {
		return 0
	}
	if uz < 1.0+math.Pow(0.5, z.theta) {
		return 1
	}
	return int64(z.countFzn * math.Pow(z.eta*u-z.eta+1.0, z.alpha))
}

func zetaStatic(n int64, theta float64) float64 {
	sum := 0.0
	limit := n
	if limit > 10000000 {
		limit = 10000000
	}
	for i := int64(0); i < limit; i++ {
		sum += 1.0 / math.Pow(float64(i+1), theta)
	}
	if n > limit {
		sum += (math.Pow(float64(n), 1.0-theta) - math.Pow(float64(limit), 1.0-theta)) / (1.0 - theta)
	}
	return sum
}
