package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/livepeer/discovery-service/pkg/discotypes"
	"github.com/redis/go-redis/v9"
)

// Layer provides in-process and optional Redis query result caching.
type Layer struct {
	ttl       time.Duration
	mem       sync.Map
	redis     *redis.Client
	versionFn func() int64
}

type memEntry struct {
	data      discotypes.QueryResponse
	expiresAt time.Time
}

// New creates a cache layer. redisURL empty disables Redis.
func New(ttl time.Duration, redisURL string, versionFn func() int64) (*Layer, error) {
	l := &Layer{ttl: ttl, versionFn: versionFn}
	if redisURL != "" {
		opt, err := redis.ParseURL(redisURL)
		if err != nil {
			return nil, err
		}
		l.redis = redis.NewClient(opt)
	}
	return l, nil
}

// Get returns cached query response if present.
func (l *Layer) Get(ctx context.Context, req discotypes.QueryRequest) (discotypes.QueryResponse, bool) {
	key := l.key(req)
	if v, ok := l.mem.Load(key); ok {
		e := v.(memEntry)
		if time.Now().Before(e.expiresAt) {
			return e.data, true
		}
		l.mem.Delete(key)
	}
	if l.redis != nil {
		val, err := l.redis.Get(ctx, "disc:"+key).Bytes()
		if err == nil {
			var resp discotypes.QueryResponse
			if json.Unmarshal(val, &resp) == nil {
				return resp, true
			}
		}
	}
	return discotypes.QueryResponse{}, false
}

// Set stores a query response.
func (l *Layer) Set(ctx context.Context, req discotypes.QueryRequest, resp discotypes.QueryResponse) {
	key := l.key(req)
	l.mem.Store(key, memEntry{data: resp, expiresAt: time.Now().Add(l.ttl)})
	if l.redis != nil {
		b, _ := json.Marshal(resp)
		_ = l.redis.Set(ctx, "disc:"+key, b, l.ttl).Err()
	}
}

// InvalidateAll clears in-process cache; Redis keys expire by TTL.
func (l *Layer) InvalidateAll() {
	l.mem = sync.Map{}
}

func (l *Layer) key(req discotypes.QueryRequest) string {
	v := int64(0)
	if l.versionFn != nil {
		v = l.versionFn()
	}
	payload, _ := json.Marshal(struct {
		Req     discotypes.QueryRequest `json:"req"`
		Version int64                   `json:"version"`
	}{Req: req, Version: v})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:16])
}

// Close closes Redis if configured.
func (l *Layer) Close() error {
	if l.redis != nil {
		return l.redis.Close()
	}
	return nil
}
