#!/usr/bin/env bash
set -euo pipefail

: "${GO_DOT_FAULT_WRITE_KEY:?set GO_DOT_FAULT_WRITE_KEY to a published test project key}"
API_URL="${GO_DOT_FAULT_API_URL:-http://127.0.0.1:8080}"
COMPOSE_PROJECT="${GO_DOT_FAULT_COMPOSE_PROJECT:-}"
COMPOSE_OVERRIDE="${GO_DOT_FAULT_COMPOSE_OVERRIDE:-}"
compose=(docker compose)
[[ -n "$COMPOSE_PROJECT" ]] && compose+=(-p "$COMPOSE_PROJECT")
compose+=(-f docker-compose.yml)
[[ -n "$COMPOSE_OVERRIDE" ]] && compose+=(-f "$COMPOSE_OVERRIDE")
EVENT_ID="018f22a0-2c5a-7bb0-8c1a-$(printf '4f15b2ae%04x' "$RANDOM")"
PAYLOAD=$(printf '{"events":[{"event_id":"%s","point_code":"product_detail_view","event_time":%s,"anonymous_id":"dlq-visitor","platform":"web","properties":{}}]}' "$EVENT_ID" "$(($(date +%s)*1000))")

cleanup() { "${compose[@]}" start clickhouse worker scheduler >/dev/null 2>&1 || true; }
trap cleanup EXIT
# Worker validates its configured ClickHouse at startup. Stopping ClickHouse
# after the healthy Worker starts makes that target unreachable while retaining
# the production startup contract and exercises the same retry/DLQ path.
"${compose[@]}" stop clickhouse
curl --fail-with-body -sS -H 'Content-Type: application/json' -H "X-Write-Key: ${GO_DOT_FAULT_WRITE_KEY}" --data "$PAYLOAD" "${API_URL}/api/v1/collect/events" >/tmp/go-dot-dlq-collect.json

deadline=$((SECONDS+120)); entry=''
while (( SECONDS < deadline )); do
  entry=$("${compose[@]}" exec -T redis redis-cli --raw XRANGE 'tracking:{queue}:dead' - + | grep -F -B8 -A8 "$EVENT_ID" || true)
  [[ -n "$entry" ]] && break
  sleep 2
done
[[ -n "$entry" ]] || { echo "message did not reach DLQ after five deliveries" >&2; exit 1; }
grep -Fq 'payload' <<<"$entry" || { echo "DLQ did not preserve original payload" >&2; exit 1; }
grep -Fq 'store valid events' <<<"$entry" || { echo "DLQ did not preserve failure stage" >&2; exit 1; }
echo "DLQ verified for event=${EVENT_ID}"
