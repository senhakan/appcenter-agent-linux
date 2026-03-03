# Live Tests

## Policy

- 2026-03-03 itibariyla operasyon karari:
  - Linux agent kodunda yapilan her teknik degisiklik sonrasinda canli test hostu (`10.6.60.88`) uzerinde dogrulama zorunludur.
  - Bu dogrulama tamamlanmadan is "tamamlandi" kabul edilmez.

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

## 2026-03-03 - Task Status + Download/Install Temel Akis Smoke

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - task status reporting + download + install temel akisli surum

### Result

- Agent command alma: OK (`commands=1`)
- Download: OK
- Install: OK
- Task status report: OK (`success`, `exit_code=0`)
- Hostta beklenen cikti dosyasi olustu: `/tmp/ac_task_ok.txt`

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=29 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=29 install success`
- Server DB (`/var/lib/appcenter/appcenter.db`) `task_history`:
  - `id=29`, `status=success`, `message=Install completed`, `exit_code=0`
- Test host dosya kaniti:
  - `/tmp/ac_task_ok.txt` icerigi: `linux task install ok via exe payload ...`

Not:
- Canli ortamda `applications` tablosundaki eski SQLite `ck_application_file_type` constraint'i Linux `sh/tar.gz/deb` uploadunu engelledigi icin bu smoke testte `.exe` uzantili script payload kullanilmistir.
- Test akisinda `app_id=11` kaydi test amacli `target_platform=linux` olarak isaretlenmistir.

## 2026-03-03 - Task Flow Hardening Retest (Cleanup + Unsupported Handling Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - unsupported action icin explicit `failed` status raporlayan
  - basarili install sonrasi indirilen paketi temizleyen surum

### Result

- Canli install task akis smoke: OK
- Download -> Install -> Success status: OK (`task_history.id=30`)
- Download cleanup: OK (indirilen paket dosyasi `downloads` dizininde kalmadi)

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=30 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=30 install success`
- Server DB (`/var/lib/appcenter/appcenter.db`) `task_history`:
  - `id=30`, `status=success`, `message=Install completed`, `exit_code=0`
- Test host:
  - `/tmp/ac_task_ok.txt` yeniden olustu (`2026-03-03T21:15:15Z`)
  - `/tmp/ac-live/downloads` dizininde paket dosyasi kalmadi (cleanup dogrulandi)

## 2026-03-03 - Unsupported Action Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Test host uzerinde lokal mock API (`127.0.0.1:18080`) ayaga kaldirildi.
  - Ilk heartbeat cevabinda `commands=[{task_id:999, action:\"noop\"}]` donduruldu.
  - Agent, bu mock endpoint'e ozel config ile foreground calistirildi.

### Result

- Unsupported action handling: OK
- Agent logu beklenen sekilde:
  - `task=999 unsupported action: noop`
- Agent task status callback'i beklenen sekilde:
  - `status=failed`
  - `message=\"Unsupported command action\"`
  - `error=\"unsupported action: noop\"`

### Evidence

- Test host log (`/tmp/ac-live/run_mock.log`):
  - `2026/03/03 21:20:02 periodic heartbeat ok: status=ok commands=1`
  - `2026/03/03 21:20:02 task=999 unsupported action: noop`
- Mock event log (`/tmp/ac-live/mock_events.jsonl`) son task status kaydi:
  - `{\"kind\":\"task_status\",\"path\":\"/api/v1/agent/task/999/status\",\"body\":{\"status\":\"failed\",\"progress\":100,\"message\":\"Unsupported command action\",\"error\":\"unsupported action: noop\"}}`

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.
