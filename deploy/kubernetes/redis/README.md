# Redis (Status Store)

Deploy Redis for crawl status storage:

```sh
kubectl apply -f deploy/kubernetes/redis/redis-deployment.yaml
kubectl apply -f deploy/kubernetes/redis/redis-service.yaml
```

Verify:

```sh
kubectl get pods -l app=relentless-redis
kubectl get svc relentless-redis
```

Check cache contents from inside the Redis pod:

```sh
kubectl exec -it $(kubectl get pods -l app=relentless-redis -o jsonpath='{.items[0].metadata.name}') -- \
  redis-cli keys 'crawl:status:*'

kubectl exec -it $(kubectl get pods -l app=relentless-redis -o jsonpath='{.items[0].metadata.name}') -- \
  redis-cli get "crawl:status:20260120152604971942636"
```
