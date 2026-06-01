-- +goose Up
-- +goose StatementBegin
ALTER TABLE `model_catalog`
  ADD COLUMN `parameters_schema` JSON DEFAULT NULL COMMENT 'frontend parameter schema for model-specific controls' AFTER `capabilities`;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE `model_catalog`
  DROP COLUMN `parameters_schema`;
-- +goose StatementEnd
