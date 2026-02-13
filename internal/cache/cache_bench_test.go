package cache

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

// createTestWeatherData creates test weather data for benchmarks.
func createTestWeatherData(location string) models.WeatherData {
	return models.WeatherData{
		Location:    location,
		Temperature: 15.5,
		Conditions:  "Clear",
		Humidity:    65,
		WindSpeed:   10.2,
		Timestamp:   time.Now(),
	}
}

// BenchmarkInMemoryCache_Get_Hit benchmarks cache Get operation on cache hit.
func BenchmarkInMemoryCache_Get_Hit(b *testing.B) {
	cache := NewInMemoryCache()
	ctx := context.Background()
	testData := createTestWeatherData("seattle")

	// Pre-populate cache
	cache.Set(ctx, "seattle", testData, 5*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = cache.Get(ctx, "seattle")
	}
}

// BenchmarkInMemoryCache_Get_Miss benchmarks cache Get operation on cache miss.
func BenchmarkInMemoryCache_Get_Miss(b *testing.B) {
	cache := NewInMemoryCache()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = cache.Get(ctx, "nonexistent")
	}
}

// BenchmarkInMemoryCache_Set benchmarks cache Set operation.
func BenchmarkInMemoryCache_Set(b *testing.B) {
	cache := NewInMemoryCache()
	ctx := context.Background()
	testData := createTestWeatherData("seattle")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Set(ctx, "seattle", testData, 5*time.Minute)
	}
}

// BenchmarkInMemoryCache_Concurrent benchmarks concurrent cache operations.
func BenchmarkInMemoryCache_Concurrent(b *testing.B) {
	cache := NewInMemoryCache()
	ctx := context.Background()
	testData := createTestWeatherData("seattle")
	cache.Set(ctx, "seattle", testData, 5*time.Minute)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = cache.Get(ctx, "seattle")
		}
	})
}

// BenchmarkMemcachedCache_Get_Hit benchmarks Memcached Get on cache hit.
// Requires: Memcached running (skip if unavailable).
func BenchmarkMemcachedCache_Get_Hit(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping Memcached benchmark in short mode")
	}

	cache, err := NewMemcachedCache("localhost:11211", 500*time.Millisecond, 2)
	if err != nil {
		b.Skipf("Memcached not available: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	testData := createTestWeatherData("seattle")
	cache.Set(ctx, "seattle", testData, 5*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = cache.Get(ctx, "seattle")
	}
}

// BenchmarkMemcachedCache_Get_Miss benchmarks Memcached Get on cache miss.
func BenchmarkMemcachedCache_Get_Miss(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping Memcached benchmark in short mode")
	}

	cache, err := NewMemcachedCache("localhost:11211", 500*time.Millisecond, 2)
	if err != nil {
		b.Skipf("Memcached not available: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = cache.Get(ctx, "nonexistent")
	}
}

// BenchmarkMemcachedCache_Set benchmarks Memcached Set operation.
func BenchmarkMemcachedCache_Set(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping Memcached benchmark in short mode")
	}

	cache, err := NewMemcachedCache("localhost:11211", 500*time.Millisecond, 2)
	if err != nil {
		b.Skipf("Memcached not available: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	testData := createTestWeatherData("seattle")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Set(ctx, "seattle", testData, 5*time.Minute)
	}
}

// BenchmarkInMemoryCache_MemoryPerEntry estimates memory usage per cache entry.
func BenchmarkInMemoryCache_MemoryPerEntry(b *testing.B) {
	cache := NewInMemoryCache()
	ctx := context.Background()
	testData := createTestWeatherData("seattle")

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < b.N; i++ {
		cache.Set(ctx, "key"+string(rune(i)), testData, 5*time.Minute)
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	bytesPerEntry := float64(m2.Alloc-m1.Alloc) / float64(b.N)
	b.ReportMetric(bytesPerEntry, "bytes/entry")
}
