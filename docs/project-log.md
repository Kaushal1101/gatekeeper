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
