#!/usr/bin/env bash
set -euo pipefail

: "${MYSQL_HOST:?MYSQL_HOST is required}"
: "${MYSQL_USER:?MYSQL_USER is required}"
: "${MYSQL_DATABASE:?MYSQL_DATABASE is required}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"

MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"

mkdir -p "${BACKUP_DIR}"
stamp="$(date +%Y%m%d-%H%M%S)"
out="${BACKUP_DIR}/${MYSQL_DATABASE}-${stamp}.sql.gz"

export MYSQL_PWD="${MYSQL_PASSWORD}"
mysqldump \
  --host="${MYSQL_HOST}" \
  --port="${MYSQL_PORT}" \
  --user="${MYSQL_USER}" \
  --single-transaction \
  --routines \
  --triggers \
  --events \
  --hex-blob \
  --default-character-set=utf8mb4 \
  "${MYSQL_DATABASE}" | gzip -9 > "${out}"
unset MYSQL_PWD

sha256sum "${out}" > "${out}.sha256"
find "${BACKUP_DIR}" -type f -name "${MYSQL_DATABASE}-*.sql.gz*" -mtime +"${RETENTION_DAYS}" -print -delete

echo "backup written: ${out}"
