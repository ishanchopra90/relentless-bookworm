# Deployment Order (kind)

Run these steps in order for a clean local deployment:

## 1) Create the kind cluster

```sh
kind create cluster --name relentless --config deploy/kubernetes/kind-config.yaml
```

## 2) Install Strimzi and Kafka

```sh
kubectl apply -f deploy/kubernetes/kafka/kafka-namespace.yaml
kubectl apply -f "https://strimzi.io/install/latest?namespace=kafka"
kubectl apply -f deploy/kubernetes/kafka/kafka-cluster.yaml
kubectl apply -f deploy/kubernetes/kafka/kafka-topics.yaml
```

## 3) Deploy Redis (status store)

```sh
kubectl apply -f deploy/kubernetes/redis/redis-deployment.yaml
kubectl apply -f deploy/kubernetes/redis/redis-service.yaml
```

## 4) Deploy Neo4j (graph store)

```sh
kubectl apply -f deploy/kubernetes/neo4j/neo4j-deployment.yaml
kubectl apply -f deploy/kubernetes/neo4j/neo4j-service.yaml
```

Apply Neo4j uniqueness constraints (required to prevent duplicates):

```cypher
// run in Neo4j Browser
CREATE CONSTRAINT book_key_unique IF NOT EXISTS
FOR (b:Book)
REQUIRE b.key IS UNIQUE;

CREATE CONSTRAINT work_key_unique IF NOT EXISTS
FOR (w:Work)
REQUIRE w.key IS UNIQUE;

CREATE CONSTRAINT author_key_unique IF NOT EXISTS
FOR (a:Author)
REQUIRE a.key IS UNIQUE;
```

## Observability (Prometheus Operator)

```sh
kubectl apply --server-side -f "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/bundle.yaml"
kubectl wait --for=condition=Available deploy/prometheus-operator --timeout=300s
```

Deploy Prometheus and ServiceMonitors:

```sh
kubectl apply -f deploy/kubernetes/prometheus/prometheus-rbac.yaml
kubectl apply -f deploy/kubernetes/prometheus/prometheus.yaml
kubectl apply -f deploy/kubernetes/prometheus/prometheus-service.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-api.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-worker.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-graph-writer.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-kafka-lag-exporter.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-kube-state-metrics.yaml
```

Install Kafka lag exporter (frontier lag metrics):

```sh
kubectl apply -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-deployment.yaml
kubectl apply -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-service.yaml
```

Install kube-state-metrics (replica counts and deployment state):

```sh
kubectl apply -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-rbac.yaml
kubectl apply -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-deployment.yaml
kubectl apply -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-service.yaml
```

## Grafana

```sh
kubectl apply -f deploy/kubernetes/grafana/grafana-deployment.yaml
kubectl apply -f deploy/kubernetes/grafana/grafana-service.yaml
kubectl wait --for=condition=Available deploy/relentless-grafana --timeout=300s
```

## KEDA (autoscaling)

```sh
helm repo add kedacore https://kedacore.github.io/charts
helm repo update
helm upgrade --install keda kedacore/keda --namespace keda --create-namespace
kubectl wait --for=condition=Available deploy/keda-operator -n keda --timeout=300s
```

## Chaos Mesh (chaos testing)

```sh
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update
helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh --create-namespace \
  --set chaosDaemon.runtime=containerd \
  --set chaosDaemon.socketPath=/run/containerd/containerd.sock
kubectl wait --for=condition=Available deploy/chaos-controller-manager -n chaos-mesh --timeout=120s
```

See [deploy/kubernetes/chaos-mesh/README.md](chaos-mesh/README.md) for details. To simulate slow network: `kubectl apply -f deploy/kubernetes/chaos-mesh/chaos/networkchaos-worker-delay.yaml`

## 5) Build and load the API image

```sh
docker build -f Dockerfile.api -t relentless-api:dev .
kind load docker-image relentless-api:dev --name relentless
```

## 6) Deploy the API

```sh
kubectl apply -f deploy/kubernetes/api/api-deployment.yaml
kubectl apply -f deploy/kubernetes/api/api-service.yaml
```

## 7) Build and deploy the worker

```sh
docker build -f Dockerfile.worker -t relentless-worker:dev .
kind load docker-image relentless-worker:dev --name relentless
kubectl apply -f deploy/kubernetes/worker/worker-deployment.yaml
kubectl apply -f deploy/kubernetes/keda/scaledobject-worker.yaml
```

## 8) Build and deploy the graph writer

```sh
docker build -f Dockerfile.graph-writer -t relentless-graph-writer:dev .
kind load docker-image relentless-graph-writer:dev --name relentless
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-deployment.yaml
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-metrics-service.yaml
```

## 9) Verify API access

```sh
curl -X POST "http://localhost:30080/crawl?url=https://openlibrary.org/books/OL59574246M/Legends_Lattes"
```

## Helm-based deployment (optional)

Use the Helm-based scripts to stand up/down the same stack:

```sh
sh scripts/helm_up.sh
sh scripts/helm_down.sh
```

## End-to-end test script

Run the integrated test (cluster + Kafka + Redis + API + worker):

```sh
sh scripts/e2e_kind.sh
```

## Teardown

Delete resources in reverse order:

```sh
kubectl delete -f deploy/kubernetes/keda/scaledobject-worker.yaml || true
helm uninstall keda -n keda || true

kubectl delete -f deploy/kubernetes/graph-writer/graph-writer-deployment.yaml
kubectl delete -f deploy/kubernetes/worker/worker-deployment.yaml

kubectl delete -f deploy/kubernetes/api/api-service.yaml
kubectl delete -f deploy/kubernetes/api/api-deployment.yaml

kubectl delete -f deploy/kubernetes/neo4j/neo4j-service.yaml
kubectl delete -f deploy/kubernetes/neo4j/neo4j-deployment.yaml

kubectl delete -f deploy/kubernetes/redis/redis-service.yaml
kubectl delete -f deploy/kubernetes/redis/redis-deployment.yaml

kubectl delete -f deploy/kubernetes/kafka/kafka-topics.yaml
kubectl delete -f deploy/kubernetes/kafka/kafka-cluster.yaml
kubectl delete -f "https://strimzi.io/install/latest?namespace=kafka"
kubectl delete -f deploy/kubernetes/kafka/kafka-namespace.yaml

kubectl delete -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-service.yaml
kubectl delete -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-deployment.yaml

kubectl delete -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-service.yaml
kubectl delete -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-deployment.yaml
kubectl delete -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-rbac.yaml
```

Delete the kind cluster:

```sh
kind delete cluster --name relentless
```
