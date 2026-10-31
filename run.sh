#!/usr/bin/env sh
set -eu
CONF_FILE="${CONF_FILE:-/root/conf/config.yaml}"
MIHOMO_HOME="${MIHOMO_HOME:-/root/.config/mihomo}"
WEB_CONFIG_PORT="${WEB_CONFIG_PORT:-9097}"
[ -s "$(dirname "$CONF_FILE")/auth.json" ] || [ -n "${WEB_CONFIG_SECRET:-${EXTERNAL_SECRET:-}}" ] || { echo '请设置 WEB_CONFIG_SECRET 登录密码' >&2; exit 1; }
mkdir -p "$(dirname "$CONF_FILE")" "$MIHOMO_HOME"
# Prepare from persisted profiles; restarting never replaces a good config with a fresh download.
mymihomo prepare
/mihomo -d "$MIHOMO_HOME" -f "$CONF_FILE" &
CORE_PID=$!
mymihomo serve -addr ":${WEB_CONFIG_PORT}" -c "$CONF_FILE" &
WEB_PID=$!
cleanup() {
  kill "$CORE_PID" "$WEB_PID" 2>/dev/null || true
  wait "$CORE_PID" 2>/dev/null || true
  wait "$WEB_PID" 2>/dev/null || true
}
trap 'cleanup; exit 0' TERM INT
# Restart the container if either required process exits.
while kill -0 "$CORE_PID" 2>/dev/null && kill -0 "$WEB_PID" 2>/dev/null; do
  sleep 2 &
  wait $! || true
done
cleanup
exit 1
