-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `api_channel` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `code`            VARCHAR(64) NOT NULL COMMENT 'stable channel code used by model routing',
  `name`            VARCHAR(128) NOT NULL,
  `provider_name`   VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'business provider name, e.g. mimo/deepseek/openai',
  `adapter`         VARCHAR(64) NOT NULL COMMENT 'protocol adapter, e.g. openai_compatible_chat/openai_compatible_images',
  `base_url`        VARCHAR(512) NOT NULL,
  `credential_enc`  BLOB NULL COMMENT 'optional legacy AES-256-GCM encrypted API key or bearer token; key pool is stored in api_channel_key',
  `models`          JSON DEFAULT NULL COMMENT 'upstream model codes this channel is allowed to serve',
  `capabilities`    JSON DEFAULT NULL COMMENT 'chat/image/video/audio capability flags',
  `proxy_id`        BIGINT UNSIGNED DEFAULT NULL,
  `priority`        INT NOT NULL DEFAULT 100,
  `weight`          INT NOT NULL DEFAULT 100,
  `rpm_limit`       INT NOT NULL DEFAULT 0,
  `tpm_limit`       INT NOT NULL DEFAULT 0,
  `timeout_seconds` INT NOT NULL DEFAULT 300,
  `status`          TINYINT NOT NULL DEFAULT 1,
  `last_test_at`    DATETIME(3) DEFAULT NULL,
  `last_test_status` TINYINT NOT NULL DEFAULT 0,
  `last_test_error` VARCHAR(512) DEFAULT NULL,
  `remark`          VARCHAR(512) DEFAULT NULL,
  `created_by`      BIGINT UNSIGNED DEFAULT NULL,
  `created_at`      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`      DATETIME(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_api_channel_code` (`code`),
  KEY `idx_api_channel_status` (`status`),
  KEY `idx_api_channel_adapter` (`adapter`),
  KEY `idx_api_channel_deleted` (`deleted_at`),
  KEY `idx_api_channel_proxy` (`proxy_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Official/API-key upstream channels';

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS `api_channel`;
