-- Decode V1 encrypted fields from base64 text into V2 raw AES-GCM BLOBs.
--
-- V1 stored AES-GCM ciphertext as base64(nonce || ciphertext || tag).
-- V2 stores the raw nonce || ciphertext || tag bytes in BLOB columns.
-- This script is safe to rerun: it only updates fields whose current value
-- still decodes as base64 and whose decoded payload is long enough for AES-GCM.

START TRANSACTION;

CREATE TABLE IF NOT EXISTS `migration_backup_v1_cipher_base64_account_20260510` AS
SELECT
  `id`,
  `credential_enc`,
  `access_token_enc`,
  `refresh_token_enc`,
  `session_token_enc`,
  CURRENT_TIMESTAMP(3) AS `backed_up_at`
FROM `account`
WHERE
  (OCTET_LENGTH(FROM_BASE64(`credential_enc`)) >= 28 AND OCTET_LENGTH(FROM_BASE64(`credential_enc`)) <> OCTET_LENGTH(`credential_enc`))
  OR (OCTET_LENGTH(FROM_BASE64(`access_token_enc`)) >= 28 AND OCTET_LENGTH(FROM_BASE64(`access_token_enc`)) <> OCTET_LENGTH(`access_token_enc`))
  OR (OCTET_LENGTH(FROM_BASE64(`refresh_token_enc`)) >= 28 AND OCTET_LENGTH(FROM_BASE64(`refresh_token_enc`)) <> OCTET_LENGTH(`refresh_token_enc`))
  OR (OCTET_LENGTH(FROM_BASE64(`session_token_enc`)) >= 28 AND OCTET_LENGTH(FROM_BASE64(`session_token_enc`)) <> OCTET_LENGTH(`session_token_enc`));

CREATE TABLE IF NOT EXISTS `migration_backup_v1_cipher_base64_proxy_20260510` AS
SELECT
  `id`,
  `password_enc`,
  CURRENT_TIMESTAMP(3) AS `backed_up_at`
FROM `proxy`
WHERE OCTET_LENGTH(FROM_BASE64(`password_enc`)) >= 28
  AND OCTET_LENGTH(FROM_BASE64(`password_enc`)) <> OCTET_LENGTH(`password_enc`);

UPDATE `account`
SET `credential_enc` = FROM_BASE64(`credential_enc`)
WHERE OCTET_LENGTH(FROM_BASE64(`credential_enc`)) >= 28
  AND OCTET_LENGTH(FROM_BASE64(`credential_enc`)) <> OCTET_LENGTH(`credential_enc`);

UPDATE `account`
SET `access_token_enc` = FROM_BASE64(`access_token_enc`)
WHERE OCTET_LENGTH(FROM_BASE64(`access_token_enc`)) >= 28
  AND OCTET_LENGTH(FROM_BASE64(`access_token_enc`)) <> OCTET_LENGTH(`access_token_enc`);

UPDATE `account`
SET `refresh_token_enc` = FROM_BASE64(`refresh_token_enc`)
WHERE OCTET_LENGTH(FROM_BASE64(`refresh_token_enc`)) >= 28
  AND OCTET_LENGTH(FROM_BASE64(`refresh_token_enc`)) <> OCTET_LENGTH(`refresh_token_enc`);

UPDATE `account`
SET `session_token_enc` = FROM_BASE64(`session_token_enc`)
WHERE OCTET_LENGTH(FROM_BASE64(`session_token_enc`)) >= 28
  AND OCTET_LENGTH(FROM_BASE64(`session_token_enc`)) <> OCTET_LENGTH(`session_token_enc`);

UPDATE `proxy`
SET `password_enc` = FROM_BASE64(`password_enc`)
WHERE OCTET_LENGTH(FROM_BASE64(`password_enc`)) >= 28
  AND OCTET_LENGTH(FROM_BASE64(`password_enc`)) <> OCTET_LENGTH(`password_enc`);

INSERT INTO `system_config` (`key`, `value`, `remark`, `updated_at`)
VALUES (
  'migration.v1_cipher_base64_decoded_20260510',
  'true',
  'Decoded V1 base64 AES-GCM ciphertext fields into V2 raw BLOB format',
  CURRENT_TIMESTAMP(3)
)
ON DUPLICATE KEY UPDATE
  `value` = VALUES(`value`),
  `remark` = VALUES(`remark`),
  `updated_at` = VALUES(`updated_at`);

COMMIT;
