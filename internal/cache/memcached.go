package cache

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/kjstillabower/weather-alert-service/internal/models"
)

const keyPrefix = "weather:"

// MemcachedCache implements Cache using memcached.
type MemcachedCache struct {
	client *memcache.Client
}

// NewMemcachedCache creates a MemcachedCache. addrs is a comma-separated list
// (e.g. "localhost:11211" or "host1:11211,host2:11211"). timeout and maxIdleConns
// configure the client; both use package defaults if zero.
func NewMemcachedCache(addrs string, timeout time.Duration, maxIdleConns int) (*MemcachedCache, error) {
	servers := parseAddrs(addrs)
	if len(servers) == 0 {
		servers = []string{"localhost:11211"}
	}
	client := memcache.New(servers...)
	if timeout > 0 {
		client.Timeout = timeout
	}
	if maxIdleConns > 0 {
		client.MaxIdleConns = maxIdleConns
	}
	return &MemcachedCache{client: client}, nil
}

func parseAddrs(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func (c *MemcachedCache) key(k string) string {
	return keyPrefix + k
}

// Get implements Cache.Get. Returns false, nil on cache miss; false, err on error.
func (c *MemcachedCache) Get(ctx context.Context, key string) (models.WeatherData, bool, error) {
	if ctx.Err() != nil {
		return models.WeatherData{}, false, ctx.Err()
	}
	item, err := c.client.Get(c.key(key))
	if err != nil {
		if err == memcache.ErrCacheMiss {
			return models.WeatherData{}, false, nil
		}
		return models.WeatherData{}, false, err
	}
	var data models.WeatherData
	if err := json.Unmarshal(item.Value, &data); err != nil {
		return models.WeatherData{}, false, err
	}
	return data, true, nil
}

// Set implements Cache.Set.
func (c *MemcachedCache) Set(ctx context.Context, key string, value models.WeatherData, ttl time.Duration) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	expSec := int32(ttl.Seconds())
	const maxRelativeExp = 30 * 24 * 60 * 60 // 30 days
	if expSec <= 0 || expSec > maxRelativeExp {
		expSec = 3600 // fallback 1h if invalid
	}
	return c.client.Set(&memcache.Item{
		Key:        c.key(key),
		Value:      raw,
		Expiration: expSec,
	})
}

// Ping checks if memcached is reachable. Used for health checks.
func (c *MemcachedCache) Ping() error {
	return c.client.Ping()
}

// Close closes the memcached client connections. Call during shutdown.
func (c *MemcachedCache) Close() error {
	return c.client.Close()
}
