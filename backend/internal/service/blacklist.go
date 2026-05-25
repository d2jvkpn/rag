package service

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// TokenBlacklist tracks revoked JWT IDs.
type TokenBlacklist interface {
	Block(jti string, expiry time.Time) error
	IsBlocked(jti string) (bool, error)
}

// MemoryBlacklist is an in-process implementation — survives only for the
// lifetime of the process, sufficient when Redis is not configured.
type MemoryBlacklist struct {
	mu      sync.RWMutex
	entries map[string]time.Time // jti → expiry
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

func NewMemoryBlacklist() *MemoryBlacklist {
	b := &MemoryBlacklist{
		entries: make(map[string]time.Time),
		done:    make(chan struct{}),
	}
	b.wg.Add(1)
	go b.sweep()
	return b
}

func (b *MemoryBlacklist) Block(jti string, expiry time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[jti] = expiry
	return nil
}

func (b *MemoryBlacklist) IsBlocked(jti string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	expiry, ok := b.entries[jti]
	return ok && time.Now().Before(expiry), nil
}

func (b *MemoryBlacklist) sweep() {
	defer b.wg.Done()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			b.mu.Lock()
			now := time.Now()
			for jti, expiry := range b.entries {
				if now.After(expiry) {
					delete(b.entries, jti)
				}
			}
			b.mu.Unlock()
		}
	}
}

func (b *MemoryBlacklist) Close() error {
	b.once.Do(func() {
		close(b.done)
	})
	b.wg.Wait()
	return nil
}

// RedisBlacklist stores revoked JTIs in Redis with automatic TTL expiry.
type RedisBlacklist struct {
	client *redis.Client
}

func NewRedisBlacklist(client *redis.Client) *RedisBlacklist {
	return &RedisBlacklist{client: client}
}

func (b *RedisBlacklist) Block(jti string, expiry time.Time) error {
	ttl := time.Until(expiry)
	if ttl <= 0 {
		return nil
	}
	return b.client.Set(context.Background(), "rag:revoked:"+jti, "1", ttl).Err()
}

func (b *RedisBlacklist) IsBlocked(jti string) (bool, error) {
	n, err := b.client.Exists(context.Background(), "rag:revoked:"+jti).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (b *RedisBlacklist) Close() error {
	return b.client.Close()
}
