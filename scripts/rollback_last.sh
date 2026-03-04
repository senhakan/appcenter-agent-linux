#!/usr/bin/env bash
set -euo pipefail

HOST="${AGENT_TEST_HOST:-10.6.60.88}"
USER="${AGENT_TEST_USER:-ubuntu}"
PASS="${AGENT_TEST_PASS:-1234asd!!!}"
REMOTE_BIN="${AGENT_REMOTE_BIN:-/tmp/ac-live/appcenter-agent-linux}"
REMOTE_CONFIG="${AGENT_REMOTE_CONFIG:-/tmp/ac-live/config.yaml}"
REMOTE_LOG="${AGENT_REMOTE_LOG:-/tmp/ac-live/run_rollback_last.log}"
RUN_SMOKE="${RUN_SMOKE:-1}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 1; }
}

need_cmd sshpass
need_cmd ssh

REMOTE_BACKUP="$(sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "if [ -f \"${REMOTE_BIN}.last_backup\" ]; then cat \"${REMOTE_BIN}.last_backup\"; else ls -1t \"${REMOTE_BIN}\".20* 2>/dev/null | head -n1; fi")"
if [[ -z "${REMOTE_BACKUP:-}" ]]; then
  echo "[rollback] no backup found" >&2
  exit 1
fi

echo "[rollback] restoring backup: ${REMOTE_BACKUP}"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "test -f '${REMOTE_BACKUP}' && cp '${REMOTE_BACKUP}' '${REMOTE_BIN}' && chmod +x '${REMOTE_BIN}'"

if [[ "$RUN_SMOKE" == "1" ]]; then
  echo "[rollback] running remote quick smoke"
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
echo '[rollback] smoke summary:'
grep -E 'linux agent runtime:|register ok|register failed|ipc server listening' "${REMOTE_LOG}" | tail -n 20 || true
EOS
fi

echo "[rollback] OK"
