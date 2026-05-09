-- DAPO V1 -> V2 point unit adjustment.
--
-- Purpose:
--   V1 stored credit values in a finer legacy unit. The V2 frontend displays
--   points as backend_points / 100, while the new product rule is:
--     CNY 10 = 100 displayed points, image base price = 4 displayed points.
--
--   Migrated V1 wallet values therefore need to be divided by 1000 in the V2
--   backend storage unit. Example:
--     V1 recharge raw 10,000,000 -> V2 backend 10,000 -> display 100.
--
-- Safety:
--   - Run only after a fresh backup and read-only divisibility checks.
--   - The marker key prevents accidental second execution.
--   - The WHERE clauses target migrated V1 rows only.

SET NAMES utf8mb4;
SET time_zone = '+00:00';

START TRANSACTION;

SET @marker_key = 'migration.v1_point_unit_adjusted_20260510';
SET @already_done = (
  SELECT COUNT(*)
  FROM `system_config`
  WHERE `key` = @marker_key
);

UPDATE `user`
SET
  points = points DIV 1000,
  frozen_points = frozen_points DIV 1000,
  total_recharge = total_recharge DIV 1000,
  updated_at = UTC_TIMESTAMP()
WHERE @already_done = 0
  AND invite_code LIKE 'V1%';

UPDATE wallet_log
SET
  points = points DIV 1000,
  points_before = points_before DIV 1000,
  points_after = points_after DIV 1000
WHERE @already_done = 0
  AND biz_id LIKE '%#%';

UPDATE recharge_record
SET
  points = points DIV 1000,
  bonus_points = bonus_points DIV 1000,
  updated_at = UTC_TIMESTAMP()
WHERE @already_done = 0
  AND JSON_EXTRACT(extra, '$.v1_package_id') IS NOT NULL;

UPDATE system_config
SET
  `value` = JSON_ARRAY_APPEND(
    CAST(`value` AS JSON),
    '$',
    CAST('{"model_code":"gpt-image-2","name":"GPT Image 2","kind":"image","provider":"gpt","upstream_model":"gpt-image-2","unit_points":400,"enabled":true}' AS JSON)
  ),
  updated_at = UTC_TIMESTAMP()
WHERE `key` = 'billing.model_prices'
  AND JSON_SEARCH(CAST(`value` AS JSON), 'one', 'gpt-image-2', NULL, '$[*].model_code') IS NULL;

INSERT INTO system_config (`key`, `value`, `remark`, `updated_by`, `updated_at`)
SELECT
  @marker_key,
  JSON_OBJECT('scale_divisor', 1000, 'applied_at', UTC_TIMESTAMP()),
  'V1 legacy credit unit adjusted to V2 backend point unit',
  NULL,
  UTC_TIMESTAMP()
WHERE @already_done = 0;

COMMIT;
