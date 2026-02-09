# HTTP proxy pool (Squid)

Simple forward proxy pool so workers can use multiple egress IPs (e.g. to bypass Open Library per-IP rate limits). Three Squid pods with stable DNS: `proxy-0.proxy`, `proxy-1.proxy`, `proxy-2.proxy`.

**Deploy (default namespace):**

```sh
kubectl apply -f deploy/kubernetes/proxy/squid-config.yaml
kubectl apply -f deploy/kubernetes/proxy/proxy-service.yaml
```

Wait until pods are ready:

```sh
kubectl get pods -l app=proxy -w
```
**Wire the worker to the pool**

Set `PROXY_POOL` on the worker to the proxy URLs (port 3128). In the same namespace (default) you can use short DNS:

- `http://proxy-0.proxy:3128,http://proxy-1.proxy:3128,http://proxy-2.proxy:3128`

If the worker runs in another namespace, use full DNS:

- `http://proxy-0.proxy.default.svc.cluster.local:3128,http://proxy-1.proxy.default.svc.cluster.local:3128,http://proxy-2.proxy.default.svc.cluster.local:3128`

Example (worker deployment env):

```yaml
env:
  - name: PROXY_POOL
    value: "http://proxy-0.proxy:3128,http://proxy-1.proxy:3128,http://proxy-2.proxy:3128"
```

Then restart or scale the worker so it picks up the env. With Helm, set the same in `values.yaml` under the worker env section.

**Check proxies**

```sh
kubectl get svc proxy
kubectl get pods -l app=proxy
```

Each pod (proxy-0, proxy-1, proxy-2) is a separate forward proxy; workers are assigned one per pod via `PROXY_POOL` and hostname hashing.
