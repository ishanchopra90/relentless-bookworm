#!/usr/bin/env sh
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-relentless}"
KIND_CONFIG="${KIND_CONFIG:-deploy/kubernetes/kind-config.yaml}"
# Set ENABLE_PROXY_POOL=0 to skip the proxy pool (workers use direct egress). Default: 1 (deploy proxy pool for multi-egress).
ENABLE_PROXY_POOL="${ENABLE_PROXY_POOL:-1}"

echo "==> Ensure kind cluster"
if ! kind get clusters | rg -x "$CLUSTER_NAME" >/dev/null 2>&1; then
  kind create cluster --name "$CLUSTER_NAME" --config "$KIND_CONFIG"
fi

echo "==> Install Strimzi and Kafka"
# strimzi.io/install only serves 'latest'; tested with Strimzi 0.50.x (Kafka CR v1, KafkaNodePool).
kubectl apply -f deploy/kubernetes/kafka/kafka-namespace.yaml
kubectl apply -f "https://strimzi.io/install/latest?namespace=kafka"
kubectl wait --for=condition=Available deploy/strimzi-cluster-operator -n kafka --timeout=300s
kubectl apply -f deploy/kubernetes/kafka/kafka-cluster.yaml
kubectl apply -f deploy/kubernetes/kafka/kafka-topics.yaml
kubectl wait --for=condition=Ready kafka/relentless-kafka -n kafka --timeout=300s

echo "==> Install Prometheus Operator"
# Use a known-good tag (v0.89.0 not found on quay). Pre-load into kind to avoid ErrImagePull.
PROM_OP_TAG="${PROM_OP_TAG:-v0.88.1}"
PROM_OP_IMAGE="quay.io/prometheus-operator/prometheus-operator:${PROM_OP_TAG}"
PROM_CFG_RELOADER="quay.io/prometheus-operator/prometheus-config-reloader:${PROM_OP_TAG}"
BUNDLE_URL="https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/bundle.yaml"
BUNDLE_FILE=$(mktemp); trap 'rm -f "$BUNDLE_FILE" "${BUNDLE_FILE}.new"' EXIT
curl -sS --max-time 30 -o "$BUNDLE_FILE" "$BUNDLE_URL"
# Overwrite bundle image refs so deployment uses the tag we pre-load (bundle may reference missing tags)
sed -E "s|quay.io/prometheus-operator/prometheus-operator:[^\"[:space:]]+|$PROM_OP_IMAGE|g; s|quay.io/prometheus-operator/prometheus-config-reloader:[^\"[:space:]]+|$PROM_CFG_RELOADER|g" "$BUNDLE_FILE" > "${BUNDLE_FILE}.new" && mv "${BUNDLE_FILE}.new" "$BUNDLE_FILE"
# Pull single-platform image so kind load works (multi-arch manifest can cause "content digest not found")
case "$(uname -m)" in aarch64|arm64) KIND_PLATFORM="${KIND_PLATFORM:-linux/arm64}";; *) KIND_PLATFORM="${KIND_PLATFORM:-linux/amd64}";; esac
for img in "$PROM_OP_IMAGE" "$PROM_CFG_RELOADER"; do
  echo "  Loading $img into kind..."
  docker pull --platform "$KIND_PLATFORM" "$img"
  kind load docker-image "$img" --name "$CLUSTER_NAME" || true
done
kubectl apply --server-side -f "$BUNDLE_FILE"
# Use pre-loaded image: avoid pull from registry (rate limits / network)
kubectl patch deploy prometheus-operator -n default -p '{"spec":{"template":{"spec":{"containers":[{"name":"prometheus-operator","imagePullPolicy":"IfNotPresent"}]}}}}' --type=merge 2>/dev/null || true
if ! kubectl wait --for=condition=Available deploy/prometheus-operator -n default --timeout=300s; then
  echo "=== Prometheus Operator deployment state ==="
  kubectl describe deploy prometheus-operator -n default
  kubectl get pods -n default -l app.kubernetes.io/name=prometheus-operator
  exit 1
fi

echo "==> Deploy Prometheus"
kubectl apply -f deploy/kubernetes/prometheus/prometheus-rbac.yaml
kubectl apply -f deploy/kubernetes/prometheus/prometheus-additional-scrape-configs.yaml
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

if [ "$ENABLE_PROXY_POOL" = "1" ] || [ "$ENABLE_PROXY_POOL" = "true" ] || [ "$ENABLE_PROXY_POOL" = "yes" ]; then
  echo "==> Deploy proxy pool (Squid)"
  kubectl apply -f deploy/kubernetes/proxy/squid-config.yaml
  kubectl apply -f deploy/kubernetes/proxy/proxy-service.yaml
  kubectl rollout status statefulset/proxy --timeout=300s
else
  echo "==> Skip proxy pool (ENABLE_PROXY_POOL not set; workers use direct egress)"
fi

echo "==> Install KEDA"
helm repo add kedacore https://kedacore.github.io/charts
helm repo update
helm upgrade --install keda kedacore/keda --namespace keda --create-namespace
kubectl wait --for=condition=Available deploy/keda-operator -n keda --timeout=300s

echo "==> Build and load images"
docker build -f Dockerfile.api -t relentless-api:dev .
docker build -f Dockerfile.worker -t relentless-worker:dev .
docker build -f Dockerfile.graph-writer -t relentless-graph-writer:dev .
kind load docker-image relentless-api:dev --name "$CLUSTER_NAME"
kind load docker-image relentless-worker:dev --name "$CLUSTER_NAME"
kind load docker-image relentless-graph-writer:dev --name "$CLUSTER_NAME"

echo "==> Install Helm charts"
helm upgrade --install relentless-api deploy/helm/api
helm upgrade --install relentless-worker deploy/helm/worker

echo "==> Deploy graph-writer"
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-deployment.yaml
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-metrics-service.yaml

kubectl wait --for=condition=Available deploy/relentless-api --timeout=300s
kubectl wait --for=condition=Available deploy/relentless-worker --timeout=300s
kubectl wait --for=condition=Available deploy/relentless-graph-writer --timeout=300s

echo "==> Apply KEDA ScaledObjects (worker: frontier lag; graph-writer: edges lag)"
kubectl apply -f deploy/kubernetes/keda/scaledobject-worker.yaml
kubectl apply -f deploy/kubernetes/keda/scaledobject-graph-writer.yaml

echo "==> Install Chaos Mesh"
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update
helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh --create-namespace \
  --set chaosDaemon.runtime=containerd \
  --set chaosDaemon.socketPath=/run/containerd/containerd.sock
echo "==> Wait for Chaos Mesh controller and daemon"
kubectl wait --for=condition=Available deploy/chaos-controller-manager -n chaos-mesh --timeout=120s
kubectl rollout status daemonset/chaos-daemon -n chaos-mesh --timeout=120s
