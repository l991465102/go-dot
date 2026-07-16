#!/usr/bin/env bash
set -euo pipefail

: "${GO_DOT_FAULT_WRITE_KEY:?set GO_DOT_FAULT_WRITE_KEY to a published test project key}"
API_URL="${GO_DOT_FAULT_API_URL:-http://127.0.0.1:8080}"
COMPOSE_PROJECT="${GO_DOT_FAULT_COMPOSE_PROJECT:-}"
COMPOSE_OVERRIDE="${GO_DOT_FAULT_COMPOSE_OVERRIDE:-}"
COMPOSE_ENV_FILE="${GO_DOT_FAULT_ENV_FILE:-.env}"
compose=(docker compose --env-file "$COMPOSE_ENV_FILE")
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
BATCH_ID=$(sed -n 's/.*"batch_id":"\([^"]*\)".*/\1/p' /tmp/go-dot-dlq-collect.json)
[[ -n "$BATCH_ID" ]] || { echo "collection response did not contain batch_id" >&2; exit 1; }

find_dlq_id() {
  "${compose[@]}" exec -T redis redis-cli --raw EVAL "local xs=redis.call('XRANGE',KEYS[1],'-','+'); for _,x in ipairs(xs) do local f=x[2]; local batch=''; local payload=''; for i=1,#f,2 do if f[i]=='batch_id' then batch=f[i+1] elseif f[i]=='payload' then payload=f[i+1] end end; if batch==ARGV[1] and string.find(payload,ARGV[2],1,true) then return x[1] end end; return ''" 1 'tracking:{queue}:dead' "$BATCH_ID" "$EVENT_ID"
}
field_value() { awk -v field="$1" '$0==field {getline; print; exit}' <<<"$entry"; }

deadline=$((SECONDS+120)); dlq_id=''; entry=''
while (( SECONDS < deadline )); do
  dlq_id=$(find_dlq_id | tr -d '\r')
  if [[ -n "$dlq_id" ]]; then
    entry=$("${compose[@]}" exec -T redis redis-cli --raw XRANGE 'tracking:{queue}:dead' "$dlq_id" "$dlq_id")
    [[ -n "$entry" ]] && break
  fi
  sleep 2
done
[[ -n "$entry" ]] || { echo "exact batch/event did not reach DLQ after five deliveries (batch=${BATCH_ID}, event=${EVENT_ID})" >&2; exit 1; }
[[ "$(field_value batch_id)" == "$BATCH_ID" ]] || { echo "DLQ batch_id mismatch" >&2; exit 1; }
[[ "$(field_value deliveries)" == "5" ]] || { echo "DLQ deliveries was $(field_value deliveries), expected 5" >&2; exit 1; }
stored_payload=$(field_value payload)
[[ "$stored_payload" == *"$EVENT_ID"* && "$stored_payload" == *'"point_code":"product_detail_view"'* ]] || { echo "DLQ did not preserve the exact original payload" >&2; exit 1; }
[[ "$(field_value last_error)" == *'store valid events'* ]] || { echo "DLQ did not preserve the ClickHouse write failure stage" >&2; exit 1; }
echo "DLQ verified: id=${dlq_id}, batch=${BATCH_ID}, event=${EVENT_ID}, deliveries=5"
