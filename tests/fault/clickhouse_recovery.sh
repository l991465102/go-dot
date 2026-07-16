#!/usr/bin/env bash
set -euo pipefail

: "${GO_DOT_FAULT_WRITE_KEY:?set GO_DOT_FAULT_WRITE_KEY to a published test project key}"
: "${GO_DOT_FAULT_PROJECT_ID:?set GO_DOT_FAULT_PROJECT_ID}"
: "${GO_DOT_FAULT_POINT_ID:?set GO_DOT_FAULT_POINT_ID}"
API_URL="${GO_DOT_FAULT_API_URL:-http://127.0.0.1:8080}"
COMPOSE_PROJECT="${GO_DOT_FAULT_COMPOSE_PROJECT:-}"
COMPOSE_OVERRIDE="${GO_DOT_FAULT_COMPOSE_OVERRIDE:-}"
COMPOSE_ENV_FILE="${GO_DOT_FAULT_ENV_FILE:-.env}"
compose=(docker compose --env-file "$COMPOSE_ENV_FILE")
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
BATCH_ID=$(sed -n 's/.*"batch_id":"\([^"]*\)".*/\1/p' /tmp/go-dot-fault-collect.json)
[[ -n "$BATCH_ID" ]] || { echo "collection response did not contain batch_id" >&2; exit 1; }

find_message_id() {
  "${compose[@]}" exec -T redis redis-cli --raw EVAL "local xs=redis.call('XRANGE',KEYS[1],'-','+'); for _,x in ipairs(xs) do local f=x[2]; local batch=''; local payload=''; for i=1,#f,2 do if f[i]=='batch_id' then batch=f[i+1] elseif f[i]=='payload' then payload=f[i+1] end end; if batch==ARGV[1] and string.find(payload,ARGV[2],1,true) then return x[1] end end; return ''" 1 'tracking:{queue}:events' "$BATCH_ID" "$EVENT_ID"
}

deadline=$((SECONDS+30)); message_id=''; pending_entry=''
while (( SECONDS < deadline )); do
  message_id=$(find_message_id | tr -d '\r')
  if [[ -n "$message_id" ]]; then
    pending_entry=$("${compose[@]}" exec -T redis redis-cli --raw XPENDING 'tracking:{queue}:events' tracking-workers "$message_id" "$message_id" 1)
    [[ "$pending_entry" == *"$message_id"* ]] && break
  fi
  sleep 1
done
[[ -n "$message_id" && "$pending_entry" == *"$message_id"* ]] || { echo "submitted batch/event did not become Pending while ClickHouse was unavailable (batch=${BATCH_ID}, event=${EVENT_ID})" >&2; exit 1; }

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
echo "ClickHouse recovery verified: batch=${BATCH_ID}, message=${message_id}, event=${EVENT_ID}, rows=${rows}, project PV=${pv}"
