#!/bin/sh
set -eu

: "${KLEIN_API_PORT:=17180}"
: "${KLEIN_ADMIN_PORT:=17188}"
: "${KLEIN_OPENAI_PORT:=17200}"
: "${KLEIN_LOG_DIR:=/app/logs}"
: "${KLEIN_STORAGE_ROOT:=/app/storage}"
: "${DAPO_GATEWAY_PORT:=3040}"

mkdir -p "$KLEIN_LOG_DIR" "$KLEIN_STORAGE_ROOT"

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
