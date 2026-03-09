# Release Notes - 2026-03-09 (Linux Agent Force Self-Update)

## Summary
- Added `mode` support to broadcast self-update handling.
- Linux self-update now supports `force` mode for same-version apply.

## Implementation
- Broadcast self-update handler now reads payload `mode`.
- `maybeApplySelfUpdate(..., mode, ...)`:
  - In `normal` mode: keeps previous same-version skip behavior.
  - In `force` mode: allows same-version update pipeline (download, verify, swap, restart).

## Safety
- SHA256 verification remains mandatory before activation.
- Existing backup/swap/restart behavior is preserved.

## Live Validation
- Live evidence captured for:
  - `self-update detected ... mode=force`
  - `self-update applied ...`
  - service restart with successful recovery.
