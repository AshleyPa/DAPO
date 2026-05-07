-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `prompt_gallery_item` (
  `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `modality`         VARCHAR(16) NOT NULL,
  `category`         VARCHAR(64) NOT NULL DEFAULT '',
  `title`            VARCHAR(80) NOT NULL,
  `subtitle`         VARCHAR(160) DEFAULT NULL,
  `cover_url`        VARCHAR(512) NOT NULL,
  `prompt`           TEXT NOT NULL,
  `tags`             JSON NOT NULL,
  `variables_schema` JSON NOT NULL,
  `sort_order`       INT NOT NULL DEFAULT 0,
  `status`           TINYINT NOT NULL DEFAULT 1,
  `locale`           VARCHAR(16) NOT NULL DEFAULT 'zh-CN',
  `created_by`       BIGINT UNSIGNED DEFAULT NULL,
  `updated_by`       BIGINT UNSIGNED DEFAULT NULL,
  `created_at`       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_modality_status_sort` (`modality`, `status`, `sort_order`, `id`),
  KEY `idx_modality_category` (`modality`, `category`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='前台快捷提示词卡片';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `prompt_gallery_item`;
-- +goose StatementEnd
