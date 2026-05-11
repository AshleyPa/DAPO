-- +goose Up
-- +goose StatementBegin
INSERT INTO `system_config` (`key`, `value`, `remark`)
SELECT 'billing.image_price_rules',
       '[{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"standard","ratios":["1:1","16:9","9:16","4:3","3:4","5:4","4:5"],"resolution":"1K","unit_points":400,"enabled":true},{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"standard","ratios":["1:1","16:9","9:16","4:3","3:4","5:4","4:5"],"resolution":"2K","unit_points":600,"enabled":true},{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"standard","ratios":["1:1","16:9","9:16","4:3","3:4","5:4","4:5"],"resolution":"4K","unit_points":800,"enabled":true},{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"extended","ratios":["3:2","2:3","21:9"],"resolution":"1K","unit_points":500,"enabled":true},{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"extended","ratios":["3:2","2:3","21:9"],"resolution":"2K","unit_points":700,"enabled":true},{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"extended","ratios":["3:2","2:3","21:9"],"resolution":"4K","unit_points":900,"enabled":true},{"model_code":"gpt-image-2","mode":"i2i","ratio_group":"standard","ratios":["1:1","16:9","9:16","4:3","3:4","5:4","4:5"],"resolution":"1K","unit_points":600,"enabled":true},{"model_code":"gpt-image-2","mode":"i2i","ratio_group":"standard","ratios":["1:1","16:9","9:16","4:3","3:4","5:4","4:5"],"resolution":"2K","unit_points":800,"enabled":true},{"model_code":"gpt-image-2","mode":"i2i","ratio_group":"standard","ratios":["1:1","16:9","9:16","4:3","3:4","5:4","4:5"],"resolution":"4K","unit_points":1000,"enabled":true},{"model_code":"gpt-image-2","mode":"i2i","ratio_group":"extended","ratios":["3:2","2:3","21:9"],"resolution":"1K","unit_points":700,"enabled":true},{"model_code":"gpt-image-2","mode":"i2i","ratio_group":"extended","ratios":["3:2","2:3","21:9"],"resolution":"2K","unit_points":900,"enabled":true},{"model_code":"gpt-image-2","mode":"i2i","ratio_group":"extended","ratios":["3:2","2:3","21:9"],"resolution":"4K","unit_points":1100,"enabled":true}]',
       '图片生成按模型、模式、比例组和分辨率配置的单张扣费矩阵，单位为点 *100'
WHERE NOT EXISTS (SELECT 1 FROM `system_config` WHERE `key`='billing.image_price_rules');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM `system_config` WHERE `key`='billing.image_price_rules';
-- +goose StatementEnd
