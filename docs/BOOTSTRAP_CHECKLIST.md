# Linux Agent Bootstrap Checklist

## Tamamlanan hazirliklar
- [x] Linux agent proje kok dizini olusturuldu: `/root/appcenter/agent_linux`
- [x] Temel dizin iskeleti olusturuldu (`cmd`, `internal`, `pkg`, `configs`, `packaging/debian`, `scripts`, `docs`)
- [x] Baslangic README olusturuldu

## Gelistirme baslangicinda ilk adimlar
- [ ] `go.mod` olustur
- [ ] `cmd/service/main.go` Linux service entrypoint
- [ ] `internal/ipc` icin unix socket server/client
- [ ] `internal/system` Linux host/profile/sessions implementasyonu
- [ ] `internal/remotesupport` x11vnc launcher + state
- [ ] `internal/downloader`, `internal/installer`, `internal/heartbeat` parity portu
- [ ] `packaging/debian` icin `postinst`, systemd unit, config yerlestirme

