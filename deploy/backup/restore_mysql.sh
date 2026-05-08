#!/usr/bin/env bash
set -euo pipefail

: "${MYSQL_HOST:?MYSQL_HOST is required}"
: "${MYSQL_USER:?MYSQL_USER is required}"
: "${MYSQL_DATABASE:?MYSQL_DATABASE is required}"
: "${BACKUP_FILE:?BACKUP_FILE is required}"

MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"

if [[ ! -f "${BACKUP_FILE}" ]]; then
  echo "backup file not found: ${BACKUP_FILE}" >&2
  exit 1
fi

echo "This will restore ${BACKUP_FILE} into ${MYSQL_DATABASE} on ${MYSQL_HOST}:${MYSQL_PORT}."
echo "Set CONFIRM_RESTORE=YES to proceed."
if [[ "${CONFIRM_RESTORE:-}" != "YES" ]]; then
  exit 2
fi

export MYSQL_PWD="${MYSQL_PASSWORD}"
if [[ "${BACKUP_FILE}" == *.gz ]]; then
  gunzip -c "${BACKUP_FILE}" | mysql --host="${MYSQL_HOST}" --port="${MYSQL_PORT}" --user="${MYSQL_USER}" "${MYSQL_DATABASE}"
else
  mysql --host="${MYSQL_HOST}" --port="${MYSQL_PORT}" --user="${MYSQL_USER}" "${MYSQL_DATABASE}" < "${BACKUP_FILE}"
fi
unset MYSQL_PWD

echo "restore completed"
