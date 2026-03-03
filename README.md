# AppCenter Linux Agent

Bu dizin, AppCenter Linux agent gelistirme ana proje dizinidir.

## Hedef platform
- Ubuntu 24.04 LTS
- Pardus 25.0 (Debian tabanli)

## Mimari hedefi
- Headless systemd service
- IPC: Unix Domain Socket (`/var/run/appcenter-agent/ipc.sock`)
- Remote support helper: `x11vnc` (yalnizca X11)
- Build stratejisi: `//go:build linux` + `_linux.go`

## Ilk Faz (hazirlik)
- Mevcut server + windows agent kontratlarinin platform-aware hale getirilmesi
- Linux agent kod iskeletinin olusturulmasi
- deb paketleme altyapisinin kurulmasi

## Referans dokumanlar
- `/root/appcenter/server/docs/LINUX_AGENT_DETAILED_PLAN.md`
- `/root/appcenter/AppCenter_Technical_Specification_v1_1.md`
- `/root/appcenter/PLAN.md`
- `/root/appcenter/REMOTE_SUPPORT_PLAN.md`

## Canli Test Ortami

- Son dogrulanan erisim tarihi: 2026-03-03
- Linux agent canli test baglanti bilgisi:
  - IP: `10.6.60.88`
  - SSH kullanici: `ubuntu`
  - SSH sifre: `1234asd!!!`
- Not: Linux agent canli testleri bu baglanti uzerinden yurutulecektir.

# appcenter-agent-linux
