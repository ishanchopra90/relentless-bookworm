# API Service (Local Image)

This assumes the kind cluster was created with `deploy/kubernetes/kind-config.yaml`
so NodePort `30080` is mapped to `localhost:30080`.

Build and load the API image into kind:

```sh
docker build -f Dockerfile.api -t relentless-api:dev .
kind load docker-image relentless-api:dev --name relentless
```

Deploy the API:

```sh
kubectl apply -f deploy/kubernetes/api/api-deployment.yaml
kubectl apply -f deploy/kubernetes/api/api-service.yaml
```

The Service provides a stable endpoint so the API can be exposed to users.

NodePort for local access (kind maps it to localhost):

```sh
curl -X POST "http://localhost:30080/crawl?url=https://openlibrary.org/books/OL51746364M/Ulysses"
```
