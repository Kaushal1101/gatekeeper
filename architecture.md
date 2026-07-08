# Architecture Decisions

---

## ADR-001 — Language: Go over Java

**Decision:** Build GateKeeper in Go.

**Reasons:**

- **Concurrency model.** Goroutines cost ~2KB each vs ~1MB for Java threads. A rate-limiting gateway handling thousands of simultaneous requests benefits directly from this — no async frameworks or thread pool tuning needed.
- **Single binary deployment.** Go compiles to a native executable with no runtime dependency. Docker images stay small (~10–20MB vs 200–400MB with the JVM), and deployment is a single file copy.
- **Standard library HTTP server.** `net/http` is production-grade out of the box. No Spring Boot or external framework needed to stand up the gateway.
- **Fast startup.** No JVM warm-up. Relevant when running multiple gateway instances in Docker Compose.

**Tradeoff:** Java has a larger ecosystem and more mature tooling for enterprise use cases. That's not relevant here — GateKeeper is a high-throughput networked service, which is exactly the workload Go was designed for.
