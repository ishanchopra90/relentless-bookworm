# Chaos Mesh

Chaos Mesh is installed via Helm for chaos testing (e.g. pod kill, network delay).

## Installation

Chaos Mesh is installed automatically by `scripts/helm_up.sh`:

```sh
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update
helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh --create-namespace \
  --set chaosDaemon.runtime=containerd \
  --set chaosDaemon.socketPath=/run/containerd/containerd.sock
```

**Kind uses containerd**, so we set `chaosDaemon.runtime=containerd` and `chaosDaemon.socketPath=/run/containerd/containerd.sock`. For Docker-based clusters, omit these flags.

## Verify

```sh
kubectl get pods -n chaos-mesh -l app.kubernetes.io/instance=chaos-mesh
```

**Layout:** One-off chaos experiments (PodChaos, NetworkChaos) are in **`chaos/`**; workflows and schedules are in **`workflows/`**.

## Worker pod kills (PodChaos)

Chaos test: **Worker pod kills** for 20-seed, max 31 workers, 1 graph writer. Validates KEDA scale-up, frontier lag spike/recovery, Kafka consumer-group rebalances, worker commit coordinator behavior.

| File | Effect |
|------|--------|
| `chaos/podchaos-worker-kill-1.yaml`  | Kill 1 worker pod (`mode: one`) |
| `chaos/podchaos-worker-kill-5.yaml`  | Kill 5 worker pods (`mode: fixed`, value 5) |
| `chaos/podchaos-worker-kill-10.yaml` | Kill 10 worker pods (`mode: fixed`, value 10) |
| `chaos/podchaos-worker-kill-all.yaml`| Kill all worker pods (`mode: all`, e.g. 31 at max scale) |

**Run (example: kill 5 workers):**

```sh
# Start a crawl so workers scale up (e.g. 20 seeds, wait for lag).
# Then inject chaos:
kubectl apply -f deploy/kubernetes/chaos-mesh/chaos/podchaos-worker-kill-5.yaml
```

**Observe:** Grafana (worker replicas, frontier lag, edges/s), `kubectl get pods -l app=relentless-worker -w`, worker logs for commit/rebalance.

**Remove:**

```sh
kubectl delete -f deploy/kubernetes/chaos-mesh/chaos/podchaos-worker-kill-5.yaml
```

PodChaos `pod-kill` runs once (kills the selected pods); the Deployment replaces them. No duration field needed.

## Graph-writer pod failure (PodChaos)

Single graph-writer pod in a **failure** state for 5–10 minutes. Use **pod-failure** (not pod-kill) so the pod isn’t replaced immediately. Observations and analysis: **results/WORKER_V3_CHAOS_README.md**.

| File | Effect |
|------|--------|
| `chaos/podchaos-graph-writer-failure-10m.yaml` | One graph-writer pod failed for **10 min** (`duration: "600s"`); use `"300s"` for 5 min. |

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/chaos/podchaos-graph-writer-failure-10m.yaml
# Delete to end early: kubectl delete -f deploy/kubernetes/chaos-mesh/chaos/podchaos-graph-writer-failure-10m.yaml
```

## Neo4j pod failure (PodChaos)

Single Neo4j pod in a **failure** state for 10 minutes. Use **pod-failure** (not pod-kill) so the pod isn't replaced immediately. Observations: **results/WORKER_V3_CHAOS_README.md**.

| File | Effect |
|------|--------|
| `chaos/podchaos-neo4j-failure-10m.yaml` | One Neo4j pod failed for **10 min** (`duration: "600s"`); use `"300s"` for 5 min. |

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/chaos/podchaos-neo4j-failure-10m.yaml
# Delete to end early: kubectl delete -f deploy/kubernetes/chaos-mesh/chaos/podchaos-neo4j-failure-10m.yaml
```

## Kafka broker pod failure (PodChaos)

Single Kafka broker pod in a **failure** state for 10 minutes (namespace `kafka`). Use **pod-failure** (not pod-kill) so the pod isn't replaced immediately. Observations: **results/WORKER_V3_CHAOS_README.md**.

| File | Effect |
|------|--------|
| `chaos/podchaos-kafka-broker-failure-10m.yaml` | One Kafka broker pod (Strimzi cluster `relentless-kafka`) failed for **10 min** (`duration: "600s"`); use `"300s"` for 5 min. |

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/chaos/podchaos-kafka-broker-failure-10m.yaml
# Delete to end early: kubectl delete -f deploy/kubernetes/chaos-mesh/chaos/podchaos-kafka-broker-failure-10m.yaml
```

## Network chaos: workers → Open Library (NetworkChaos)

Use **delay** and/or **loss** on egress from worker pods to Open Library during a 20-seed run. Validates: worker retries, DLQ (for loss), and lag behavior at 31w scale. Observations and analysis: **results/WORKER_V3_CHAOS_README.md**.

| File | Effect |
|------|--------|
| `chaos/networkchaos-worker-delay.yaml` | Delay **500ms** (+ 100ms jitter) on worker → openlibrary.org. All workers. |
| `chaos/networkchaos-worker-loss.yaml`  | **25%** packet loss on worker → openlibrary.org. All workers. |

**Observe:** Fetch latency p95, frontier lag, worker error rate; with loss, check DLQ (dead-letter topic) for failed jobs.

**Run (delay):**

```sh
# Crawl running (e.g. 20 seeds, workers scaled). Then:
kubectl apply -f deploy/kubernetes/chaos-mesh/chaos/networkchaos-worker-delay.yaml
```

**Run (loss):**

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/chaos/networkchaos-worker-loss.yaml
```

**Remove:**

```sh
kubectl delete -f deploy/kubernetes/chaos-mesh/chaos/networkchaos-worker-delay.yaml
kubectl delete -f deploy/kubernetes/chaos-mesh/chaos/networkchaos-worker-loss.yaml
```

**Note:** NET_SCH_NETEM must be available in the cluster (common on Kind). Only traffic from worker pods to `openlibrary.org` is affected (Kafka, Redis, etc. unchanged).

### Workflows and schedules (`workflows/`)

To run kills repeatedly or as a one-off sequence, use a **Workflow** or **Schedule**:

| File | Effect |
|------|--------|
| `workflows/workflow-worker-pod-kill-sequence.yaml` | **One run only:** kill 1 → wait 5 min → kill 5 → wait 5 min → kill 10 → wait 5 min → kill all (31); then workflow completes |
| `workflows/workflow-network-chaos-delay-then-loss.yaml` | **One run only:** delay (worker → openlibrary.org) 5 min → wait 5 min → **100%** packet loss 5 min; then workflow completes |
| `workflows/schedule-worker-pod-kill-5.yaml` | Kill 5 workers **every 5 minutes** (`@every 5m`) |
| `workflows/schedule-worker-pod-kill-sequence.yaml` | **Repeating sequence:** kill 1 → 5 → 10 → 31 (5 min apart), **cycle repeats every 20 min** |

**One run of the sequence (Workflow):**

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/workflows/workflow-worker-pod-kill-sequence.yaml
```

The workflow runs once: kill 1 worker, suspend 5 min, kill 5, suspend 5 min, kill 10, suspend 5 min, kill all. Then it finishes. To run again, delete and re-apply.

```sh
kubectl delete -f deploy/kubernetes/chaos-mesh/workflows/workflow-worker-pod-kill-sequence.yaml
```

**One run: delay 5 min → wait 5 min → 100% loss 5 min (worker → Open Library):**

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/workflows/workflow-network-chaos-delay-then-loss.yaml
kubectl delete -f deploy/kubernetes/chaos-mesh/workflows/workflow-network-chaos-delay-then-loss.yaml
```

**Single schedule (e.g. kill 5 every 5 min):**

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/workflows/schedule-worker-pod-kill-5.yaml
kubectl annotate schedule worker-pod-kill-5 -n chaos-mesh experiment.chaos-mesh.org/pause=true   # pause
kubectl annotate schedule worker-pod-kill-5 -n chaos-mesh experiment.chaos-mesh.org/pause-       # resume
kubectl delete -f deploy/kubernetes/chaos-mesh/workflows/schedule-worker-pod-kill-5.yaml
```

**Repeating sequence (Schedule; cycle every 20 min):**

```sh
kubectl apply -f deploy/kubernetes/chaos-mesh/workflows/schedule-worker-pod-kill-sequence.yaml
```

Pause all four schedules:

```sh
for n in worker-kill-1 worker-kill-5 worker-kill-10 worker-kill-all; do kubectl annotate schedule $n -n chaos-mesh experiment.chaos-mesh.org/pause=true; done
```

Remove all:

```sh
kubectl delete -f deploy/kubernetes/chaos-mesh/workflows/schedule-worker-pod-kill-sequence.yaml
```

Cron times: kill 1 at :00/:20/:40, kill 5 at :05/:25/:45, kill 10 at :10/:30/:50, kill all at :15/:35/:55. Each schedule uses `concurrencyPolicy: Forbid`. To change the 5 min gap, edit the minute lists in each `schedule` (e.g. `"0,15,30,45"`, `"5,20,35,50"`, etc.).

See **results/WORKER_V3_CHAOS_README.md** for full chaos test procedure and observations.
