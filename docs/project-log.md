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
- Docker Compose: nginx LB + 2 gateway instances + mock backend + Redis
