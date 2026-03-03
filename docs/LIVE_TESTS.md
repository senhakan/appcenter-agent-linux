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
