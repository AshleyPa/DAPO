-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `api_channel_key` (
  `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `channel_id`     BIGINT UNSIGNED NOT NULL COMMENT 'api_channel.id',
  `name`           VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'operator-facing key label',
  `credential_enc` BLOB NOT NULL COMMENT 'AES-256-GCM encrypted API key or bearer token',
  `priority`       INT NOT NULL DEFAULT 100,
  `weight`         INT NOT NULL DEFAULT 100,
  `rpm_limit`      INT NOT NULL DEFAULT 0,
  `tpm_limit`      INT NOT NULL DEFAULT 0,
  `status`         TINYINT NOT NULL DEFAULT 1,
  `last_used_at`   DATETIME(3) DEFAULT NULL,
  `last_error`     VARCHAR(512) DEFAULT NULL,
  `created_at`     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`     DATETIME(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_api_channel_key_channel_status` (`channel_id`, `status`, `priority`),
  KEY `idx_api_channel_key_deleted` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='API Channel credential pool';

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS `api_channel_key`;
