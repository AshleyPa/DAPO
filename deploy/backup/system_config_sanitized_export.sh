#!/usr/bin/env bash
set -euo pipefail

: "${MYSQL_HOST:?MYSQL_HOST is required}"
: "${MYSQL_USER:?MYSQL_USER is required}"
: "${MYSQL_DATABASE:?MYSQL_DATABASE is required}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"

MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"

mkdir -p "${BACKUP_DIR}"
stamp="$(date +%Y%m%d-%H%M%S)"
out="${BACKUP_DIR}/system_config-sanitized-${stamp}.tsv"

export MYSQL_PWD="${MYSQL_PASSWORD}"
mysql \
  --host="${MYSQL_HOST}" \
  --port="${MYSQL_PORT}" \
  --user="${MYSQL_USER}" \
  --batch \
  --raw \
  "${MYSQL_DATABASE}" \
  --execute="
SELECT
  \`key\`,
  CASE
    WHEN LOWER(\`key\`) LIKE '%password%'
      OR LOWER(\`key\`) LIKE '%private_key%'
      OR LOWER(\`key\`) LIKE '%access_key_secret%'
      OR LOWER(\`key\`) LIKE '%api_v3_key%'
      OR LOWER(\`key\`) LIKE '%secret%'
      OR LOWER(\`key\`) LIKE '%cookies%'
      OR LOWER(\`key\`) LIKE '%clearance%'
    THEN JSON_QUOTE('********')
    ELSE \`value\`
  END AS \`value\`,
  COALESCE(\`remark\`, '') AS \`remark\`,
  COALESCE(CAST(\`updated_by\` AS CHAR), '') AS \`updated_by\`,
  \`updated_at\`
FROM system_config
ORDER BY \`key\` ASC;" > "${out}"
unset MYSQL_PWD

sha256sum "${out}" > "${out}.sha256"
find "${BACKUP_DIR}" -type f -name "system_config-sanitized-*.tsv*" -mtime +"${RETENTION_DAYS}" -print -delete

echo "sanitized config export written: ${out}"
