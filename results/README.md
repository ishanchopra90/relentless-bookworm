# Results and Run Analysis

Run analyses in this directory are based on **exported metrics** (e.g. Prometheus/Grafana) and **screenshots** from crawls. Raw metric exports (e.g. CSV) are not committed; when we say "data indicates" we mean metrics from those runs. Screenshots live under `raw_data/metrics/`.

**Story (read in order):**

1. [**WORKER_V1_README.md**](WORKER_V1_README.md) — Synchronous fetch; worker throughput bottleneck; lag grows with worker count.
2. [**WORKER_V2_README.md**](WORKER_V2_README.md) — Concurrent fetch + commit coordinator; ~5–8× edges/s; same bottleneck, better utilization.
3. [**WORKER_V3_AUTOSCALE_README.md**](WORKER_V3_AUTOSCALE_README.md) — KEDA on frontier (and edges) lag; scale-up/scale-down; Neo4j/KEDA limits; 8G node; 31w+1gwr optimal for 20-seed.
4. [**WORKER_V3_CHAOS_README.md**](WORKER_V3_CHAOS_README.md) — Chaos testing (pod kill, graph-writer/Neo4j/Kafka failure, network delay/loss) under 20-seed 31w+1gwr.
5. [**WORKER_V4_MULTIPLE_EGRESS_IP**](WORKER_V4_MULTIPLE_EGRESS_IP.md) — Proxy pool for multi-egress; reduces Open Library per-IP rate-limit stagnation; 3-proxy cap on 8G budget.

**Supporting docs:**

- [**FAILURE_MODES.MD**](FAILURE_MODES.MD) — Observed failure modes (Neo4j OOM, KEDA crash-loop, Kafka coordinator, stuck partition, cluster load) and mitigations.
- [**RESOURCE_BUDGET_8G.md**](RESOURCE_BUDGET_8G.md) — Bounded components, remainder for workers+graph writers, max replicas, proxy cap (§6a).
