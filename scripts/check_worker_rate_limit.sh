#!/usr/bin/env sh
# Check worker logs for Open Library rate limiting (HTTP 429).
# Fetches logs from relentless-worker pods, parses for 429, and reports.
# Usage: ./scripts/check_worker_rate_limit.sh
#   Run from repo root. Requires kubectl and worker deployment (e.g. after helm_up.sh).
#
# Env:
#   WORKER_NS     namespace (default: default)
#   WORKER_LABEL  label selector for worker pods (default: app=relentless-worker)
#   SINCE         log time window, e.g. 1h, 24h (default: 1h)
#
# Exit: 0 if no 429s; 1 if any 429s (rate limiting detected).
set -euo pipefail

WORKER_NS="${WORKER_NS:-default}"
WORKER_LABEL="${WORKER_LABEL:-app=relentless-worker}"
SINCE="${SINCE:-1h}"

echo "==> Checking worker logs for rate limiting (429)"
echo "    namespace=$WORKER_NS label=$WORKER_LABEL since=$SINCE"
echo ""

LOGS=$(kubectl logs -n "$WORKER_NS" -l "$WORKER_LABEL" --all-containers=true --since="$SINCE" 2>/dev/null || true)
if [ -z "$LOGS" ]; then
  echo "No worker logs found (no pods or empty)."
  exit 0
fi

# Match lines containing 429 (e.g. "job handler error: ... unexpected status 429 for ...")
RATE_LIMIT_LINES=$(echo "$LOGS" | grep -E '429|unexpected status 429' || true)
COUNT=$(echo "$RATE_LIMIT_LINES" | grep -c . 2>/dev/null) || COUNT=0
COUNT=$((COUNT + 0))

if [ "$COUNT" -eq 0 ]; then
  echo "No 429 (rate limit) responses found in worker logs."
  exit 0
fi

echo "Rate limiting detected: $COUNT line(s) with 429"
echo ""
echo "Sample (first 20):"
echo "$RATE_LIMIT_LINES" | head -20
echo ""
if [ "$COUNT" -gt 20 ]; then
  echo "... and $((COUNT - 20)) more."
fi
echo ""
echo "Consider: enable PROXY_POOL (multi-egress) to spread requests across IPs. See results/WORKER_V4_MULTIPLE_EGRESS_IP."
exit 1
