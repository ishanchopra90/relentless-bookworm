# Grafana (Local Deployment)

Deploy Grafana:

```sh
kubectl apply -f deploy/kubernetes/grafana/grafana-deployment.yaml
kubectl apply -f deploy/kubernetes/grafana/grafana-service.yaml
```

Wait for readiness:

```sh
kubectl wait --for=condition=Available deploy/relentless-grafana --timeout=300s
```

Port-forward for access:

```sh
kubectl port-forward svc/relentless-grafana 3000:3000
```

Default credentials:
- user: `admin`
- password: `relentless`

Import the dashboard:

1) Open Grafana at `http://localhost:3000`
2) Add a Prometheus data source named `Prometheus` with URL:
   `http://relentless-prometheus.default.svc.cluster.local:9090`
3) Import `deploy/kubernetes/grafana/dashboards/relentless-overview.json`

The dashboard works with or without proxy pool: when proxy is disabled, the proxy variable and proxy-section panels (Workers by Proxy, Fetch Latency by Proxy, etc.) show no data; when `PROXY_URL` or `PROXY_POOL` is enabled, those panels populate.

**Port-forward timeouts:** If you see "Timeout occurred" or "context canceled" in the port-forward terminal, Grafana or Prometheus is slow (e.g. many panels, heavy queries, or "database is locked" in Grafana logs). Reduce load by setting the dashboard refresh to 1m or 5m (or Off) instead of 30s. Restart the port-forward if the stream drops.
