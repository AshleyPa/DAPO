-- +goose Up
-- +goose StatementBegin
SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'user'
        AND COLUMN_NAME = 'token_version') < 1,
    'ALTER TABLE `user` ADD COLUMN `token_version` BIGINT NOT NULL DEFAULT 0 COMMENT ''token invalidation version'' AFTER `plan_expire_at`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'admin_user'
        AND COLUMN_NAME = 'token_version') < 1,
    'ALTER TABLE `admin_user` ADD COLUMN `token_version` BIGINT NOT NULL DEFAULT 0 COMMENT ''token invalidation version'' AFTER `role_id`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'recharge_record'
        AND COLUMN_NAME = 'idem_key') < 1,
    'ALTER TABLE `recharge_record` ADD COLUMN `idem_key` VARCHAR(64) DEFAULT NULL COMMENT ''client idempotency key'' AFTER `client_ip`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'recharge_record'
        AND INDEX_NAME = 'uk_user_recharge_idem') < 1,
    'ALTER TABLE `recharge_record` ADD UNIQUE KEY `uk_user_recharge_idem` (`user_id`, `idem_key`)',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'wallet_log'
        AND INDEX_NAME = 'uk_wallet_biz') < 1,
    'ALTER TABLE `wallet_log` ADD UNIQUE KEY `uk_wallet_biz` (`biz_type`, `biz_id`)',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'wallet_log'
        AND INDEX_NAME = 'uk_wallet_biz') > 0,
    'ALTER TABLE `wallet_log` DROP INDEX `uk_wallet_biz`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'recharge_record'
        AND INDEX_NAME = 'uk_user_recharge_idem') > 0,
    'ALTER TABLE `recharge_record` DROP INDEX `uk_user_recharge_idem`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'recharge_record'
        AND COLUMN_NAME = 'idem_key') > 0,
    'ALTER TABLE `recharge_record` DROP COLUMN `idem_key`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'admin_user'
        AND COLUMN_NAME = 'token_version') > 0,
    'ALTER TABLE `admin_user` DROP COLUMN `token_version`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;

SET @__stmt := (
  SELECT IF(
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'user'
        AND COLUMN_NAME = 'token_version') > 0,
    'ALTER TABLE `user` DROP COLUMN `token_version`',
    'SELECT 1'
  )
);
PREPARE __prep FROM @__stmt;
EXECUTE __prep;
DEALLOCATE PREPARE __prep;
-- +goose StatementEnd
