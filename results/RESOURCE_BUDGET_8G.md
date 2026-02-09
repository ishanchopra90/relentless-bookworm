# Resource budget for 8G Kind node

This document lists **bounded** components (from `deploy/`), **observed memory** from run data (exported metrics), **suggested bounds** for all components, and the **remainder for workers + graph writers**. It then gives suggested resources for workers and graph writers and **max possible replicas**. Raw metric exports are not stored in the repo; analysis is based on exported metrics from the runs cited below.

**Data source:** Observed memory is from run data (exported container and Kafka metrics) for:

- **Default namespace:** runs 3_seed, 10_seed, single_seed_14worker_14graph_writer_increased_mem in `raw_data/metrics/worker_with_autoscaler/`.
- **Kafka namespace:** Kafka broker + Strimzi cluster operator + entity operator (topic/user) from the 10_seed run.

---

## 1. Bounded components (from deploy/)

| Component | Replicas | Request (mem / CPU) | Limit (mem / CPU) | Limit total (mem / CPU) |
|-----------|----------|---------------------|--------------------|---------------------------|
| Kafka (KafkaNodePool) | 1 | 512Mi / 500m | 1Gi / 1 | 1Gi / 1 |
| Neo4j | 1 | 512Mi / — | 1Gi / — | 1Gi / — |
| Grafana | 1 | 256Mi / — | 1Gi / — | 1Gi / — |
| Proxy (StatefulSet) | 3 | 128Mi / 50m each | 384Mi / 200m each | **1152Mi / 600m** |

**Sum of bounded limits:** **4224 Mi (~4.1 Gi) memory**, **1.6 CPU**.

---

## 2. Observed memory (from run data)

Observed working-set memory (min–max MB) across the three runs above:

| Component | Observed (min–max MB) | Notes |
|-----------|------------------------|--------|
| api | 6.5–14.7 |  |
| redis | 8.4–9.4 |  |
| kafka-lag-exporter | 7.6–12.3 |  |
| kube-state-metrics | 18.3–21.6 |  |
| prometheus | 77.8–122 | Prometheus server (operator-managed) |
| prometheus-operator | 17.4–18.3 | config-reloader ~7.5 MB |
| grafana | 46.8–70.3 | already bounded 1Gi in deploy |
| neo4j | 505–654 | already bounded 1Gi in deploy |
| graph-writer | 5.7–11.6 | per replica |
| worker | 6.7–28 | per replica |

**Worker memory across runs:** Worker memory is **per replica** and does **not** scale with the number of seeds. All three runs use 14 workers; the per-pod range is similar across runs. Run data (worker columns):

| Run | Worker columns | Observed per-pod range (MB) |
|-----|----------------|-----------------------------|
| `single_seed_14worker_14graph_writer_increased_mem` | 14 | ~0.4–29 (steady state ~20–28) |
| `3_seed` | 14 | ~0.4–32.5 (steady state ~19–27) |
| `10_seed` | 14 | ~2.7–32.5 (steady state ~18–28) |

So **total worker memory** scales with **replica count** (e.g. 14 × ~25 MB ≈ 350 MB for 14 workers), not with seed count. Peaks (up to ~32 MB per pod) are run-to-run variance; the 64Mi limit in section 5 covers these.

**Factors governing per-worker memory** (from `cmd/worker/` and `internal/ol/`):

| Factor | Effect |
|--------|--------|
| **CONCURRENT_JOBS** (default 5) | Number of in-flight crawl jobs. Each job holds: Kafka message, HTTP response body (full read), parsed node (including RawJSON), and outbound Kafka payloads. Memory scales roughly with concurrent jobs × (response size + parsed size + outbound). |
| **HTTP response size** | Open Library responses vary (small author pages vs large work/edition JSON). `FetchJSONWithClient` uses `io.ReadAll(resp.Body)`, so one large response increases peak memory for that job. |
| **RawJSON in nodes** | `BookNode`, `WorkNode`, and `AuthorNode` keep the full response body in `RawJSON` until result, edges, and frontier are published. So per job we hold the body at least twice (read buffer + node.RawJSON) until the job completes. |
| **Job type** | Search jobs (`handleSearch`) do not return a node with RawJSON; work/author/edition jobs do. So workload mix (search vs entity URLs) affects average memory per job. |
| **Kafka** | One message at a time from the reader; `commitCh` buffer is `concurrentJobs*2`. Writers write per message. Kafka-side memory is small relative to HTTP + parsed node. |
| **Go runtime** | Goroutine stacks (main loop + `concurrentJobs` worker goroutines + commit coordinator), HTTP transport connection pool (defaults), and GC. |

To reduce per-worker memory: lower `CONCURRENT_JOBS`, avoid retaining full RawJSON after parse (if the pipeline can be changed to publish without it), or cap HTTP response size (e.g. limit read length).

---

**Kafka namespace** (10_seed run data):

| Component | Observed (min–max MB) | Notes |
|-----------|------------------------|--------|
| kafka (broker) | 469–765 | KafkaNodePool; limit 1Gi in deploy |
| strimzi-cluster-operator | 248–281 |  |
| entity-operator (topic-operator) | 188–208 |  |
| entity-operator (user-operator) | 202–225 |  |
| entity-operator pod total | ~390–435 | topic + user + other |

---

## 3. Unbounded components → suggested bounds (from observed)

Suggested requests/limits are set from observed max with headroom (≈2× or round up to 32/64/128 Mi). Components in deploy/ now have resources; operator/Helm components are reserved in the budget.

| Component | Observed max (MB) | Suggested request | Suggested limit | In deploy? |
|-----------|-------------------|-------------------|-----------------|------------|
| Redis | 9.4 | 64Mi / 50m | 128Mi / 100m | yes |
| API | 14.7 | 32Mi / 50m | 64Mi / 100m | yes |
| Kafka lag exporter | 12.3 | 32Mi / 50m | 64Mi / 100m | yes |
| Kube-state-metrics | 21.6 | 32Mi / 50m | 64Mi / 100m | yes |
| Prometheus server | 122 | — | 256Mi (set in CR if supported) | no (operator) |
| Prometheus operator | 18.3 | — | 64Mi (operator defaults) | no |
| Strimzi cluster operator | 281 | — | 384Mi (set via operator) | no (strimzi.io/install) |
| Strimzi entity operator (topic+user) | 390–435 | — | 512Mi (one pod; set via Kafka CR) | no |
| KEDA | — | — | 384Mi (README) | no (Helm) |

**Sum of bounds in deploy/ (Redis, API, kafka-lag, kube-state):** **320 Mi** memory, **350m** CPU.

**Reserved for operator/Helm (not in deploy/):** Prometheus 256Mi + Prometheus operator 64Mi + Strimzi cluster 384Mi + Strimzi entity 512Mi + KEDA 384Mi = **1600 Mi** (Strimzi from Kafka-namespace run data).

---

## 4. 8G node budget and remainder for workers + graph writers

- **Node:** 8192 Mi (8 Gi).
- **Bounded (deploy/):** 4224 Mi.
- **Unbounded now bounded in deploy/ (Redis, API, kafka-lag, kube-state):** 320 Mi.
- **Reserved for operator/Helm (Prometheus + operator, Strimzi, KEDA):** 1600 Mi.
- **Remainder for workers + graph writers:**  
  **8192 − 4224 − 320 − 1600 = 2048 Mi (~2 Gi).**

(If you run without Prometheus/Strimzi/KEDA or with smaller limits, remainder increases.)

---

## 5. Suggested resources for workers and graph writers

- **Worker:** request 32Mi / 50m, limit **64Mi / 100m**.
- **Graph writer:** request 32Mi / 50m, limit **64Mi / 100m**.

Run data: workers ~0.4–32.5 MB per replica (steady state ~18–28 MB; see section 2), graph-writer 5.7–11.6 MB per replica. 64Mi limit gives headroom and keeps capacity predictable.

---

## 6. Max possible replicas (workers + graph writers)

With **64Mi memory limit each** for workers and graph writers:

- **(workers + graph_writers) × 64 Mi ≤ 2048 Mi**  
- **workers + graph_writers ≤ 32.**

So **max 32 replicas combined** (e.g. 18 workers + 14 graph writers, or 16 + 16). Partition count (e.g. 50) caps how many workers are *useful*; this is the **memory cap** on total replicas.

If you use **128Mi limit** each: **(workers + graph_writers) × 128 ≤ 2048** → **workers + graph_writers ≤ 16** (e.g. 10 workers + 6 graph writers).

---

## 6a. Proxy count and scaling limit (8G)

Proxy pods are bounded in `deploy/kubernetes/proxy/proxy-service.yaml` (128Mi / 50m request, 384Mi / 200m limit each). The remainder for workers + graph writers (2048 Mi) is computed *after* allocating for bounded components, which already include **3 proxies** (3 × 384 Mi = 1152 Mi). So with the current budget, **3 proxies is the maximum** if we keep the full 2048 Mi for workers + graph writers (32 × 64 Mi). Adding more proxies trades off worker/graph-writer capacity: 4 proxies → 26 combined replicas max, 5 → 20, 6 → 14. For runs that need both multi-egress and high worker count (e.g. 31 workers), we therefore cap the proxy pool at 3 on an 8G node; see [WORKER_V4_MULTIPLE_EGRESS_IP](WORKER_V4_MULTIPLE_EGRESS_IP.md) for the run analysis.

---

## 7. Binding all components

To know **max possible replicas** accurately, **bind all components** (set requests/limits on every workload). Then:

- Sum all **limits** (or requests) in deploy/ + operator/Helm.
- Remainder = 8192 − that sum.
- Max (workers + graph_writers) = floor(remainder / limit_per_pod) with your chosen worker and graph-writer limits.

This repo now adds resources to: Redis, API, kafka-lag-exporter, kube-state-metrics, worker, graph-writer. Prometheus, Strimzi, and KEDA are outside deploy/; set their limits via CR/Helm and include them in the sum when calculating remainder.

---

## 8. References

- Bounded components: `deploy/kubernetes/` (kafka-cluster.yaml, neo4j-deployment.yaml, grafana-deployment.yaml, proxy-service.yaml).
- Observed memory: run data (exported metrics) for default and Kafka namespaces; see section 2 for min–max MB per component. Screenshots in `raw_data/metrics/`; raw metric exports are not in the repo.
- Failure modes: [FAILURE_MODES.MD](FAILURE_MODES.MD), [deploy/kubernetes/kafka/README.md](../deploy/kubernetes/kafka/README.md), [deploy/kubernetes/worker/README.md](../deploy/kubernetes/worker/README.md).
