#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="${SERVER_URL:-}"
BUNDLE_BASE_URL="${BUNDLE_BASE_URL:-}"
BINARY_URL="${BINARY_URL:-}"
SHA256_URL="${SHA256_URL:-}"
AGENT_UUID="${AGENT_UUID:-}"
AGENT_SECRET="${AGENT_SECRET:-}"
AGENT_VERSION="${AGENT_VERSION:-0.1.0-live}"
HEARTBEAT_INTERVAL_SEC="${HEARTBEAT_INTERVAL_SEC:-20}"
RS_ENABLED="${RS_ENABLED:-true}"
RS_APPROVAL_TIMEOUT_SEC="${RS_APPROVAL_TIMEOUT_SEC:-30}"
RS_DISPLAY="${RS_DISPLAY:-:0}"
RS_PORT="${RS_PORT:-20010}"
DISABLE_WAYLAND_GDM="${DISABLE_WAYLAND_GDM:-true}"

BIN_PATH="/opt/appcenter-agent/appcenter-agent-linux"
CONFIG_PATH="/etc/appcenter-agent/config.yaml"
STATE_PATH="/var/lib/appcenter-agent/state.json"
LOG_PATH="/var/log/appcenter-agent/agent.log"
SOCKET_PATH="/var/run/appcenter-agent/ipc.sock"
TMP_BIN="/tmp/appcenter-agent-linux.new.$$"
TMP_SHA="/tmp/appcenter-agent-linux.sha256.$$"
SERVICE_NAME="appcenter-agent.service"

usage() {
  cat <<'USAGE'
Usage: bootstrap_client.sh --server-url <http://SERVER:8000> [options]

Options:
  --server-url URL
  --bundle-base-url URL             (default: <server-url>/uploads/agent_linux)
  --binary-url URL                  (default: <bundle-base-url>/appcenter-agent-linux)
  --sha256-url URL                  (default: <bundle-base-url>/appcenter-agent-linux.sha256)
  --agent-uuid UUID                 (optional; empty -> keep existing or generate)
  --agent-secret SECRET             (optional; usually empty for first registration)
  --agent-version VERSION           (default: 0.1.0-live)
  --heartbeat-interval-sec N        (default: 20)
  --rs-enabled true|false           (default: true)
  --rs-approval-timeout-sec N       (default: 30)
  --rs-display :0                   (default: :0)
  --rs-port N                       (default: 20010)
  --disable-wayland-gdm true|false  (default: true)
USAGE
}

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    if command -v sudo >/dev/null 2>&1; then
      exec sudo -E bash "$0" "$@"
    fi
    echo "[bootstrap] root or sudo required" >&2
    exit 1
  fi
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --server-url) SERVER_URL="${2:-}"; shift 2 ;;
      --bundle-base-url) BUNDLE_BASE_URL="${2:-}"; shift 2 ;;
      --binary-url) BINARY_URL="${2:-}"; shift 2 ;;
      --sha256-url) SHA256_URL="${2:-}"; shift 2 ;;
      --agent-uuid) AGENT_UUID="${2:-}"; shift 2 ;;
      --agent-secret) AGENT_SECRET="${2:-}"; shift 2 ;;
      --agent-version) AGENT_VERSION="${2:-}"; shift 2 ;;
      --heartbeat-interval-sec) HEARTBEAT_INTERVAL_SEC="${2:-}"; shift 2 ;;
      --rs-enabled) RS_ENABLED="${2:-}"; shift 2 ;;
      --rs-approval-timeout-sec) RS_APPROVAL_TIMEOUT_SEC="${2:-}"; shift 2 ;;
      --rs-display) RS_DISPLAY="${2:-}"; shift 2 ;;
      --rs-port) RS_PORT="${2:-}"; shift 2 ;;
      --disable-wayland-gdm) DISABLE_WAYLAND_GDM="${2:-}"; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) echo "[bootstrap] unknown arg: $1" >&2; usage; exit 1 ;;
    esac
  done
}

ensure_server_vars() {
  if [[ -z "${SERVER_URL}" ]]; then
    echo "[bootstrap] --server-url is required" >&2
    usage
    exit 1
  fi
  SERVER_URL="${SERVER_URL%/}"
  if [[ -z "${BUNDLE_BASE_URL}" ]]; then
    BUNDLE_BASE_URL="${SERVER_URL}/uploads/agent_linux"
  fi
  BUNDLE_BASE_URL="${BUNDLE_BASE_URL%/}"
  if [[ -z "${BINARY_URL}" ]]; then
    BINARY_URL="${BUNDLE_BASE_URL}/appcenter-agent-linux"
  fi
  if [[ -z "${SHA256_URL}" ]]; then
    SHA256_URL="${BUNDLE_BASE_URL}/appcenter-agent-linux.sha256"
  fi
}

install_packages() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y
  apt-get install -y \
    ca-certificates curl procps uuid-runtime \
    x11vnc zenity xauth x11-xserver-utils \
    dbus-user-session dbus-x11
}

resolve_uuid() {
  if [[ -n "${AGENT_UUID}" ]]; then
    return 0
  fi
  if [[ -f "${CONFIG_PATH}" ]]; then
    local existing
    existing="$(awk -F\" '/^[[:space:]]*uuid:/{print $2}' "${CONFIG_PATH}" | head -n 1 || true)"
    if [[ -n "${existing}" ]]; then
      AGENT_UUID="${existing}"
      return 0
    fi
  fi
  AGENT_UUID="$(uuidgen | tr '[:upper:]' '[:lower:]')"
}

download_and_verify_binary() {
  echo "[bootstrap] downloading agent binary: ${BINARY_URL}"
  curl -fsSL "${BINARY_URL}" -o "${TMP_BIN}"
  echo "[bootstrap] downloading sha256: ${SHA256_URL}"
  curl -fsSL "${SHA256_URL}" -o "${TMP_SHA}"
  local expected actual
  expected="$(awk '{print $1}' "${TMP_SHA}" | head -n 1)"
  actual="$(sha256sum "${TMP_BIN}" | awk '{print $1}')"
  if [[ -z "${expected}" || "${expected}" != "${actual}" ]]; then
    echo "[bootstrap] sha256 mismatch expected=${expected} actual=${actual}" >&2
    exit 1
  fi
  install -d -m 0755 /opt/appcenter-agent
  install -m 0755 "${TMP_BIN}" "${BIN_PATH}"
  rm -f "${TMP_BIN}" "${TMP_SHA}"
}

write_config() {
  install -d -m 0755 /etc/appcenter-agent /var/lib/appcenter-agent /var/log/appcenter-agent /var/run/appcenter-agent
  cat > "${CONFIG_PATH}" <<EOF
server:
  url: "${SERVER_URL}"
agent:
  uuid: "${AGENT_UUID}"
  secret_key: "${AGENT_SECRET}"
  version: "${AGENT_VERSION}"
heartbeat:
  interval_sec: ${HEARTBEAT_INTERVAL_SEC}
logging:
  file: "${LOG_PATH}"
paths:
  state_file: "${STATE_PATH}"
ipc:
  socket_path: "${SOCKET_PATH}"
remote_support:
  enabled: ${RS_ENABLED}
  approval_timeout_sec: ${RS_APPROVAL_TIMEOUT_SEC}
  display: "${RS_DISPLAY}"
  port: ${RS_PORT}
download:
  temp_dir: /var/lib/appcenter-agent/downloads
EOF
  chmod 0644 "${CONFIG_PATH}"
}

maybe_disable_wayland_gdm() {
  local ans
  ans="$(echo "${DISABLE_WAYLAND_GDM}" | tr '[:upper:]' '[:lower:]')"
  if [[ "${ans}" != "true" ]]; then
    return 0
  fi
  if [[ ! -f /etc/gdm3/custom.conf ]]; then
    return 0
  fi
  if grep -Eq '^[[:space:]]*WaylandEnable=false' /etc/gdm3/custom.conf; then
    return 0
  fi
  if grep -Eq '^[[:space:]]*#?[[:space:]]*WaylandEnable=' /etc/gdm3/custom.conf; then
    sed -i 's/^[[:space:]]*#\?[[:space:]]*WaylandEnable=.*/WaylandEnable=false/' /etc/gdm3/custom.conf
  else
    printf '\n[daemon]\nWaylandEnable=false\n' >> /etc/gdm3/custom.conf
  fi
  echo "[bootstrap] gdm Wayland disabled for x11vnc compatibility (re-login/reboot may be required)"
}

write_service() {
  cat > "/etc/systemd/system/${SERVICE_NAME}" <<EOF
[Unit]
Description=AppCenter Linux Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN_PATH} -config ${CONFIG_PATH}
Restart=always
RestartSec=2
User=root
WorkingDirectory=/opt/appcenter-agent

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "/etc/systemd/system/${SERVICE_NAME}"
}

start_service() {
  systemctl daemon-reload
  systemctl enable --now "${SERVICE_NAME}"
  systemctl restart "${SERVICE_NAME}"
  sleep 2
  systemctl is-active --quiet "${SERVICE_NAME}" || {
    echo "[bootstrap] service failed to start" >&2
    systemctl status "${SERVICE_NAME}" --no-pager -n 80 || true
    exit 1
  }
}

health_check() {
  echo "[bootstrap] checking server health: ${SERVER_URL}/health"
  curl -fsSL "${SERVER_URL}/health" >/dev/null
}

main() {
  parse_args "$@"
  need_root "$@"
  ensure_server_vars
  health_check
  install_packages
  resolve_uuid
  download_and_verify_binary
  write_config
  maybe_disable_wayland_gdm
  write_service
  start_service
  echo "[bootstrap] OK"
  echo "[bootstrap] service: ${SERVICE_NAME}"
  echo "[bootstrap] config: ${CONFIG_PATH}"
  echo "[bootstrap] uuid: ${AGENT_UUID}"
  journalctl -u "${SERVICE_NAME}" --no-pager -n 30 || true
}

main "$@"
