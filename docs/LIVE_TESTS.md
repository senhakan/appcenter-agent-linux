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

## 2026-03-03 - Signal Wake-Up Smoke (Long Poll)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build: signal long-poll destekli (`feat: add signal long-poll and immediate heartbeat trigger`)

### Result

- Long-poll endpoint baglantisi: OK (`GET /api/v1/agent/signal?timeout=55`)
- Wake signal tetigi: OK (remote-support session create ile)
- Agent tarafi anlik heartbeat: OK
  - Log kaniti: `signal-triggered heartbeat ok: status=ok`

### Evidence

- Session create:
  - `POST /api/v1/remote-support/sessions` -> `200` (session id: `217`)
- Server journal:
  - `10.6.60.88 -> GET /api/v1/agent/signal?timeout=55` (tekrarlayan)
  - session create sonrasi `10.6.60.88 -> POST /api/v1/agent/heartbeat 200`
- Agent runtime output:
  - `2026/03/03 20:50:22 signal-triggered heartbeat ok: status=ok`

Not:
- Bu test sirasinda sunucuda `REMOTE_SUPPORT_ENABLED` ayari `true` yapilmistir.
- 2026-03-03 itibariyla operasyon karari: test/canli Linux agent denemeleri icin bu ayar `true` olarak korunur.

## 2026-03-03 - Test Host Tooling (10.6.60.88)

- Host: `10.6.60.88` (`ubuntu`)
- Kurulan/teyit edilen araclar:
  - `curl 8.5.0`
  - `ripgrep 14.1.0` (`rg`)
  - `jq 1.7`
- Kurulum yontemi:
  - `sudo apt-get update -y`
  - `sudo apt-get install -y curl ripgrep jq`
- Operasyon notu:
  - Bu test hostunda Linux agent canli testlerini hizlandirmak icin gerekli ek araclari onay beklemeden kurma yetkisi vardir.
