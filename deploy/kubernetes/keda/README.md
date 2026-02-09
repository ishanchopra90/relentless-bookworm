# KEDA ScaledObjects

## Worker (frontier lag)

Apply so workers scale on Kafka frontier lag:

```sh
kubectl apply -f deploy/kubernetes/keda/scaledobject-worker.yaml
```

## Proxy pool (multi-egress / Open Library rate limit bypass)

To bypass Open Library per-IP rate limits and increase throughput:

1. **Deploy a proxy pool** (e.g. 3 HTTP proxy services with different egress IPs).
2. **Set `PROXY_POOL`** on the worker deployment (comma-separated proxy URLs). Each pod picks a proxy by `HOSTNAME` (e.g. `relentless-worker-0` → proxy 0) so replicas spread across proxies.
3. **Increase KEDA `maxReplicaCount`** in `scaledobject-worker.yaml` to 14–21 (e.g. 3 proxies × 7 workers per proxy). The default is 21 when using a proxy pool.

Worker env (Helm or raw manifests):

- `PROXY_URL`: single proxy URL (all workers use it).
- `PROXY_POOL`: comma-separated URLs; worker selects one by HOSTNAME.

Example (Helm values):

```yaml
env:
  PROXY_POOL: "http://proxy-0:8080,http://proxy-1:8080,http://proxy-2:8080"
```

Then apply the ScaledObject with `maxReplicaCount: 21` (or 14 for 2 proxies).

## Graph writer (edges lag)

Apply so graph-writer scales on Kafka **edges topic** consumer-group lag (recommended when running 20+ workers; see `results/WORKER_V4_MULTIPLE_EGRESS_IP`):

```sh
kubectl apply -f deploy/kubernetes/keda/scaledobject-graph-writer.yaml
```

- **Trigger:** `relentless.graph.edges` topic, consumer group `relentless-graph-edges`. Total lag ≥ `lagThreshold` (default 150) scales up; lag below scales down.
- **Replicas:** `minReplicaCount: 1`, `maxReplicaCount: 14` (matches 14 edges partitions). Tune `lagThreshold` if you want to scale earlier (e.g. 100) or later (e.g. 300).