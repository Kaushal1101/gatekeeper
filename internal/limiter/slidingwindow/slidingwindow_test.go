package slidingwindow

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func setup(t *testing.T) (*SlidingWindow, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	return New(client, 60, 5), func() { client.Close() }
}

func TestAllow_WithinLimit(t *testing.T) {
	sw, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		allowed, err := sw.allow(ctx, "test", 1, now)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestAllow_ExceedsLimit(t *testing.T) {
	sw, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		sw.allow(ctx, "test", 1, now)
	}

	allowed, err := sw.allow(ctx, "test", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("6th request should be rejected")
	}
}

func TestAllow_WindowReset(t *testing.T) {
	sw, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	t0 := time.Now().UnixMilli()
	t1 := t0 + 61000 // 61s later — fully past the 60s window

	for i := 0; i < 5; i++ {
		sw.allow(ctx, "test", 1, t0)
	}

	allowed, _ := sw.allow(ctx, "test", 1, t0)
	if allowed {
		t.Fatal("should be rejected before window reset")
	}

	allowed, err := sw.allow(ctx, "test", 1, t1)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("should be allowed after full window has passed")
	}
}

func TestAllow_WeightedCost(t *testing.T) {
	sw, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// limit=5, cost=4 allowed
	allowed, _ := sw.allow(ctx, "test", 4, now)
	if !allowed {
		t.Fatal("cost=4 should be allowed with limit=5")
	}

	// only 1 unit left, cost=2 rejected
	allowed, _ = sw.allow(ctx, "test", 2, now)
	if allowed {
		t.Fatal("cost=2 should be rejected with 1 unit remaining")
	}
}

func TestAllow_PreviousWindowWeight(t *testing.T) {
	sw, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	// Fill previous window completely at t0 (start of some window)
	t0 := int64(0) // epoch start, clean window boundary
	windowMs := int64(60 * 1000)
	// Move to start of a window for predictability
	now := (time.Now().UnixMilli()/windowMs)*windowMs + windowMs // start of next clean window

	// Fill the previous window
	for i := 0; i < 5; i++ {
		sw.allow(ctx, "prev-test", 1, now-windowMs)
	}

	// At 50% into current window: 50% of prev window still counts = 2.5 effective
	// With limit=5, we should still have 2.5 units available
	halfwayNow := now + windowMs/2
	_ = t0 // suppress unused warning

	allowed, err := sw.allow(ctx, "prev-test", 2, halfwayNow)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("cost=2 should be allowed: 2.5 effective from prev window leaves room")
	}

	// But cost=3 should be rejected (2.5 + 2 already used + 3 > 5)
	allowed, _ = sw.allow(ctx, "prev-test", 3, halfwayNow)
	if allowed {
		t.Fatal("cost=3 should be rejected: would exceed limit with previous window weight")
	}
}

func TestAllow_IsolatedKeys(t *testing.T) {
	sw, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		sw.allow(ctx, "key-a", 1, now)
	}

	allowed, err := sw.allow(ctx, "key-b", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("key-b should have its own independent window")
	}
}
