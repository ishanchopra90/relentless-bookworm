#!/usr/bin/env sh
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-relentless}"

echo "==> Teardown Helm resources"
kubectl delete -f deploy/kubernetes/keda/scaledobject-worker.yaml || true
kubectl delete -f deploy/kubernetes/keda/scaledobject-graph-writer.yaml || true
helm uninstall relentless-api || true
helm uninstall relentless-worker || true
helm uninstall keda -n keda || true
helm uninstall chaos-mesh -n chaos-mesh || true

echo "==> Teardown graph-writer"
kubectl delete -f deploy/kubernetes/graph-writer/graph-writer-deployment.yaml || true
kubectl delete -f deploy/kubernetes/graph-writer/graph-writer-metrics-service.yaml || true

echo "==> Teardown proxy pool"
kubectl delete -f deploy/kubernetes/proxy/proxy-service.yaml || true
kubectl delete -f deploy/kubernetes/proxy/squid-config.yaml || true

echo "==> Teardown core services"
kubectl delete -f deploy/kubernetes/neo4j/neo4j-service.yaml || true
kubectl delete -f deploy/kubernetes/neo4j/neo4j-deployment.yaml || true
kubectl delete -f deploy/kubernetes/redis/redis-service.yaml || true
kubectl delete -f deploy/kubernetes/redis/redis-deployment.yaml || true

kubectl delete -f deploy/kubernetes/grafana/grafana-service.yaml || true
kubectl delete -f deploy/kubernetes/grafana/grafana-deployment.yaml || true
kubectl delete -f deploy/kubernetes/kafka/kafka-topics.yaml || true
kubectl delete -f deploy/kubernetes/kafka/kafka-cluster.yaml || true
kubectl delete -f "https://strimzi.io/install/latest?namespace=kafka" || true
kubectl delete -f deploy/kubernetes/kafka/kafka-namespace.yaml || true

kubectl delete -f "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/bundle.yaml" || true

kubectl delete -f deploy/kubernetes/prometheus/servicemonitor-worker.yaml || true
kubectl delete -f deploy/kubernetes/prometheus/servicemonitor-api.yaml || true
kubectl delete -f deploy/kubernetes/prometheus/servicemonitor-graph-writer.yaml || true
kubectl delete -f deploy/kubernetes/prometheus/servicemonitor-kafka-lag-exporter.yaml || true
kubectl delete -f deploy/kubernetes/prometheus/servicemonitor-kube-state-metrics.yaml || true
kubectl delete -f deploy/kubernetes/prometheus/prometheus-service.yaml || true
kubectl delete -f deploy/kubernetes/prometheus/prometheus.yaml || true
kubectl delete -f deploy/kubernetes/prometheus/prometheus-rbac.yaml || true

kubectl delete -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-service.yaml || true
kubectl delete -f deploy/kubernetes/kafka-lag-exporter/kafka-lag-exporter-deployment.yaml || true

kubectl delete -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-service.yaml || true
kubectl delete -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-deployment.yaml || true
kubectl delete -f deploy/kubernetes/kube-state-metrics/kube-state-metrics-rbac.yaml || true

echo "==> Delete kind cluster"
kind delete cluster --name "$CLUSTER_NAME" || true

echo "==> Docker cleanup"
docker system prune -af --volumes || true
