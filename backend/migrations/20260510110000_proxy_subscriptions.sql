-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `proxy_subscription` (
  `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`              VARCHAR(128) NOT NULL,
  `url_enc`           BLOB NOT NULL COMMENT 'AES-256-GCM encrypted subscription URL',
  `port_start`        INT NOT NULL DEFAULT 17001,
  `node_count`        INT NOT NULL DEFAULT 0,
  `auto_sync`         TINYINT(1) NOT NULL DEFAULT 1,
  `sync_interval_min` INT NOT NULL DEFAULT 60,
  `last_sync_at`      DATETIME(3) DEFAULT NULL,
  `last_error`        VARCHAR(512) DEFAULT NULL,
  `status`            TINYINT NOT NULL DEFAULT 1,
  `created_by`        BIGINT UNSIGNED DEFAULT NULL,
  `created_at`        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`        DATETIME(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_status` (`status`),
  KEY `idx_deleted` (`deleted_at`),
  KEY `idx_auto_sync` (`auto_sync`, `sync_interval_min`, `last_sync_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Clash/Mihomo proxy subscriptions';

ALTER TABLE `proxy`
  ADD COLUMN `subscription_id` BIGINT UNSIGNED DEFAULT NULL AFTER `remark`,
  ADD COLUMN `sub_node_name` VARCHAR(256) NOT NULL DEFAULT '' AFTER `subscription_id`,
  ADD KEY `idx_proxy_subscription` (`subscription_id`);

INSERT INTO `system_config` (`key`, `value`, `remark`) VALUES
  ('proxy.sub_sync_enabled',      'true', '是否启用代理订阅自动同步'),
  ('proxy.sub_sync_interval_min', '60',   '代理订阅默认同步间隔（分钟）')
ON DUPLICATE KEY UPDATE `value`=VALUES(`value`);

-- +goose StatementEnd

-- +goose Down
DELETE FROM `system_config` WHERE `key` IN ('proxy.sub_sync_enabled', 'proxy.sub_sync_interval_min');
ALTER TABLE `proxy`
  DROP KEY `idx_proxy_subscription`,
  DROP COLUMN `sub_node_name`,
  DROP COLUMN `subscription_id`;
DROP TABLE IF EXISTS `proxy_subscription`;
