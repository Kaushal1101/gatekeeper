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
