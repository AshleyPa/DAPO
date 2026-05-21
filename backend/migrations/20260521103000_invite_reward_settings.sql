-- +goose Up
-- +goose StatementBegin
INSERT INTO `system_config` (`key`, `value`, `remark`) VALUES
  ('billing.free_initial_points',       '0',     '新用户注册赠送积分（点 *100）'),
  ('invite.enabled',                    'false', '是否启用邀请奖励发放'),
  ('invite.new_user_points',            '0',     '被邀请人注册赠送积分（点 *100）'),
  ('invite.inviter_register_reward',    '0',     '邀请人获得的注册奖励（点 *100）'),
  ('invite.first_recharge_reward',      '5000',  '邀请好友首充固定奖励（点 *100）'),
  ('invite.lifetime_share_pct',         '5',     '邀请好友充值长期分润比例 %')
ON DUPLICATE KEY UPDATE `remark`=VALUES(`remark`);
-- +goose StatementEnd

-- +goose Down
DELETE FROM `system_config`
WHERE `key` IN (
  'billing.free_initial_points',
  'invite.enabled',
  'invite.new_user_points',
  'invite.inviter_register_reward'
);
