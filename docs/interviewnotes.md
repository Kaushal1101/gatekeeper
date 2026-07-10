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

---

# `internal/config` — Config Loader

---

## 1. High-Level Purpose

This package owns the bridge between the operator-facing YAML file and the Go type system. It defines the shape of every configurable value in GateKeeper and provides a single `Load(path)` function that validates and deserialises the file at startup.

Without it, the policy engine has no structured input — every other package would need to know about YAML directly, and config changes would require code changes.

---

## 2. Core Concepts

### Struct tags for YAML mapping
Go doesn't have annotations like Java. Instead, struct fields carry backtick-delimited **tags** that libraries read at runtime using reflection. `yaml:"field_name"` tells `gopkg.in/yaml.v3` which YAML key maps to which struct field. If the tag is missing, yaml.v3 does case-insensitive matching by convention, but explicit tags are safer and self-documenting.

### Zero-value trap for optional fields
Go initialises all numeric struct fields to `0` when deserialised from YAML if the key is absent. `cost:` is optional in the config (most endpoints cost 1). If you read `s.Cost` directly where `cost:` is omitted, you get `0` — which would tell the limiter to deduct nothing and always allow. `EffectiveCost()` intercepts this and returns `1`, making omission equivalent to `cost: 1`.

### Why `os.ReadFile` not `os.Open`
`os.ReadFile` reads the entire file into a `[]byte` in one call. `os.Open` returns a handle that requires separate `Read` and `Close` calls. For a small config file read once at startup, `ReadFile` is simpler and idiomatic. `yaml.Unmarshal` works directly on the byte slice.

### Dual algorithm params in one struct
`Scope` holds both token bucket fields (`Capacity`, `RefillRate`) and sliding window fields (`Limit`, `Window`) simultaneously. The inactive fields are simply ignored by the policy engine based on the `algorithm` field in the parent `Policy`. This design choice exists specifically to enable one-line algorithm toggling — you never need to add or remove fields when switching algorithms, only change the `algorithm:` value.

---

## 3. Important Implementation Details

**`Config` struct** — top-level container. `DefaultAction string` is `"allow"` or `"deny"`, interpreted by the policy engine as `defaultAllow bool`. `OnLimiterError string` is `"allow"` or `"deny"`, controlling what happens when a rate limiter returns an error (Redis down). Both are kept as strings (not bools) so the YAML is readable to an operator who may not know the codebase.

**`Policy` struct** — one entry per endpoint. `Algorithm string` is free-form here; validation happens in the policy engine's `buildLimiter` switch, which returns an error for unknown values. The config package itself does no validation beyond YAML syntax.

**`Scope` struct** — `RefillRate` is `float64` (not `int`) because token refill rates can reasonably be fractional (e.g. 0.5 tokens/second). `Window` is `string` (e.g. `"60s"`, `"5m"`) not `time.Duration` because yaml.v3 cannot unmarshal Go duration strings natively — `time.ParseDuration` is called later in the policy engine.

**`EffectiveCost() int`** — method on value receiver `Scope` (not pointer). This is idiomatic in Go when the method doesn't mutate state. Works the same on both `Scope` and `*Scope`.

**`Load(path string) (*Config, error)`** — returns a pointer so the caller can nil-check and the struct isn't copied on return. The two error paths are file I/O failure (`os.ReadFile`) and malformed YAML (`yaml.Unmarshal`). Neither is caught and wrapped with additional context here — they're returned raw because they already contain enough information.

---

## 4. Interview Questions

**Q: Why does `Scope` contain fields for both algorithms when only one is active?**
A: The primary use case is comparing algorithm behaviour under real traffic. You configure both sets of parameters once, then switch algorithms with a single `algorithm:` field change and restart. If you only stored the active algorithm's params, switching would require adding new fields — more friction, more risk of misconfiguration.

**Q: Why is `Window` a string instead of `time.Duration`?**
A: `gopkg.in/yaml.v3` doesn't know about Go's `time.Duration` type. It would unmarshal `"60s"` as a string. You could write a custom `UnmarshalYAML` method on a wrapper type to parse it during deserialisation, but that adds complexity. The simpler approach is to parse it with `time.ParseDuration` at the point of use in the policy engine, and to fail loudly there if the format is invalid.

**Q: Why is there no validation in the config package?**
A: Validation is done in the policy engine's `buildLimiter` function, which returns a descriptive error wrapping the policy path and scope. Centralising validation at the point of use means the error message includes full context (`policy "/api/fast" scope "ip": unknown algorithm "tokenbucket"`). Validating in `Load` would require duplicating the algorithm registry or coupling config to policy.

**Q: What happens if `default_action` is set to something other than `"allow"` or `"deny"`?**
A: The policy engine evaluates `cfg.DefaultAction == "allow"`. Any value other than `"allow"` (including typos like `"Allow"`, empty string, or `"permit"`) results in `defaultAllow = false` — fail-closed. This is a safe default but silent. An improvement would be to validate the field in `Load` and return an error for unrecognised values.

**Q: What happens if the YAML file doesn't exist at the path given?**
A: `os.ReadFile` returns a `*PathError` wrapping `syscall.ENOENT`. `Load` returns it unwrapped. The caller (typically `main.go`) should check the error and fail fast — the gateway cannot start without a valid config.

**Q: Why use `gopkg.in/yaml.v3` instead of `encoding/json` or a different format?**
A: YAML is more readable for operator-facing config — it supports comments (used in `config.yaml` to annotate algorithm choices), doesn't require quoting strings, and is the standard for Kubernetes-style infrastructure config. `yaml.v3` is the mature, well-maintained Go library for it. JSON has no comment support, which matters for a config file operators are expected to read and edit.

---

## 5. Edge Cases and Failure Modes

**Omitted `cost:` field silently becomes 0.** Go zero-initialises numeric fields. Without `EffectiveCost()`, passing `s.Cost` directly to the limiter would mean all requests are free. This is a subtle, non-obvious gotcha — the value parses without error, it's just wrong.

**`algorithm:` typo not caught at load time.** `config.Load` will succeed even if `algorithm: tokenbuckt` (typo). The error only surfaces when `policy.New()` calls `buildLimiter` and the switch hits the default case. The gateway fails at startup, not at request time — acceptable behaviour.

**Duplicate `path:` entries.** If two policies have the same path, `policy.Matcher.Match()` returns the first match (linear scan). The second is silently ignored. There's no duplicate detection.

**Empty `policies:` list.** Parses fine. Every request hits the `default_action` path. If `default_action: deny`, every request is rejected. This is a misconfiguration that produces valid, predictable (if surprising) behaviour.

**YAML indentation errors.** YAML is whitespace-sensitive. A misaligned `scopes:` block can cause fields to be parsed as siblings instead of children, silently producing a `Policy` with no scopes. `yaml.Unmarshal` may not error — it just produces a zero-value struct. Careful YAML linting is important.

---

## 6. Modification Scenarios

**Adding a new scope type (e.g. `user_id`).** Add the extraction logic to the policy engine's `scopeValue` function. No changes to the config structs — `by: user_id` already parses as a string.

**Adding config validation.** Add a `Validate() error` method on `Config` that checks: `DefaultAction` is `"allow"` or `"deny"`, each `Policy.Algorithm` is a known value, each `Scope.Window` parses as a duration, no duplicate paths. Call it in `Load` before returning.

**Hot-reload without restart.** Replace `Load` with a `Watch(path string, onChange func(*Config))` function using `fsnotify` (a Go file-watching library). The callback rebuilds the `Matcher` via `policy.New()` and swaps it atomically. `Config` and its structs don't change at all — only the loading mechanism does.

**Supporting multiple config files (e.g. per-environment overrides).** Load a base config and an override config, then merge `Policies` slices (override wins on path collision). The struct types are already sufficient; only `Load` needs a variant.

---

## 7. Must-Know Summary

- **Struct tags (`yaml:"..."`) map YAML keys to Go fields.** Without them, yaml.v3 falls back to case-insensitive name matching, which is fragile.
- **Omitted numeric fields default to Go's zero value (0), not a meaningful default.** `EffectiveCost()` exists to guard against this specific trap for `cost:`.
- **No validation in the config package — validation is in the policy engine.** Errors from unknown algorithms are returned with full context from `buildLimiter`.
- **`Window` is a string, parsed later with `time.ParseDuration`.** yaml.v3 cannot unmarshal duration strings natively.
- **`default_action` is a string in config, converted to `bool` in policy.** Any value other than `"allow"` means deny. Typos fail closed.
- **`on_limiter_error` controls behavior when Redis is unavailable.** `"deny"` returns 500 (fail-closed, default); `"allow"` passes traffic through (fail-open). Converted to `onErrorAllow bool` in the policy engine.
- **`Load` reads the file once at startup.** There is no watching or hot-reload. Config changes require a process restart.
- **Both algorithm param sets live in every `Scope`.** Unused ones are ignored. This is a deliberate design choice to reduce friction when toggling algorithms.

---

# `internal/policy` — Policy Engine

---

## 1. High-Level Purpose

The policy engine is the decision router. It takes the structured config and compiles it into a ready-to-use set of limiter instances at startup. At request time, it maps an incoming request's identity (path, API key, IP) to the exact set of checks that must pass for that request to proceed.

Without it, there's no connection between the config file and the rate limiter algorithms. The algorithms exist in isolation; the policy engine is what makes them apply to real traffic.

---

## 2. Core Concepts

### Two-phase design: compile then match
The policy engine deliberately separates expensive work (startup) from cheap work (per-request). `New()` parses durations, calls algorithm constructors, and builds the full internal policy tree — once. `Match()` does a linear scan and struct population — on every request. This is the same principle as compiling a regex once and matching it many times.

### Compiled internal representation
The config structs (`config.Policy`, `config.Scope`) are the operator's view. `compiledPolicy` and `compiledScope` are the runtime's view. The difference: compiled structs hold live `Limiter` objects (already connected to Redis) and resolved `int` costs — everything the middleware needs, nothing it doesn't. The YAML string fields (`algorithm`, `window`) have already been interpreted and discarded.

### Separation of concerns: match vs enforce
`Match()` returns `[]Check` — it does not call `Allow()`. The middleware calls `Allow()` on each check and applies AND logic. This separation means the policy engine has one job (routing) and the middleware has one job (enforcing). Neither knows about the other's internals.

### AND logic for multi-scope
A request must pass every scope check to be allowed. If `/api/fast` has two scopes (`api_key` and `ip`), both limiters must return `true`. A request that exceeds the per-IP limit is rejected even if its API key is within limits. The AND logic isn't implemented in the policy engine — it's a contract the middleware must honour when iterating `[]Check`.

### Redis key design
Keys follow the format `{path}:{scope}:{value}` — e.g. `/api/fast:api_key:abc123`. This encodes three isolation dimensions into one string. Changing any dimension produces a different key, so counters are always fully isolated. There's no risk of the same API key's counter being shared across endpoints, or two different scope types colliding.

---

## 3. Important Implementation Details

**`Check` struct** — the unit the middleware receives. Contains `Limiter` (the live limiter object to call), `Key` (the fully-resolved Redis key string), and `Cost` (tokens or requests to deduct). The middleware calls `check.Limiter.Allow(ctx, check.Key, check.Cost)` for each.

**`compiledScope`** — unexported; internal only. Holds `by` (scope type string), `limiter` (live `Limiter` interface), and `cost` (resolved int). The `by` field is kept so `Match()` can call `scopeValue(s.by, apiKey, ip)` to pick the right identity value at request time.

**`Matcher.Match()` signature** — returns `([]Check, bool)`. The bool's meaning depends on which branch returns: if a policy matches, `true` means "a policy was found" (not "allow"). If no policy matches, `true` means "default is allow". The middleware must distinguish these — one case means "run the checks", the other means "skip checks, use default."

**`buildLimiter()`** — the algorithm switch. `token_bucket` calls `tokenbucket.New(client, s.Capacity, s.RefillRate)`. `sliding_window` calls `time.ParseDuration(s.Window)` first (only error that can surface here), then `slidingwindow.New(client, int(d.Seconds()), s.Limit)`. The `float64 → int` truncation in `int(d.Seconds())` is safe for human-scale durations (`"60s"` → `60`).

**Error wrapping with `%w`** — `fmt.Errorf("policy %q scope %q: %w", p.Path, s.By, err)` uses the `%w` verb to wrap the underlying error. The caller can unwrap it with `errors.As` or `errors.Is`. The outer message pinpoints exactly which policy/scope caused the failure.

**`scopeValue()`** — a simple two-branch function: `api_key` returns `apiKey`, anything else returns `ip`. Adding a third scope type (`user_id`) requires adding a branch here and passing the new identity value to `Match()`.

**Tests use `stubMatcher()` directly** — bypassing `New()` entirely. This means tests don't need a Redis client or config file. The `Matcher` struct is constructed with hand-built `compiledPolicy` slices. This is possible because `compiledPolicy` and `compiledScope` are in the same package as the tests (`package policy`), so unexported types are accessible.

---

## 4. Interview Questions

**Q: Why does `Match()` return `[]Check` instead of making the `Allow()` calls itself?**
A: Separation of concerns. The policy engine's job is to identify *which* limits apply to a request. The middleware's job is to enforce them. If `Match()` called `Allow()` internally, it would also need to handle Redis errors, decide fail-open/fail-closed behaviour, and construct HTTP responses — none of which belong in the policy layer. Returning `[]Check` keeps each layer focused.

**Q: What's the difference between `compiledPolicy` and `config.Policy`?**
A: `config.Policy` is the deserialised YAML — strings, ints, raw values. `compiledPolicy` is the runtime representation — live `Limiter` objects already wired to Redis, resolved costs, nothing unparsed. The compilation step in `New()` translates between them. If you called `buildLimiter` on every request instead of at startup, you'd be constructing Redis script objects and parsing duration strings per request — unnecessary and slow.

**Q: How does the AND logic work for multiple scopes?**
A: `Match()` returns all checks for the matching policy. The middleware iterates them and calls `Allow()` on each. If any returns `false`, the request is rejected. The policy engine doesn't enforce AND — it just hands back all the checks. The AND contract lives in the middleware.

**Q: What happens if two policies have the same path?**
A: `Match()` does a linear scan and returns on the first match. The second policy for that path is silently ignored. This is a known limitation — the config has no duplicate detection, and `New()` compiles all policies including duplicates. An operator who accidentally duplicates a path will get unexpected behaviour.

**Q: What does the second return value of `Match()` mean?**
A: It has two different meanings depending on the branch taken. If a policy is found, `Match()` returns `(checks, true)` — `true` signals "policy matched, run these checks." If no policy is found, `Match()` returns `(nil, m.defaultAllow)` — the bool is the default action (allow or deny). The middleware must check whether `checks` is nil to know which case it's in.

**Q: How would you add a third rate limiting algorithm?**
A: Create the implementation in `internal/limiter/newalgo/`. Add a case to `buildLimiter`'s switch in `policy.go`. Update the YAML schema and `config.Scope` struct with any new fields. Zero changes to `Matcher`, `Match()`, `Check`, or the middleware.

**Q: Could the policy engine support regex or wildcard path matching?**
A: Currently no — `Match()` uses exact string equality. Adding prefix matching would change `if p.path != path` to `strings.HasPrefix(path, p.path)`. Regex matching would use `regexp.MustCompile` at compile time (in `New()`) and `.MatchString(path)` at match time. The compiled representation would store a `*regexp.Regexp` instead of a plain string. `Match()` itself would need to handle priority (most-specific match wins vs first match wins).

**Q: Why doesn't `New()` validate that `default_action` is a known value?**
A: It converts it directly: `cfg.DefaultAction == "allow"`. Any non-`"allow"` string becomes `false` (deny). This is a safe default but silent — a typo like `"alow"` silently becomes deny. A production-hardened version would validate in `config.Load` and return an error for unrecognised values.

---

## 5. Edge Cases and Failure Modes

**`Match()` called with empty `apiKey` or `ip`.** If the middleware fails to extract a header, it might pass `""` as the API key. This produces a valid Redis key `/api/fast:api_key:` — a shared bucket for all requests with no API key. Depending on intent, this could be a security hole (unauthenticated requests share one pool) or a legitimate catch-all. The policy engine doesn't validate identity values; the middleware must guard against empty strings.

**`buildLimiter` with `window: ""`.** If `Window` is empty string (scope block in YAML has no `window:` field), `time.ParseDuration("")` returns an error. `New()` returns that error, the gateway fails to start. Clear failure, but only surfaced at startup.

**`Match()` second return value ambiguity.** When a policy is found, the second return is always `true` regardless of `defaultAllow`. When no policy is found, it's `m.defaultAllow`. A middleware that doesn't nil-check `checks` before reading the bool could misinterpret a matched policy with `defaultAllow=false` as "denied." The nil check is the correct guard.

**Linear scan performance under many policies.** `Match()` is O(n) where n is the number of policies. For 3 endpoints this is irrelevant. For thousands of endpoints, a `map[string]compiledPolicy` would give O(1) lookup. The current design is correct for the use case and easy to upgrade.

**Limiter objects are shared across all requests.** Each `compiledScope` holds a single `Limiter` instance used by all goroutines concurrently. Both `TokenBucket` and `SlidingWindow` are stateless in Go (their state is in Redis) so this is safe — but adding any in-memory mutable state to a limiter implementation would introduce a race condition.

---

## 6. Modification Scenarios

**Adding `user_id` as a third scope type.** In `policy.go`: add a branch to `scopeValue()` returning the user ID. Update `Match()` signature to accept `userID string`. Pass the `X-User-ID` header value from the middleware. No struct changes needed.

**Switching from linear scan to map lookup.** In `Matcher`, change `policies []compiledPolicy` to `policies map[string]compiledPolicy`. In `New()`, insert into the map. In `Match()`, replace the loop with a single map lookup. Handle the "not found" case with `m.defaultAllow`. Reduces match time from O(n) to O(1) at the cost of losing order (irrelevant for exact-match).

**Supporting wildcard/prefix policies.** In `compiledPolicy`, add a `matcher func(string) bool` field instead of a plain `path string`. `New()` compiles `"*"` to `func(_ string) bool { return true }` and exact paths to `func(s string) bool { return s == path }`. `Match()` calls `p.matcher(path)`. Clean extension with no API change.

**Enabling hot-reload.** Wrap `*Matcher` in `atomic.Pointer[Matcher]`. A background goroutine watches the config file, calls `New()` on change, and atomically swaps the pointer. `Match()` is called on the pointer's loaded value — in-flight requests finish against the old matcher, new requests use the new one. Changes required: `main.go` (swap holder), middleware (load from atomic pointer). `policy.go` itself doesn't change.

---

## 7. Must-Know Summary

- **`New()` compiles; `Match()` looks up.** Expensive work happens once at startup. Per-request work is a linear scan and struct assembly.
- **`compiledPolicy`/`compiledScope` are the runtime view; `config.Policy`/`config.Scope` are the operator view.** The compilation step bridges them.
- **`Match()` returns `[]Check`, not a decision.** The middleware enforces AND logic; the policy engine only routes.
- **The second return value of `Match()` has two meanings** — "policy found" (bool is always true) vs "no policy found" (bool is `defaultAllow`). Always nil-check `checks` first.
- **Redis keys encode path + scope + identity.** Format: `/api/fast:api_key:abc123`. All three dimensions are necessary to prevent counter collisions.
- **Tests bypass `New()` using `stubMatcher()`** — unexported types are accessible within the same package. No Redis client needed for policy matching tests.
- **`scopeValue()` is the only place that knows which identity value maps to which scope type.** Adding a new scope type (`user_id`, `tenant_id`) requires a change here and in `Match()`'s signature.
- **Linear scan is fine for a small number of policies; a `map[string]compiledPolicy` is a drop-in upgrade for scale.**
- **`OnErrorAllow()` exposes Redis-error behavior to the middleware.** Set from `cfg.OnLimiterError == "allow"` in `New()`. The middleware reads this to decide between 500 and pass-through on limiter errors.

---

# `internal/middleware` — Rate Limit Middleware

---

## 1. High-Level Purpose

The middleware is where all the earlier work comes together. It intercepts every HTTP request flowing through the gateway, extracts the caller's identity, consults the policy engine, runs each rate limit check, and either forwards the request or returns a structured error response. Without it, the gateway is a dumb proxy — the algorithms, config, and policy matching all exist but have no effect on real traffic.

---

## 2. Core Concepts

### Middleware pattern in Go
In `net/http`, a middleware is a function that wraps an `http.Handler` and returns a new `http.Handler`. The outer handler adds behaviour before and/or after calling the inner handler. `RateLimit` is a factory: it accepts the `Matcher` once at startup and returns the actual middleware wrapper. The wrapper accepts `next http.Handler` (the proxy) and returns the final request handler. Calling `next.ServeHTTP(w, r)` is what forwards the request — not calling it short-circuits the chain.

### Identity extraction
Two identity values are extracted per request:
- `X-API-Key` header — set by the client
- Real IP — `X-Real-IP` header (set by nginx to `$remote_addr`), falling back to `r.RemoteAddr`

`X-Real-IP` is preferred over `X-Forwarded-For` because it is set by our own nginx and is always exactly one IP. `X-Forwarded-For` can contain a chain of IPs and can be spoofed by the client by setting the header before it reaches nginx.

### AND logic
Every check in `[]Check` must pass. The loop short-circuits on the first failure — subsequent checks are never run. There is no OR logic anywhere in the enforcement path.

### Four outcomes
Every request through the middleware ends in one of four ways:
1. **No policy match + `default_action: deny`** → 403 Forbidden
2. **No policy match + `default_action: allow`** → pass through
3. **Rate limit exceeded** → 429 Too Many Requests
4. **Redis error** → 500 (if `on_limiter_error: deny`) or pass through (if `on_limiter_error: allow`)

---

## 3. Important Implementation Details

**`RateLimit(matcher *policy.Matcher) func(http.Handler) http.Handler`** — three nested function literals. Outer: factory, called once at startup. Middle: middleware wrapper, receives `next`. Inner: request handler, called on every request.

**`realIP(r *http.Request) string`** — prefers `X-Real-IP`, falls back to `net.SplitHostPort(r.RemoteAddr)` to strip the port. `r.RemoteAddr` in Go is always `host:port` — it cannot be used directly as a key without stripping the port.

**`writeJSON(w http.ResponseWriter, code int, body string)`** — sets `Content-Type: application/json`, then writes status code and body. `http.Error` would override the Content-Type to `text/plain; charset=utf-8`. Writing manually avoids that.

**Short-circuit discipline** — every error branch calls `return` immediately after writing a response or calling `next.ServeHTTP`. Once a response is started, any further write to `w` corrupts the HTTP response. The `return` on every branch enforces this invariant.

**`/health` bypass** — `/health` is registered directly on the `http.ServeMux` before the middleware wraps the proxy. `loggingMiddleware` wraps the full mux (so `/health` is still logged), but the rate limit middleware only wraps the proxy. `/health` requests are never touched by rate limiting logic or Redis.

---

## 4. Interview Questions

**Q: Why is `/health` exempt from rate limiting?**
A: The rate limit middleware calls Redis to check counters. If Redis is down and `on_limiter_error: deny` is configured, the middleware returns 500. Docker uses `/health` to decide whether to route traffic to this gateway instance. If `/health` went through the middleware, a Redis outage would cause Docker to mark the gateway as unhealthy and pull it from rotation — even though the gateway process itself is fine. By wiring `/health` outside the middleware, Docker measures gateway liveness, not Redis availability.

**Q: Why use `X-Real-IP` instead of `X-Forwarded-For`?**
A: In a controlled deployment where nginx is our own load balancer, `X-Real-IP` is set to `$remote_addr` by our nginx config — always exactly one IP set by a trusted component. `X-Forwarded-For` accumulates across proxies and can be pre-set by the client before the request reaches nginx, making it spoofable. In a multi-proxy environment (CDN → nginx → gateway), you'd need `X-Forwarded-For` with a trusted-IP allowlist to extract the real client IP.

**Q: What happens if `X-API-Key` is not provided?**
A: `r.Header.Get("X-API-Key")` returns `""`. The Redis key becomes `/api/fast:api_key:` — a shared bucket for all unauthenticated requests on that endpoint. All unkeyed traffic competes for the same rate limit pool. This could be a security concern (one heavy user exhausts the pool for everyone) or an intentional collective cap. The middleware doesn't validate identity values; the operator must decide whether to reject empty API keys upstream.

**Q: What is the difference between a 403 and a 429 response from this middleware?**
A: 403 means no policy exists for the requested path and `default_action` is `deny` — the gateway has no authorization to let this path through. 429 means a policy was found but a rate limit was exceeded. From a client perspective: 403 suggests a misconfigured path or missing access; 429 suggests backing off and retrying.

**Q: What happens when two scope checks both fail?**
A: The first failure short-circuits the loop with a 429 and `return`. The second check is never evaluated. The client always sees one 429 — there's no "which limit did I hit" information in the response (by design; that would be information disclosure).

**Q: How would you add `Retry-After` to the 429 response?**
A: The Lua scripts currently return only 0/1. To support `Retry-After`, the scripts would need to return the time-to-next-token (Token Bucket) or time-until-window-reset (Sliding Window). The `Limiter` interface would need a richer return type, and the middleware would write the header before the 429 body. It's a non-trivial interface change affecting both algorithms.

---

## 5. Edge Cases and Failure Modes

**Calling `next.ServeHTTP` after a partial write.** If code accidentally writes a 429 header and then calls `next.ServeHTTP`, the response is corrupted — HTTP only allows one status code per response. Every branch in the middleware ends with `return`, preventing this.

**Empty `X-API-Key` producing a shared bucket.** All requests without an API key share one counter per path per IP scope. Whether this is desired depends on the operator's intent. The middleware does not validate identity values.

**Redis unavailable at request time (not startup).** `policy.New()` succeeds even if Redis is unreachable at startup (go-redis connects lazily). The first request that triggers `Allow()` will surface the error. `on_limiter_error` controls the outcome. This is a deliberate design choice: the gateway can start and serve health checks even if Redis is temporarily unavailable.

**`r.RemoteAddr` without a port.** Theoretically possible in edge cases. `net.SplitHostPort` returns an error; `realIP` falls back to returning `r.RemoteAddr` as-is, which is still a usable key string.

---

## 6. Modification Scenarios

**Logging rejections.** Add `log.Printf("RATE_LIMITED path=%s key=%s", r.URL.Path, c.Key)` in the 429 branch and `log.Printf("NO_POLICY path=%s", r.URL.Path)` in the 403 branch. One line each.

**Adding `X-RateLimit-Remaining` header.** The Lua scripts return 0 or 1. To return remaining capacity, change return value to a two-element table. Update `Allow()` return type to `(bool, int, error)`. Write `w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))` before calling `next.ServeHTTP`.

**Supporting dynamic cost based on request body size.** Read `r.ContentLength` in the middleware and override `c.Cost` before calling `c.Limiter.Allow(...)`. Currently cost is config-driven; dynamic cost requires no interface change — just a different value passed to `Allow`.

---

## 7. Must-Know Summary

- **Middleware is a chain of handler wrappers.** Not calling `next.ServeHTTP` short-circuits the chain. Every error branch must `return` after writing a response.
- **`/health` bypasses rate limiting** — registered directly on the mux before the middleware wraps the proxy. Docker health checks must not depend on Redis.
- **Identity: `X-API-Key` (client-set) + `X-Real-IP` (nginx-set).** `X-Real-IP` is a single trusted IP; prefer it over `X-Forwarded-For` in a controlled environment.
- **AND logic: every check must pass.** First failure returns 429 and stops.
- **Four outcomes: 403 (no policy, closed), pass-through (no policy, open), 429 (rate limited), 500 or pass-through (Redis error, configurable via `on_limiter_error`).**
- **`writeJSON` not `http.Error`** — `http.Error` forces `Content-Type: text/plain`; the gateway returns JSON errors consistently.
- **go-redis connects lazily.** Redis unavailability surfaces at request time, not startup. `on_limiter_error` determines what happens.

---

# `cmd/gateway` — Gateway Entry Point

---

## 1. High-Level Purpose

`main.go` is the wiring layer. It reads configuration, connects to Redis, builds the policy matcher, creates the HTTP server, and assembles the middleware chain. It contains no business logic — it delegates everything to the packages in `internal/`. Its job is to compose the components into a running server in the correct order.

---

## 2. Core Concepts

### Startup sequence
Config → Redis client → Policy Matcher → Proxy → Routes → Server. Each step depends on the previous. Any error in the startup sequence calls `log.Fatalf`, which logs and exits with code 1. Docker Compose detects the non-zero exit and reports the container as failed. This fail-fast approach makes misconfiguration visible immediately rather than serving broken responses.

### Environment variables for portability
Three env vars control runtime behavior:
- `CONFIG_PATH` — which YAML config to load (default: `config/config.yaml`)
- `BACKEND_URL` — where to proxy accepted requests (default: `http://localhost:8081`)
- `REDIS_ADDR` — which Redis to connect to (default: `localhost:6379`)

The same binary runs identically in local dev (localhost defaults) and in Docker (service-name addresses injected by Compose via `environment:`). No recompilation needed between environments.

### Route registration
`/health` is registered with a plain handler. `/` is registered with the rate-limit-wrapped proxy. `loggingMiddleware` wraps the full mux — every request is logged, including health checks. Rate limiting wraps only the proxy, so `/health` is logged but never rate limited.

---

## 3. Important Implementation Details

**`config.Load(cfgPath)`** — reads and parses the YAML config. Fails fast if the file is missing or malformed. Called before Redis connects so configuration errors are caught first.

**`gatewayredis.NewClient()`** — reads `REDIS_ADDR` and returns a configured `*goredis.Client`. Does not connect — go-redis is lazy. The connection is established on the first `Allow()` call.

**`policy.New(cfg, redisClient)`** — compiles the full config into live limiter instances. This is the expensive work done once at startup. Unknown algorithm names or unparseable duration strings cause `New()` to return an error, which `log.Fatalf` turns into a startup failure.

**`middleware.RateLimit(matcher)(proxy)`** — two calls chained. `RateLimit(matcher)` returns the middleware wrapper function. That function is immediately called with `proxy` as `next`, producing the final rate-limited handler.

**`loggingMiddleware(mux)`** — wraps the entire mux. Registered as the outermost handler so it sees every request before any routing occurs.

---

## 4. Interview Questions

**Q: Why not validate the Redis connection at startup with a `Ping()`?**
A: go-redis connects lazily. A startup `Ping()` would add a hard requirement that Redis be reachable before the gateway can start — even if the gateway is configured with `on_limiter_error: allow` (fail-open). Validating lazily means the gateway can start, serve health checks, and handle `on_limiter_error` correctly even during a Redis outage. The `depends_on: redis: condition: service_healthy` in Docker Compose already ensures Redis is up before the gateway starts in the normal case.

**Q: What's the startup failure behavior?**
A: `log.Fatalf(format, args...)` is called on any startup error. It logs the message and calls `os.Exit(1)`. Docker Compose sees the non-zero exit code and marks the container as failed. This is preferable to starting a partially-broken gateway that returns 500 on every request.

**Q: Why is `httputil.NewSingleHostReverseProxy` used instead of a custom HTTP client?**
A: The standard library proxy handles header forwarding (including hop-by-hop header removal per RFC 7230), response streaming, and connection reuse via a shared transport. Writing an equivalent custom client correctly is significant work with no benefit for this use case.

**Q: What controls startup order in Docker Compose?**
A: The `depends_on` conditions in `docker-compose.yml`. Redis must pass `redis-cli ping` (healthy) before gateways start. mock-backend must be healthy before gateways start. Gateways must be healthy (via their `/health` endpoint) before nginx starts. This prevents nginx from routing to gateways that are still initializing.

---

## 5. Must-Know Summary

- **`main.go` is a wiring layer, not a logic layer.** All business logic lives in `internal/`. `main` composes and starts.
- **Fail-fast at startup.** Any config, parse, or compilation error calls `log.Fatalf` and exits 1.
- **Three env vars:** `CONFIG_PATH`, `BACKEND_URL`, `REDIS_ADDR`. All have localhost defaults for local dev; Docker Compose injects production values.
- **Route registration:** `/health` → plain handler. `/` → `RateLimit(matcher)(proxy)`. `loggingMiddleware` wraps the full mux.
- **go-redis is lazy.** Redis connection is established on the first `Allow()` call, not at startup.
- **Docker Compose startup order:** Redis healthy → gateways start → gateways healthy → nginx starts.
