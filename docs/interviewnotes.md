# Interview Notes

---

# `internal/limiter` — Rate Limiting Package

---

## 1. High-Level Purpose

This package is the core of GateKeeper. It defines the contract for rate limiting (`Limiter` interface) and provides two concrete implementations — Token Bucket and Sliding Window Counter — each backed by a Lua script that executes atomically inside Redis.

Without this package, the gateway has no mechanism to enforce limits. Everything else (the policy engine, the middleware, the gateway itself) calls into this package to make allow/reject decisions.

---

## 2. Core Concepts

### Strategy Pattern
`Limiter` is an interface with one method. Both algorithms implement it. The gateway never knows which algorithm it's talking to — it just calls `Allow`. This means you can add a third algorithm (e.g. Fixed Window, Leaky Bucket) without touching the gateway code at all.

### Why Lua scripts in Redis
Redis is single-threaded. Lua scripts execute as a single atomic unit — no other command can run between the first and last line of the script. This is critical for rate limiting: the sequence "read current count → check limit → update count" must be atomic. Without Lua, two gateway instances could both read the same token count, both decide the request is allowed, and both decrement — allowing a request that should have been rejected. This is the classic TOCTOU (time-of-check to time-of-use) race condition.

The alternative is Redis transactions (`WATCH`/`MULTI`/`EXEC`) with retry logic, which is more complex and slower under contention.

### Key design: plain string key
The `Limiter` interface takes `key string`, not structured fields like IP or user ID. The caller constructs the key. This keeps the algorithms reusable across any scope — the policy engine decides whether the key is `"ip:1.2.3.4"` or `"user:42:POST:/api/expensive"`.

### Weighted cost
Every `Allow` call takes a `cost int`. A lightweight endpoint might cost 1 token; an expensive endpoint might cost 5. This lets a single rate limit policy reflect real resource consumption rather than assuming all requests are equal.

### Why Redis over an in-process cache

An in-process cache (e.g. a Go `map` protected by a `sync.Mutex`) lives inside a single process's RAM. Reading it requires no network — the CPU reaches directly into memory (~100ns). But GateKeeper runs two gateway instances. Each would maintain its own separate copy of every counter, so a user hitting gateway-1 and gateway-2 alternately would see two independent limits and effectively double their allowance. The rate limiter would be broken.

Redis is a separate process that both gateways connect to, making it a single shared source of truth. The cost is a **network hop** — even on the same machine, Go must send data out through a network socket, Redis processes it, and sends a response back (~0.1ms locally). This is roughly 1000× slower than an in-process read, but still imperceptible on a per-request basis and a necessary tradeoff for correctness across instances.

```
In-process:  CPU → RAM                              ~100ns   (no hop)
Redis:       CPU → network socket → Redis → back    ~0.1ms   (one round-trip hop)
```

The mutex approach would work for a single gateway but fails the moment you scale horizontally. Redis is the correct tool precisely because it externalises state.

### Clock injection for testability
Both implementations have a public `Allow(ctx, key, cost)` and an unexported `allow(ctx, key, cost, nowMs int64)`. The public method calls `time.Now().UnixMilli()`. The unexported method is called directly in tests with explicit timestamps, making time-dependent tests (refill, window reset) instant and deterministic — no sleeping required.

---

## 3. Important Implementation Details

### `internal/limiter/limiter.go`
```go
type Limiter interface {
    Allow(ctx context.Context, key string, cost int) (bool, error)
}
```
The entire algorithm abstraction is this one interface. Both `*TokenBucket` and `*SlidingWindow` satisfy it implicitly — Go interfaces are implemented structurally, not with `implements` keywords.

### Token Bucket — Redis data structure
Each bucket is a **Redis hash** with two fields: `tokens` (float) and `last_refill` (int64 ms timestamp). Both are stored under a single key so TTL can be set with one `EXPIRE` call.

### Token Bucket — Lua logic
1. `HMGET` to fetch `tokens` and `last_refill`
2. If nil (first request), initialise to full capacity
3. Calculate `elapsed = (now - last_refill) / 1000.0` (convert ms to seconds)
4. Refill: `new_tokens = min(capacity, tokens + elapsed * refill_rate)`
5. If `new_tokens < cost` → save state, return 0 (reject)
6. Otherwise → deduct cost, save state, return 1 (allow)
7. `EXPIRE` is reset on every call to `ceil(capacity / refill_rate) * 2` seconds — enough time for a full refill, doubled as safety margin

### Sliding Window — Redis data structure
Two **string keys** per logical bucket:
- `{key}:{current_window_ms}` → count in the current fixed window
- `{key}:{prev_window_ms}` → count in the previous fixed window

Window boundaries are calculated by flooring `now` to the nearest window boundary: `math.floor(now / window_ms) * window_ms`.

### Sliding Window — Lua logic
1. Calculate `current_window` and `prev_window` timestamps
2. `GET` both counts (defaulting to 0 if nil)
3. Compute weighted effective count: `prev_count * (1 - elapsed_fraction) + curr_count`
   - `elapsed_fraction = (now - current_window) / window_ms`
   - At 0% into current window: 100% of prev applies
   - At 50% into current window: 50% of prev applies
   - At 100% into current window: 0% of prev applies (it's now the new prev)
4. If `effective_count + cost > limit` → return 0
5. `INCRBY curr_key cost` and set `EXPIRE` to `window_size * 2`

### `//go:embed` directive
```go
//go:embed token_bucket.lua
var luaScript string
```
This bakes the Lua file contents into the compiled binary at build time. The Lua script is not read from disk at runtime — it's compiled in. The path must be relative to the `.go` file and cannot use `..` to reference parent directories. This is why scripts are colocated with their package rather than in a top-level `lua/` folder.

### `goredis.NewScript(luaScript)`
Creates a `*redis.Script` object which uses `EVALSHA` for efficient execution (sends only the SHA of the script, not the full text, on repeat calls). Falls back to `EVAL` if the script hasn't been loaded into Redis yet. This is transparent — you just call `.Run()`.

### miniredis in tests
`miniredis.RunT(t)` starts an in-memory Redis server that is automatically stopped when the test ends. The test connects a real go-redis client to it. This means Lua scripts actually execute (miniredis uses `gopher-lua` internally) without requiring a running Redis instance.

---

## 4. Interview Questions

**Q: Why use Redis instead of an in-process cache like a Go map?**
A: GateKeeper runs two gateway instances behind a load balancer. An in-process cache lives inside one process — each gateway would have its own independent counters, so a client alternating between gateway-1 and gateway-2 would see two separate limits and effectively double their allowance. Redis is a single external process that both gateways share, so every counter is consistent regardless of which instance handles the request. The cost is a network hop (~0.1ms locally), which is negligible per request but necessary for correctness across instances. A Go map with a mutex would work fine for a single-instance deployment, but fails the moment you scale horizontally.

**Q: Why use Lua scripts instead of regular Redis commands?**
A: Rate limiting requires a read-modify-write cycle: read the current count, check if the request is within the limit, then update the count. If these are separate Redis commands, two gateway instances can both read the same count simultaneously, both decide the request is allowed, and both decrement — allowing a request that should have been blocked. Lua scripts run atomically in Redis's single-threaded command loop, so no other command can interleave. It's the simplest way to get atomic multi-step operations in Redis.

**Q: What's the difference between Token Bucket and Sliding Window?**
A: Token Bucket allows bursting — if you haven't used your tokens for a while, they accumulate up to capacity and can be spent all at once. It's good for clients that need burst headroom. Sliding Window Counter is smoother — it prevents the boundary exploit where a client sends max requests at 11:59 and max again at 12:00, effectively doubling throughput. The sliding window approximation weights the previous window's count based on how far into the current window you are, giving a continuous rather than step-function limit. The tradeoff: sliding window uses more Redis keys (two per bucket vs one) and the limit is approximate, not exact.

**Q: How does Token Bucket handle the refill?**
A: It doesn't use a background timer. On every request, the Lua script calculates how much time has elapsed since the last request and adds the appropriate number of tokens (`elapsed_seconds × refill_rate`), capped at capacity. This is "lazy refill" — tokens only get added when a request arrives. No background goroutines, no scheduled jobs, no extra complexity.

**Q: What happens to Redis keys when a bucket is inactive?**
A: Token Bucket sets `EXPIRE` to `ceil(capacity / refill_rate) * 2` on every write. This is the time it would take to fully refill the bucket, doubled. If a user stops making requests, the key expires automatically — no manual cleanup needed. Sliding Window sets `EXPIRE` to `window_size * 2` on the current window key.

**Q: How does the Limiter interface enable the Strategy Pattern?**
A: `*TokenBucket` and `*SlidingWindow` both implement `Limiter` with an `Allow` method. The policy engine in Phase 3 holds a `Limiter` reference, not a `*TokenBucket` or `*SlidingWindow`. This means you can configure which algorithm to use per-policy in YAML, and the gateway code never changes. Adding a new algorithm means writing a new struct that implements `Allow` — nothing else changes.

**Q: Why does `Allow` return `(bool, error)` instead of just `bool`?**
A: Redis can be unavailable, the Lua script can fail, the context can be cancelled. The `error` return lets the caller handle Redis failures with a configurable fail-open or fail-closed policy (Phase 4). If it returned only `bool`, there'd be no way to distinguish "rejected because rate limited" from "rejected because Redis is down."

**Q: How are tests deterministic without sleeping?**
A: Both implementations expose an unexported `allow(ctx, key, cost, nowMs int64)` method. Tests call this directly with explicit timestamps (`t0`, `t0 + 5000`) rather than relying on `time.Now()`. This means a test for "tokens refill after 5 seconds" runs in milliseconds — it just passes a timestamp 5000ms ahead, not actually waits 5 seconds.

**Q: What's the boundary exploit in fixed window rate limiting and how does sliding window fix it?**
A: With a fixed window of 60 requests per minute, a client can send 60 at 11:59:59 and 60 more at 12:00:00 — 120 requests in 2 seconds, both within their respective windows. The sliding window prevents this by continuously weighting the previous window's count. At 12:00:00, the previous window is 0 seconds old, so 100% of its count still applies — the client would see their 60 prior requests fully counted against the new window.

**Q: Could this implementation be used across multiple services, not just GateKeeper?**
A: Yes. The `Limiter` interface and its implementations are completely generic — they take a string key and an int cost. Any service with a Redis connection could use this package for rate limiting user actions, background jobs, third-party API calls, etc.

---

## 5. Edge Cases and Failure Modes

**Clock skew between gateway instances.** The Lua scripts receive `time.Now()` from Go, not from Redis. If two gateway instances have different system clocks, they'll calculate different elapsed times for the same bucket, leading to inconsistent refill amounts. In practice, NTP keeps machines within milliseconds, which is negligible for second-scale rate limits. For millisecond-scale limits this could matter.

**Token Bucket with very slow refill rate.** If `refillRate` is very small (e.g. 0.001 tokens/second), the TTL formula `ceil(capacity / refill_rate) * 2` produces a very large value (e.g. for capacity=100, refillRate=0.001: TTL = 200,000 seconds ≈ 55 hours). The key stays in Redis a long time. Not a bug, but worth being aware of for memory planning.

**Sliding Window approximate, not exact.** The weighted formula is an approximation. A client making exactly `limit` requests in the final millisecond of a window could get slightly more than `limit` requests in a rolling 60-second period due to floating point weighting. The error is bounded and small, but it's not a hard guarantee.

**Cost of 0.** If `cost = 0` is passed, the Lua scripts will always allow the request and never deduct anything. The `HSET`/`INCRBY` will still fire, resetting TTL. This is the intended behavior for `/health` endpoints (0 cost = exempt from limits) but the caller must be careful not to pass 0 accidentally for billable operations.

**Redis unavailability.** If Redis is down, `script.Run()` returns a non-nil error. The `Allow` method returns `(false, err)`. The caller (Phase 4 middleware) must decide: fail-open (allow the request despite the error) or fail-closed (reject it). Neither is right or wrong — it's a configurable policy tradeoff between availability and security.

**Key collision.** If two different scopes accidentally produce the same Redis key (e.g., a user with ID `"ip:1.2.3.4"` and an IP of `"1.2.3.4"` both hashing to `"ip:1.2.3.4"`), they'd share a bucket. The policy engine must construct keys with unambiguous prefixes.

---

## 6. Modification Scenarios

**Adding a new algorithm (e.g. Leaky Bucket).** Create `internal/limiter/leakybucket/leakybucket.go` implementing `Allow(ctx, key, cost) (bool, error)`. Add a `leaky_bucket.lua` alongside it. Register it in the policy engine (Phase 3). Zero changes to the gateway, zero changes to the `Limiter` interface.

**Switching from Redis hashes to strings in Token Bucket.** Change the Lua script to use `GET`/`SET` on two separate string keys instead of `HMGET`/`HSET` on one hash. You'd need two `EXPIRE` calls instead of one. Performance-wise this is equivalent; the hash approach is slightly cleaner because one key = one bucket = one TTL.

**Making refill rate configurable per request.** Currently `refillRate` is set at construction time. To support per-request rates, add a `refillRate` parameter to `Allow` and pass it as `ARGV[5]` in the Lua script. The interface would need to change — consider a config struct parameter instead of positional args.

**Adding a rate limit header in responses.** The Lua script currently returns only 0 or 1. To return remaining tokens, change the return to a table: `return {1, new_tokens - cost}`. In Go, parse the result as a slice instead of a single int. Expose the remaining count through the `Allow` signature or a richer return type.

**Scaling to Redis Cluster.** Redis Cluster routes keys by hash slot. The `{key}:{window_timestamp}` pattern in the sliding window may route `curr_key` and `prev_key` to different nodes, causing `MULTI`-key Lua operations to fail. Fix: use Redis hash tags — `{base_key}:{window_timestamp}` — so both keys are forced to the same slot. Token Bucket is safe already (single key per bucket).

---

## 7. Must-Know Summary

- **Lua scripts are atomic in Redis** — the entire script runs without interruption. This is the only reason distributed rate limiting without race conditions is possible without distributed locks.
- **Token Bucket allows bursting; Sliding Window is smooth.** Know when to use each and be able to explain the boundary exploit that sliding window prevents.
- **Refill in Token Bucket is lazy** — no background timers. Elapsed time is calculated and tokens are added on the next request.
- **The `Limiter` interface is the Strategy Pattern.** The gateway calls `Allow`; it doesn't know if it's talking to a TokenBucket or SlidingWindow. New algorithms require zero changes outside their own package.
- **`//go:embed` bakes the Lua script into the binary at compile time.** It cannot use `..` paths; scripts must live in the same directory or a subdirectory of the Go file.
- **`goredis.NewScript` uses EVALSHA.** The script body is only sent to Redis once; subsequent calls send only a hash. This is a meaningful performance optimization under load.
- **Tests are deterministic because time is injected** via the unexported `allow(nowMs int64)` method. Never sleep in rate limiter tests.
- **`(bool, error)` return enables fail-open/fail-closed.** A Redis error is distinguishable from a rate limit rejection, which is essential for the Phase 4 middleware to implement the correct failure policy.
