# Architecture Decisions

## System Topology

```
Client (curl / k6)
        ↓
      nginx  :80        ← load balancer, single entry point
     ↙       ↘
gateway-1   gateway-2   ← stateless Go reverse proxies (Phase 4: rate limiting middleware)
        ↘   ↙
    mock-backend :8081  ← fake API with /api/fast, /api/slow, /api/expensive

      Redis :6379        ← shared rate limit state (Phase 2+)
```

All five services run in Docker Compose on a private internal network. Clients only reach nginx; everything else is internal.

---

## ADR-002 — Git Workflow: Feature Branches + PRs per Phase

**Decision:** Each phase gets its own branch (e.g. `phase-2-rate-limiting`), merged to `main` via a pull request when complete and verified.

**Reasons:**

- Demonstrates production-style engineering workflow to internship recruiters reviewing the GitHub profile
- PR descriptions serve as built-in documentation of what was built and why, which mirrors what interviewers ask about
- Clean `main` history — only complete, working phases land on the default branch

**Tradeoff:** Slight overhead per phase. Negligible given the project cadence.

---

## ADR-001 — Language: Go over Java

**Decision:** Build GateKeeper in Go.

**Reasons:**

- **Concurrency model.** Goroutines cost ~2KB each vs ~1MB for Java threads. A rate-limiting gateway handling thousands of simultaneous requests benefits directly from this — no async frameworks or thread pool tuning needed.
- **Single binary deployment.** Go compiles to a native executable with no runtime dependency. Docker images stay small (~10–20MB vs 200–400MB with the JVM), and deployment is a single file copy.
- **Standard library HTTP server.** `net/http` is production-grade out of the box. No Spring Boot or external framework needed to stand up the gateway.
- **Fast startup.** No JVM warm-up. Relevant when running multiple gateway instances in Docker Compose.

**Tradeoff:** Java has a larger ecosystem and more mature tooling for enterprise use cases. That's not relevant here — GateKeeper is a high-throughput networked service, which is exactly the workload Go was designed for.

---

## ADR-003 — Lua Scripts Colocated with Go Packages

**Decision:** Each Lua script lives in the same directory as the Go package that embeds it (e.g. `internal/limiter/tokenbucket/token_bucket.lua`), not in a shared top-level `lua/` folder.

**Reason:** Go's `//go:embed` directive cannot reference files outside the package's own directory tree — paths with `../` are rejected at compile time. This is an intentional security boundary: a package may only bundle files it owns. Colocation respects that constraint and also keeps each package self-contained.

**Tradeoff:** Lua files are not in one central place. Acceptable because each script is tightly coupled to the package that calls it and has no reason to be shared.

---

## ADR-005 — Policy Engine: Compile at Startup, Match at Request Time

**Decision:** `policy.New()` compiles the full config into limiter instances once when the gateway starts. `Matcher.Match()` performs only a path lookup and key construction at request time.

**Reason:** Parsing YAML, resolving algorithm types, and constructing Redis script objects are all one-time costs. Doing this per-request would add unnecessary latency on the hot path. Since all rate limiting state is in Redis — not in the `Matcher` — rebuilding the `Matcher` (e.g. on config change) is stateless and loses nothing.

**Tradeoff:** Config changes require a restart. Hot-reload via etcd or Zookeeper was deliberately deferred. The architecture supports adding it later: replacing `config.Load()` with a watcher and holding the `Matcher` behind an `atomic.Pointer` would be sufficient.

---

## ADR-006 — Multi-Scope Policy Layout (Option A)

**Decision:** Multiple rate limit scopes (e.g. per `api_key` and per `ip`) are expressed as sub-entries within a single policy block, not as separate top-level policy entries per scope.

**Reason:** The `algorithm` field sits at the policy level. With a single policy per endpoint, changing `algorithm: token_bucket` to `algorithm: sliding_window` covers all scopes on that endpoint in one edit. Separate entries per scope would require updating each entry individually — adding friction to the algorithm comparison use case this project is built around.

**Tradeoff:** All scopes on a given endpoint share the same algorithm. Mixing algorithms per scope on the same endpoint is not supported. This is an acceptable constraint given the design goal.

---

## ADR-007 — Dual Algorithm Params in Scope Config

**Decision:** Both token bucket params (`capacity`, `refill_rate`) and sliding window params (`limit`, `window`) are always present in every scope block in `config.yaml`, regardless of which algorithm is active.

**Reason:** The primary use case is switching between algorithms to observe traffic behaviour differences. Requiring the operator to also add/remove parameter fields when switching algorithms creates unnecessary friction. With both param sets always present, only the `algorithm:` field changes.

**Tradeoff:** Scope blocks contain "dead" fields at any given time. Acceptable because the config file is small and the intent of each field is clear from its name.

---

## ADR-008 — Health Endpoint Bypasses Rate Limit Middleware

**Decision:** `/health` is registered directly on the mux before the rate limit middleware wraps the proxy. The middleware never sees health check requests.

**Reason:** The rate limit middleware calls Redis to check counters. If Redis is unavailable, the middleware returns 500 (when `on_limiter_error: deny`). If `/health` went through the middleware, Docker's health check would receive that 500, mark the gateway as unhealthy, and nginx would stop routing traffic to it — even though the gateway process itself is perfectly functional. Bypassing the middleware for `/health` ensures Docker measures the liveness of the gateway process, not the availability of Redis.

**Tradeoff:** The health endpoint does not validate Redis connectivity. A separate readiness probe endpoint (e.g. `/ready`) that explicitly pings Redis could be added if distinguishing liveness from readiness becomes necessary.

---

## ADR-009 — Configurable Limiter Error Behavior (`on_limiter_error`)

**Decision:** A top-level `on_limiter_error` field in `config.yaml` controls what happens when a rate limiter returns an error (e.g. Redis is unreachable). `deny` returns 500 and blocks the request; `allow` passes the request through. Default is `deny`.

**Reason:** The right behaviour when Redis is down depends on the use case. A payments API should block all traffic rather than risk unbounded load on a degraded backend (fail-closed). A read-heavy public API may prefer to stay available and accept the risk of temporarily unenforced limits (fail-open). Neither is universally correct, so the operator chooses explicitly.

**Tradeoff:** `allow` during a Redis outage means rate limits are completely unenforced for the duration. `deny` means the gateway returns 500s to all rate-limited paths, which may be indistinguishable from a backend failure to the caller. Both are documented so the operator understands what they are choosing.

---

## ADR-004 — Clock Injection for Deterministic Tests

**Decision:** The core logic of each rate limiter is exposed as an unexported method accepting the current timestamp as a parameter (e.g. `allow(nowMs int64)`). The public `Allow()` method calls this with `time.Now().UnixMilli()`.

**Reason:** Tests that depend on real elapsed time require actual sleeps, making the test suite slow and potentially flaky. By accepting the clock as an input, tests can simulate any time interval by passing a fake timestamp directly — no sleeping required.

**Tradeoff:** A thin split between the public and internal method. Negligible complexity cost versus the benefit of a fast, deterministic test suite.
