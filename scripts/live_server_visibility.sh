#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="${SERVER_URL:-http://10.6.100.170:8000}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-admin123}"
AGENT_TEST_HOST="${AGENT_TEST_HOST:-10.6.60.88}"
AGENT_TEST_USER="${AGENT_TEST_USER:-ubuntu}"
AGENT_TEST_PASS="${AGENT_TEST_PASS:-1234asd!!!}"
INTERVAL_SEC="${INTERVAL_SEC:-5}"
ITERATIONS="${ITERATIONS:-0}" # 0=infinite

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 1; }; }
need_cmd curl
need_cmd sshpass
need_cmd python3

AGENT_UUID="$(sshpass -p "$AGENT_TEST_PASS" ssh -o StrictHostKeyChecking=no "${AGENT_TEST_USER}@${AGENT_TEST_HOST}" "python3 - <<'PY'
import json
print(json.load(open('/tmp/ac-live/state.json')).get('uuid',''))
PY")"

if [[ -z "$AGENT_UUID" ]]; then
  echo "agent uuid not found on test host" >&2
  exit 1
fi

echo "[visibility] server=${SERVER_URL} agent_uuid=${AGENT_UUID}"

count=0
while :; do
  TOKEN="$(curl -sS -m 10 -X POST "${SERVER_URL}/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('access_token',''))")"
  if [[ -z "$TOKEN" ]]; then
    echo "[visibility] login token empty"
    sleep "$INTERVAL_SEC"
    continue
  fi

  RESP="$(curl -sS -m 12 -H "Authorization: Bearer ${TOKEN}" "${SERVER_URL}/api/v1/agents/${AGENT_UUID}")"
  python3 - <<'PY' "$RESP"
import json,sys
obj=json.loads(sys.argv[1])
keys=['status','last_seen','hostname','ip_address','os_user','os_version','platform','arch','distro','distro_version','cpu_model','ram_gb','disk_free_gb','version']
print('---')
for k in keys:
    print(f"{k}: {obj.get(k)}")
PY

  count=$((count+1))
  if [[ "$ITERATIONS" != "0" && "$count" -ge "$ITERATIONS" ]]; then
    break
  fi
  sleep "$INTERVAL_SEC"
done
