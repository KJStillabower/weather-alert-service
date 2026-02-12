package cache

import (
	"context"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

func TestInMemoryCache_GetSet(t *testing.T) {
	ctx := context.Background()
	c := NewInMemoryCache()

	val := models.WeatherData{Location: "seattle", Temperature: 12.5}
	err := c.Set(ctx, "seattle", val, time.Minute)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
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

func TestInMemoryCache_Get_Miss(t *testing.T) {
	ctx := context.Background()
	c := NewInMemoryCache()

	_, ok, err := c.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() ok = true, want false for miss")
	}
}

func TestInMemoryCache_Get_Expired(t *testing.T) {
	ctx := context.Background()
	c := NewInMemoryCache()

	val := models.WeatherData{Location: "seattle"}
	err := c.Set(ctx, "seattle", val, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	time.Sleep(2 * time.Millisecond)

	_, ok, err := c.Get(ctx, "seattle")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() ok = true, want false for expired entry")
	}

	// Expired entry should be removed
	_, ok2, _ := c.Get(ctx, "seattle")
	if ok2 {
		t.Error("Expired entry should be deleted from cache")
	}
}
