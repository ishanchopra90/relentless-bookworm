#!/usr/bin/env sh
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-relentless}"
KIND_CONFIG="${KIND_CONFIG:-deploy/kubernetes/kind-config.yaml}"

echo "==> Ensure kind cluster"
if ! kind get clusters | rg -x "$CLUSTER_NAME" >/dev/null 2>&1; then
  kind create cluster --name "$CLUSTER_NAME" --config "$KIND_CONFIG"
fi

echo "==> Install Strimzi and Kafka"
kubectl apply -f deploy/kubernetes/kafka/kafka-namespace.yaml
kubectl apply -f "https://strimzi.io/install/latest?namespace=kafka"
kubectl wait --for=condition=Available deploy/strimzi-cluster-operator -n kafka --timeout=300s
kubectl apply -f deploy/kubernetes/kafka/kafka-cluster.yaml
kubectl apply -f deploy/kubernetes/kafka/kafka-topics.yaml
kubectl wait --for=condition=Ready kafka/relentless-kafka -n kafka --timeout=300s

echo "==> Install Prometheus Operator"
kubectl apply --server-side -f "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/bundle.yaml"
kubectl wait --for=condition=Available deploy/prometheus-operator --timeout=300s

echo "==> Deploy Prometheus"
kubectl apply -f deploy/kubernetes/prometheus/prometheus-rbac.yaml
kubectl apply -f deploy/kubernetes/prometheus/prometheus.yaml
kubectl apply -f deploy/kubernetes/prometheus/prometheus-service.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-api.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-worker.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-graph-writer.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-kafka-lag-exporter.yaml
kubectl apply -f deploy/kubernetes/prometheus/servicemonitor-kube-state-metrics.yaml

echo "==> Deploy Kafka lag exporter"
kubectl apply -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-deployment.yaml
kubectl apply -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-service.yaml
kubectl wait --for=condition=Available deploy/kafka-lag-exporter --timeout=300s

echo "==> Deploy kube-state-metrics"
kubectl apply -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-rbac.yaml
kubectl apply -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-deployment.yaml
kubectl apply -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-service.yaml
kubectl wait --for=condition=Available deploy/kube-state-metrics --timeout=300s

echo "==> Deploy Redis"
kubectl apply -f deploy/kubernetes/redis/redis-deployment.yaml
kubectl apply -f deploy/kubernetes/redis/redis-service.yaml
kubectl wait --for=condition=Available deploy/relentless-redis --timeout=300s

echo "==> Deploy Neo4j"
kubectl apply -f deploy/kubernetes/neo4j/neo4j-deployment.yaml
kubectl apply -f deploy/kubernetes/neo4j/neo4j-service.yaml
kubectl wait --for=condition=Available deploy/relentless-neo4j --timeout=300s

echo "==> Deploy Grafana"
kubectl apply -f deploy/kubernetes/grafana/grafana-deployment.yaml
kubectl apply -f deploy/kubernetes/grafana/grafana-service.yaml
kubectl wait --for=condition=Available deploy/relentless-grafana --timeout=300s

echo "==> Deploy proxy pool (Squid)"
kubectl apply -f deploy/kubernetes/proxy/squid-config.yaml
kubectl apply -f deploy/kubernetes/proxy/proxy-service.yaml
kubectl wait --for=condition=Available statefulset/proxy --timeout=300s

echo "==> Build and load API/worker/graph-writer images"
docker build -f Dockerfile.api -t relentless-api:dev .
docker build -f Dockerfile.worker -t relentless-worker:dev .
docker build -f Dockerfile.graph-writer -t relentless-graph-writer:dev .
kind load docker-image relentless-api:dev --name "$CLUSTER_NAME"
kind load docker-image relentless-worker:dev --name "$CLUSTER_NAME"
kind load docker-image relentless-graph-writer:dev --name "$CLUSTER_NAME"

echo "==> Deploy API, worker, and graph-writer"
kubectl apply -f deploy/kubernetes/api/api-deployment.yaml
kubectl apply -f deploy/kubernetes/api/api-service.yaml
kubectl apply -f deploy/kubernetes/worker/worker-deployment.yaml
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-deployment.yaml
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-metrics-service.yaml
kubectl wait --for=condition=Available deploy/relentless-api --timeout=300s
kubectl wait --for=condition=Available deploy/relentless-worker --timeout=300s
kubectl wait --for=condition=Available deploy/relentless-graph-writer --timeout=300s
