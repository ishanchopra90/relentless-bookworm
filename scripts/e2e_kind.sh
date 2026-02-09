#!/usr/bin/env sh
set -euo pipefail

CLUSTER_NAME="relentless"
KIND_CONFIG="deploy/kubernetes/kind-config.yaml"
SEED_URL="https://openlibrary.org/books/OL59574246M"
SEED_JSON_URL="https://openlibrary.org/books/OL59574246M.json"
SEED_KEY="/books/OL59574246M"
API_URL="http://localhost:30080/crawl?url=$SEED_URL"

cleanup() {
  sh scripts/kind_down.sh
}

trap cleanup EXIT

sh scripts/kind_up.sh

echo "==> Wait for API readiness"
READY=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if [ "$(kubectl get pod -l app=relentless-api -o jsonpath='{.items[0].status.containerStatuses[0].ready}' 2>/dev/null || true)" = "true" ]; then
    if curl -sf "http://localhost:30080/metrics" >/dev/null 2>&1; then
      READY=1
      break
    fi
  fi
  sleep 3
done

if [ "$READY" -ne 1 ]; then
  echo "API is not ready or NodePort not reachable"
  kubectl get pods -l app=relentless-api
  exit 1
fi

echo "==> Wait for worker readiness"
WORKER_READY=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if [ "$(kubectl get pod -l app=relentless-worker -o jsonpath='{.items[0].status.containerStatuses[0].ready}' 2>/dev/null || true)" = "true" ]; then
    WORKER_READY=1
    break
  fi
  sleep 3
done

if [ "$WORKER_READY" -ne 1 ]; then
  echo "Worker is not ready"
  kubectl get pods -l app=relentless-worker
  exit 1
fi

echo "==> Submit crawl request"
RESPONSE="$(curl -s -X POST "$API_URL" || true)"
SESSION_ID="$(python3 - <<PY
import json, sys
payload = json.loads('''$RESPONSE''')
print(payload["session_id"])
PY
)"

if [ -z "$SESSION_ID" ]; then
  echo "Failed to get session_id from API response"
  echo "Response was: $RESPONSE"
  exit 1
fi
echo "Session ID: $SESSION_ID"

echo "==> Verify Redis status key"
REDIS_POD="$(kubectl get pods -l app=relentless-redis -o jsonpath='{.items[0].metadata.name}')"
REDIS_VALUE="$(kubectl exec "$REDIS_POD" -- redis-cli get "crawl:status:$SESSION_ID")"
python3 - <<PY
import json
value = json.loads('''$REDIS_VALUE''')
assert value["session_id"] == "$SESSION_ID"
assert value["seed_url"] == "$SEED_JSON_URL"
assert value["status"] == "queued"
assert value.get("created_at")
assert value["created_at"].endswith("Z") or value["created_at"].endswith("z")
print("Redis status OK")
PY

echo "==> Verify results topic"
BROKER_POD=""
for _ in 1 2 3 4 5 6 7 8 9 10; do
  BROKER_POD="$(kubectl get pods -n kafka -l strimzi.io/name=relentless-kafka-kafka -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [ -n "$BROKER_POD" ]; then
    break
  fi
  sleep 3
done

if [ -z "$BROKER_POD" ]; then
  echo "Failed to locate Kafka broker pod"
  kubectl get pods -n kafka
  exit 1
fi
RESULT=""
for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
  RESULT="$(kubectl exec -n kafka "$BROKER_POD" -c kafka -- /opt/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 \
    --topic relentless.crawl.results \
    --from-beginning \
    --max-messages 1 \
    --timeout-ms 30000 2>/dev/null || true)"
  if [ -n "$RESULT" ]; then
    break
  fi
  sleep 5
done

if [ -z "$RESULT" ]; then
  echo "No messages found in relentless.crawl.results"
  kubectl logs deploy/relentless-worker || true
  exit 1
fi
python3 - <<PY
import json
payload = json.loads('''$RESULT''')
try:
    assert payload["session_id"] == "$SESSION_ID"
    assert payload["url"] == "$SEED_JSON_URL"
    assert payload["node_type"] == "edition"
    node = payload["node"]
    assert isinstance(node, dict)
    assert node.get("key") == "$SEED_KEY"
    assert node.get("title"), "missing node.title"
    assert isinstance(node.get("works"), list)
    assert node["works"], "expected at least one work"
    assert all(w.startswith("/works/") for w in node["works"])
    print("Results topic OK")
except AssertionError as exc:
    print("Results payload assertion failed:", exc)
    print("Payload:", json.dumps(payload, indent=2))
    raise
PY

echo "==> Verify frontier topic has data"
FRONTIER=""
for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
  FRONTIER="$(kubectl exec -n kafka "$BROKER_POD" -c kafka -- /opt/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 \
    --topic relentless.crawl.frontier \
    --from-beginning \
    --max-messages 1 \
    --timeout-ms 30000 2>/dev/null || true)"
  if [ -n "$FRONTIER" ]; then
    break
  fi
  sleep 5
done

if [ -z "$FRONTIER" ]; then
  echo "No messages found in relentless.crawl.frontier"
  kubectl logs deploy/relentless-worker || true
  exit 1
fi
python3 - <<PY
import json
payload = json.loads('''$FRONTIER''')
assert payload["session_id"] == "$SESSION_ID"
assert payload["seed_url"] == "$SEED_JSON_URL"
assert payload["url"] == "$SEED_JSON_URL"
assert payload["depth"] == 0
assert payload.get("created_at")
assert payload["created_at"].endswith("Z") or payload["created_at"].endswith("z")
print("Frontier topic OK")
PY

echo "==> Done"
