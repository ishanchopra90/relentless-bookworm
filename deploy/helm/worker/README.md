# Relentless Worker Helm Chart

Install the worker chart from the repo root:

```sh
helm install relentless-worker deploy/helm/worker
```

Upgrade after changing values:

```sh
helm upgrade relentless-worker deploy/helm/worker
```

Override values:

```sh
helm upgrade --install relentless-worker deploy/helm/worker \
  --set image.tag=dev \
  --set env.MAX_DEPTH=3
```

Uninstall:

```sh
helm uninstall relentless-worker
```
