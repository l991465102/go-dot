#!/usr/bin/env bash
set -euo pipefail

: "${GO_DOT_FAULT_WRITE_KEY:?set GO_DOT_FAULT_WRITE_KEY to a published test project key}"
: "${GO_DOT_FAULT_PROJECT_ID:?set GO_DOT_FAULT_PROJECT_ID}"
: "${GO_DOT_FAULT_POINT_ID:?set GO_DOT_FAULT_POINT_ID}"
API_URL="${GO_DOT_FAULT_API_URL:-http://127.0.0.1:8080}"
COMPOSE_PROJECT="${GO_DOT_FAULT_COMPOSE_PROJECT:-}"
COMPOSE_OVERRIDE="${GO_DOT_FAULT_COMPOSE_OVERRIDE:-}"
compose=(docker compose)
[[ -n "$COMPOSE_PROJECT" ]] && compose+=(-p "$COMPOSE_PROJECT")
compose+=(-f docker-compose.yml)
[[ -n "$COMPOSE_OVERRIDE" ]] && compose+=(-f "$COMPOSE_OVERRIDE")
EVENT_ID="018f22a0-2c5a-7bb0-8c1a-$(printf '4f15b2ad%04x' "$RANDOM")"
PAYLOAD=$(printf '{"events":[{"event_id":"%s","point_code":"product_detail_view","event_time":%s,"anonymous_id":"fault-visitor","platform":"web","properties":{}}]}' "$EVENT_ID" "$(($(date +%s)*1000))")

sum_pv() { "${compose[@]}" exec -T redis redis-cli --scan --pattern "rt:pv:hour:{project:${GO_DOT_FAULT_PROJECT_ID}}:*:${GO_DOT_FAULT_POINT_ID}:*" | while read -r key; do "${compose[@]}" exec -T redis redis-cli GET "$key"; done | awk '{n+=$1} END{print n+0}'; }
before_pv=$(sum_pv)
cleanup() { "${compose[@]}" start clickhouse >/dev/null 2>&1 || true; }
trap cleanup EXIT
"${compose[@]}" stop clickhouse
curl --fail-with-body -sS -H 'Content-Type: application/json' -H "X-Write-Key: ${GO_DOT_FAULT_WRITE_KEY}" --data "$PAYLOAD" "${API_URL}/api/v1/collect/events" >/tmp/go-dot-fault-collect.json

deadline=$((SECONDS+30)); pending=0
while (( SECONDS < deadline )); do
  pending=$("${compose[@]}" exec -T redis redis-cli XPENDING 'tracking:{queue}:events' tracking-workers | head -n1 | tr -d '\r')
  [[ "${pending:-0}" =~ ^[1-9] ]] && break
  sleep 1
done
[[ "${pending:-0}" =~ ^[1-9] ]] || { echo "expected a Pending message while ClickHouse is unavailable" >&2; exit 1; }

"${compose[@]}" start clickhouse
deadline=$((SECONDS+90)); rows=0
while (( SECONDS < deadline )); do
  rows=$("${compose[@]}" exec -T clickhouse clickhouse-client --database go_dot --format TSV --query "SELECT count() FROM tracking_event FINAL WHERE project_id=${GO_DOT_FAULT_PROJECT_ID} AND event_id='${EVENT_ID}'" 2>/dev/null || echo 0)
  [[ "$rows" == "1" ]] && break
  sleep 2
done
[[ "$rows" == "1" ]] || { echo "event did not recover within 90 seconds" >&2; exit 1; }

pv=$(sum_pv)
[[ "$pv" -eq $((before_pv+1)) ]] || { echo "expected exactly one PV increment after recovery (before=${before_pv}, after=${pv})" >&2; exit 1; }
echo "ClickHouse recovery verified: event=${EVENT_ID}, rows=${rows}, project PV=${pv}"
