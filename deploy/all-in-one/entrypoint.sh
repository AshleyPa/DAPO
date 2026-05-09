#!/bin/sh
set -eu

: "${KLEIN_API_PORT:=17180}"
: "${KLEIN_ADMIN_PORT:=17188}"
: "${KLEIN_OPENAI_PORT:=17200}"
: "${KLEIN_LOG_DIR:=/app/logs}"
: "${KLEIN_STORAGE_ROOT:=/app/storage}"
: "${KLEIN_MIHOMO_HOME:=/app/private/mihomo}"
: "${DAPO_GATEWAY_PORT:=3040}"

mkdir -p "$KLEIN_LOG_DIR" "$KLEIN_STORAGE_ROOT" "$KLEIN_MIHOMO_HOME"

if [ ! -f "$KLEIN_MIHOMO_HOME/config.yaml" ]; then
  cat > "$KLEIN_MIHOMO_HOME/config.yaml" <<'EOF'
allow-lan: false
bind-address: 127.0.0.1
external-controller: 127.0.0.1:9090
log-level: warning
proxies: []
proxy-groups: []
listeners: []
rules:
  - MATCH,DIRECT
EOF
fi

term_children() {
  if [ "${PIDS:-}" != "" ]; then
    kill -TERM $PIDS 2>/dev/null || true
  fi
}

trap 'term_children; exit 143' TERM INT

/app/api &
PIDS="$!"

/app/admin &
PIDS="$PIDS $!"

/app/openai &
PIDS="$PIDS $!"

/app/worker &
PIDS="$PIDS $!"

if command -v mihomo >/dev/null 2>&1; then
  mihomo -d "$KLEIN_MIHOMO_HOME" &
  PIDS="$PIDS $!"
fi

/docker-entrypoint.sh nginx -g 'daemon off;' &
PIDS="$PIDS $!"

while :; do
  for pid in $PIDS; do
    if ! kill -0 "$pid" 2>/dev/null; then
      wait "$pid" || status=$?
      term_children
      wait 2>/dev/null || true
      exit "${status:-1}"
    fi
  done
  sleep 2
done
