# GateKeeper — Build Plan

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

## Phase 1 — Foundation
- [x] Go module setup and folder structure
- [x] Mock backend (`cmd/mock-backend/main.go`)
- [x] Gateway skeleton (`cmd/gateway/main.go`)
- [ ] Docker Compose: nginx LB + 2 gateway instances + Redis + mock backend

## Phase 2 — Core Rate Limiting
- [ ] Redis client wrapper
- [ ] Rate limiter interface (Strategy Pattern)
- [ ] Token Bucket algorithm with Lua script for atomic Redis ops
- [ ] Sliding Window Counter algorithm with Lua script
- [ ] Unit tests for both algorithms

## Phase 3 — Policy Engine
- [ ] YAML config loader
- [ ] Policy matching by endpoint, IP, user ID, API key
- [ ] Weighted request costs (configurable cost per endpoint)
- [ ] Hierarchical limit scopes (per IP, per user, per API key, per endpoint)

## Phase 4 — Gateway Integration
- [ ] Request classification middleware
- [ ] Policy lookup and rate limit enforcement
- [ ] Allow/reject responses with appropriate headers
- [ ] Fail-open / fail-closed modes (configurable)
- [ ] Request forwarding to mock backend on allow

## Phase 5 — Observability
- [ ] Prometheus metrics: allowed/rejected counts, gateway latency, Redis latency, RPS, algorithm usage, failure counts
- [ ] Grafana dashboard
- [ ] Health check endpoint

## Phase 6 — Load Testing & Benchmarks
- [ ] k6 load test scripts
- [ ] Benchmark results demonstrating concurrent load across both gateway instances
- [ ] Document measured performance and tradeoffs
