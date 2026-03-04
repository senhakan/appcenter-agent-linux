# Linux Agent Deploy / Rollback Runbook

## Scope
Bu runbook, Linux agent binary deploy, smoke validation ve rollback adimlarini standartlastirir.

## Chat Komut Protokolu
- `+2` komutu: "Plana gore islemlere onaysiz devam et" anlamina gelir.
- Bu komutla birlikte is akisi durdurulmadan su zincir takip edilir:
  - kod degisikligi
  - test
  - canli dogrulama
  - dokumantasyon guncelleme
  - commit/push

## Ortam
- Test host: `10.6.60.88`
- SSH user: `ubuntu`
- SSH pass: `1234asd!!!`
- Remote binary: `/tmp/ac-live/appcenter-agent-linux`
- Remote config: `/tmp/ac-live/config.yaml`

## Pre-deploy Checklist
- `main` branch guncel ve pushlanmis olmali.
- Lokal testler gecmeli: `go test ./...`
- Binary build alinmali: `go build -o build/service ./cmd/service`
- `docs/LIVE_TESTS.md` son degisiklikler ile uyumlu olmali.

## Deploy (Smoke)
1. Otomasyon scripti ile deploy + smoke:
```bash
./scripts/live_smoke.sh
```
2. Script tamamlandiginda su kontroller yapilmis olur:
- Binary SHA local/remote eslesir.
- Agent foreground kosusunda runtime + heartbeat loglari gorulur.

## Remote Support Regression (opsiyonel ama onerilen)
```bash
./scripts/live_regression_remote_support.sh
```
Dogrulananlar:
- IPC `code` semasi (`ok`, `unsupported_action`)
- 4xx callback non-retry davranisi

## Manuel Dogrulama (gerekirse)
- Son log satirlari:
```bash
sshpass -p '1234asd!!!' ssh ubuntu@10.6.60.88 "tail -n 80 /tmp/ac-live/run_smoke_automation.log"
```
- Binary hash:
```bash
sha256sum build/service
sshpass -p '1234asd!!!' ssh ubuntu@10.6.60.88 "sha256sum /tmp/ac-live/appcenter-agent-linux"
```

## Rollback
1. Son iyi binary yolunu belirle.
- Oneri: deploy oncesi binary'yi tarihle sakla (`/tmp/ac-live/appcenter-agent-linux.<ts>`)
2. Geri donus:
```bash
sshpass -p '1234asd!!!' ssh ubuntu@10.6.60.88 "cp /tmp/ac-live/appcenter-agent-linux.<ts> /tmp/ac-live/appcenter-agent-linux"
```
3. Smoke tekrar:
```bash
./scripts/live_smoke.sh
```

## Failure Handling
- Deploy script SHA mismatch verirse deploy durdurulur.
- Smoke heartbeat gorulmezse deploy basarisiz sayilir.
- Regression scriptte non-retry assert fail olursa ilgili commit geri alinmadan yeni deploy yapilmaz.

## Sonraki Islem
- Basarili deploy sonrasi test kanitini `docs/LIVE_TESTS.md` icine tarihli bolum olarak ekle.

## Scripted Deploy / Rollback

- Yedekli deploy:
```bash
./scripts/deploy_with_backup.sh
```
- Son yedege rollback:
```bash
./scripts/rollback_last.sh
```

Not:
- Her deploy oncesinde remote binary timestamp ile yedeklenir.
- Son yedek yolu `${REMOTE_BIN}.last_backup` dosyasina yazilir.

## Auto Rollback Davranisi
- `deploy_with_backup.sh` varsayilan olarak `AUTO_ROLLBACK_ON_FAIL=1` ile calisir.
- Smoke fail olursa son olusturulan backup otomatik geri yuklenir.
- Bu davranisi kapatmak icin:
```bash
AUTO_ROLLBACK_ON_FAIL=0 ./scripts/deploy_with_backup.sh
```
