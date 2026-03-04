#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  echo "usage: $0 <version>" >&2
  echo "example: $0 0.1.36-live" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_BIN="${ROOT_DIR}/build/appcenter-agent-linux"
UPLOAD_DIR="${UPLOAD_DIR:-/var/lib/appcenter/uploads/agent_updates}"
SERVER_DIR="${SERVER_DIR:-/opt/appcenter/server}"

echo "[publish-self-update] building linux agent binary"
cd "${ROOT_DIR}"
go build -o "${BUILD_BIN}" ./cmd/service

SHA="$(sha256sum "${BUILD_BIN}" | awk '{print $1}')"
SHORT="${SHA:0:8}"
FILENAME="agent_linux_${VERSION}_${SHORT}.bin"
DEST="${UPLOAD_DIR}/${FILENAME}"
DOWNLOAD_URL="/api/v1/agent/update/download/${FILENAME}"

echo "[publish-self-update] installing file: ${DEST}"
install -o appcenter -g appcenter -m 0755 "${BUILD_BIN}" "${DEST}"

echo "[publish-self-update] updating DB settings"
cd "${SERVER_DIR}"
"${SERVER_DIR}/venv/bin/python" - <<PY
from datetime import datetime, timezone
from app.database import SessionLocal
from app.models import Setting

pairs = {
    "agent_latest_version_linux": "${VERSION}",
    "agent_download_url_linux": "${DOWNLOAD_URL}",
    "agent_hash_linux": "sha256:${SHA}",
    "agent_update_filename_linux": "${FILENAME}",
}
now = datetime.now(timezone.utc)
db = SessionLocal()
try:
    for key, value in pairs.items():
        item = db.query(Setting).filter(Setting.key == key).first()
        if not item:
            item = Setting(key=key, value=value, description="Agent update metadata", updated_at=now)
        else:
            item.value = value
            item.updated_at = now
        db.add(item)
    db.commit()
finally:
    db.close()
print("settings_updated")
PY

echo "[publish-self-update] done"
echo "  version=${VERSION}"
echo "  file=${FILENAME}"
echo "  sha256=${SHA}"
echo "  download_url=${DOWNLOAD_URL}"
