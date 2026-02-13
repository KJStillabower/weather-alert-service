//go:build integration
// +build integration

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

// TestMemcachedCache_GetSet_Integration verifies that MemcachedCache successfully
// stores and retrieves values when memcached server is available.
func TestMemcachedCache_GetSet_Integration(t *testing.T) {
	c, err := NewMemcachedCache("localhost:11211", 500*time.Millisecond, 2)
	if err != nil {
		t.Fatalf("NewMemcachedCache() error = %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	val := models.WeatherData{Location: "seattle", Temperature: 12.5}
	if err := c.Set(ctx, "seattle", val, time.Minute); err != nil {
		t.Skipf("Set failed (memcached may not be running): %v", err)
	}

	got, ok, err := c.Get(ctx, "seattle")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.Location != val.Location || got.Temperature != val.Temperature {
		t.Errorf("Get() = %+v, want %+v", got, val)
	}
}

// TestMemcachedCache_Get_Miss_Integration verifies that MemcachedCache returns
// ok=false when requested key does not exist in memcached.
func TestMemcachedCache_Get_Miss_Integration(t *testing.T) {
	c, err := NewMemcachedCache("localhost:11211", 500*time.Millisecond, 2)
	if err != nil {
		t.Fatalf("NewMemcachedCache() error = %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	_, ok, err := c.Get(ctx, "nonexistent")
	if err != nil {
		t.Skipf("Get failed (memcached may not be running): %v", err)
	}
	if ok {
		t.Error("Get() ok = true, want false for miss")
	}
}
