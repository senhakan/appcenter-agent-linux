#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="${AGENT_TEST_HOST:-10.6.60.88}"
USER="${AGENT_TEST_USER:-ubuntu}"
PASS="${AGENT_TEST_PASS:-1234asd!!!}"
REMOTE_BIN="${AGENT_REMOTE_BIN:-/tmp/ac-live/appcenter-agent-linux}"
REMOTE_CONFIG="${AGENT_REMOTE_CONFIG:-/tmp/ac-live/config.yaml}"
REMOTE_LOG="${AGENT_REMOTE_LOG:-/tmp/ac-live/run_deploy_with_backup.log}"
RUN_SMOKE="${RUN_SMOKE:-1}"
AUTO_ROLLBACK_ON_FAIL="${AUTO_ROLLBACK_ON_FAIL:-1}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 1; }
}

need_cmd go
need_cmd sshpass
need_cmd scp
need_cmd ssh
need_cmd sha256sum

cd "$ROOT_DIR"

echo "[deploy] building service binary"
go build -o build/service ./cmd/service
LOCAL_SHA="$(sha256sum build/service | awk '{print $1}')"

STAMP="$(date -u +%Y%m%d_%H%M%S)"
REMOTE_BACKUP="${REMOTE_BIN}.${STAMP}"

echo "[deploy] creating remote backup: ${REMOTE_BACKUP}"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "if [ -f '${REMOTE_BIN}' ]; then cp '${REMOTE_BIN}' '${REMOTE_BACKUP}'; fi; echo '${REMOTE_BACKUP}' > '${REMOTE_BIN}.last_backup'"

echo "[deploy] uploading new binary"
sshpass -p "$PASS" scp -o StrictHostKeyChecking=no build/service "${USER}@${HOST}:${REMOTE_BIN}.new"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "mv '${REMOTE_BIN}.new' '${REMOTE_BIN}'; chmod +x '${REMOTE_BIN}'"

REMOTE_SHA="$(sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "sha256sum '${REMOTE_BIN}' | cut -d' ' -f1")"
if [[ "$LOCAL_SHA" != "$REMOTE_SHA" ]]; then
  echo "[deploy] sha mismatch local=${LOCAL_SHA} remote=${REMOTE_SHA}" >&2
  exit 1
fi

echo "[deploy] deployed sha=${REMOTE_SHA}"

echo "[deploy] backup pointer: ${REMOTE_BIN}.last_backup"

rollback_to_backup() {
  echo "[deploy] rolling back to backup: ${REMOTE_BACKUP}"
  sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "cp '${REMOTE_BACKUP}' '${REMOTE_BIN}'; chmod +x '${REMOTE_BIN}'"
}

run_remote_smoke() {
  sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "bash -s" <<EOS
set -euo pipefail
rm -f "${REMOTE_LOG}"
nohup timeout 65s "${REMOTE_BIN}" -config "${REMOTE_CONFIG}" >"${REMOTE_LOG}" 2>&1 &
PID=\$!
sleep 8
kill "\$PID" 2>/dev/null || true
sleep 1
grep -q 'linux agent runtime:' "${REMOTE_LOG}"
grep -q 'ipc server listening:' "${REMOTE_LOG}"
if ! grep -q 'register ok' "${REMOTE_LOG}" && ! grep -q 'register failed' "${REMOTE_LOG}"; then
  echo '[deploy] register status line missing' >&2
  tail -n 80 "${REMOTE_LOG}" || true
  exit 1
fi
echo '[deploy] smoke summary:'
grep -E 'linux agent runtime:|register ok|register failed|ipc server listening' "${REMOTE_LOG}" | tail -n 20 || true
EOS
}

if [[ "$RUN_SMOKE" == "1" ]]; then
  echo "[deploy] running remote quick smoke"
  if ! run_remote_smoke; then
    echo "[deploy] smoke failed"
    if [[ "$AUTO_ROLLBACK_ON_FAIL" == "1" ]]; then
      rollback_to_backup
      echo "[deploy] rollback after failed smoke: done"
    fi
    exit 1
  fi
fi

echo "[deploy] OK"
