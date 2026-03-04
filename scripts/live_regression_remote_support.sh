#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST="${AGENT_TEST_HOST:-10.6.60.88}"
USER="${AGENT_TEST_USER:-ubuntu}"
PASS="${AGENT_TEST_PASS:-1234asd!!!}"
REMOTE_BIN="${AGENT_REMOTE_BIN:-/tmp/ac-live/appcenter-agent-linux}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 1; }
}

need_cmd go
need_cmd sshpass
need_cmd scp
need_cmd ssh
need_cmd sha256sum

cd "$ROOT_DIR"
echo "[regression] building service binary"
go build -o build/service ./cmd/service

LOCAL_SHA="$(sha256sum build/service | awk '{print $1}')"
sshpass -p "$PASS" scp -o StrictHostKeyChecking=no build/service "${USER}@${HOST}:${REMOTE_BIN}.new"
sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "mv '${REMOTE_BIN}.new' '${REMOTE_BIN}'; chmod +x '${REMOTE_BIN}'"
REMOTE_SHA="$(sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "sha256sum ${REMOTE_BIN} | cut -d' ' -f1")"
[[ "$LOCAL_SHA" == "$REMOTE_SHA" ]] || { echo "sha mismatch" >&2; exit 1; }

sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "${USER}@${HOST}" "bash -s" <<'EOS'
set -euo pipefail
cd /tmp/ac-live

echo "[regression] ensure xvfb"
if ! command -v Xvfb >/dev/null 2>&1; then
  echo '1234asd!!!' | sudo -S apt-get update -y >/tmp/ac-live/apt_update_xvfb_auto.log 2>&1
  echo '1234asd!!!' | sudo -S apt-get install -y xvfb >/tmp/ac-live/apt_install_xvfb_auto.log 2>&1
fi

cat > mock_remote_support_4xx_auto.py <<'PY'
#!/usr/bin/env python3
import json,re
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path
EV=Path('/tmp/ac-live/mock_remote_support_4xx_auto_events.jsonl')
EV.write_text('')
class H(BaseHTTPRequestHandler):
  def log_message(self, *a): return
  def _j(self,c,p):
    b=json.dumps(p).encode();self.send_response(c);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(b)));self.end_headers();self.wfile.write(b)
  def do_GET(self):
    if self.path.startswith('/api/v1/agent/signal'): return self._j(200,{'status':'timeout'})
    return self._j(404,{'status':'error'})
  def do_POST(self):
    ln=int(self.headers.get('Content-Length','0'));raw=self.rfile.read(ln) if ln>0 else b'{}'
    try:d=json.loads(raw.decode() or '{}')
    except Exception:d={'_raw':raw.decode(errors='replace')}
    EV.open('a').write(json.dumps({'path':self.path,'body':d})+'\\n')
    if self.path=='/api/v1/agent/register': return self._j(200,{'status':'ok','secret_key':'mock-secret'})
    if self.path=='/api/v1/agent/heartbeat': return self._j(200,{'status':'ok','config':{'heartbeat_interval_sec':2,'inventory_sync_required':False,'inventory_scan_interval_min':60,'remote_support_enabled':True},'commands':[]})
    if re.match(r'^/api/v1/agent/remote-support/\\d+/approve$',self.path): return self._j(400,{'status':'error','message':'bad request'})
    if re.match(r'^/api/v1/agent/remote-support/\\d+/ready$',self.path): return self._j(200,{'status':'ok'})
    if re.match(r'^/api/v1/agent/remote-support/\\d+/ended$',self.path): return self._j(200,{'status':'ok'})
    return self._j(404,{'status':'error'})
ThreadingHTTPServer(('127.0.0.1',18105),H).serve_forever()
PY
chmod +x mock_remote_support_4xx_auto.py
cat > config_remote_support_4xx_auto.yaml <<'YAML'
server:
  url: "http://127.0.0.1:18105"
agent:
  version: "0.1.0"
heartbeat:
  interval_sec: 2
download:
  temp_dir: "/tmp/ac-live/downloads_remote_support_4xx_auto"
  max_size_bytes: 209715200
install:
  timeout_sec: 1800
  queue_capacity: 4
  worker_count: 1
logging:
  file: "/tmp/ac-live/agent_remote_support_4xx_auto.log"
paths:
  state_file: "/tmp/ac-live/state_remote_support_4xx_auto.json"
ipc:
  socket_path: "/tmp/ac-live/ipc_remote_support_4xx_auto.sock"
remote_support:
  enabled: true
  approval_timeout_sec: 120
  display: ":99"
  port: 5902
YAML

pkill -f mock_remote_support_4xx_auto.py || true
pkill Xvfb || true
nohup Xvfb :99 -screen 0 1024x768x24 >/tmp/ac-live/xvfb99_auto.log 2>&1 &
XVFB_PID=$!
nohup python3 /tmp/ac-live/mock_remote_support_4xx_auto.py >/tmp/ac-live/mock_remote_support_4xx_auto.log 2>&1 &
MOCK_PID=$!
rm -f /tmp/ac-live/ipc_remote_support_4xx_auto.sock /tmp/ac-live/run_remote_support_4xx_auto.log /tmp/ac-live/mock_remote_support_4xx_auto_events.jsonl
nohup timeout 65s /tmp/ac-live/appcenter-agent-linux -config /tmp/ac-live/config_remote_support_4xx_auto.yaml >/tmp/ac-live/run_remote_support_4xx_auto.log 2>&1 &
AGENT_PID=$!
trap 'kill "$AGENT_PID" 2>/dev/null || true; kill "$MOCK_PID" 2>/dev/null || true; kill "$XVFB_PID" 2>/dev/null || true' EXIT
for _ in $(seq 1 40); do
  if ss -lnt | grep -q ':18105'; then
    break
  fi
  sleep 0.25
done
for _ in $(seq 1 80); do [ -S /tmp/ac-live/ipc_remote_support_4xx_auto.sock ] && break; sleep 0.25; done
for _ in $(seq 1 40); do grep -q 'register ok' /tmp/ac-live/run_remote_support_4xx_auto.log && break; sleep 0.25; done

for _ in $(seq 1 20); do
  PING_RAW="$(printf '%s\n' '{"action":"ping"}' | nc -U -w 2 /tmp/ac-live/ipc_remote_support_4xx_auto.sock || true)"
  if [[ -n "$PING_RAW" ]]; then
    break
  fi
  sleep 0.2
done
if [[ -z "${PING_RAW:-}" ]]; then
  echo '[regression] ipc ping returned empty response' >&2
  tail -n 120 /tmp/ac-live/run_remote_support_4xx_auto.log || true
  exit 1
fi
UNK_RAW="$(printf '%s\n' '{"action":"nope"}' | nc -U -w 2 /tmp/ac-live/ipc_remote_support_4xx_auto.sock || true)"
REQ_RAW="$(printf '%s\n' '{"action":"remote_support_session_request","session_id":9401,"admin_name":"qa","reason":"auto regression"}' | nc -U -w 2 /tmp/ac-live/ipc_remote_support_4xx_auto.sock || true)"
APPR_RAW="$(printf '%s\n' '{"action":"remote_support_approve"}' | nc -U -w 2 /tmp/ac-live/ipc_remote_support_4xx_auto.sock || true)"
export PING_RAW UNK_RAW REQ_RAW APPR_RAW
python3 - <<'PY'
import json, os
def parse_one(v):
 lines=[x.strip() for x in v.splitlines() if x.strip()]
 for line in lines:
  if line.startswith('{') and line.endswith('}'):
   return json.loads(line)
 raise RuntimeError(f'no json line in value: {v!r}')
ping=parse_one(os.environ['PING_RAW'])
unk=parse_one(os.environ['UNK_RAW'])
req=parse_one(os.environ['REQ_RAW'])
appr=parse_one(os.environ['APPR_RAW'])
print(ping); print(unk); print(req); print(appr)
assert ping.get('code')=='ok'
assert unk.get('code')=='unsupported_action'
assert req.get('code')=='remote_support_session_pending'
assert appr.get('code')=='remote_support_approve_callback_failed'
PY
sleep 1

APPROVE_COUNT="$(grep -o '/remote-support/.*/approve' /tmp/ac-live/mock_remote_support_4xx_auto_events.jsonl | wc -l | tr -d ' ')"
if [[ "$APPROVE_COUNT" != "1" ]]; then
  echo "[regression] expected single approve call, got ${APPROVE_COUNT}" >&2
  tail -n 80 /tmp/ac-live/mock_remote_support_4xx_auto_events.jsonl || true
  exit 1
fi
echo "[regression] 4xx non-retry OK"

kill "$AGENT_PID" 2>/dev/null || true
kill "$MOCK_PID" 2>/dev/null || true
kill "$XVFB_PID" 2>/dev/null || true
trap - EXIT

echo "[regression] OK"
EOS
