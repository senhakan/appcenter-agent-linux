#!/usr/bin/env bash
set -euo pipefail

UPLOAD_DIR="${UPLOAD_DIR:-/var/lib/appcenter/uploads/agent_linux}"
PUBLIC_BASE_URL="${PUBLIC_BASE_URL:-http://10.6.100.170:8000/uploads/agent_linux}"
BIN_NAME="appcenter-agent-linux"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_BIN="${ROOT_DIR}/build/${BIN_NAME}"
BOOTSTRAP_SRC="${ROOT_DIR}/scripts/bootstrap_client.sh"
BOOTSTRAP_DST="${UPLOAD_DIR}/bootstrap.sh"
BIN_DST="${UPLOAD_DIR}/${BIN_NAME}"
SHA_DST="${UPLOAD_DIR}/${BIN_NAME}.sha256"

echo "[publish] root=${ROOT_DIR}"
echo "[publish] building linux agent binary"
mkdir -p "${ROOT_DIR}/build"
go -C "${ROOT_DIR}" build -o "${BUILD_BIN}" ./cmd/service

echo "[publish] preparing upload dir: ${UPLOAD_DIR}"
mkdir -p "${UPLOAD_DIR}"
install -m 0755 "${BUILD_BIN}" "${BIN_DST}"
install -m 0755 "${BOOTSTRAP_SRC}" "${BOOTSTRAP_DST}"
sha256sum "${BIN_DST}" | awk '{print $1}' > "${SHA_DST}"

SHA_VAL="$(cat "${SHA_DST}")"
echo "[publish] binary sha256=${SHA_VAL}"
echo "[publish] bootstrap url: ${PUBLIC_BASE_URL}/bootstrap.sh"
echo "[publish] binary url:    ${PUBLIC_BASE_URL}/${BIN_NAME}"
echo "[publish] sha url:       ${PUBLIC_BASE_URL}/${BIN_NAME}.sha256"
echo "[publish] client one-liner:"
echo "curl -fsSL ${PUBLIC_BASE_URL}/bootstrap.sh | sudo bash -s -- --server-url http://10.6.100.170:8000"
