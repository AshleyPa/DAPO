// 后台管理 - 与后端 dto / response 对齐的前端类型。
// 注意：所有 *_points / points 字段单位为「点 *100」，展示请除以 100。

export interface ApiBody<T> {
  code: number;
  msg: string;
  data?: T;
  trace_id?: string;
}

export interface PageData<T> {
  list: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface AdminLoginResp {
  id: number;
  username: string;
  nickname: string;
  role_id: number;
  token: {
    access_token: string;
    refresh_token: string;
    token_type: string;
    access_expire_in: number;
    refresh_expire_in: number;
  };
}

export interface AdminMe {
  id: number;
  username: string;
  nickname: string;
  email?: string;
  role_id: number;
  role_code: string;
  role_name: string;
}

export type AdminSystemReadinessStatus = 'ok' | 'warn' | 'error' | string;

export interface AdminSystemReadinessCheck {
  category: string;
  key: string;
  label: string;
  status: AdminSystemReadinessStatus;
  message: string;
  source?: string;
  required: boolean;
}

export interface AdminSystemReadinessResp {
  refreshed_at: number;
  overall: AdminSystemReadinessStatus;
  summary: {
    ok: number;
    warn: number;
    error: number;
  };
  checks: AdminSystemReadinessCheck[];
}

/** 账号池条目 */
export interface AdminUserItem {
  id: number;
  uuid: string;
  email?: string;
  phone?: string;
  username?: string;
  avatar?: string;
  points: number;
  frozen_points: number;
  total_recharge: number;
  plan_code: string;
  plan_expire_at?: number;
  inviter_id?: number;
  invite_code: string;
  status: 0 | 1 | number;
  register_ip?: string;
  last_login_at?: number;
  last_login_ip?: string;
  created_at: number;
  updated_at: number;
}

export interface AdminUserCreateBody {
  account: string;
  password: string;
  username?: string;
  points?: number;
  status?: 0 | 1;
}

export interface AdminUserUpdateBody {
  email?: string | null;
  phone?: string | null;
  username?: string | null;
  avatar?: string | null;
  password?: string;
  status?: 0 | 1;
  plan_code?: string;
  plan_expire_at?: number | null;
}

export interface AdminUserAdjustPointsBody {
  action: 'recharge' | 'deduct';
  points: number;
  remark?: string;
}

export interface AdminUserAdjustPointsResp {
  points_before: number;
  points_after: number;
}

export interface AdminGenerationLogItem {
  task_id: string;
  created_at: number;
  user_id: number;
  user_label: string;
  api_key_id?: number;
  key_label?: string;
  kind: 'image' | 'video' | 'chat' | 'text' | string;
  model_code: string;
  prompt: string;
  status: 0 | 1 | 2 | 3 | 4 | number;
  duration_ms?: number;
  cost_points: number;
  preview_url?: string;
  error?: string;
  model_gateway_route_snapshot?: ModelGatewayRouteSnapshot;
  pricing_snapshot?: PricingAuditSnapshot;
}

export interface ModelGatewayRouteSnapshot {
  version?: number;
  model_code?: string;
  kind?: string;
  selected_index?: number;
  candidate_count?: number;
  skipped_count?: number;
  candidates?: ModelGatewayRouteSnapshotCandidate[];
  skipped_candidates?: ModelGatewayRouteSnapshotCandidate[];
}

export interface ModelGatewayRouteSnapshotCandidate {
  index?: number;
  source_type?: string;
  source_code?: string;
  source_name?: string;
  provider?: string;
  adapter?: string;
  upstream_model?: string;
  strategy?: string;
  auth_type?: string;
  image_api_mode?: string;
  skip_reason?: string;
}

export interface PricingAuditSnapshot {
  version?: number;
  model_code?: string;
  kind?: string;
  pricing_source?: string;
  pricing_mode?: string;
  unit_basis?: string;
  count?: number;
  estimated_unit_points?: number;
  estimated_total_points?: number;
  estimated_points?: number;
  pre_deduct_points?: number;
  actual_points?: number;
  refund_points?: number;
  extra_points?: number;
  input_unit_points?: number;
  output_unit_points?: number;
  estimated_prompt_tokens?: number;
  estimated_completion_tokens?: number;
  usage?: {
    prompt_tokens?: number;
    completion_tokens?: number;
    total_tokens?: number;
  };
  usage_missing?: boolean;
  settlement?: string;
  request_mode?: string;
  ratio?: string;
  resolution?: string;
  duration_sec?: number;
  quality?: string;
  matched_rule?: Record<string, unknown>;
  failure_reason?: string;
}

export interface AdminGenerationLogPurgeResp {
  deleted: number;
}

export interface AdminGenerationUpstreamLogItem {
 id: number;
 task_id: string;
  provider: string;
  account_id?: number;
  kind?: string;
  model_code?: string;
  stage: string;
  method?: string;
  url?: string;
  status_code: number;
  duration_ms: number;
  request_excerpt?: string;
  response_excerpt?: string;
  error?: string;
 meta?: string;
 created_at: number;
}

export interface AdminGenerationConsumeRecord {
  id: number;
  task_id: string;
  user_id: number;
  kind: string;
  model_code: string;
  count: number;
  unit_points: number;
  total_points: number;
  status: number;
  account_id?: number;
  created_at: number;
  updated_at: number;
}

export interface AdminGenerationBillingWalletLog {
  id: number;
  user_id: number;
  direction: 1 | -1 | number;
  biz_type: string;
  biz_id: string;
  points: number;
  points_before: number;
  points_after: number;
  remark?: string;
  created_at: number;
}

export interface AdminGenerationRefundRecord {
  id: number;
  task_id: string;
  user_id: number;
  points: number;
  reason: string;
  operator: string;
  created_at: number;
}

export interface AdminGenerationBillingProof {
  task_id: string;
  consume_record?: AdminGenerationConsumeRecord;
  wallet_logs: AdminGenerationBillingWalletLog[];
  refund_records: AdminGenerationRefundRecord[];
  summary: {
    consume_record_found: boolean;
    consume_status?: number;
    consume_total_points?: number;
    wallet_log_count: number;
    refund_record_count: number;
    wallet_net_points: number;
    wallet_spend_points: number;
    wallet_refund_points: number;
    wallet_extra_points: number;
  };
}

export type APIChannelAdapter =
  | 'openai_compatible_chat'
  | 'openai_compatible_images'
  | 'openai_compatible_video'
  | 'openai_responses'
  | 'nova_async'
  | 'pic2api_images'
  | string;

export interface APIChannelItem {
  id: number;
  code: string;
  name: string;
  provider_name: string;
  adapter: APIChannelAdapter;
  base_url: string;
  has_api_key: boolean;
  key_count: number;
  enabled_key_count: number;
  models: string[];
  capabilities: string[];
  proxy_id?: number;
  priority: number;
  weight: number;
  rpm_limit: number;
  tpm_limit: number;
  timeout_seconds: number;
  status: 0 | 1 | number;
  last_test_at?: number;
  last_test_status: number;
  last_test_error?: string;
  remark?: string;
  created_at: number;
  updated_at: number;
}

export interface APIChannelBody {
  code: string;
  name: string;
  provider_name?: string;
  adapter: APIChannelAdapter;
  base_url: string;
  api_key?: string;
  clear_api_key?: boolean;
  models?: string[];
  capabilities?: string[];
  proxy_id?: number;
  clear_proxy?: boolean;
  priority?: number;
  weight?: number;
  rpm_limit?: number;
  tpm_limit?: number;
  timeout_seconds?: number;
  status?: 0 | 1;
  remark?: string;
}

export interface APIChannelSecretsResp {
  api_key: string;
}

export interface APIChannelTestResp {
  ok: boolean;
  status: number;
  latency_ms: number;
  error?: string;
  tested_at: number;
  credential_source?: string;
  key_id?: number;
  key_name?: string;
}

export interface APIChannelKeyItem {
  id: number;
  channel_id: number;
  name: string;
  has_api_key: boolean;
  priority: number;
  weight: number;
  rpm_limit: number;
  tpm_limit: number;
  status: 0 | 1 | number;
  last_used_at?: number;
  last_error?: string;
  created_at: number;
  updated_at: number;
}

export interface APIChannelKeyBody {
  name?: string;
  api_key?: string;
  priority?: number;
  weight?: number;
  rpm_limit?: number;
  tpm_limit?: number;
  status?: 0 | 1;
}

export type ModelCatalogKind = 'text' | 'image' | 'video' | 'chat';
export type ModelPricingMode = 'fixed' | 'token' | 'char' | 'matrix' | 'manual';
export type ModelSourceType = 'api_channel' | 'account_pool';

export interface ModelCatalogItem {
  id: number;
  model_code: string;
  display_name: string;
  entry_kind: ModelCatalogKind;
  provider_hint: string;
  upstream_default_model: string;
  capabilities: string[];
  parameters_schema?: unknown;
  pricing_mode: ModelPricingMode;
  unit_points: number;
  input_unit_points: number;
  output_unit_points: number;
  price_rules?: unknown;
  min_plan: string;
  tags: string[];
  description?: string;
  sort_order: number;
  visible: 0 | 1;
  status: 0 | 1;
  created_at: number;
  updated_at: number;
}

export interface ModelCatalogBody {
  model_code: string;
  display_name: string;
  entry_kind: ModelCatalogKind;
  provider_hint?: string;
  upstream_default_model?: string;
  capabilities?: string[];
  parameters_schema?: unknown;
  clear_parameters_schema?: boolean;
  pricing_mode?: ModelPricingMode;
  unit_points?: number;
  input_unit_points?: number;
  output_unit_points?: number;
  price_rules?: unknown;
  clear_price_rules?: boolean;
  min_plan?: string;
  tags?: string[];
  description?: string;
  sort_order?: number;
  visible?: 0 | 1;
  status?: 0 | 1;
}

export interface ModelSourceItem {
  id: number;
  model_code: string;
  source_type: ModelSourceType;
  source_code: string;
  upstream_model: string;
  adapter?: string;
  auth_type?: string;
  image_api_mode?: string;
  strategy: 'round_robin' | 'weighted_rr' | string;
  priority: number;
  weight: number;
  status: 0 | 1;
  remark?: string;
  created_at: number;
  updated_at: number;
}

export interface ModelSourceConflictItem {
  id: number;
  model_code: string;
  source_type: ModelSourceType | string;
  source_code: string;
  upstream_model: string;
  status: 0 | 1;
  reason: string;
}

export interface ModelSourceBody {
  model_code: string;
  source_type: ModelSourceType;
  source_code: string;
  upstream_model?: string;
  adapter?: string;
  auth_type?: string;
  image_api_mode?: string;
  strategy?: 'round_robin' | 'weighted_rr' | string;
  priority?: number;
  weight?: number;
  status?: 0 | 1;
  remark?: string;
}

export interface ModelGatewayDryRunReq {
  model_code: string;
  entry_kind?: ModelCatalogKind | '';
}

export interface ModelGatewayDryRunResp {
  model_code: string;
  display_name: string;
  entry_kind: string;
  matched_model: boolean;
  selected_index: number;
  candidate_count: number;
  available_count: number;
  warning?: string;
  candidates: ModelGatewayDryRunCandidate[];
}

export interface ModelGatewayDryRunCandidate {
  index: number;
  source_type: string;
  source_code: string;
  source_name?: string;
  upstream_model: string;
  adapter?: string;
  auth_type?: string;
  image_api_mode?: string;
  strategy: string;
  priority: number;
  weight: number;
  status: number;
  available: boolean;
  skip_reason?: string;
  candidate_accounts?: number;
  available_accounts?: number;
}

export interface ModelGatewayAuditItem {
  task_id: string;
  created_at: number;
  user_id: number;
  user_label: string;
  kind: 'image' | 'video' | 'chat' | 'text' | string;
  model_code: string;
  status: 0 | 1 | 2 | 3 | 4 | number;
  duration_ms?: number;
  cost_points: number;
  preview_url?: string;
  selected_source_type?: string;
  selected_source_code?: string;
  selected_source_name?: string;
  selected_provider?: string;
  selected_adapter?: string;
  selected_upstream_model?: string;
  selected_index?: number;
  candidate_count?: number;
  skipped_count?: number;
  skip_reasons?: string[];
  pricing_source?: string;
  pricing_mode?: string;
  settlement?: string;
  pre_deduct_points?: number;
  actual_points?: number;
  refund_points?: number;
  extra_points?: number;
  model_gateway_route_snapshot?: ModelGatewayRouteSnapshot;
  pricing_snapshot?: PricingAuditSnapshot;
  output_snapshot?: Record<string, unknown>;
  video_job_snapshot?: Record<string, unknown>;
}

export interface ProviderHealthAuthItem {
  auth_type: string;
  total: number;
  available: number;
  cooldown_active: number;
  last_test_ok: number;
  last_test_fail: number;
}

export interface ProviderHealthErrorItem {
  account_id: number;
  name: string;
  auth_type: string;
  status: number;
  error_count: number;
  last_error?: string;
  last_test_error?: string;
  last_test_at?: number;
  cooldown_until?: number;
  access_token_expires_at?: number;
  updated_at: number;
}

export interface ProviderHealthProviderItem {
  provider: string;
  total: number;
  enabled: number;
  disabled: number;
  broken: number;
  banned: number;
  available: number;
  cooldown_active: number;
  token_expired: number;
  last_test_ok: number;
  last_test_fail: number;
  last_test_unknown: number;
  quota_zero: number;
  success_count: number;
  error_count: number;
  auth_types: ProviderHealthAuthItem[];
  recent_errors: ProviderHealthErrorItem[];
}

export interface ProviderHealthSummaryResp {
  refreshed_at: number;
  providers: ProviderHealthProviderItem[];
}

export interface AdminWalletLogItem {
  id: number;
  created_at: number;
  user_id: number;
  user_label: string;
  direction: 1 | -1 | number;
  biz_type: string;
  biz_id: string;
  points: number;
  points_before: number;
  points_after: number;
  remark?: string;
}

export interface AdminPromoItem {
  id: number;
  code: string;
  name: string;
  discount_type: 1 | 2 | 3 | number;
  discount_val: number;
  min_amount: number;
  apply_to: string;
  total_qty: number;
  used_qty: number;
  per_user_limit: number;
  start_at: number;
  end_at: number;
  status: 0 | 1 | number;
  created_at: number;
  updated_at: number;
}

export interface AdminPromoBody {
  code?: string;
  name?: string;
  discount_type?: 1 | 2 | 3;
  discount_val?: number;
  min_amount?: number;
  apply_to?: string;
  total_qty?: number;
  per_user_limit?: number;
  start_at?: number;
  end_at?: number;
  status?: 0 | 1;
}

export type PromptGalleryModality = 'image' | 'text' | 'video';

export interface AdminPromptGalleryItem {
  id: number;
  modality: PromptGalleryModality;
  category: string;
  title: string;
  subtitle?: string;
  cover_url: string;
  prompt: string;
  tags: string[];
  variables_schema: Record<string, unknown>;
  sort_order: number;
  status: 0 | 1 | number;
  locale: string;
  created_at: number;
  updated_at: number;
}

export interface AdminPromptGalleryBody {
  modality?: PromptGalleryModality;
  category?: string;
  title?: string;
  subtitle?: string;
  cover_url?: string;
  prompt?: string;
  tags?: string[];
  variables_schema?: Record<string, unknown>;
  sort_order?: number;
  status?: 0 | 1;
  locale?: string;
}

export interface AdminPromptGalleryReorderBody {
  items: Array<{ id: number; sort_order: number }>;
}

export interface DashboardProviderRow {
  provider: string;
  total: number;
  enabled: number;
  available: number;
  broken: number;
  test_ok: number;
  quota_remaining: number;
  quota_total: number;
  quota_used: number;
  success_count: number;
  error_count: number;
}

export interface DashboardRecentTask {
  task_id: string;
  created_at: number;
  user_label: string;
  kind: 'image' | 'video' | string;
  model_code: string;
  count: number;
  status: number;
  cost_points: number;
}

export interface DashboardTrendPoint {
  date: string;
  generated: number;
  cost_points: number;
}

export interface DashboardOverviewResp {
  generated_today: number;
  generated_total: number;
  image_today: number;
  image_total: number;
  video_today: number;
  video_total: number;
  text_tokens_today: number;
  text_tokens_total: number;
  cost_points_today: number;
  cost_points_total: number;
  wallet_spend_today: number;
  wallet_spend_total: number;
  users_total: number;
  users_today: number;
  active_users_today: number;
  success_rate_today: number;
  account_providers: DashboardProviderRow[];
  recent_generations: DashboardRecentTask[];
  trend: DashboardTrendPoint[];
}

export interface AccountItem {
  id: number;
  provider: 'gpt' | 'grok' | string;
  name: string;
  auth_type: 'api_key' | 'cookie' | 'oauth' | string;
  credential_mask: string;
  base_url?: string;
  proxy_id?: number;
  weight: number;
  rpm_limit: number;
  tpm_limit: number;
  daily_quota: number;
  monthly_quota: number;
  /** -1 软删 / 0 禁用 / 1 启用 / 2 熔断 */
  status: -1 | 0 | 1 | 2 | number;
  cooldown_until?: number;
  last_used_at?: number;
  last_error?: string;
  error_count: number;
  success_count: number;
  remark?: string;
  /** OAuth 状态 */
  has_refresh_token?: boolean;
  has_access_token?: boolean;
  access_token_expire_at?: number;
  last_refresh_at?: number;
  /** 最近一次连通性测试 */
  last_test_at?: number;
  /** 0 未测 / 1 OK / 2 FAIL */
  last_test_status?: 0 | 1 | 2 | number;
  last_test_latency_ms?: number;
  last_test_error?: string;
  plan_type?: string;
  default_model?: string;
  image_quota_remaining?: number;
  image_quota_total?: number;
  image_quota_reset_at?: number;
  created_at: number;
  updated_at: number;
}

/** 账号连通性测试结果 */
export interface AccountTestResp {
  ok: boolean;
  latency_ms: number;
  error?: string;
  plan_type?: string;
  default_model?: string;
  image_quota_remaining?: number;
  image_quota_total?: number;
  image_quota_reset_at?: number;
}

/** OAuth 刷新结果 */
export interface AccountRefreshResp {
  ok: boolean;
  expires_in?: number;
  refreshed_at: number;
  has_refresh_token: boolean;
}

/** 批量刷新结果 */
export interface AccountBatchRefreshResp {
  refreshed: number;
  failed_ids: number[];
  page: number;
  page_size: number;
  total: number;
  has_more: boolean;
  next_page?: number;
}

/** 创建账号入参（明文，后端加密）；OAuth 可与 sora2ok 一致拆 AT/RT/ST/client_id。 */
export interface AccountCreateBody {
  provider: 'gpt' | 'grok';
  name: string;
  auth_type: 'api_key' | 'cookie' | 'oauth';
  /** api_key / cookie 必填；oauth 可与 access_token / refresh_token 组合 */
  credential?: string;
  access_token?: string;
  refresh_token?: string;
  session_token?: string;
  client_id?: string;
  base_url?: string;
  /** 绑定代理 ID；0/undefined = 不绑定 */
  proxy_id?: number;
  weight?: number;
  rpm_limit?: number;
  tpm_limit?: number;
  daily_quota?: number;
  monthly_quota?: number;
  remark?: string;
}

/** POST /accounts/batch-delete、/accounts/purge 响应 */
export interface AccountBulkOpResult {
  deleted: number;
}

export interface AccountPurgeBody {
  scope: 'all' | 'invalid' | 'zero_quota';
  provider?: 'gpt' | 'grok';
  confirm?: string;
}

/** 单个账号的明文凭证（管理员编辑面板回显用，解密失败为空串） */
export interface AccountSecretsResp {
  credential?: string;
  access_token?: string;
  refresh_token?: string;
  session_token?: string;
  client_id?: string;
}

export interface AccountUpdateBody {
  name?: string;
  credential?: string;
  /** OAuth 账号专用：单独替换三件套（空字符串表示清空对应列） */
  access_token?: string;
  refresh_token?: string;
  session_token?: string;
  client_id?: string;
  base_url?: string;
  /** 绑定代理 ID；0 = 不绑定 */
  proxy_id?: number;
  weight?: number;
  rpm_limit?: number;
  tpm_limit?: number;
  daily_quota?: number;
  monthly_quota?: number;
  status?: -1 | 0 | 1 | 2;
  remark?: string;
}

/** sub2api / Codex 导出 JSON 中单条账号 */
export interface Sub2APIAccountItem {
  name?: string;
  platform?: string;
  type?: string;
  priority?: number;
  concurrency?: number;
  credentials?: {
    access_token?: string;
    refresh_token?: string;
    client_id?: string;
    id_token?: string;
    email?: string;
    chatgpt_account_id?: string;
    chatgpt_user_id?: string;
    organization_id?: string;
    plan_type?: string;
  };
}

export interface AccountBatchImportBody {
  /** 默认 lines；sub2api 为 JSON 分片导入 */
  format?: 'lines' | 'sub2api';
  provider: 'gpt' | 'grok';
  /** lines 模式必填 */
  auth_type?: 'api_key' | 'cookie' | 'oauth';
  base_url?: string;
  /** 默认绑定代理 ID；0/undefined = 不绑定 */
  proxy_id?: number;
  weight?: number;
  /**
   * lines：一行一条；支持 `<name>@@<credential>` / `<credential>@<base_url>` / `<credential>`。
   */
  text?: string;
  /** sub2api：当前分片的账号列表（建议每批 ≤500） */
  accounts?: Sub2APIAccountItem[];
}

/** POST /accounts/import 响应 */
export interface AccountBatchImportResult {
  imported: number;
  skipped: number;
  detected?: number;
  pending?: number;
  failed?: number;
}

export interface AccountBatchAssignProxyBody {
  mode: 'single' | 'cycle';
  account_ids: number[];
  proxy_id?: number;
  proxy_ids?: number[];
}

export interface AccountBatchAssignProxyResp {
  updated: number;
}

export interface PoolStatsResp {
  pool: Record<string, number>;
}
export interface CDKCreateBatchBody {
  batch_no: string;
  name: string;
  /** 单码价值（后端 *100，传 *100 后的整数） */
  points: number;
  qty: number;
  per_user_limit?: number;
  /** unix 秒；0/不传 = 永不过期 */
  expire_at?: number;
}

export interface CDKCreateBatchResp {
  id: number;
  batch_no: string;
  total_qty: number;
}

export interface CDKBatchItem {
  id: number;
  batch_no: string;
  name: string;
  reward_type: string;
  points: number;
  total_qty: number;
  used_qty: number;
  per_user_limit: number;
  expire_at?: number;
  status: 0 | 1 | 2;
  created_by?: number;
  created_at: number;
}

export interface CDKCodeItem {
  id: number;
  batch_id: number;
  code: string;
  status: 0 | 1 | 2;
  used_by?: number;
  used_at?: number;
  created_at: number;
}

// ==================== 代理 ====================

export interface ProxyItem {
  id: number;
  name: string;
  protocol: 'http' | 'https' | 'socks5' | 'socks5h' | string;
  host: string;
  port: number;
  username?: string;
  has_password: boolean;
  /** 0 禁用 / 1 启用 */
  status: 0 | 1 | number;
  last_check_at?: number;
  /** 0 未测 / 1 OK / 2 FAIL */
  last_check_ok: 0 | 1 | 2 | number;
  last_check_ms: number;
  last_error?: string;
  remark?: string;
  subscription_id?: number;
  sub_node_name?: string;
  created_at: number;
  updated_at: number;
}

export interface ProxyCreateBody {
  name: string;
  protocol: 'http' | 'https' | 'socks5' | 'socks5h';
  host: string;
  port: number;
  username?: string;
  password?: string;
  remark?: string;
}

export interface ProxyUpdateBody {
  name?: string;
  protocol?: 'http' | 'https' | 'socks5' | 'socks5h';
  host?: string;
  port?: number;
  username?: string;
  password?: string;
  status?: 0 | 1;
  remark?: string;
}

export interface ProxyTestResp {
  ok: boolean;
  latency_ms: number;
  error?: string;
}

export interface ProxyBatchImportBody {
  text: string;
}

export interface ProxyBatchImportResult {
  created: number;
  skipped: number;
  failed: number;
  errors?: string[];
}

export interface ProxyBatchTestResp {
  tested: number;
  ok: number;
  failed: number;
  ids?: number[];
}

export interface ProxySubscriptionItem {
  id: number;
  name: string;
  port_start: number;
  node_count: number;
  auto_sync: boolean;
  sync_interval_min: number;
  last_sync_at?: number;
  last_error?: string;
  status: 0 | 1 | number;
  created_at: number;
  updated_at: number;
}

export interface ProxySubscriptionCreateBody {
  name: string;
  url: string;
  port_start?: number;
  auto_sync?: boolean;
  sync_interval_min?: number;
}

export interface ProxySubscriptionPreviewResp {
  node_count: number;
  tunnel: number;
  direct: number;
  nodes: Array<{
    name: string;
    type: string;
    server: string;
    port: number;
  }>;
}

export interface ProxySubscriptionSyncResp {
  node_count: number;
  tunnel: number;
  direct: number;
  created: number;
}

export interface ProxySubscriptionCreateResp {
  subscription: ProxySubscriptionItem;
  sync: ProxySubscriptionSyncResp;
}

// ==================== 系统配置 ====================

export type ProviderRouteKind = 'image' | 'text' | 'video' | 'chat' | '*';
export type ProviderRouteStrategy = 'round_robin' | 'weighted_rr';
export type ProviderRouteProvider = 'gpt' | 'grok' | string;
export type ProviderRouteAuthType = '' | 'api_key' | 'cookie' | 'oauth';
export type ProviderRouteImageAPIMode = '' | 'openai_responses' | 'openai_images' | 'pic2api' | 'nova_async';

export interface ProviderRouteOption {
  provider: ProviderRouteProvider;
  upstream_model?: string;
  auth_type?: ProviderRouteAuthType;
  image_api_mode?: ProviderRouteImageAPIMode;
  strategy?: ProviderRouteStrategy;
  weight?: number;
  priority?: number;
  enabled?: boolean;
}

export interface ProviderRouteRule {
  kind: ProviderRouteKind;
  model_code: string;
  enabled?: boolean;
  strategy?: ProviderRouteStrategy;
  routes: ProviderRouteOption[];
}

export interface ProviderRouteTestReq {
  kind: 'image' | 'text' | 'video' | 'chat';
  model_code: string;
  fallback_provider?: 'gpt' | 'grok' | '';
}

export interface ProviderRouteTestResp {
  kind: string;
  model_code: string;
  fallback_provider: string;
  provider: string;
  upstream_model: string;
  auth_type?: string;
  image_api_mode?: string;
  strategy: string;
  matched_config: boolean;
  matched_kind?: string;
  matched_model_code?: string;
  fallback_reason?: string;
  candidate_accounts: number;
  available_accounts: number;
  warning?: string;
  candidates?: ProviderRouteCandidateResp[];
}

export interface ProviderRouteCandidateResp {
  index: number;
  provider: string;
  upstream_model: string;
  auth_type?: string;
  image_api_mode?: string;
  strategy: string;
  candidate_accounts: number;
  available_accounts: number;
  warning?: string;
}

/** 已知 key（前端只列展示需要的，未列的也允许保存） */
export interface SystemSettings {
  /** 是否启用全局代理 */
  'proxy.global_enabled'?: boolean;
  /** 全局代理 ID（0 表示不启用） */
  'proxy.global_id'?: number;
  /** 全局代理选择模式 */
  'proxy.selection_mode'?: 'fixed' | 'random' | string;
  /** OAuth access_token 距过期 N 小时内自动刷新 */
  'oauth.refresh_before_hours'?: number;
  /** OpenAI Codex CLI client_id */
  'oauth.openai_client_id'?: string;
  /** OpenAI OAuth Token Endpoint */
  'oauth.openai_token_url'?: string;
  /** 图片/文字/视频上游账号池路由配置 */
  'provider.routes'?: ProviderRouteRule[];
  [key: string]: unknown;
}
