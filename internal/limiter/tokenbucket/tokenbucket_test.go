package tokenbucket

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func setup(t *testing.T) (*TokenBucket, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	return New(client, 5, 1.0), func() { client.Close() }
}

func TestAllow_WithinLimit(t *testing.T) {
	tb, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		allowed, err := tb.allow(ctx, "test", 1, now)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestAllow_ExceedsLimit(t *testing.T) {
	tb, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		tb.allow(ctx, "test", 1, now)
	}

	allowed, err := tb.allow(ctx, "test", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("6th request should be rejected")
	}
}

func TestAllow_RefillOverTime(t *testing.T) {
	tb, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	t0 := time.Now().UnixMilli()
	t1 := t0 + 5000 // 5 seconds later — 5 tokens refilled at rate 1/s

	for i := 0; i < 5; i++ {
		tb.allow(ctx, "test", 1, t0)
	}

	// still rejected at t0
	allowed, _ := tb.allow(ctx, "test", 1, t0)
	if allowed {
		t.Fatal("should be rejected before refill")
	}

	// allowed after 5 seconds
	allowed, err := tb.allow(ctx, "test", 1, t1)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("should be allowed after refill")
	}
}

func TestAllow_WeightedCost(t *testing.T) {
	tb, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// capacity=5, cost=4 allowed
	allowed, _ := tb.allow(ctx, "test", 4, now)
	if !allowed {
		t.Fatal("cost=4 should be allowed with capacity=5")
	}

	// only 1 token left, cost=2 rejected
	allowed, _ = tb.allow(ctx, "test", 2, now)
	if allowed {
		t.Fatal("cost=2 should be rejected with 1 token remaining")
	}
}

func TestAllow_IsolatedKeys(t *testing.T) {
	tb, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		tb.allow(ctx, "key-a", 1, now)
	}

	// key-b is a separate bucket, unaffected
	allowed, err := tb.allow(ctx, "key-b", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("key-b should have its own independent bucket")
	}
}
