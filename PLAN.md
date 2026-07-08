# GateKeeper — Build Plan

## Progress

| Phase | Status |
|---|---|
| Phase 1 — Foundation | In progress |
| Phase 2 — Core Rate Limiting | Not started |
| Phase 3 — Policy Engine | Not started |
| Phase 4 — Gateway Integration | Not started |
| Phase 5 — Observability | Not started |
| Phase 6 — Load Testing & Benchmarks | Not started |

### Phase 1 checklist
- [x] Go module setup (`go.mod`)
- [x] Folder structure
- [x] Mock backend (`cmd/mock-backend/main.go`)
- [ ] Gateway skeleton (`cmd/gateway/main.go`)
- [ ] Docker Compose

---

## Stack
- **Language:** Go
- **State:** Redis
- **Infrastructure:** Docker Compose (nginx LB + 2 gateway instances + Redis + mock backend)
- **Config:** YAML
- **Observability:** Prometheus + Grafana
- **Load Testing:** k6

## Request Identity
- IP: extracted from request
- User: `X-User-ID` header
- API Key: `X-API-Key` header

---

## Claude Code Tooling

### MCP Servers
| Tool | When to use |
|------|-------------|
| **Context7** | Throughout — fetches current Go library docs (go-redis, prometheus/client_golang, net/http) to avoid stale API usage |
| **GitHub MCP** | Throughout — create issues, review PRs, manage branches without leaving the session |
| **Redis MCP** | Phase 2+ — inspect key state, TTLs, and Lua script results while debugging rate limiters |
| **Docker MCP** | Phase 1+ — manage containers, stream logs, exec into services without copy-pasting docker output |

### Setup order
1. **Now** — install Context7, GitHub, Docker MCPs before writing any code
2. **Phase 2** — add Redis MCP when rate limiter work begins

---

## Phase 1 — Foundation
- Go module setup and folder structure
- Simple mock backend API (minimal HTTP server)
- Basic HTTP gateway server skeleton
- Docker Compose: nginx load balancer + 2 gateway instances + Redis + mock backend

## Phase 2 — Core Rate Limiting
- Redis client wrapper
- Rate limiter interface (Strategy Pattern)
- Token Bucket algorithm with Lua script for atomic Redis ops
- Sliding Window Counter algorithm with Lua script
- Unit tests for both algorithms

## Phase 3 — Policy Engine
- YAML config loader
- Policy matching by endpoint, IP, user ID, API key
- Weighted request costs (configurable cost per endpoint)
- Hierarchical limit scopes (per IP, per user, per API key, per endpoint)

## Phase 4 — Gateway Integration
- Request classification middleware
- Policy lookup and rate limit enforcement
- Allow/reject responses with appropriate headers
- Fail-open / fail-closed modes (configurable)
- Request forwarding to mock backend on allow

## Phase 5 — Observability
- Prometheus metrics: allowed/rejected counts, gateway latency, Redis latency, RPS, algorithm usage, failure counts
- Grafana dashboard
- Health check endpoint

## Phase 6 — Load Testing & Benchmarks
- k6 load test scripts
- Benchmark results demonstrating concurrent load across both gateway instances
- Document measured performance and tradeoffs
