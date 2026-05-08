-- +goose Up
-- +goose StatementBegin
ALTER TABLE `user`
  ADD COLUMN `email_verified_at` DATETIME(3) DEFAULT NULL COMMENT '邮箱验证时间' AFTER `email`;

CREATE TABLE IF NOT EXISTS `email_verification_code` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `email`      VARCHAR(128) NOT NULL,
  `scene`      VARCHAR(32)  NOT NULL COMMENT 'register / reset_password',
  `code_hash`  CHAR(64)     NOT NULL,
  `salt`       VARCHAR(32)  NOT NULL,
  `status`     TINYINT      NOT NULL DEFAULT 0 COMMENT '0 pending 1 used 2 expired',
  `send_ip`    VARCHAR(45)  DEFAULT NULL,
  `attempts`   INT          NOT NULL DEFAULT 0,
  `expires_at` DATETIME(3)  NOT NULL,
  `used_at`    DATETIME(3)  DEFAULT NULL,
  `created_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_email_scene_status` (`email`, `scene`, `status`),
  KEY `idx_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='邮箱验证码';
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS `email_verification_code`;
ALTER TABLE `user` DROP COLUMN `email_verified_at`;
