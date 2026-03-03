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

## 2026-03-03 - Task Progress Retest (Install Started 90%)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - Download tamamlandiktan sonra install baslangici icin ek ara status gonderen surum

### Result

- Canli install task akis smoke: OK (`task_id=31`)
- Ara status adimi eklendi: OK
  - `downloading(10)` -> `downloading(80)` -> `downloading(90, Install started)` -> `success(100)`
- Download cleanup davranisi korundu: OK

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=31 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=31 install success`
- Server journal (`appcenter` unit):
  - `POST /api/v1/agent/task/31/status` ayni task icin art arda 4 kez `200 OK` (ara progress + final success)
- Server DB (`task_history`):
  - `id=31`, `status=success`, `message=Install completed`, `exit_code=0`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T21:23:27Z`)
  - `/tmp/ac-live/downloads` bos (paket temizligi devam ediyor)

## 2026-03-03 - Task Status Reporting Hardening Retest

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - task status gonderimlerini tek helper uzerinden yapan
  - status post hatalarinda log warning ureten surum

### Result

- Canli install task akis smoke: OK (`task_id=32`)
- Task status adimlari beklendigi gibi production servera iletildi: OK
  - heartbeat + download + 4 adet status callback
- Download cleanup davranisi korundu: OK

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=32 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=32 install success`
- Server journal (`appcenter` unit):
  - `POST /api/v1/agent/task/32/status` ayni task icin 4 kez `200 OK`
  - `GET /api/v1/agent/download/11` `200 OK`
- Server DB (`task_history`):
  - `id=32`, `status=success`, `message=Install completed`, `exit_code=0`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T21:26:26Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Task Status Retry/Backoff Retest

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - task status post icin 3 deneme + artan kisa backoff (300ms, 600ms) ekli surum

### Result

- Canli install task akis smoke: OK (`task_id=33`)
- Task status callback zinciri sorunsuz: OK (4 adet status `200`)
- Retry eklenmesine ragmen normal akista regressionsuz: OK
- Download cleanup davranisi korundu: OK

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=33 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=33 install success`
- Server journal (`appcenter` unit):
  - `POST /api/v1/agent/task/33/status` ayni task icin 4 kez `200 OK`
  - `GET /api/v1/agent/download/11` `200 OK`
- Server DB (`task_history`):
  - `id=33`, `status=success`, `message=Install completed`, `exit_code=0`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T21:28:48Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Install Timeout Classification Live Test

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - install timeout durumunu `failed` yerine `timeout` olarak raporlayan surum
- Test setup:
  - Sunucuya timeout test payload app eklendi: `app_id=12` (`Linux Timeout Test App`)
  - Payload scripti: `sleep 10` (zaman asimi tetiklemek icin)
  - Agent bu testte ozel config ile calistirildi: `install.timeout_sec=2`

### Result

- Canli install timeout smoke: OK (`task_id=34`)
- Timeout sınıflandirma: OK (`task_history.status=timeout`)
- Agent app durumu: OK (`agent_applications.status=failed`, `retry_count=1`)
- Timeout payload beklenen cikti dosyasini olusturmadi: OK

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=34 download ok: bytes=156 path=/tmp/ac-live/downloads/linux_timeout_payload.exe`
  - `task=34 install timeout: Install timed out`
- Server journal (`appcenter` unit):
  - `POST /api/v1/agent/task/34/status` timeout akisi boyunca 4 kez `200 OK`
  - `GET /api/v1/agent/download/12` `200 OK`
- Server DB (`task_history`):
  - `id=34`, `status=timeout`, `message=Install timed out`, `install_duration_sec=10`
- Server DB (`agent_applications`):
  - `id=24`, `status=failed`, `retry_count=1`, `error_message=Install timed out`
- Test host:
  - `/tmp/ac_task_timeout_should_not_exist.txt` yok (beklenen)

## 2026-03-03 - Timeout/Fail Artifact Cleanup Retest

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - indirilen installer paketini sadece success'te degil timeout/failed durumlarinda da temizleyen surum
- Test setup:
  - Timeout payload (`app_id=12`) yeniden pending'e alinip calistirildi
  - Agent timeout config: `install.timeout_sec=2`

### Result

- Timeout task sonucu: OK (`task_id=35`, `status=timeout`)
- Agent app hata akisi: OK (`agent_applications.id=24 -> status=failed, retry_count=1`)
- Asil dogrulama:
  - Timeout sonrasi indirilen payload dosyasi temizlendi: OK
  - Beklenmeyen cikti dosyasi olusmadi: OK

### Evidence

- Agent runtime log:
  - `task=35 download ok: bytes=156 path=/tmp/ac-live/downloads/linux_timeout_payload.exe`
  - `task=35 install timeout: Install timed out`
- Server DB (`task_history`):
  - `id=35`, `status=timeout`, `message=Install timed out`, `install_duration_sec=10`
- Server journal (`appcenter` unit):
  - `POST /api/v1/agent/task/35/status` callbacklari `200 OK`
  - `GET /api/v1/agent/download/12` `200 OK`
- Test host:
  - `CLEANED_TIMEOUT_PAYLOAD` (download klasorunde timeout payload yok)
  - `NO_TIMEOUT_OUTPUT_FILE` (`/tmp/ac_task_timeout_should_not_exist.txt` olusmadi)

## 2026-03-03 - Download Retry/Backoff Retest

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - download asamasina 3 deneme + kademeli backoff ekli surum

### Result

- Canli install task akis smoke: OK (`task_id=36`)
- Download/Install/Success zinciri regressionsuz: OK
- Download cleanup davranisi korunuyor: OK

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=36 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=36 install success`
- Server DB (`task_history`):
  - `id=36`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/36/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T21:38:39Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Duplicate Task Idempotency Live Test (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Test host uzerinde lokal mock API (`127.0.0.1:18081`) calistirildi.
  - Ilk iki heartbeat cevabinda ayni komut donduruldu:
    - `task_id=777`, `action=install`, ayni download payload/hash
  - Agent mock config ile foreground calistirildi.

### Result

- Ilk heartbeat:
  - task normal calisti (`download + install success`)
- Ikinci heartbeat:
  - ayni task icin tekrar calistirma olmadi
  - log: `task=777 duplicate command skipped`
- Idempotency beklenen sekilde: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_dup.log`):
  - `2026/03/03 21:41:46 task=777 install success`
  - `2026/03/03 21:41:51 task=777 duplicate command skipped`
- Mock event log (`/tmp/ac-live/mock_dup_events.jsonl`):
  - `task_status` callback sayisi: `4` (tek install akisinin adimlari)
  - `download` endpoint cagrisi: `1` kez
- Test host:
  - `/tmp/ac_dup_task.txt` satir sayisi: `1` (komut tek kez calisti)

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Duplicate Task Persistence Across Restart (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Test host uzerinde lokal mock API (`127.0.0.1:18082`) calistirildi.
  - Her heartbeat'te ayni komut donduruldu (`task_id=888`, `action=install`).
  - Agent ayni state dosyasi ile iki kez ayri process olarak calistirildi (restart simulasyonu).

### Result

- Birinci calistirma:
  - task bir kez calisti, sonraki heartbeat'te skip edildi.
- Ikinci calistirma (restart sonrasi):
  - ilk heartbeat'te bile ayni task tekrar calistirilmadi
  - log: `task=888 duplicate command skipped`
- Kalici dedupe: OK

### Evidence

- Agent loglari:
  - Run#1: `task=888 install success`, sonra `task=888 duplicate command skipped`
  - Run#2: ilk heartbeat'ten itibaren `task=888 duplicate command skipped`
- State dosyasi (`/tmp/ac-live/state_persistdup.json`):
  - `processed_tasks` icinde `task_id=888` kaydi mevcut
- Mock event log (`/tmp/ac-live/mock_persist_dup_events.jsonl`):
  - `download` endpoint cagrisi: `1` kez
  - `task_status` callback sayisi: `4` (tek install akisinin adimlari)
- Test host:
  - `/tmp/ac_persist_dup_task.txt` satir sayisi: `1`

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Normal Flow Regression Smoke (Post Validation+Cap)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - install command validation + processed task cap iyilestirmeleri iceren surum

### Result

- Canli install task akis smoke: OK (`task_id=37`)
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log:
  - `periodic heartbeat ok: status=ok commands=1`
  - `task=37 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=37 install success`
- Server DB (`task_history`):
  - `id=37`, `status=success`, `message=Install completed`, `exit_code=0`

## 2026-03-03 - Invalid Install Command Rejection (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18083`) ilk heartbeat'te eksik alanli komut dondurdu:
    - `task_id=901`, `action=install`, `download_url=\"\"`, `file_hash=\"\"`
  - Agent mock config ile foreground calistirildi.

### Result

- Agent komutu indirime girmeden reject etti: OK
- Beklenen log:
  - `task=901 invalid install command: download_url is required`
- Task status callback:
  - `status=failed`, `message=Invalid install command`, `error=download_url is required`
- Download endpoint hic cagirilmadi: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_invalid.log`):
  - `2026/03/03 21:48:51 task=901 invalid install command: download_url is required`
- Mock event log (`/tmp/ac-live/mock_invalid_events.jsonl`):
  - `task_status` kaydi:
    - `{\"kind\":\"task_status\",\"path\":\"/api/v1/agent/task/901/status\",\"body\":{\"status\":\"failed\",\"progress\":100,\"message\":\"Invalid install command\",\"error\":\"download_url is required\"}}`
  - `download` cagrisi: `0`

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Processed Task Cap (500) Live Verification

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - `/tmp/ac-live/state_captest.json` icine `processed_tasks` listesi 620 kayitla preload edildi.
  - Agent ayni state dosyasiyla baslatildi.

### Result

- Startup sonrasi state otomatik trim: OK
- `processed_tasks` kayit sayisi `620 -> 500`

### Evidence

- Test host state check:
  - `processed_count 500`
  - `first_task_id 100000`
  - `last_task_id 100499`

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Invalid Hash Command Rejection (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18084`) ilk heartbeat'te hatali `file_hash` iceren komut dondurdu:
    - `task_id=902`, `action=install`, `download_url` dolu, `file_hash=sha256:XYZ`
  - Agent mock config ile foreground calistirildi.

### Result

- Agent komutu indirmeye girmeden reject etti: OK
- Beklenen log:
  - `task=902 invalid install command: file_hash must be sha256 hex`
- Task status callback:
  - `status=failed`, `message=Invalid install command`, `error=file_hash must be sha256 hex`
- Download endpoint hic cagirilmadi: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_invalidhash.log`):
  - `2026/03/03 21:52:11 task=902 invalid install command: file_hash must be sha256 hex`
- Mock event log (`/tmp/ac-live/mock_invalid_hash_events.jsonl`):
  - `task_status` kaydi:
    - `{\"kind\":\"task_status\",\"path\":\"/api/v1/agent/task/902/status\",\"body\":{\"status\":\"failed\",\"progress\":100,\"message\":\"Invalid install command\",\"error\":\"file_hash must be sha256 hex\"}}`
  - `download` cagrisi: `0`

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Normal Flow Regression Smoke (Post Hash Validation)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - install command hash format validation dahil surum

### Result

- Canli install task akis smoke: OK (`task_id=38`)
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log:
  - `task=38 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=38 install success`
- Server DB (`task_history`):
  - `id=38`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/38/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T21:52:53Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Terminal Status Fail Retry Behavior (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18085`) her heartbeat'te ayni install komutunu dondurdu (`task_id=950`).
  - Mock, `status=success` task callback'lerinde bilincli olarak `HTTP 500` dondu.
  - Agent mock config ile foreground calistirildi.

### Result

- Beklenen yeni davranis dogrulandi:
  - Terminal status callback basarisiz oldugu icin task kalici `done` olarak isaretlenmedi.
  - Ayni task sonraki heartbeat'lerde yeniden denendi.
- Log kaniti:
  - `task=950 status report warning ... HTTP 500` (3 deneme)
  - Sonraki heartbeat'lerde tekrar `task=950 download ok ...` ve `task=950 install success`

### Evidence

- Agent runtime log (`/tmp/ac-live/run_terminalfail.log`):
  - 3 heartbeat boyunca task yeniden calisma kaydi mevcut.
- Test host:
  - `/tmp/ac_terminal_fail_task.txt` satir sayisi: `3` (task 3 kez calisti)
- Mock event log (`/tmp/ac-live/mock_terminal_fail_events.jsonl`):
  - `download` cagrisi: `3`
  - `status=success` callback girisi: `9` (her calisma icin 3 retry)
- State dosyasi (`/tmp/ac-live/state_terminalfail.json`):
  - `processed_tasks` alani yok (terminal report basarili olmadigi icin kalici done yazilmadi)

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Normal Flow Regression Smoke (Post Inflight/Done Dedupe)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - inflight/done dedupe modeli + terminal report basarisizliginda yeniden deneme davranisli surum

### Result

- Canli install task akis smoke: OK (`task_id=39`)
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log:
  - `task=39 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=39 install success`
- Server DB (`task_history`):
  - `id=39`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/39/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T21:58:11Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Download Size Validation Rejection (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18086`) ayni install komutunu dondurdu (`task_id=960`).
  - Komutta `file_hash` dogru, ancak `file_size_bytes` bilerek hatali (`9999`) verildi.
  - Agent mock config ile foreground calistirildi.

### Result

- Download sonrasi boyut kontrolu fail: OK
- Beklenen log:
  - `task=960 download size mismatch: got=90 expected=9999`
- Task status callback:
  - `downloading(10)` ardindan `failed(100, Download size mismatch)`
- Kurulum calismadi ve payload temizlendi: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_sizemismatch.log`):
  - `task=960 download ok: bytes=90 path=/tmp/ac-live/downloads_sizemismatch/size_mismatch_install.sh`
  - `task=960 download size mismatch: got=90 expected=9999`
- Mock event log (`/tmp/ac-live/mock_size_mismatch_events.jsonl`):
  - `download` cagrisi: `1`
  - `task_status`:
    - `{\"status\":\"downloading\",\"progress\":10,...}`
    - `{\"status\":\"failed\",\"progress\":100,\"message\":\"Download size mismatch\",\"error\":\"download size mismatch: got=90 expected=9999\"}`
- Test host:
  - `NO_INSTALL_OUTPUT` (`/tmp/ac_size_mismatch_should_not_exist.txt` yok)
  - `CLEANED_SIZE_MISMATCH_PAYLOAD` (indirilen dosya temizlendi)

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Normal Flow Regression Smoke (Post Size Validation)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - download size validation iyilestirmesi dahil surum

### Result

- Canli install task akis smoke: OK (`task_id=40`)
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log:
  - `task=40 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=40 install success`
- Server DB (`task_history`):
  - `id=40`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/40/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:03:03Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Terminal Status Failure Cooldown (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18085`) her heartbeat'te ayni install komutunu dondurdu (`task_id=950`).
  - Mock, `status=success` callback'lerinde bilerek `HTTP 500` dondu.
  - Agent terminal status fail sonrasinda task tekrarini aninda yapmamak icin cooldown (30s) ile test edildi.

### Result

- Ilk heartbeat:
  - task bir kez calisti, success status callback'i 3 denemede de fail oldu.
- Sonraki heartbeat'ler (5s aralik):
  - task yeniden calismadi, `duplicate command skipped` goruldu (cooldown devrede).
- Beklenen davranis: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_terminalfail2.log`):
  - `task=950 ... status=success ... attempt=1/2/3 ... HTTP 500`
  - sonraki heartbeat'lerde: `task=950 duplicate command skipped`
- Test host:
  - `/tmp/ac_terminal_fail_task.txt` satir sayisi: `1`
- Mock event log (`/tmp/ac-live/mock_terminal_fail_events.jsonl`):
  - `download` cagrisi: `1`
  - `status=success` callback girisi: `3` (retry denemeleri)

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Normal Flow Regression Smoke (Post Terminal-Fail Cooldown)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - terminal status fail cooldown iyilestirmesi dahil surum

### Result

- Canli install task akis smoke: OK (`task_id=41`)
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log:
  - `task=41 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=41 install success`
- Server DB (`task_history`):
  - `id=41`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/41/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:06:20Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Download Max Size Limit Rejection (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18087`) install komutu dondurdu (`task_id=970`).
  - Komutta `file_hash` ve `file_size_bytes` dogru verildi.
  - Agent config:
    - `download.max_size_bytes: 50`
  - Gelen payload boyutu: `92 bytes` (limiti asti).

### Result

- Download limit kontrolu fail: OK
- Beklenen log:
  - `task=970 download size limit exceeded: got=92 max=50`
- Task status callback:
  - `downloading(10)` ardindan `failed(100, Download exceeds size limit)`
- Kurulum calismadi ve payload temizlendi: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_maxsize.log`):
  - `task=970 download ok: bytes=92 path=/tmp/ac-live/downloads_maxsize/maxsize_install.sh`
  - `task=970 download size limit exceeded: got=92 max=50`
- Mock event log (`/tmp/ac-live/mock_maxsize_events.jsonl`):
  - `download` cagrisi: `1`
  - `task_status`:
    - `{\"status\":\"downloading\",\"progress\":10,...}`
    - `{\"status\":\"failed\",\"progress\":100,\"message\":\"Download exceeds size limit\",\"error\":\"download size limit exceeded: got=92 max=50\"}`
- Test host:
  - `NO_MAXSIZE_INSTALL_OUTPUT` (`/tmp/ac_maxsize_should_not_exist.txt` yok)
  - `CLEANED_MAXSIZE_PAYLOAD` (indirilen dosya temizlendi)

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Normal Flow Regression Smoke (Post Max-Size Limit)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - `download.max_size_bytes` limiti iyilestirmesi dahil surum

### Result

- Canli install task akis smoke: OK (`task_id=42`)
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log:
  - `task=42 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=42 install success`
- Server DB (`task_history`):
  - `id=42`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/42/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:10:43Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Command Start Log Enrichment Regression Smoke

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - task baslangicinda komut ozeti loglayan surum (`app_id`, `version`, `priority`, `force_update`)

### Result

- Canli install task akis smoke: OK (`task_id=43`)
- Yeni start log satiri beklendigi gibi gorundu: OK
  - `task=43 start install: app_id=11 version=0.0.2 priority=8 force_update=false`
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log:
  - `task=43 start install: app_id=11 version=0.0.2 priority=8 force_update=false`
  - `task=43 download ok: bytes=134 path=/tmp/ac-live/downloads/linux_install_ok.exe`
  - `task=43 install success`
- Server DB (`task_history`):
  - `id=43`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/43/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:13:10Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Inventory Submit Live Validation (Phase-2 Alignment)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - Linux package envanterini `dpkg-query` ile toplayip `/api/v1/agent/inventory` endpoint'ine gonderen surum

### Result

- Inventory collect: OK
- Inventory submit: OK (`POST /api/v1/agent/inventory 200`)
- Server agent kaydi guncellendi:
  - `software_count=1659`
  - `inventory_hash` dolu

### Evidence

- Agent runtime log (`/tmp/ac-live/run_inventory.log`):
  - `inventory submitted: count=1659`
- Server journal (`appcenter` unit):
  - `10.6.60.88 -> POST /api/v1/agent/inventory 200 OK`
- Server DB (`agents`):
  - `uuid=79001ca1-70cb-4734-8f35-233bb38aec9a`
  - `software_count=1659`
  - `inventory_hash=e56135917cca0193...`

## 2026-03-03 - Inventory Unchanged + Install Regression Smoke

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - inventory hash cache'li (degismediyse yeniden submit etmeyen) surum

### Result

- Heartbeat'te inventory hash degismedigi icin yeniden submit edilmedi: OK
  - Log: `inventory unchanged: count=1659`
- Ayni calismada install task akis regression smoke: OK (`task_id=44`)

### Evidence

- Agent runtime log (`/tmp/ac-live/run_inventory_regression.log`):
  - `inventory unchanged: count=1659`
  - `task=44 start install: app_id=11 version=0.0.2 priority=8 force_update=false`
  - `task=44 install success`
- Server DB (`task_history`):
  - `id=44`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/44/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:19:31Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Heartbeat Inventory Hash + Server-Driven Sync (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18088`) heartbeat config'inde:
    - ilk heartbeat: `inventory_sync_required=true`
    - takip heartbeat'ler: `inventory_sync_required=false`
    - `inventory_scan_interval_min=60`
  - Agent mock config ile foreground calistirildi.

### Result

- Heartbeat payload'ta `inventory_hash` alaninin tasindigi dogrulandi:
  - ilk heartbeat: `inventory_hash=null` (submit oncesi)
  - sonraki heartbeat'ler: guncel hash degeri dolu
- Server-driven forced sync dogrulandi:
  - ilk heartbeat sonrasi inventory submit tetiklendi (`count=1659`)

### Evidence

- Agent runtime log (`/tmp/ac-live/run_invctl.log`):
  - `inventory submitted: count=1659`
- Mock event log (`/tmp/ac-live/mock_inventory_control_events.jsonl`):
  - heartbeat `inventory_hash` degerleri:
    - `null`
    - `e56135917cca0193c5e4b968288a2d98d9bb65f81e13af4e73ba56816771df4b`
    - `e56135917cca0193c5e4b968288a2d98d9bb65f81e13af4e73ba56816771df4b`
  - inventory post:
    - `hash=e5613591...`, `software_count=1659`

Not:
- Bu test, production servera dokunmadan kontrollu canli host ortaminda yapilmistir.

## 2026-03-03 - Normal Flow Regression Smoke (Post Inventory-Hash Heartbeat)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - heartbeat'e `inventory_hash` ekleyen ve `inventory_sync_required` config'ini dikkate alan surum

### Result

- Canli install task akis smoke: OK (`task_id=45`)
- Inventory hash cache davranisi korundu: OK
  - Log: `inventory unchanged: count=1659`
- Download/Install/Success zinciri regressionsuz: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_invhash_regression.log`):
  - `inventory unchanged: count=1659`
  - `task=45 start install: app_id=11 version=0.0.2 priority=8 force_update=false`
  - `task=45 install success`
- Server DB (`task_history`):
  - `id=45`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/45/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:23:11Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Unix Socket IPC Ping/Pong Live Validation

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Agent foreground calistirildi (`/tmp/ac-live/config.yaml`)
  - IPC socket: `/tmp/ac-live/ipc.sock`
  - Ayrica Python UNIX socket client ile `{\"action\":\"ping\"}` request gonderildi.

### Result

- IPC socket acilisi: OK
  - Log: `ipc server listening: /tmp/ac-live/ipc.sock`
- Socket request/response: OK
  - Cevap: `{\"status\":\"ok\",\"message\":\"pong\"}`

### Evidence

- Test host:
  - Socket tipi: `srw-rw-rw- ... /tmp/ac-live/ipc.sock`
  - IPC cevap kaydi:
    - `{"status":"ok","message":"pong"}`
- Agent runtime log (`/tmp/ac-live/run_ipc_ping2.log`):
  - `ipc server listening: /tmp/ac-live/ipc.sock`

## 2026-03-03 - Normal Flow Regression Smoke (Post IPC Enablement)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - Unix socket IPC server aktif + heartbeat inventory-hash/sync-hint iyilestirmeli surum

### Result

- Canli install task akis smoke: OK (`task_id=46`)
- IPC aktifken install akisi regressionsuz: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_ipc_regression.log`):
  - `ipc server listening: /tmp/ac-live/ipc.sock`
  - `task=46 start install: app_id=11 version=0.0.2 priority=8 force_update=false`
  - `task=46 install success`
- Server DB (`task_history`):
  - `id=46`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/46/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:28:06Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - IPC Store Install Action Live Validation

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Agent calisirken Unix socket uzerinden su istek gonderildi:
    - `{\"action\":\"store_install\",\"app_id\":11}`
  - Socket: `/tmp/ac-live/ipc.sock`

### Result

- IPC action routing: OK
- Agent, server store-install endpoint'ini cagirdi: OK
- IPC response alindi:
  - `{\"status\":\"already_installed\",\"message\":\"Application already installed\"}`

### Evidence

- Server journal (`appcenter` unit):
  - `10.6.60.88 -> POST /api/v1/agent/store/11/install 200 OK`
- Test host IPC output (`/tmp/ac-live/ipc_store.out`):
  - `{"status":"already_installed","message":"Application already installed"}`
- Agent runtime log:
  - `ipc server listening: /tmp/ac-live/ipc.sock`

## 2026-03-03 - Normal Flow Regression Smoke (Post IPC Store Action)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - IPC `store_install` aksiyonu aktif surum

### Result

- Canli install task akis smoke: OK (`task_id=47`)
- IPC server aktifken install akisi regressionsuz: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_ipc_store_regression.log`):
  - `ipc server listening: /tmp/ac-live/ipc.sock`
  - `task=47 start install: app_id=11 version=0.0.2 priority=8 force_update=false`
  - `task=47 install success`
- Server DB (`task_history`):
  - `id=47`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/47/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:32:36Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - IPC Store List Action Live Validation

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Agent calisirken Unix socket uzerinden ardarda iki istek gonderildi:
    - `{\"action\":\"store_list\"}`
    - `{\"action\":\"store_install\",\"app_id\":11}`
  - Socket: `/tmp/ac-live/ipc.sock`

### Result

- IPC `store_list` action: OK
  - Store listesi socket response `data` alaninda dondu.
- IPC `store_install` action: OK (onceki davranis korunuyor)
  - `already_installed` cevabi alindi.

### Evidence

- IPC output (`/tmp/ac-live/ipc_storelist.out`):
  - `STORE_LIST={"status":"ok","message":"store apps fetched: 1","data":[{"id":11,...}]}`
  - `STORE_INSTALL={"status":"already_installed","message":"Application already installed"}`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/store 200 OK`
  - `POST /api/v1/agent/store/11/install 200 OK`
- Agent runtime log:
  - `ipc server listening: /tmp/ac-live/ipc.sock`

## 2026-03-03 - Normal Flow Regression Smoke (Post IPC Store List)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Agent build:
  - IPC `store_list` + `store_install` aksiyonlu surum

### Result

- Canli install task akis smoke: OK (`task_id=48`)
- IPC aktif + store aksiyonlari aktifken install akisi regressionsuz: OK

### Evidence

- Agent runtime log (`/tmp/ac-live/run_storelist_regression.log`):
  - `ipc server listening: /tmp/ac-live/ipc.sock`
  - `task=48 start install: app_id=11 version=0.0.2 priority=8 force_update=false`
  - `task=48 install success`
- Server DB (`task_history`):
  - `id=48`, `status=success`, `message=Install completed`, `exit_code=0`
- Server journal (`appcenter` unit):
  - `GET /api/v1/agent/download/11` `200 OK`
  - `POST /api/v1/agent/task/48/status` callbacklari `200 OK`
- Test host:
  - `/tmp/ac_task_ok.txt` guncellendi (`2026-03-03T22:36:33Z`)
  - `/tmp/ac-live/downloads` bos

## 2026-03-03 - Install Queue Worker Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Test host uzerinde lokal mock API (`127.0.0.1:18081`) ayaga kaldirildi.
  - Ilk heartbeat cevabinda uzun sureli install komutu donduruldu (`task_id=9901`, payload `sleep 18`).
  - Agent `heartbeat.interval_sec=5` ile foreground calistirildi.

### Result

- Install queue davranisi: OK
  - `task=9901` heartbeat dongusunde senkron calistirilmadi, kuyruga alindi.
- Uzun install sirasinda heartbeat devam etti: OK
  - Heartbeat zamanlari: `0, 5, 10, 15, 20, 25, 30, 35` saniye.
- Task tamamlanmasi: OK (`success`)

### Evidence

- Agent runtime log (`/tmp/ac-live/run_queue_worker.log`):
  - `task=9901 start install: app_id=11 version=1.0.0 priority=1 force_update=false`
  - `task=9901 queued for install`
  - `periodic heartbeat ok: status=ok commands=0` (install devam ederken)
  - `task=9901 install success`
- Mock event ozeti (`/tmp/ac-live/mock_queue_worker_events.jsonl`):
  - `HB_COUNT=8`
  - `HB_TIMES_SEC=[0.0, 5.0, 10.0, 15.0, 20.0, 25.0, 30.0, 35.0]`
  - `STATUS_COUNT=4`, `FINAL_STATUS=success`
- Test host dosya kaniti:
  - `/tmp/ac_queue_worker_ok.txt` olustu.

## 2026-03-03 - Production Smoke (Queue Worker Build + REMOTE_SUPPORT_ENABLED=true)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` icinde `remote_support.enabled=true` dogrulandi.
  - Yeni build foreground calistirildi (`95s`).

### Result

- Production register/heartbeat akisi: OK
- `REMOTE_SUPPORT_ENABLED=true` ile ajan runtime baslangici: OK
- Bu kosuda sunucudan yeni install komutu gelmedi (`commands=0`), dolayisiyla install regresyonu bu adimda tetiklenmedi.

### Evidence

- Agent runtime log (`/tmp/ac-live/run_queue_regression_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `periodic heartbeat ok: status=ok commands=0` (tekrarlayan)
- Config kaniti:
  - `/tmp/ac-live/config.yaml` satiri: `enabled: true`

## 2026-03-03 - Install Queue Capacity Config Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18082`) ile tek heartbeat'te 4 adet install komutu donduruldu.
  - Agent `install.queue_capacity=1` ile foreground calistirildi.
  - Payload install suresi `sleep 18` olarak ayarlandi.

### Result

- `install.queue_capacity` konfig uygulandi: OK
- Queue backpressure davranisi: OK
  - Ilk iki task kuyruga alinip tamamlandi.
  - Sonraki iki task `Install queue is full` ile `failed` raporlandi.

### Evidence

- Agent runtime log (`/tmp/ac-live/run_queue_capacity.log`):
  - `linux agent install queue capacity=1`
  - `task=9911 queued for install`
  - `task=9912 queued for install`
  - `task=9913 install queue is full`
  - `task=9914 install queue is full`
  - `task=9911 install success`
  - `task=9912 install success`
- Mock event ozeti (`/tmp/ac-live/mock_queue_capacity_events.jsonl`):
  - `TASK_STATUS_TOTAL=10`
  - `QUEUE_FULL_FAILED=2` (`/api/v1/agent/task/9913/status`, `/api/v1/agent/task/9914/status`)
  - `SUCCESS_TOTAL=2`

## 2026-03-03 - Production Smoke (Queue Capacity Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` icinde `remote_support.enabled=true` dogrulandi.
  - Yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Varsayilan queue capacity runtime logu: OK (`32`)
- Bu kosuda yeni install komutu gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_queue_capacity_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue capacity=32`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Queue Status Ordering Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18083`) tek install komutu dondurdu (`task_id=9921`).
  - Agent queue worker status iyilestirmeli build ile foreground calistirildi.

### Result

- Queue ara statusu eklendi: OK
- Status siralamasi duzeltildi: OK
  - `Queued for install (5)` -> `Download started (10)` -> `Download completed (80)` -> `Install started (90)` -> `Install completed (100)`
- Worker log sirasi beklendigi gibi: OK
  - Once `queued for install`, sonra worker tarafinda `start install`.

### Evidence

- Mock event ozeti (`/tmp/ac-live/mock_queue_status_events.jsonl`):
  - `STATUSES [('downloading', 5, 'Queued for install'), ('downloading', 10, 'Download started'), ('downloading', 80, 'Download completed'), ('downloading', 90, 'Install started'), ('success', 100, 'Install completed')]`
- Agent runtime log (`/tmp/ac-live/run_queue_status.log`):
  - `task=9921 queued for install`
  - `task=9921 start install: app_id=12 version=1.0.0 priority=2 force_update=false`
  - `task=9921 install success`

## 2026-03-03 - Production Smoke (Queue Status Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` icinde `remote_support.enabled=true` korunarak yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Queue capacity runtime logu korunuyor: OK (`32`)
- Bu kosuda yeni install komutu gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_queue_capacity_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue capacity=32`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Dynamic Heartbeat CurrentStatus Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18084`) uzun sureli tek install komutu dondurdu (`task_id=9931`, `sleep 12`).
  - Agent `heartbeat.interval_sec=3` ile foreground calistirildi.
  - Mock heartbeat body icindeki `current_status` alani kaydedildi.

### Result

- Heartbeat `current_status` dinamik hesaplama: OK
  - Install devam ederken `Busy`, sonrasinda tekrar `Idle` goruldu.
- Queue/worker akis regression: OK
  - `queued for install` -> `start install` -> `install success`

### Evidence

- Mock event ozeti (`/tmp/ac-live/mock_hb_busy_events.jsonl`):
  - `HEARTBEAT_STATUS_SEQ=['Idle','Busy','Busy','Busy','Busy','Idle','Idle','Idle','Idle','Idle']`
  - `HAS_BUSY=True`
- Agent runtime log (`/tmp/ac-live/run_hb_busy.log`):
  - `task=9931 queued for install`
  - `task=9931 start install: app_id=22 version=1.0.0 priority=1 force_update=false`
  - `task=9931 install success`

## 2026-03-03 - Production Smoke (Dynamic CurrentStatus Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` (`remote_support.enabled=true`) ile yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Runtime queue capacity logu korunuyor: OK (`32`)
- Bu kosuda yeni install komutu gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_queue_capacity_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue capacity=32`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Install WorkerCount Parallelism Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18085`) ilk heartbeat'te 2 install komutu dondurdu (`task_id=9941`, `task_id=9942`).
  - Payload `sleep 10` olacak sekilde hazirlandi.
  - Agent `install.worker_count=2`, `install.queue_capacity=4` ile foreground calistirildi.

### Result

- Paralel worker calismasi: OK
  - Iki task ayni saniyede baslayip ayni saniyede tamamlandi.
- Beklenen sure kazanci: OK
  - Ilk baslangictan son basariya toplam sure `10s` (sirali olsaydi ~20s).

### Evidence

- Hesaplanan kanit (test host):
  - `SUCCESS_EVENTS=2`
  - `SUCCESS_SPREAD_SEC=0.0`
  - `RUNTIME_FROM_FIRST_START_TO_LAST_SUCCESS_SEC=10`
  - `TASK_ROWS=[('9942','start install','23:03:23'),('9941','start install','23:03:23'),('9942','install success','23:03:33'),('9941','install success','23:03:33')]`
- Agent runtime log (`/tmp/ac-live/run_install_workers.log`):
  - `linux agent install queue: capacity=4 workers=2`
  - `install worker started: id=1`
  - `install worker started: id=2`
  - `task=9941 queued for install`
  - `task=9942 queued for install`
  - `task=9942 start install ... worker_id=1`
  - `task=9941 start install ... worker_id=2`
  - `task=9942 install success`
  - `task=9941 install success`

## 2026-03-03 - Production Smoke (WorkerCount Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` (`remote_support.enabled=true`) ile yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Runtime log yeni queue formatini yansitiyor: OK (`capacity=32 workers=1`)
- Bu kosuda yeni install komutu gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_workers_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue: capacity=32 workers=1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Remote Support Env IPC Live Validation

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Agent foreground calisirken Unix socket uzerinden `{"action":"remote_support_env"}` istendi.
  - `remote_support.enabled=true` korunarak production server URL ile kosuldu.

### Result

- Yeni IPC action `remote_support_env`: OK
- x11vnc probe bilgisi socket cevabinda dondu: OK

### Evidence

- IPC output:
  - `{"status":"ok","message":"x11vnc is installed","data":{"x11vnc_path":"/usr/bin/x11vnc","installed":true}}`
- Agent runtime log (`/tmp/ac-live/run_remote_support_env.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `ipc server listening: /tmp/ac-live/ipc.sock`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Test Host Tooling Update (x11vnc)

- Host: `10.6.60.88` (`ubuntu`)
- Kurulan ek arac:
  - `x11vnc` (`/usr/bin/x11vnc`)
- Kurulum yontemi:
  - `sudo apt-get update -y`
  - `sudo apt-get install -y x11vnc`
- Not:
  - Linux agent remote support gelistirmeleri icin test hostta gerekli ek arac kurulumlari onay beklemeden uygulanir.

## 2026-03-03 - Remote Support Manager IPC (status/start/stop) Live Validation

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Agent production config ile foreground calistirildi (`remote_support.enabled=true`).
  - IPC uzerinden sirasiyla aksiyonlar gonderildi:
    - `remote_support_status`
    - `remote_support_start`
    - `remote_support_status`
    - `remote_support_stop`
    - `remote_support_status`

### Result

- `remote_support_status`: OK
  - x11vnc kurulu/path/durum bilgisi dondu.
- `remote_support_start`: OK
  - x11vnc process baslatildi (`pid` dondu).
- `remote_support_stop`: OK
  - Process state temiz sekilde `running=false` oldu.
- Test hostta aktif X11 display olmadigi icin x11vnc beklendigi gibi `exit status 1` ile sonlandi ve `last_error` alanina yansidi.

### Evidence

- IPC output:
  - `STATUS_1 {"status":"ok","message":"remote support status","data":{"installed":true,"x11vnc_path":"/usr/bin/x11vnc","running":false,"display":":0","port":5900}}`
  - `START {"status":"ok","message":"remote support started","data":{"installed":true,"x11vnc_path":"/usr/bin/x11vnc","running":true,"pid":20131,"display":":0","port":5900,...}}`
  - `STATUS_2 ... "running":false ... "last_error":"exit status 1"`
  - `STOP {"status":"ok","message":"remote support stopped",...}`
  - `STATUS_3 ... "running":false ... "last_error":"exit status 1"`
- Agent runtime log (`/tmp/ac-live/run_remote_support_manager.log`):
  - `remote support x11vnc started: pid=20131 display=:0 port=5900`
  - `remote support x11vnc exited with error: exit status 1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Remote Support Session Approval State Flow Live Validation

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Agent production config ile foreground calistirildi (`remote_support.enabled=true`).
  - IPC uzerinden session state aksiyonlari sirali test edildi:
    - `remote_support_session_request`
    - `remote_support_status`
    - `remote_support_reject`
    - ikinci `remote_support_session_request`
    - `remote_support_approve`
    - `remote_support_status`
    - `remote_support_end`

### Result

- Session state machine akisi: OK
  - `idle -> pending_approval -> rejected`
  - `pending_approval -> active -> error -> ended`
- Approve sonrasi daemon dususunde otomatik session error gecisi: OK
  - `remote_support_status` cagrisiyla `daemon.last_error` (`exit status 1`) session'a yansitildi.

### Evidence

- IPC outputs (ozet):
  - `STATUS_0 ... "session":{"state":"idle"}`
  - `REQ_1 ... "session":{"state":"pending_approval","session_id":7001,...}`
  - `REJECT_1 ... "session":{"state":"rejected",...,"last_error":"user rejected"}`
  - `REQ_2 ... "session":{"state":"pending_approval","session_id":7002,...}`
  - `APPROVE_2 ... "session":{"state":"active",...}`
  - `STATUS_3 ... "session":{"state":"error",...,"last_error":"exit status 1"}`
  - `END_2 ... "session":{"state":"ended",...}`
- Agent runtime log (`/tmp/ac-live/run_remote_support_session.log`):
  - `remote support x11vnc started: pid=20336 display=:0 port=5900`
  - `remote support x11vnc exited with error: exit status 1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Remote Support Agent API Integration Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18086`) ile agent remote-support endpointleri taklit edildi.
  - Agent IPC uzerinden su akislar calistirildi:
    - `remote_support_session_request(session_id=8101)` + `remote_support_reject`
    - `remote_support_session_request(session_id=8102)` + `remote_support_approve` + `remote_support_end`
  - Mock, endpoint hit'lerini ve heartbeat `remote_support` payload'ini kaydetti.

### Result

- Agent -> Server remote-support callback entegrasyonu: OK
  - Reject akisi: `POST /agent/remote-support/8101/approve` body `{"approved": false}`
  - Approve akisi: `POST /agent/remote-support/8102/approve` body `{"approved": true}`
  - Ready callback: `POST /agent/remote-support/8102/ready` body `{"vnc_ready": true}`
  - End callback: `POST /agent/remote-support/8102/ended` body `{"ended_by":"agent","reason":"ended from ipc"}`
- Heartbeat `remote_support` payload gonderimi: OK

### Evidence

- Mock event ozeti (`/tmp/ac-live/mock_remote_support_api_events.jsonl`):
  - `REMOTE_API_CALLS=[('/api/v1/agent/remote-support/8101/approve', {'approved': False}), ('/api/v1/agent/remote-support/8102/approve', {'approved': True}), ('/api/v1/agent/remote-support/8102/ready', {'vnc_ready': True}), ('/api/v1/agent/remote-support/8102/ended', {'ended_by': 'agent', 'reason': 'ended from ipc'})]`
  - `HB_REMOTE_SUPPORT_SAMPLE=[{'state':'active','session_id':8102,'helper_running':False}, {'state':'ended','session_id':8102,'helper_running':False}, ...]`
- Agent runtime log (`/tmp/ac-live/run_remote_support_api.log`):
  - `remote support x11vnc started: pid=20498 display=:0 port=5900`
  - `remote support x11vnc exited with error: exit status 1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Production Smoke (Remote Support API Integration Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` (`remote_support.enabled=true`) ile yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Install queue runtime logu korundu: OK
- Bu kosuda yeni komut gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_workers_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue: capacity=32 workers=1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Heartbeat RemoteSupport Request/End Signal Handling (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18087`) heartbeat response alanlarini kontrollu dondurdu:
    - 1. heartbeat: `remote_support_request={session_id:8201,...}`
    - 3. heartbeat: `remote_support_end={session_id:8201}`
  - Agent bu mock config ile foreground calistirildi.

### Result

- Heartbeat `remote_support_request` isleme: OK
  - Session pending state'e alindi.
- Heartbeat `remote_support_end` isleme: OK
  - Agent end sinyalini isleyip `ended` callback gonderdi.

### Evidence

- Agent runtime log (`/tmp/ac-live/run_remote_support_hb_signal.log`):
  - `remote support request received: session_id=8201`
  - `remote support end signal handled: session_id=8201`
  - `periodic heartbeat ok: status=ok commands=0`
- Mock event ozeti (`/tmp/ac-live/mock_remote_support_hb_signal_events.jsonl`):
  - `ENDED_CALLBACK_COUNT=1`

## 2026-03-03 - Remote Support Feature-Flag Guard Live Validation (Controlled Mock)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18088`) heartbeat config icinde `remote_support_enabled=false` dondurdu.
  - Agent local IPC ile `remote_support_session_request(session_id=8301)` tetiklendi.
  - Sonraki heartbeat'lerde feature kapali oldugu icin guard davranisi izlendi.

### Result

- Feature kapali iken heartbeat'ten gelen remote-support request ignore edildi: OK
- Feature kapali iken mevcut pending session sonlandirildi: OK
- Agent server'a `ended` callback gonderdi: OK

### Evidence

- IPC output:
  - `STATUS_A ... "session":{"state":"pending_approval","session_id":8301,...}`
  - `STATUS_B ... "session":{"state":"ended","session_id":8301,...,"message":"disabled by config"}`
- Mock event ozeti (`/tmp/ac-live/mock_remote_support_disabled_events.jsonl`):
  - `ENDED_CALLBACK_COUNT=1`
- Agent runtime log (`/tmp/ac-live/run_remote_support_disabled.log`):
  - `remote support request ignored: feature disabled`
  - `remote support session terminated because feature disabled`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Production Smoke (Remote Support Feature-Flag Guard Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` (`remote_support.enabled=true`) ile yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Install queue runtime logu korundu: OK
- Bu kosuda yeni komut gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_workers_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue: capacity=32 workers=1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Remote Support Session State Persistence Across Restart

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Ayrı state dosyali config ile agent calistirildi (`/tmp/ac-live/state_remote_support_persist.json`).
  - IPC ile `remote_support_session_request(session_id=8401)` tetiklendi (pending state).
  - Agent durdurulup ayni config ile tekrar baslatildi.
  - Restart sonrasi IPC `remote_support_status` ile state dogrulandi.

### Result

- Remote support session state persistence: OK
  - Run#1 sonunda state dosyasinda `pending_approval` session kaydi yazildi.
  - Run#2 baslangicinda session state state dosyasindan restore edildi.

### Evidence

- Run#1 IPC outputs:
  - `REQ1 ... "session":{"state":"pending_approval","session_id":8401,...}`
  - `STATUS1 ... "session":{"state":"pending_approval","session_id":8401,...}`
- State file (`/tmp/ac-live/state_remote_support_persist.json`):
  - `remote_support_session={"state":"pending_approval","session_id":8401,"admin_name":"persist-admin",...}`
- Run#2 IPC output:
  - `STATUS2 ... "session":{"state":"pending_approval","session_id":8401,...}`
- Run#2 log (`/tmp/ac-live/run_remote_support_persist_2.log`):
  - `remote support session restored: state=pending_approval session_id=8401`

## 2026-03-03 - Production Smoke (Remote Support Session Persistence Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` (`remote_support.enabled=true`) ile yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Install queue runtime logu korundu: OK
- Bu kosuda yeni komut gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_workers_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue: capacity=32 workers=1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Remote Support Pending Session Startup Timeout Guard

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Ayrı config ile `remote_support.approval_timeout_sec=5` ayarlandi.
  - State dosyasi elle stale pending session ile preload edildi (`requested_at_unix=1700000000`, `session_id=8501`).
  - Agent baslatildi ve IPC `remote_support_status` ile durum kontrol edildi.

### Result

- Startup timeout guard: OK
  - Restore edilen stale pending session otomatik `ended` durumuna cekildi.
  - Mesaj: `approval timeout after restart`.
- State file persistence: OK
  - Guncel ended state state dosyasina yazildi.

### Evidence

- IPC output:
  - `STATUS ... "session":{"state":"ended","session_id":8501,...,"message":"approval timeout after restart"}`
- State file (`/tmp/ac-live/state_remote_support_startup_timeout.json`):
  - `remote_support_session={"state":"ended","session_id":8501,...,"message":"approval timeout after restart"}`
- Agent runtime log (`/tmp/ac-live/run_remote_support_startup_timeout.log`):
  - `remote support session restored: state=pending_approval session_id=8501`
  - `remote support session expired on startup: session_id=8501`

## 2026-03-03 - Production Smoke (Startup Timeout Guard Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` (`remote_support.enabled=true`) ile yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Install queue runtime logu korundu: OK
- Bu kosuda yeni komut gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_workers_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue: capacity=32 workers=1`
  - `periodic heartbeat ok: status=ok commands=0`

## 2026-03-03 - Remote Support Approve MonitorCount Payload Live Validation

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test setup:
  - Lokal mock API (`127.0.0.1:18089`) approve endpoint body'sini kaydetti.
  - IPC akisi:
    - `remote_support_session_request(session_id=8601)`
    - `remote_support_approve(monitor_count=2)`

### Result

- IPC -> Agent API `monitor_count` tasima: OK
- Approve callback body dogrulandi: `{"approved": true, "monitor_count": 2}`

### Evidence

- IPC output:
  - `APPROVE ... "session":{"state":"active","session_id":8601,...}`
- Mock event ozeti (`/tmp/ac-live/mock_remote_support_monitor_events.jsonl`):
  - `APPROVE_PAYLOADS=[{'approved': True, 'monitor_count': 2}]`

## 2026-03-03 - Production Smoke (MonitorCount Support Build)

- Test host:
  - IP: `10.6.60.88`
  - User: `ubuntu`
- Test server URL: `http://10.6.100.170:8000`
- Test setup:
  - `/tmp/ac-live/config.yaml` (`remote_support.enabled=true`) ile yeni build foreground calistirildi (`55s`).

### Result

- Production heartbeat akisi: OK
- Install queue runtime logu korundu: OK
- Bu kosuda yeni komut gelmedi (`commands=0`).

### Evidence

- Agent runtime log (`/tmp/ac-live/run_workers_prod.log`):
  - `linux agent runtime: ipc=/tmp/ac-live/ipc.sock remote_support_enabled=true`
  - `linux agent install queue: capacity=32 workers=1`
  - `periodic heartbeat ok: status=ok commands=0`
