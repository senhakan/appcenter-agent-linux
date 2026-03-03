# Live Tests

## 2026-03-03 - Linux Agent Register/Heartbeat Smoke

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Run mode: foreground + timeout (`75s`)

### Result

- SSH connection: OK
- `GET /health` from test host to server: OK
- Agent register: OK
- Heartbeat loop: OK (20s aralikla)
- State file persisted: OK (`uuid` + `secret_key`)

### Evidence

- Agent runtime log (test host):
  - `register ok: uuid=79001ca1-70cb-4734-8f35-233bb38aec9a`
  - `heartbeat ok: status=ok` (birden fazla kez)
- Server journal:
  - `10.6.60.88 -> POST /api/v1/agent/register 200`
  - `10.6.60.88 -> POST /api/v1/agent/heartbeat 200`
- Server DB (`/var/lib/appcenter/appcenter.db`) row:
  - `platform=linux`, `arch=amd64`, `distro=ubuntu`, `distro_version=24.04`

