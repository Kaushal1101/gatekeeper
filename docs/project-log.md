# Project Log

---

## 2026-07-08 — Phase 1 foundation (branch: main)

### Work completed
- Initialized Go module (`github.com/kaushaljayapragash/gatekeeper`)
- Scaffolded full folder structure: `cmd/`, `internal/`, `lua/`, `config/`, `docker/`, `k6/`
- Built mock backend (`cmd/mock-backend/main.go`) with four endpoints:
  - `GET /api/fast` — lightweight read, will receive low token cost in policy config
  - `GET /api/slow` — 200ms artificial delay, simulates DB/IO-bound endpoint
  - `POST /api/expensive` — write endpoint, will receive higher token cost
  - `GET /health` — liveness check for Docker Compose and gateway
- Built gateway skeleton (`cmd/gateway/main.go`) — reverse proxy using `httputil.NewSingleHostReverseProxy`, forwards all traffic to `BACKEND_URL` (default: `localhost:8081`)
- Both services verified working end-to-end via curl

### Decisions made
- `BACKEND_URL` is read from an environment variable so Docker Compose can configure each gateway instance without code changes
- Gateway owns its own `/health` endpoint (responds locally, not proxied to backend)
- `loggingMiddleware` introduced as the first middleware — rate limiter will slot in at the same point in Phase 4
- Three mock API endpoints chosen specifically to exercise weighted token costs in Phase 3 (cheap read, slow read, expensive write)

### Remaining in Phase 1
- ~~Docker Compose~~ — completed in next entry

---

## 2026-07-08 — Phase 1 complete: Docker Compose + logging fix (branch: main)

### Work completed
- Multi-stage Dockerfiles for gateway and mock-backend (`docker/gateway.Dockerfile`, `docker/mock-backend.Dockerfile`) — stage 1 compiles the binary, stage 2 copies it into a bare Alpine image (~10MB final size)
- nginx config (`docker/nginx.conf`) — round-robin upstream across `gateway-1:8080` and `gateway-2:8080`; explicitly passes `X-User-ID` and `X-API-Key` headers downstream
- `docker-compose.yml` — five services (nginx, gateway-1, gateway-2, mock-backend, Redis) with health-check ordering; mock-backend must be healthy before gateways start, gateways must be healthy before nginx accepts traffic
- Fixed logging middleware in `cmd/gateway/main.go` — moved from wrapping only the proxy to wrapping the entire mux, so `/health` requests are now visible in gateway logs alongside proxied requests
- Full stack verified: `docker compose up --build`, requests alternating between gateway-1 and gateway-2

### Decisions made
- Redis port 6379 exposed externally for Redis MCP inspection during Phase 2 debugging; mock-backend has no external port (internal only)
- nginx passes `X-User-ID` and `X-API-Key` headers explicitly — these would otherwise be silently dropped before reaching the gateway
- `loggingMiddleware` wraps the top-level mux so all routes are covered, not just the proxy

### Phase 1 status: complete

---

## 2026-07-08 — Phase 2 complete: Core Rate Limiting (branch: phase-2-rate-limiting)

### Work completed
- `internal/redis/client.go` — `NewClient()` reads `REDIS_ADDR` env var (default: `localhost:6379`), returns a configured `*goredis.Client`
- `internal/limiter/limiter.go` — `Limiter` interface: `Allow(ctx, key, cost) (bool, error)`
- `internal/limiter/tokenbucket/` — Token Bucket implementation:
  - `token_bucket.lua` — atomic Lua script: lazy refill via elapsed time, HMGET/HSET on a single Redis hash, TTL auto-set to `ceil(capacity/refillRate)*2`
  - `tokenbucket.go` — Go wrapper using `//go:embed` and `goredis.NewScript`; unexported `allow(nowMs)` for clock injection
  - `tokenbucket_test.go` — 5 tests: within limit, exceeds limit, refill over time, weighted cost, isolated keys
- `internal/limiter/slidingwindow/` — Sliding Window Counter implementation:
  - `sliding_window.lua` — two Redis string keys (current + previous window); weighted formula: `prev * (1 - elapsed_fraction) + curr`
  - `slidingwindow.go` — same pattern as tokenbucket
  - `slidingwindow_test.go` — 6 tests including previous-window weighting verification
- All 11 tests pass via miniredis (no real Redis required for tests)

### Decisions made
- **Lua scripts for atomicity** — prevents TOCTOU race conditions across multiple gateway instances; simpler than WATCH/MULTI/EXEC
- **Lua scripts colocated with packages** — `//go:embed` cannot reference parent directories; colocation is cleaner than workarounds
- **Plain string keys** — `Limiter` takes `key string`; the policy engine constructs the appropriate key per scope
- **Clock injection via unexported `allow(nowMs)`** — deterministic tests without sleeping; real production path uses `time.Now()`
- **miniredis for tests** — full Lua execution without a running Redis instance; fast, isolated, no Docker dependency for unit tests

### Phase 2 status: complete

---

## 2026-07-09 — Phase 3 complete: Policy Engine (branch: phase-2-rate-limiting)

### Work completed
- `config/config.yaml` — operator-facing rate limit config: three endpoints (`/api/fast`, `/api/slow`, `/api/expensive`), each with an `algorithm` toggle and two scopes (`api_key`, `ip`); both algorithm param sets always present to enable one-line algorithm switching
- `internal/config/config.go` — `Config`, `Policy`, `Scope` structs with `Load(path string)` and `EffectiveCost()` helper (guards against YAML-omitted `cost` field defaulting to Go's zero value)
- `internal/policy/policy.go` — `Matcher` with two phases:
  - `New(cfg, redisClient)` — compiles config into limiter instances at startup; parses durations, selects algorithms, builds `compiledPolicy` list once
  - `Match(path, apiKey, ip)` — at request time, finds matching policy and returns `[]Check` (limiter + Redis key + cost per scope); returns configured default action if no policy matches
- `internal/policy/policy_test.go` — 6 tests: known path returns correct checks, unknown path respects default deny/allow, Redis keys encode path+scope+identity, cost defaults to 1 when omitted, explicit cost preserved, correct policy selected when multiple policies present
- Added `gopkg.in/yaml.v3` dependency

### Decisions made
- **Option A multi-scope layout** — scopes are sub-entries within one policy block rather than separate top-level entries per scope; toggling `algorithm:` in one place covers all scopes on that endpoint simultaneously
- **Both algorithm params always present** — `capacity`/`refill_rate` (token bucket) and `limit`/`window` (sliding window) coexist in the same scope block; inactive params are ignored; enables algorithm comparison with a single field change and restart
- **Compile at startup, match at request time** — `New()` does all expensive work (parsing, object creation, algorithm selection) once; `Match()` is a fast linear scan; rate limit state lives in Redis so rebuilding the `Matcher` is stateless and safe
- **Redis key format** `/path:scope:value` (e.g. `/api/fast:api_key:abc123`) — encodes all three isolation dimensions so counters are always per-endpoint, per-scope-type, per-identity
- **`Matcher` returns `[]Check`, does not enforce** — enforcement (calling `Allow()`, applying AND logic, returning 429) is the middleware's responsibility; clean separation of policy lookup from policy enforcement
- **`default_action`** in YAML controls fail-open (`allow`) vs fail-closed (`deny`) for unmatched paths; no default-default — operator must be explicit

### Phase 3 status: complete

---

## 2026-07-10 — Phase 4 complete: Gateway Integration (branch: phase-2-rate-limiting)

### Work completed
- `internal/middleware/ratelimit.go` — `RateLimit(matcher *policy.Matcher) func(http.Handler) http.Handler`
  - Extracts `X-API-Key` header and real client IP (`X-Real-IP` from nginx, fallback to `r.RemoteAddr`)
  - Calls `matcher.Match(path, apiKey, ip)` → iterates `[]Check` in order (AND logic)
  - Returns 403 if no policy matches and `default_action: deny`
  - Returns 429 if any limiter returns false
  - Returns 500 or passes through (configurable via `on_limiter_error`) if any limiter returns an error
  - `writeJSON` helper ensures all error responses carry `Content-Type: application/json`
- `cmd/gateway/main.go` — fully wired startup sequence:
  - Loads config from `CONFIG_PATH` env var (default: `config/config.yaml`)
  - Creates Redis client via `gatewayredis.NewClient()`
  - Builds `policy.Matcher` from config + Redis client
  - `/health` registered directly on mux (bypasses rate limiting)
  - `/` wrapped: `middleware.RateLimit(matcher)(proxy)`
  - `loggingMiddleware` wraps the full mux so all routes are logged
- `internal/config/config.go` — added `OnLimiterError string` field (`yaml:"on_limiter_error"`)
- `config/config.yaml` — added `on_limiter_error: deny` top-level field with comment
- `internal/policy/policy.go` — added `onErrorAllow bool` to `Matcher`; `OnErrorAllow()` accessor method; `New()` sets it from `cfg.OnLimiterError == "allow"`
- `docker-compose.yml` — added `REDIS_ADDR=redis:6379` to both gateways; added Redis `healthcheck` (`redis-cli ping`); gateways now `depends_on` Redis being healthy
- `docker/gateway.Dockerfile` — added `COPY config/ config/` to final image stage so config YAML is present at runtime
- `docs/architecture-decisions.md` — added ADR-008 (health endpoint bypass) and ADR-009 (configurable limiter error behavior)

### Decisions made
- **`/health` bypasses rate limiting** — registered directly on the mux before the middleware wraps the proxy; Docker health checks must not depend on Redis availability (ADR-008)
- **`on_limiter_error` is configurable** — `deny` returns 500 (fail-closed, default); `allow` passes traffic through when Redis is down (fail-open); operator chooses explicitly (ADR-009)
- **`Matcher.OnErrorAllow()` accessor** — middleware reads error behavior from the Matcher rather than accepting it as a separate constructor parameter; keeps error policy co-located with all other policy config
- **`X-Real-IP` over `X-Forwarded-For`** — nginx sets `X-Real-IP: $remote_addr`; single trusted value, cannot be spoofed by client unlike `X-Forwarded-For`
- **`writeJSON` not `http.Error`** — `http.Error` forces `Content-Type: text/plain`; the gateway returns structured JSON errors for all 4xx/5xx responses
- **Redis healthcheck in Compose** — `redis-cli ping` as the check; gateways won't start until Redis is healthy, preventing startup failures from connection refused

### Phase 4 status: complete

---

## 2026-07-14 — Phase 4 smoke test verified (branch: phase-2-rate-limiting)

### Work completed
- Smoke tested the full Docker Compose stack end-to-end:
  - `GET /api/fast` with `X-API-Key: abc123` returns 200 correctly
  - Spamming 105 requests returns all 200s (expected — `capacity: 100` with `refill_rate: 10` means the refill keeps pace with the loop; `/api/slow` with limit of 10 triggers 429s as expected)
  - `GET /api/unknown` returns `{"error":"no policy for this path"}` (403) confirming policy engine and middleware are correctly wired
- Removed duplicate local `.claude/commands/` directory — skills now sourced exclusively from global `~/.claude/commands/`

### Decisions made
- No new engineering decisions. Smoke test confirmed existing implementation is correct.

### Phase 4 status: verified working end-to-end. Ready for Phase 5 (Observability).
