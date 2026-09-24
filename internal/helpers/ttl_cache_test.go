package helpers

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewTTLCacheRejectsInvalidCapacity(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewTTLCache did not panic for a zero capacity")
		}
	}()
	NewTTLCache[int, int](0, time.Minute, time.Minute)
}

func TestNewTTLCacheRejectsInvalidDurations(t *testing.T) {
	tests := []struct {
		name    string
		ttl     time.Duration
		cleanup time.Duration
	}{
		{name: "TTL", ttl: 0, cleanup: time.Minute},
		{name: "cleanup", ttl: time.Minute, cleanup: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("NewTTLCache did not panic")
				}
			}()
			NewTTLCache[int, int](1, test.ttl, test.cleanup)
		})
	}
}

func TestTTLCacheCapacityAndLRU(t *testing.T) {
	now := time.Unix(100, 0)
	cache := NewTTLCache[int, string](2, time.Hour, time.Minute)
	cache.now = func() time.Time { return now }

	load := func(value string) func() (string, error) {
		return func() (string, error) { return value, nil }
	}
	if _, err := cache.GetOrLoad(1, load("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.GetOrLoad(2, load("two")); err != nil {
		t.Fatal(err)
	}
	if value, ok := cache.Get(1); !ok || value != "one" {
		t.Fatalf("Get(1) = %q, %v; want one, true", value, ok)
	}
	if _, err := cache.GetOrLoad(3, load("three")); err != nil {
		t.Fatal(err)
	}

	if _, ok := cache.Get(2); ok {
		t.Fatal("least recently used entry was not evicted")
	}
	if value, ok := cache.Get(1); !ok || value != "one" {
		t.Fatalf("Get(1) after eviction = %q, %v; want one, true", value, ok)
	}
	if value, ok := cache.Get(3); !ok || value != "three" {
		t.Fatalf("Get(3) after eviction = %q, %v; want three, true", value, ok)
	}

	cache.cleanup(now.Add(2*time.Hour), cache.cleaner)
}

func TestTTLCacheSlidingExpiration(t *testing.T) {
	now := time.Unix(100, 0)
	cache := NewTTLCache[string, int](1, time.Minute, time.Minute)
	cache.now = func() time.Time { return now }
	loads := 0
	loader := func() (int, error) {
		loads++
		return loads, nil
	}

	if value, err := cache.GetOrLoad("key", loader); err != nil || value != 1 {
		t.Fatalf("first load = %d, %v; want 1, nil", value, err)
	}
	now = now.Add(45 * time.Second)
	if value, ok := cache.Get("key"); !ok || value != 1 {
		t.Fatalf("Get before expiry = %d, %v; want 1, true", value, ok)
	}
	now = now.Add(45 * time.Second)
	if value, ok := cache.Get("key"); !ok || value != 1 {
		t.Fatalf("Get after original expiry = %d, %v; want renewed entry", value, ok)
	}
	now = now.Add(61 * time.Second)
	if _, ok := cache.Get("key"); ok {
		t.Fatal("entry did not expire after the renewed TTL")
	}
	if cache.cleaner != nil {
		t.Fatal("cleaner still running after the cache became empty")
	}
}

func TestTTLCacheLoaderErrorIsNotCached(t *testing.T) {
	cache := NewTTLCache[string, int](1, time.Minute, time.Minute)
	wantErr := errors.New("load failed")
	loads := 0
	loader := func() (int, error) {
		loads++
		if loads == 1 {
			return 0, wantErr
		}
		return 7, nil
	}

	if _, err := cache.GetOrLoad("key", loader); !errors.Is(err, wantErr) {
		t.Fatalf("first load error = %v, want %v", err, wantErr)
	}
	if _, ok := cache.Get("key"); ok {
		t.Fatal("failed load was cached")
	}
	if value, err := cache.GetOrLoad("key", loader); err != nil || value != 7 {
		t.Fatalf("retry = %d, %v; want 7, nil", value, err)
	}
	if loads != 2 {
		t.Fatalf("loader called %d times, want 2", loads)
	}

	cache.cleanup(time.Now().Add(2*time.Minute), cache.cleaner)
}

func TestTTLCacheGetOrLoadHit(t *testing.T) {
	cache := NewTTLCache[string, int](1, time.Minute, time.Minute)
	loads := 0
	loader := func() (int, error) {
		loads++
		return 5, nil
	}
	if _, err := cache.GetOrLoad("key", loader); err != nil {
		t.Fatal(err)
	}
	if value, err := cache.GetOrLoad("key", loader); err != nil || value != 5 {
		t.Fatalf("cache hit = %d, %v; want 5, nil", value, err)
	}
	if loads != 1 {
		t.Fatalf("loader called %d times, want 1", loads)
	}

	cache.cleanup(time.Now().Add(2*time.Minute), cache.cleaner)
}

func TestTTLCacheRejectsNilLoader(t *testing.T) {
	cache := NewTTLCache[int, int](1, time.Minute, time.Minute)
	defer func() {
		if recover() == nil {
			t.Fatal("GetOrLoad did not panic for a nil loader")
		}
	}()
	cache.GetOrLoad(1, nil)
}

func TestTTLCacheCombinesConcurrentLoads(t *testing.T) {
	cache := NewTTLCache[string, int](1, time.Minute, time.Minute)
	var loads atomic.Int32
	loaderStarted := make(chan struct{})
	releaseLoader := make(chan struct{})
	loader := func() (int, error) {
		if loads.Add(1) == 1 {
			close(loaderStarted)
		}
		<-releaseLoader
		return 9, nil
	}

	const callers = 16
	start := make(chan struct{})
	results := make(chan int, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value, err := cache.GetOrLoad("key", loader)
			results <- value
			errors <- err
		}()
	}
	close(start)
	<-loaderStarted
	close(releaseLoader)
	wg.Wait()
	close(results)
	close(errors)

	if got := loads.Load(); got != 1 {
		t.Fatalf("loader called %d times, want 1", got)
	}
	for value := range results {
		if value != 9 {
			t.Fatalf("concurrent result = %d, want 9", value)
		}
	}
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent load error = %v", err)
		}
	}

	cache.cleanup(time.Now().Add(2*time.Minute), cache.cleaner)
}

func TestTTLCacheLoadsDifferentKeysConcurrently(t *testing.T) {
	cache := NewTTLCache[int, int](2, time.Minute, time.Minute)
	started := make(chan int, 2)
	release := make(chan struct{})
	load := func(key int) func() (int, error) {
		return func() (int, error) {
			started <- key
			<-release
			return key, nil
		}
	}

	var wg sync.WaitGroup
	for key := 1; key <= 2; key++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.GetOrLoad(key, load(key)); err != nil {
				t.Errorf("GetOrLoad(%d): %v", key, err)
			}
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("loaders for different keys did not run concurrently")
		}
	}
	close(release)
	wg.Wait()

	cache.cleanup(time.Now().Add(2*time.Minute), cache.cleaner)
}

func TestTTLCacheCleanerLifecycle(t *testing.T) {
	now := time.Unix(100, 0)
	cache := NewTTLCache[string, int](1, time.Minute, time.Minute)
	cache.now = func() time.Time { return now }
	if cache.cleaner != nil {
		t.Fatal("empty cache started a cleaner")
	}
	loader := func() (int, error) { return 1, nil }
	if _, err := cache.GetOrLoad("key", loader); err != nil {
		t.Fatal(err)
	}
	firstCleaner := cache.cleaner
	if firstCleaner == nil {
		t.Fatal("first insertion did not start a cleaner")
	}
	if cache.cleanup(now.Add(2*time.Minute), firstCleaner) {
		t.Fatal("cleaner remained active after removing the final entry")
	}
	if cache.cleaner != nil {
		t.Fatal("cache retained the stopped cleaner")
	}

	now = now.Add(3 * time.Minute)
	if _, err := cache.GetOrLoad("key", loader); err != nil {
		t.Fatal(err)
	}
	secondCleaner := cache.cleaner
	if secondCleaner == nil || secondCleaner == firstCleaner {
		t.Fatal("insertion did not start a new cleaner")
	}
	if cache.cleanup(now, firstCleaner) {
		t.Fatal("stale cleaner was accepted as current")
	}
	if cache.cleaner != secondCleaner {
		t.Fatal("stale cleaner disturbed the current cleaner")
	}

	cache.cleanup(now.Add(2*time.Minute), secondCleaner)
}
