-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `model_catalog` (
  `id`                     BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `model_code`             VARCHAR(128) NOT NULL COMMENT 'public model code selected by users',
  `display_name`           VARCHAR(128) NOT NULL,
  `entry_kind`             VARCHAR(16) NOT NULL COMMENT 'text/image/video/chat',
  `provider_hint`          VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'display or legacy provider hint',
  `upstream_default_model` VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'default upstream model when a source mapping leaves it empty',
  `capabilities`           JSON DEFAULT NULL COMMENT 'model capability flags, e.g. chat/image/edit',
  `pricing_mode`           VARCHAR(32) NOT NULL DEFAULT 'fixed' COMMENT 'fixed/token/matrix/manual',
  `unit_points`            BIGINT NOT NULL DEFAULT 0 COMMENT 'fixed unit price, points *100',
  `input_unit_points`      BIGINT NOT NULL DEFAULT 0 COMMENT 'text input price per 1k tokens/chars, points *100',
  `output_unit_points`     BIGINT NOT NULL DEFAULT 0 COMMENT 'text output price per 1k tokens/chars, points *100',
  `price_rules`            JSON DEFAULT NULL COMMENT 'matrix price rules for image/video',
  `min_plan`               VARCHAR(32) NOT NULL DEFAULT 'free',
  `tags`                   JSON DEFAULT NULL,
  `description`            TEXT DEFAULT NULL,
  `sort_order`             INT NOT NULL DEFAULT 0,
  `visible`                TINYINT NOT NULL DEFAULT 1,
  `status`                 TINYINT NOT NULL DEFAULT 1,
  `created_by`             BIGINT UNSIGNED DEFAULT NULL,
  `created_at`             DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`             DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`             DATETIME(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_model_catalog_code` (`model_code`),
  KEY `idx_model_catalog_kind_status` (`entry_kind`, `status`),
  KEY `idx_model_catalog_visible_sort` (`visible`, `sort_order`),
  KEY `idx_model_catalog_deleted` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Model Gateway public model catalog';

CREATE TABLE IF NOT EXISTS `model_source_mapping` (
  `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `model_code`     VARCHAR(128) NOT NULL COMMENT 'public model code',
  `source_type`    VARCHAR(32) NOT NULL COMMENT 'api_channel/account_pool',
  `source_code`    VARCHAR(128) NOT NULL COMMENT 'api_channel.code or provider/account pool code',
  `upstream_model` VARCHAR(128) NOT NULL DEFAULT '',
  `adapter`        VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'api adapter override or protocol hint',
  `auth_type`      VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'account-pool auth type filter',
  `image_api_mode` VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'legacy image adapter hint for account pool',
  `strategy`       VARCHAR(32) NOT NULL DEFAULT 'round_robin',
  `priority`       INT NOT NULL DEFAULT 100,
  `weight`         INT NOT NULL DEFAULT 100,
  `status`         TINYINT NOT NULL DEFAULT 1,
  `remark`         VARCHAR(512) DEFAULT NULL,
  `created_at`     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`     DATETIME(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_model_source_model` (`model_code`, `status`, `priority`),
  KEY `idx_model_source_source` (`source_type`, `source_code`),
  KEY `idx_model_source_deleted` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Model Gateway model-to-source mappings';

-- Seed the current public defaults without changing runtime behavior.
INSERT INTO `model_catalog`
  (`model_code`, `display_name`, `entry_kind`, `provider_hint`, `upstream_default_model`, `capabilities`, `pricing_mode`, `unit_points`, `input_unit_points`, `output_unit_points`, `min_plan`, `tags`, `sort_order`, `visible`, `status`)
VALUES
  ('gpt-4o-mini', '文字对话', 'text', 'gpt', 'gpt-4o-mini', JSON_ARRAY('chat'), 'token', 0, 100, 300, 'free', JSON_ARRAY('文字','对话'), 10, 1, 1),
  ('gpt-image-2', 'GPT Image 2', 'image', 'gpt', 'gpt-image-2', JSON_ARRAY('image','edit'), 'matrix', 400, 0, 0, 'free', JSON_ARRAY('图片','海报'), 20, 1, 1),
  ('sora2', 'Sora2 视频', 'video', 'grok', 'sora2', JSON_ARRAY('video'), 'fixed', 1500, 0, 0, 'free', JSON_ARRAY('视频'), 30, 1, 1),
  ('sora2-pro', 'Sora2 Pro 视频', 'video', 'grok', 'sora2-pro', JSON_ARRAY('video'), 'fixed', 2000, 0, 0, 'pro', JSON_ARRAY('视频'), 31, 1, 1)
ON DUPLICATE KEY UPDATE
  `display_name`=VALUES(`display_name`),
  `entry_kind`=VALUES(`entry_kind`),
  `provider_hint`=VALUES(`provider_hint`),
  `upstream_default_model`=VALUES(`upstream_default_model`);

INSERT INTO `model_source_mapping`
  (`model_code`, `source_type`, `source_code`, `upstream_model`, `adapter`, `auth_type`, `image_api_mode`, `strategy`, `priority`, `weight`, `status`)
SELECT 'gpt-4o-mini', 'account_pool', 'gpt', 'gpt-4o-mini', '', 'api_key', '', 'round_robin', 100, 100, 1
WHERE NOT EXISTS (SELECT 1 FROM `model_source_mapping` WHERE `model_code`='gpt-4o-mini' AND `source_type`='account_pool' AND `source_code`='gpt' AND `deleted_at` IS NULL);

INSERT INTO `model_source_mapping`
  (`model_code`, `source_type`, `source_code`, `upstream_model`, `adapter`, `auth_type`, `image_api_mode`, `strategy`, `priority`, `weight`, `status`)
SELECT 'gpt-image-2', 'account_pool', 'gpt', 'gpt-image-2', '', 'api_key', 'openai_images', 'round_robin', 100, 100, 1
WHERE NOT EXISTS (SELECT 1 FROM `model_source_mapping` WHERE `model_code`='gpt-image-2' AND `source_type`='account_pool' AND `source_code`='gpt' AND `deleted_at` IS NULL);

INSERT INTO `model_source_mapping`
  (`model_code`, `source_type`, `source_code`, `upstream_model`, `adapter`, `auth_type`, `image_api_mode`, `strategy`, `priority`, `weight`, `status`)
SELECT 'sora2', 'account_pool', 'grok', 'sora2', '', 'api_key', '', 'round_robin', 100, 100, 1
WHERE NOT EXISTS (SELECT 1 FROM `model_source_mapping` WHERE `model_code`='sora2' AND `source_type`='account_pool' AND `source_code`='grok' AND `deleted_at` IS NULL);

INSERT INTO `model_source_mapping`
  (`model_code`, `source_type`, `source_code`, `upstream_model`, `adapter`, `auth_type`, `image_api_mode`, `strategy`, `priority`, `weight`, `status`)
SELECT 'sora2-pro', 'account_pool', 'grok', 'sora2-pro', '', 'api_key', '', 'round_robin', 100, 100, 1
WHERE NOT EXISTS (SELECT 1 FROM `model_source_mapping` WHERE `model_code`='sora2-pro' AND `source_type`='account_pool' AND `source_code`='grok' AND `deleted_at` IS NULL);

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS `model_source_mapping`;
DROP TABLE IF EXISTS `model_catalog`;
