# GateKeeper

A distributed API rate-limiting gateway built in Go. Two stateless gateway instances sit behind an nginx load balancer, enforce configurable per-endpoint and per-identity rate limits using atomic Redis operations, and proxy accepted traffic to a backend service.

Built as a flagship backend/distributed systems project — emphasis on production-inspired design, clean architecture, and demonstrable engineering tradeoffs.

---

## System Architecture

```
Client (curl / k6)
        │
      nginx :80          ← load balancer, round-robin
     ↙       ↘
gateway-1   gateway-2    ← stateless Go reverse proxies + rate limit middleware
        ↘   ↙
    mock-backend :8081   ← fake API (/api/fast, /api/slow, /api/expensive)

      Redis :6379         ← shared distributed rate limit state
    Prometheus :9090      ← metrics scraper (Phase 5)
      Grafana :3000       ← dashboards (Phase 5)
```

All services run in Docker Compose on a private internal network. Clients only reach nginx.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go |
| Load balancer | nginx |
| Distributed state | Redis (atomic Lua scripts) |
| Rate limit algorithms | Token Bucket, Sliding Window Counter |
| Config | YAML |
| Observability | Prometheus + Grafana *(Phase 5)* |
| Load testing | k6 *(Phase 6)* |
| Infrastructure | Docker Compose |

---

## Features

- **Two rate limiting algorithms** — Token Bucket (burst-friendly) and Sliding Window Counter (smooth, prevents boundary exploit). Toggled per-endpoint with a single config field.
- **Multi-scope enforcement** — limits enforced independently per API key and per IP on every endpoint. AND logic: a request must pass all scopes to proceed.
- **Atomic Redis operations** — all read-modify-write cycles run inside Lua scripts, preventing TOCTOU races across gateway instances.
- **YAML policy config** — operators define per-endpoint algorithms, limits, and costs without touching code.
- **Configurable failure behavior** — `on_limiter_error: deny` (fail-closed, 500) or `allow` (fail-open, pass-through) when Redis is unavailable.
- **Structured JSON errors** — all 403/429/500 responses carry `Content-Type: application/json`.
- **Health endpoint bypass** — `/health` is wired outside the rate limit middleware so Docker health checks never depend on Redis availability.

---

## Project Structure

```
.
├── cmd/
│   ├── gateway/          # Gateway entry point — wires config, Redis, policy, middleware
│   └── mock-backend/     # Fake API with /api/fast, /api/slow, /api/expensive, /health
├── internal/
│   ├── config/           # YAML config loader and typed structs
│   ├── limiter/
│   │   ├── tokenbucket/  # Token Bucket — Lua script + Go wrapper + tests
│   │   └── slidingwindow/# Sliding Window Counter — Lua script + Go wrapper + tests
│   ├── middleware/        # RateLimit middleware — extracts identity, enforces checks
│   ├── policy/           # Policy engine — compiles config into Limiter instances, matches requests
│   └── redis/            # Redis client wrapper
├── config/
│   └── config.yaml       # Operator-facing rate limit policy config
├── docker/
│   ├── gateway.Dockerfile
│   ├── mock-backend.Dockerfile
│   └── nginx.conf
├── docker-compose.yml
├── docs/                 # Architecture decisions, project log, interview notes
└── k6/                   # Load test scripts (Phase 6)
```

---

## Getting Started

**Prerequisites:** Docker, Docker Compose

```bash
git clone https://github.com/Kaushal1101/gatekeeper.git
cd gatekeeper
git checkout phase-4-gateway-integration
docker compose up --build
```

Test it:

```bash
# Should return 200
curl -H "X-API-Key: abc123" http://localhost/api/fast

# Spam to trigger 429 (limit: 10 req/60s per API key)
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: abc123" http://localhost/api/slow
done

# Unknown path — 403 (default_action: deny)
curl http://localhost/api/unknown
```

---

## Rate Limit Config

Configured in `config/config.yaml`. Each endpoint defines an algorithm and one or more scopes:

```yaml
default_action: deny
on_limiter_error: deny

policies:
  - path: /api/fast
    algorithm: token_bucket     # swap to "sliding_window" to compare
    scopes:
      - by: api_key
        capacity: 100
        refill_rate: 10
      - by: ip
        capacity: 200
        refill_rate: 20

  - path: /api/slow
    algorithm: sliding_window
    scopes:
      - by: api_key
        limit: 10
        window: 60s
```

Both algorithm param sets are always present in each scope block — switching algorithms requires changing only the `algorithm:` field and restarting.

---

## Build Phases

| Phase | Description | Status |
|---|---|---|
| 1 | Foundation — Docker Compose stack, mock backend, gateway skeleton | ✅ Complete |
| 2 | Core rate limiting — Token Bucket + Sliding Window, Lua scripts, unit tests | ✅ Complete |
| 3 | Policy engine — YAML config loader, Matcher, multi-scope enforcement | ✅ Complete |
| 4 | Gateway integration — rate limit middleware wired end-to-end, smoke tested | ✅ Complete |
| 5 | Observability — Prometheus metrics, Grafana dashboards | 🔧 In progress |
| 6 | Load testing — k6 scripts, concurrent benchmark across both gateway instances | 📋 Planned |
