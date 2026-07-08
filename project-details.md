PROJECT_DETAILS.md

GateKeeper Technical Direction

Technology Stack

Primary language:

* Go (preferred)

Infrastructure:

* Redis
* Docker
* Docker Compose

Testing & Benchmarking:

* k6 (preferred) or Locust

Observability:

* Prometheus
* Grafana

Version Control:

* Git
* GitHub

⸻

Core Architecture

High-level architecture:

Client
→ Load Balancer
→ Multiple Gateway Instances
→ Shared Redis
→ Protected API Endpoint

Each gateway instance is identical and stateless.

All rate limiting state is stored in Redis to support horizontal scaling.

Redis serves as the single source of truth for distributed rate limiting.

⸻

Initial Algorithms

Implement:

1. Token Bucket
2. Sliding Window Counter

Design the system so additional algorithms can easily be added later through a common interface (Strategy Pattern or equivalent).

Possible future algorithms:

* Fixed Window Counter
* Sliding Window Log

⸻

Redis Usage

Redis stores shared runtime state including:

* Token counts
* Last refill timestamps
* Sliding window counters
* Temporary rate limit metadata

Redis operations should be atomic.

Lua scripts should be used whenever multiple Redis operations must execute as a single transaction.

⸻

Gateway Responsibilities

Each gateway should:

* Receive API requests
* Classify requests
* Determine applicable policy
* Execute rate limiting
* Return Allow / Reject
* Expose metrics
* Forward successful requests to the protected endpoint

Gateways should remain stateless.

⸻

Policy Engine

Policies should be configuration-driven.

Examples include:

* Different algorithms per endpoint
* Different limits for different endpoints
* Different limits per user or API key
* Weighted requests
* Simple deny rules

The architecture should allow adding new policy types without major refactoring.

⸻

Weighted Rate Limiting

Support request “cost” instead of assuming every request costs one token.

Examples:

GET /health
Cost = 0

GET /users
Cost = 1

POST /login
Cost = 2

POST /upload
Cost proportional to request size

This better models real production APIs where different requests consume different resources.

⸻

Hierarchical Limits

Support multiple independent limit scopes.

Examples:

* Per IP
* Per User
* Per API Key
* Per Endpoint

Future combinations should be possible.

⸻

Failure Behaviour

Support configurable modes:

Fail Open

* Allow requests if Redis becomes unavailable.

Fail Closed

* Reject requests if Redis becomes unavailable.

Document the engineering tradeoffs between consistency and availability.

⸻

Performance Goals

Target:

* Low-latency rate limit decisions
* Efficient Redis usage
* Minimal memory overhead
* Horizontally scalable architecture

Performance should be measured using benchmarks rather than assumptions.

⸻

Observability

Expose metrics such as:

* Allowed requests
* Rejected requests
* Gateway latency
* Redis latency
* Requests per second
* Algorithm usage
* Failure counts

Visualize these metrics with Grafana dashboards.

⸻

Testing

The project should include:

* Unit tests
* Integration tests
* Concurrent load testing
* Benchmark results

Performance claims should be backed by measured data.

⸻

Design Priorities

When making engineering decisions, prioritize:

1. Simplicity
2. Correctness
3. Extensibility
4. Performance
5. Production-inspired architecture

Avoid unnecessary complexity that does not strengthen the project’s learning value or interview discussion.

⸻

Future Extensions (Optional)

Potential future improvements include:

* Dynamic policy updates
* Redis Cluster support
* Request prioritization
* Adaptive rate limiting
* Multi-region deployment discussion
* Kubernetes deployment
* Admin API for policy management

These are enhancements and should only be implemented after the core distributed rate limiter is complete and well-tested.