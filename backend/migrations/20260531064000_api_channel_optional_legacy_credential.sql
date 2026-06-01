-- +goose Up
-- +goose StatementBegin

ALTER TABLE `api_channel`
  MODIFY COLUMN `credential_enc` BLOB NULL COMMENT 'optional legacy AES-256-GCM encrypted API key or bearer token; key pool is stored in api_channel_key';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE `api_channel`
SET `credential_enc` = X''
WHERE `credential_enc` IS NULL;

ALTER TABLE `api_channel`
  MODIFY COLUMN `credential_enc` BLOB NOT NULL COMMENT 'AES-256-GCM encrypted API key or bearer token';

-- +goose StatementEnd
