# Relentless API Helm Chart

Install the API chart from the repo root:

```sh
helm install relentless-api deploy/helm/api
```

Upgrade after changing values:

```sh
helm upgrade relentless-api deploy/helm/api
```

Override values:

```sh
helm upgrade --install relentless-api deploy/helm/api \
  --set image.tag=dev \
  --set service.nodePort=30080
```

Uninstall:

```sh
helm uninstall relentless-api
```
