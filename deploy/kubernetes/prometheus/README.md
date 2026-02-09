## Scrape config

- **ServiceMonitors** (api, worker, graph-writer, kafka-lag-exporter, kube-state-metrics): app metrics.
- **Additional scrape config** (`prometheus-additional-scrape-configs.yaml` Secret): scrapes kubelet `/metrics/cadvisor` for container memory/CPU (used by the Grafana "Container memory (working set)" panel). Requires RBAC `nodes/proxy` for the Prometheus service account.

## Prometheus UI

Forward the Prometheus service to localhost:

```sh
kubectl port-forward svc/relentless-prometheus 9090:9090
```

Open `http://localhost:9090/targets` to confirm active scrape targets.