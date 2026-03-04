#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="${AGENT_TEST_HOST:-10.6.60.88}"
USER="${AGENT_TEST_USER:-ubuntu}"
PASS="${AGENT_TEST_PASS:-1234asd!!!}"
REMOTE_BIN="${AGENT_REMOTE_BIN:-/tmp/ac-live/appcenter-agent-linux}"
REMOTE_CONFIG="${AGENT_REMOTE_CONFIG:-/tmp/ac-live/config.yaml}"
REMOTE_LOG="${AGENT_REMOTE_LOG:-/tmp/ac-live/run_smoke_automation.log}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 1; }
}

need_cmd go
need_cmd sshpass
need_cmd scp
need_cmd ssh
need_cmd sha256sum

cd "$ROOT_DIR"
echo "[smoke] building service binary"
go build -o build/service ./cmd/service

LOCAL_SHA="$(sha256sum build/service | awk '{print $1}')"
echo "[smoke] uploading binary to ${USER}@${HOST}:${REMOTE_BIN}"
sshpass -p "$PASS" scp -o StrictHostKeyChecking=no build/service "${USER}@${HOST}:${REMOTE_BIN}.new"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "mv '${REMOTE_BIN}.new' '${REMOTE_BIN}'; chmod +x '${REMOTE_BIN}'"

REMOTE_SHA="$(sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "sha256sum ${REMOTE_BIN} | cut -d' ' -f1")"
if [[ "$LOCAL_SHA" != "$REMOTE_SHA" ]]; then
  echo "[smoke] sha mismatch local=${LOCAL_SHA} remote=${REMOTE_SHA}" >&2
  exit 1
fi

echo "[smoke] running remote foreground smoke"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "bash -s" <<EOS
set -euo pipefail
rm -f "$REMOTE_LOG"
nohup timeout 65s "$REMOTE_BIN" -config "$REMOTE_CONFIG" >"$REMOTE_LOG" 2>&1 &
PID=\$!
sleep 8
kill "\$PID" 2>/dev/null || true
sleep 1
if ! grep -q 'linux agent runtime:' "$REMOTE_LOG"; then
  echo '[smoke] runtime line missing' >&2
  tail -n 80 "$REMOTE_LOG" || true
  exit 1
fi
if ! grep -q 'register ok' "$REMOTE_LOG"; then
  echo '[smoke] register line missing' >&2
  tail -n 80 "$REMOTE_LOG" || true
  exit 1
fi
if ! grep -q 'ipc server listening:' "$REMOTE_LOG"; then
  echo '[smoke] ipc line missing' >&2
  tail -n 80 "$REMOTE_LOG" || true
  exit 1
fi
echo '[smoke] remote log summary:'
grep -E 'linux agent runtime:|linux agent install queue:|heartbeat ok|register ok|register failed|ipc server listening' "$REMOTE_LOG" | tail -n 20 || true
EOS

echo "[smoke] OK"
