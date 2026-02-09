# Worker Service (Local Image)

Build and load the worker image into kind:

```sh
docker build -f Dockerfile.worker -t relentless-worker:dev .
kind load docker-image relentless-worker:dev --name relentless
```

If using the **proxy pool** (PROXY_POOL for multi-egress), deploy the proxy **first** so DNS resolves:

```sh
kubectl apply -f deploy/kubernetes/proxy/squid-config.yaml
kubectl apply -f deploy/kubernetes/proxy/proxy-service.yaml
kubectl get pods -l app=proxy -w   # wait until proxy-0, proxy-1, proxy-2 are Running
```

Deploy the worker:

```sh
kubectl apply -f deploy/kubernetes/worker/worker-deployment.yaml
kubectl apply -f deploy/kubernetes/worker/worker-metrics-service.yaml
```

Verify commit offset:

```sh
 kubectl exec -n kafka -it relentless-kafka-relentless-kafka-pool-0 -- \
  /opt/kafka/bin/kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --describe --group relentless-worker
```

Check worker logs:

```sh
kubectl logs deploy/relentless-worker
```

Worker scaling:

```sh
kubectl scale deploy/relentless-worker --replicas=5
```