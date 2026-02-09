package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"relentless-bookworm/internal/models"
)

// RedisStatusStore stores crawl status in Redis.
type RedisStatusStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisStatusStore initializes a Redis-backed StatusStore.
func NewRedisStatusStore(addr, prefix string, ttl time.Duration) *RedisStatusStore {
	return &RedisStatusStore{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		prefix: prefix,
		ttl:    ttl,
	}
}

// Close closes the Redis client.
func (s *RedisStatusStore) Close() error {
	return s.client.Close()
}

// SetStatus writes the status record to Redis.
func (s *RedisStatusStore) SetStatus(ctx context.Context, status models.CrawlStatus) error {
	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	key := s.prefix + status.SessionID
	return s.client.Set(ctx, key, payload, s.ttl).Err()
}

// GetStatus reads the status record from Redis.
func (s *RedisStatusStore) GetStatus(ctx context.Context, sessionID string) (models.CrawlStatus, bool, error) {
	key := s.prefix + sessionID
	val, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return models.CrawlStatus{}, false, nil
		}
		return models.CrawlStatus{}, false, err
	}

	var status models.CrawlStatus
	if err := json.Unmarshal([]byte(val), &status); err != nil {
		return models.CrawlStatus{}, false, err
	}

	return status, true, nil
}
