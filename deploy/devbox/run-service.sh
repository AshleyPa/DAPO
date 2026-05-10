#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <api|admin|openai|worker|gateway>" >&2
  exit 64
fi

service="$1"
release_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${DAPO_RELEASE_ENV_FILE:-${release_dir}/.env}"

if [[ -f "$env_file" ]]; then
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
fi

export KLEIN_LOG_DIR="${KLEIN_LOG_DIR:-${release_dir}/logs}"
export KLEIN_STORAGE_ROOT="${KLEIN_STORAGE_ROOT:-${release_dir}/storage}"
export DAPO_STATIC_ROOT="${DAPO_STATIC_ROOT:-${release_dir}/public}"
mkdir -p "$KLEIN_LOG_DIR" "$KLEIN_STORAGE_ROOT"

case "$service" in
  api)
    exec "${release_dir}/bin/api"
    ;;
  admin)
    exec "${release_dir}/bin/admin"
    ;;
  openai)
    exec "${release_dir}/bin/openai"
    ;;
  worker)
    exec "${release_dir}/bin/worker"
    ;;
  gateway)
    exec node "${release_dir}/gateway.mjs"
    ;;
  *)
    echo "unknown service: $service" >&2
    exit 64
    ;;
esac
