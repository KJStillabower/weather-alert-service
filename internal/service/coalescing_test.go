package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kjstillabower/weather-alert-service/internal/models"
)

func TestRequestCoalescer_GetOrDo_ConcurrentRequests(t *testing.T) {
	coalescer := newRequestCoalescer(5 * time.Second)
	callCount := 0
	var mu sync.Mutex

	fn := func() (models.WeatherData, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		time.Sleep(50 * time.Millisecond) // Simulate API call
		return models.WeatherData{Location: "seattle", Temperature: 10.0}, nil
	}

	// Launch 10 concurrent requests for same key
	var wg sync.WaitGroup
	results := make([]models.WeatherData, 10)
	errs := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = coalescer.GetOrDo(context.Background(), "seattle", fn)
		}(i)
	}
	wg.Wait()

	// Verify all got same result
	for i, result := range results {
		if errs[i] != nil {
			t.Errorf("Request %d error = %v, want nil", i, errs[i])
		}
		if result.Location != "seattle" {
			t.Errorf("Request %d location = %q, want seattle", i, result.Location)
		}
	}

	// Verify fn was called only once (coalescing worked)
	mu.Lock()
	actualCalls := callCount
	mu.Unlock()
	if actualCalls != 1 {
		t.Errorf("fn call count = %d, want 1 (coalescing failed)", actualCalls)
	}
}

func TestRequestCoalescer_GetOrDo_ErrorPropagation(t *testing.T) {
	coalescer := newRequestCoalescer(5 * time.Second)
	wantErr := errors.New("api failure")

	fn := func() (models.WeatherData, error) {
		return models.WeatherData{}, wantErr
	}

	// Launch multiple concurrent requests
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = coalescer.GetOrDo(context.Background(), "seattle", fn)
		}(i)
	}
	wg.Wait()

	// All should get same error
	for i, err := range errs {
		if err == nil {
			t.Errorf("Request %d error = nil, want error", i)
		}
		if !errors.Is(err, wantErr) && err.Error() != wantErr.Error() {
			t.Errorf("Request %d error = %v, want %v", i, err, wantErr)
		}
	}
}

func TestRequestCoalescer_GetOrDo_Timeout(t *testing.T) {
	coalescer := newRequestCoalescer(100 * time.Millisecond)

	fn := func() (models.WeatherData, error) {
		time.Sleep(200 * time.Millisecond) // Longer than timeout
		return models.WeatherData{Location: "seattle"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := coalescer.GetOrDo(ctx, "seattle", fn)
	if err == nil {
		t.Fatal("GetOrDo() error = nil, want timeout error")
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("GetOrDo() error = %v, want context deadline exceeded or canceled", err)
	}
}

func TestRequestCoalescer_GetOrDo_DifferentKeys(t *testing.T) {
	coalescer := newRequestCoalescer(5 * time.Second)
	callCount := 0
	var mu sync.Mutex

	fn := func() (models.WeatherData, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return models.WeatherData{Location: "test"}, nil
	}

	// Different keys should not coalesce
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, _ = coalescer.GetOrDo(context.Background(), key, fn)
		}("key" + string(rune('a'+i)))
	}
	wg.Wait()

	mu.Lock()
	actualCalls := callCount
	mu.Unlock()
	if actualCalls != 5 {
		t.Errorf("fn call count = %d, want 5 (no coalescing for different keys)", actualCalls)
	}
}
