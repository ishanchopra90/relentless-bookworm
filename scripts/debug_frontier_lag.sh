#!/usr/bin/env sh
# Debug stuck partitions on Kafka frontier lag for relentless-worker consumer group.
# Lists partitions with LAG > 0 and prints commands to read the first uncommitted message.
# Usage: ./scripts/debug_frontier_lag.sh
#   Run from repo root. Requires kubectl and a running Kafka broker pod (e.g. after helm_up.sh).
#
# Defaults match deploy/:
#   deploy/kubernetes/kafka/kafka-namespace.yaml     -> KAFKA_NS
#   deploy/kubernetes/kafka/kafka-cluster.yaml       -> KAFKA_POD (Strimzi: <cluster>-<nodepool>-0)
#   deploy/kubernetes/kafka/kafka-topics.yaml        -> KAFKA_TOPIC (relentless.crawl.frontier)
#   deploy/kubernetes/keda/scaledobject-worker.yaml  -> KAFKA_GROUP (relentless-worker)
#   deploy/kubernetes/kafka/README.md                -> BOOTSTRAP (localhost:9092 inside broker pod)
#   deploy/kubernetes/worker/worker-deployment.yaml  -> WORKER_NS, WORKER_DEPLOYMENT
set -euo pipefail

# Kafka broker (exec target) and consumer group/topic
KAFKA_NS="${KAFKA_NS:-kafka}"
KAFKA_POD="${KAFKA_POD:-relentless-kafka-relentless-kafka-pool-0}"
KAFKA_GROUP="${KAFKA_GROUP:-relentless-worker}"
KAFKA_TOPIC="${KAFKA_TOPIC:-relentless.crawl.frontier}"
BOOTSTRAP="${BOOTSTRAP:-localhost:9092}"

# Worker deployment (for "next steps" restart / logs)
WORKER_NS="${WORKER_NS:-default}"
WORKER_DEPLOYMENT="${WORKER_DEPLOYMENT:-relentless-worker}"

describe() {
  kubectl exec -n "$KAFKA_NS" "$KAFKA_POD" -- \
    /opt/kafka/bin/kafka-consumer-groups.sh \
    --bootstrap-server "$BOOTSTRAP" \
    --group "$KAFKA_GROUP" \
    --describe 2>/dev/null
}

echo "==> Consumer group: $KAFKA_GROUP, topic: $KAFKA_TOPIC"
echo "==> Describing group (pod: $KAFKA_POD, ns: $KAFKA_NS)..."
echo ""

# Header line; then only lines for our topic
OUTPUT=$(describe)
echo "$OUTPUT" | head -1
echo "$OUTPUT" | awk -v topic="$KAFKA_TOPIC" '$2 == topic'

# Partitions with LAG > 0
STUCK=$(echo "$OUTPUT" | awk -v topic="$KAFKA_TOPIC" '$2 == topic && $6 > 0 { print $3, $4, $5, $6, $7 }')
if [ -z "$STUCK" ]; then
  echo ""
  echo "No stuck partitions (all LAG = 0)."
  exit 0
fi

echo ""
echo "==> Stuck partition(s) (LAG > 0):"
echo "$OUTPUT" | awk -v topic="$KAFKA_TOPIC" '$2 == topic && $6 > 0 { printf "  PARTITION=%s CURRENT-OFFSET=%s LOG-END-OFFSET=%s LAG=%s CONSUMER=%s\n", $3, $4, $5, $6, $7 }'
echo ""

# CURRENT-OFFSET = last committed offset. First uncommitted (blocking) message is at CURRENT-OFFSET + 1.
# Columns: GROUP TOPIC PARTITION CURRENT-OFFSET LOG-END-OFFSET LAG CONSUMER-ID HOST CLIENT-ID
while read -r part current log_end lag consumer; do
  [ -z "$part" ] && continue
  next=$((current + 1))
  echo "==> To read the stuck message at partition $part offset $next (first uncommitted):"
  echo "kubectl exec -n $KAFKA_NS -it $KAFKA_POD -- \\"
  echo "  /opt/kafka/bin/kafka-console-consumer.sh \\"
  echo "  --bootstrap-server $BOOTSTRAP \\"
  echo "  --topic $KAFKA_TOPIC \\"
  echo "  --partition $part --offset $next --max-messages 1"
  echo ""
done <<EOF
$(echo "$OUTPUT" | awk -v topic="$KAFKA_TOPIC" '$2 == topic && $6 > 0 { print $3, $4, $5, $6, $7 }')
EOF

echo "==> Next steps:"
echo "  1. Check worker logs for the consumer pod shown in CONSUMER (e.g. relentless-worker-...-flpb4):"
echo "     kubectl logs -n $WORKER_NS <pod-name>"
echo "  2. Restart workers to clear stuck goroutines:"
echo "     kubectl rollout restart deployment $WORKER_DEPLOYMENT -n $WORKER_NS"
echo "  3. See deploy/kubernetes/kafka/README.md for more Kafka CLI commands."
