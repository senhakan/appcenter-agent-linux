# Release Notes (2026-03-09) - Linux Remote Support Stability

## Version
- `0.1.53-live`

## Scope
- Linux remote-support stability fix for desktop sessions where agent runs as `root`.

## Change
- `internal/remotesupport/manager_linux.go`
  - Added `-noshm` to x11vnc launch arguments.

## Why
- Some Linux desktops reject MIT-SHM attach from root-owned x11vnc process.
- This produced immediate helper exit and server-side `Connection refused` on `:20010`.

## Validation Summary
- Publish metadata updated to:
  - `agent_latest_version_linux=0.1.53-live`
  - `agent_update_filename_linux=agent_linux_0.1.53-live_29812418.bin`
  - `agent_hash_linux=sha256:298124189057400e17779a6e28eff7110d0b512b92bd1b615a27fad0e0768c1d`
- Live broadcast (`self_update`, `mode=normal`) delivered to online agents.
- Online Linux agents reached latest version.
- Test host `10.6.60.88` remote-support session reached `active` and `10.6.60.88:20010` became reachable.
