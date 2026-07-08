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
- `/interviewnotes` command created; `docs/interviewnotes.md` written for `internal/limiter`

### Decisions made
- **Lua scripts for atomicity** — prevents TOCTOU race conditions across multiple gateway instances; simpler than WATCH/MULTI/EXEC
- **Lua scripts colocated with packages** — `//go:embed` cannot reference parent directories; colocation is cleaner than workarounds
- **Plain string keys** — `Limiter` takes `key string`; the policy engine constructs the appropriate key per scope
- **Clock injection via unexported `allow(nowMs)`** — deterministic tests without sleeping; real production path uses `time.Now()`
- **miniredis for tests** — full Lua execution without a running Redis instance; fast, isolated, no Docker dependency for unit tests

### Phase 2 status: complete
