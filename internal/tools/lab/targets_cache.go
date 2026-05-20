package lab

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// targetsCache is the small interface the tool consumes so tests can swap in
// an in-memory implementation without touching the filesystem.
type targetsCache interface {
	Get(key string) ([]byte, bool, error)
	Put(key string, value []byte) error
}

// targetsBucket holds one row per cached target key (PDB ID or UniProt AC).
const targetsBucket = "targets"

// TargetsCache is a thin BoltDB-backed key/value cache used by lab.targets_search.
// Each stored value is prefixed with an 8-byte big-endian unix timestamp so
// reads can enforce a TTL without a separate index. The file lives at
// ~/.fova/cache/targets.db when opened via OpenTargetsCacheDefault.
type TargetsCache struct {
	db  *bolt.DB
	ttl time.Duration
}

// OpenTargetsCache opens (or creates) the BoltDB file inside dir with the given
// TTL applied to every read. A zero TTL disables expiry.
func OpenTargetsCache(dir string, ttl time.Duration) (*TargetsCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("targets cache: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "targets.db")
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("targets cache: open %s: %w", path, err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, e := tx.CreateBucketIfNotExists([]byte(targetsBucket))
		return e
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("targets cache: bucket: %w", err)
	}
	return &TargetsCache{db: db, ttl: ttl}, nil
}

// OpenTargetsCacheDefault opens the cache at ~/.fova/cache/targets.db with
// a 7-day TTL.
func OpenTargetsCacheDefault() (*TargetsCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("targets cache: home dir: %w", err)
	}
	return OpenTargetsCache(filepath.Join(home, ".fova", "cache"), 7*24*time.Hour)
}

// Path returns the absolute path of the underlying BoltDB file.
func (c *TargetsCache) Path() string {
	if c == nil || c.db == nil {
		return ""
	}
	return c.db.Path()
}

// Close releases the BoltDB file handle.
func (c *TargetsCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Get returns the cached value for key. ok=false when the key is missing or
// the entry has expired (older than the cache TTL).
func (c *TargetsCache) Get(key string) ([]byte, bool, error) {
	if c == nil || c.db == nil {
		return nil, false, nil
	}
	var raw []byte
	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(targetsBucket))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v != nil {
			raw = append([]byte(nil), v...) // copy out of the txn
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if raw == nil || len(raw) < 8 {
		return nil, false, nil
	}
	ts := time.Unix(int64(binary.BigEndian.Uint64(raw[:8])), 0)
	if c.ttl > 0 && time.Since(ts) > c.ttl {
		return nil, false, nil
	}
	return raw[8:], true, nil
}

// Put stores value under key with the current timestamp.
func (c *TargetsCache) Put(key string, value []byte) error {
	return c.putWithTime(key, value, time.Now())
}

// memTargetsCache is an in-memory targetsCache for tests; it stores raw
// timestamps inside the record so the on-disk and in-memory paths share the
// same TTL semantics.
type memTargetsCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

// newMemTargetsCache returns a fresh in-memory cache with no TTL.
func newMemTargetsCache() *memTargetsCache {
	return &memTargetsCache{m: map[string][]byte{}}
}

// Get returns the stored value for key.
func (c *memTargetsCache) Get(key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), v...), true, nil
}

// Put stores value under key.
func (c *memTargetsCache) Put(key string, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = append([]byte(nil), value...)
	return nil
}

// putWithTime is exposed for tests that need to forge an expired timestamp.
func (c *TargetsCache) putWithTime(key string, value []byte, when time.Time) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("targets cache: not open")
	}
	rec := make([]byte, 8+len(value))
	binary.BigEndian.PutUint64(rec[:8], uint64(when.Unix()))
	copy(rec[8:], value)
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(targetsBucket))
		if b == nil {
			var err error
			b, err = tx.CreateBucket([]byte(targetsBucket))
			if err != nil {
				return err
			}
		}
		return b.Put([]byte(key), rec)
	})
}
