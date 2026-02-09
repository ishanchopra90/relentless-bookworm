# Failure Modes: Architectural Summary

This document summarizes failure modes observed in the Relentless Bookworm pipeline (API → Kafka → Workers → Graph Writer → Neo4j) and the architectural lessons they imply. It draws on findings from [**WORKER_V3_AUTOSCALE_README.md**](WORKER_V3_AUTOSCALE_README.md) (autoscaled runs, Neo4j/KEDA limits) and from debugging **graph writer throughput drop, edge lag growth, and Kafka Group Coordinator unavailability**.

---

## 1. Architecture and Shared Resources

The pipeline runs on a **single Kind node** (or a small cluster) with:

- **Kafka** (Strimzi): broker + controller + **group coordinator** (consumer groups, offset commits, rebalances).
- **Workers**: consume frontier, produce to results + edges topics; scale with KEDA on frontier lag.
- **Graph writers**: consume results + edges, write to **Neo4j**; scale with KEDA on edges lag.
- **Neo4j**: single instance; all graph writers connect to it.
- **KEDA**: operator + metrics server; HPA depends on it for Kafka lag metrics.

**Architectural takeaway:** The **broker is a shared critical path**. The group coordinator runs inside the same Kafka JVM. When the broker is starved (no CPU/memory guarantees) or overloaded (many consumers joining, readiness probe timeout), the coordinator stops responding → consumers see "Group Coordinator Not Available" and throughput drops to zero while lag grows. Similarly, **Neo4j** is a single point of contention: many writers → lock/throughput/OOM risk. **KEDA** is another shared dependency: if the operator is OOM or crash-looping, scaling breaks (replicas don’t scale down when lag clears).

---

## 2. Failure Modes (Summary)

### 2.1 From [WORKER_V3_AUTOSCALE_README.md](WORKER_V3_AUTOSCALE_README.md)

| Failure | Symptom | Cause | Fix |
|--------|----------|------|-----|
| **Neo4j OOM** | "connection refused", edge lag stuck, Neo4j CrashLoopBackOff (exit 137). | No resource limits (BestEffort); 14 graph writers → high write load → memory growth → OOMKilled. | Set `requests.memory: "512Mi"`, `limits.memory: "1Gi"` and cap JVM heap (e.g. 256m/512m) in Neo4j deployment. |
| **KEDA operator crash-loop** | Edges lag → 0 but graph-writer replicas stay at 14 (no scale-down). HPA `TARGETS <unknown>`, `FailedGetExternalMetric`. | Single node ~4 Gi; total pod limits **> node memory** (e.g. ~128%) → KEDA operator OOM-starved, never serves metrics. | Increase Kind node memory (e.g. 6–8 Gi); optionally reduce KEDA component memory limits. |
| **Node overcommit** | Multiple components (Neo4j, KEDA, Kafka, workers, graph writers, monitoring) unstable or OOM. | All workloads on one node with no or weak limits → contention and OOM. | Give node more resources; set **resource requests/limits** for Kafka, Neo4j, KEDA, Grafana so critical path has guarantees. |
| **Graph writer lag despite many replicas** | Edge lag builds (e.g. 600–2600/partition); graph-writer errors spike. | Neo4j down or refusing connections (OOM/restart) → writers can’t write → lag grows. | Fix Neo4j (resources + heap); then writers drain lag. |

### 2.2 From Debugging: Graph Writer Throughput Zero, Edge Lag Rising

| Failure | Symptom | Cause | Fix |
|--------|----------|------|-----|
| **Kafka: "use of closed network connection"** | Graph writer logs: `edges commit error: write tcp ... use of closed network connection`. Throughput → 0, edge lag rises. | Broker or client closed the TCP connection (e.g. after idle, rebalance, or broker under load). Commit fails; reader may be in bad state. | Restart graph writers for fresh connections. Long-term: optional commit retry/backoff; tune `session.timeout.ms` / `max.poll.interval.ms` only when broker is **healthy** and consumer is slow. |
| **Kafka: "Group Coordinator Not Available"** | Graph writer logs: `[15] Group Coordinator Not Available` on every fetch. Throughput → 0, edge lag rises. | **Broker overload**: coordinator runs in same JVM; broker has no resources (BestEffort), small heap (e.g. 128M), readiness probe **timing out** → coordinator can’t respond. | **Increase Kafka resources** (memory for broker pod, larger heap in Kafka CR). Scale down consumers (e.g. graph writers) to reduce load on coordinator; then scale back up. |
| **Broker overload (no visibility)** | Readiness probe timeout; coordinator unavailable; hard to diagnose. | Broker in `kafka` namespace; Grafana only showed **default** namespace container memory → Kafka CPU/memory not tracked. | Add Grafana panels for `kafka` namespace (e.g. `container_memory_working_set_bytes{namespace="kafka"}`, CPU rate). Set **resource requests/limits** and heap for Kafka so it’s not BestEffort. |
| **Worker: commit failure and re-queue** | Worker commit coordinator logs `commit error partition=… offset=…`; `relentless_worker_commit_errors_total` increases; offsets may be retried on next drain. | Broker or client closed the connection, coordinator unavailable, or transient I/O error during `CommitMessages`. Coordinator does **not** advance `nextOffset` on failure; it re-queues the message so the same offset is retried on the next drain (avoids skipping offsets). | Restart workers for fresh Kafka connections if errors persist. Fix broker/coordinator (resources, stability). Coordinator stops draining that partition on failure so it does not hammer a failing commit; retry happens when the next message for that partition arrives or on flush. |

---

## 3. Observability Gaps

- **Kafka resource utilization** was not in Grafana: container memory/CPU panels used `namespace="default"`, so the Kafka broker (and Strimzi pods in `kafka` namespace) were not visible. Broker overload (readiness timeout, Group Coordinator Not Available) was only confirmed via `kubectl describe` and logs.
- **Neo4j** memory was called out in V3 readme as something to track (e.g. panel for `relentless-neo4j`); same idea applies to Kafka (broker pod name / namespace).
- **Recommendation:** Track memory (and optionally CPU) for **all** critical path components: `default` (workers, graph writer, Neo4j) **and** `kafka` (broker, entity-operator). Alert or at least visualize when broker or Neo4j approach their limits.

---

## 4. Mitigations Summary (Architectural)

1. **Resource guarantees for shared critical path**
   - **Kafka:** Set memory (and optionally CPU) requests/limits; increase JVM heap so broker and group coordinator can respond under load. Prevents "Group Coordinator Not Available" and readiness timeout when many consumers connect.
   - **Neo4j:** Memory limits + JVM heap cap (already applied) to avoid OOM when many graph writers write.
   - **KEDA / Grafana:** Set limits so they don’t OOM when node is busy; optionally reduce component sizes if needed.

2. **Node capacity**
   - Single Kind node must have enough memory for Kafka + Neo4j + KEDA + workers + graph writers + monitoring. Overcommit (e.g. 128% of node memory) leads to OOM and crash-loops (KEDA, Neo4j, or broker).

3. **Consumer vs coordinator load**
   - Many consumers (e.g. 14 graph writers) joining at once can overwhelm the **group coordinator**. Scale down to stabilize (e.g. 1–2 graph writers), then scale up gradually. Match graph writer replicas to edge topic partition count to avoid idle replicas and unnecessary rebalances.

4. **Connection and session tuning**
   - **"Use of closed network connection"**: Restart consumers for fresh Kafka connections; optionally add commit retry/backoff in graph writer.
   - **Session timeout / max.poll.interval**: Only tune when the **broker is healthy** and the **consumer** is slow (e.g. long Neo4j writes). Do not use as a substitute for fixing broker resource starvation.

5. **Observability**
   - Add Kafka (and any other `kafka` namespace) resource utilization to Grafana so broker overload is visible before it causes "Group Coordinator Not Available" and graph writer throughput drop.

---

## 5. Reduce Cluster Load: Conclusion from 20-Seed Worker/Graph-Writer Comparison

The run analysis in [**WORKER_V3_AUTOSCALE_README.md**](WORKER_V3_AUTOSCALE_README.md) (20-seed: 31w+1gwr vs 28w+3gwr vs 26w+5gwr) compares three configurations with total replicas ≤ 32 on an 8G node. **Conclusion from that analysis: 31w+1gwr is best** for this setup—Neo4j has headroom, edge lag stays 0, Kafka load is moderate. As graph writers increase (1 → 3 → 5) and workers decrease (31 → 28 → 26), Neo4j memory, Kafka broker load, and edge lag worsen; at 26w+5gwr, edge lag grows and never drains, and Strimzi/Entity Operator exhibit the failure mode below.

### Failure mode (architectural)

When **cluster load is too high** for the node (many workers + many graph writers), shared components suffer:

- **Neo4j:** Memory approaches its limit; writes slow (GC, pressure); graph writers cannot drain the edges topic fast enough → edge lag rises and does not subside.
- **Kafka:** Broker and group coordinator CPU/memory rise; under load the coordinator can stop responding → "Group Coordinator Not Available", "Group Load In Progress"; graph writers stall.
- **Strimzi:** Cluster operator blocks on the event loop (reconciliation, Entity Operator) and loses leader lease → CrashLoopBackOff; Entity Operator may fail to connect (timeout) → CrashLoopBackOff.

The result is rising edge lag, commit/coordinator errors, and operator crash-loops—all traceable to **high cluster load**, not to a single bug. Data and metrics for the 26w+5gwr run are in [WORKER_V3_AUTOSCALE_README.md](WORKER_V3_AUTOSCALE_README.md); this document only summarizes the architectural lesson.

### Mitigations

- **Reduce replicas or give more node:** Fewer workers and/or fewer graph writers (e.g. prefer 31w+1gwr over 26w+5gwr on 8G), or increase node memory/CPU.
- **Cap Neo4j write pressure:** Memory limits + JVM heap cap; if Neo4j is near limit, scale down graph writers so Neo4j can drain the backlog.
- **Stabilize Kafka and Strimzi:** Resource requests/limits for broker and operators (not BestEffort); fix or disable Entity Operator if it can’t connect. Reducing pod count and API churn helps Strimzi renew its leader lease.

---

## 6. Stuck Partition: In-Order Commit and Publish Blocking

**Symptom:** One or more frontier partitions show high, non-dropping lag for a long time (e.g. 20–40+ minutes) even though the consumer pod is assigned and running.

**Root cause (architectural):** The worker uses an **in-order commit coordinator** per partition: it only commits offset N once the message for offset N has been completed (handled and sent to `commitCh`). If the goroutine processing offset N never returns (e.g. stuck inside the job handler on a long HTTP retry, or **blocked on a Kafka produce** to results/edges/DLQ that doesn’t respect context), it never sends that message to `commitCh`. The coordinator then cannot commit N, and by in-order rule it cannot commit N+1, N+2, … even if those later messages have already completed. So one stuck goroutine blocks all subsequent commits for that partition; completed messages pile up in the coordinator’s pending buffer.

**Observability:** The metric **`relentless_worker_commit_pending_total`** (per worker pod) is the number of messages buffered in the coordinator awaiting commit. If one or two pods show **orders of magnitude** higher values (e.g. hundreds or thousands) than the rest (single digits), that indicates those pods have a partition stuck as above: many messages have completed and been enqueued, but an earlier offset never arrived because its goroutine is stuck. Scrape from Prometheus: `relentless_worker_commit_pending_total`.

**Mitigation (implemented):** A **bounded publish phase** ensures the commit path is never blocked indefinitely. After the job handler returns, all Kafka writes (result, edges/frontier, DLQ) run under a **`PUBLISH_TIMEOUT`** (default 90s). If the publish phase exceeds that, the goroutine logs and returns so the deferred `commitCh <- msg` always runs and the partition can advance. One message’s side effects may be partial or lost, but the consumer offset moves and the partition is no longer stuck. See **cmd/worker/main.go** (`publishTimeout`, `publishCtx`, and "publish start/done" logging).

**Takeaway:** With in-order commit, a single goroutine that blocks (in handler or in Kafka produce) can stall a whole partition. Use **commit_pending_total** to confirm; harden by bounding the publish phase with a timeout so the commit path is always reached.

---

## 7. Open Library rate limits

Open Library applies **per-IP** rate limits (and optional shared limits for non-identifying crawlers). Blessed IPs or User-Agents are exempt; otherwise API traffic is capped per IP (e.g. 180 req/min API, 1 req/s web). With a single egress, all workers share one IP and hit the cap quickly → HTTP 429 and stuck partitions. Spreading traffic across multiple egress IPs (proxy pool) avoids that; see [**WORKER_V4_MULTIPLE_EGRESS_IP**](WORKER_V4_MULTIPLE_EGRESS_IP.md). Rate limit zones and rules are defined in Open Library’s nginx config: [docker/nginx.conf](https://github.com/internetarchive/openlibrary/blob/master/docker/nginx.conf).

---

## 8. References

- [**WORKER_V3_AUTOSCALE_README.md**](WORKER_V3_AUTOSCALE_README.md) – Autoscaled runs (7+1, 14+7, 14+14), Neo4j OOM, KEDA crash-loop, 8G node run with single graph writer.
- [**WORKER_V4_MULTIPLE_EGRESS_IP**](WORKER_V4_MULTIPLE_EGRESS_IP.md) – Proxy pool, multi-egress, rate-limit behaviour.
- **Deploy:** `deploy/kubernetes/kafka/kafka-cluster.yaml`, `deploy/kubernetes/neo4j/neo4j-deployment.yaml`, `deploy/kubernetes/grafana/dashboards/relentless-overview.json`.
