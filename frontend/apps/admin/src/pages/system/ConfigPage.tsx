import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, CheckCircle2, Cloud, CreditCard, Database, GitBranch, Mail, RefreshCw, Save, ShieldAlert, Trash2, XCircle } from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';

import { ApiError } from '../../lib/api';
import { providerRoutesApi, proxiesApi, systemApi } from '../../lib/services';
import type { AdminSystemReadinessCheck, AdminSystemReadinessResp, ProviderHealthProviderItem, ProviderRouteRule, ProviderRouteTestReq, ProviderRouteTestResp, ProxyItem, SystemSettings } from '../../lib/types';
import { toast } from '../../stores/toast';
import ProviderRoutesEditor, { defaultProviderRoutes, normalizeProviderRoutes, providerRoutesValue } from './ProviderRoutesEditor';

interface FormState {
  retry_max_attempts: number;
  retry_base_delay_ms: number;
  retry_timeout_seconds: number;
  tolerance_circuit_failures: number;
  tolerance_circuit_cooldown_seconds: number;
  provider_routes: ProviderRouteRule[];
  proxy_global_enabled: boolean;
  proxy_selection_mode: 'fixed' | 'random';
  proxy_global_id: number;
  oauth_refresh_before_hours: number;
  storage_history_retention_days: number;
  storage_result_retention_days: number;
  storage_result_cache_driver: string;
  oss_enabled: boolean;
  oss_provider: string;
  oss_endpoint: string;
  oss_region: string;
  oss_bucket: string;
  oss_access_key_id: string;
  oss_access_key_secret: string;
  oss_public_base_url: string;
  oss_path_prefix: string;
  payment_enabled: boolean;
  payment_provider: string;
  payment_notify_url: string;
  alipay_app_id: string;
  alipay_seller_id: string;
  alipay_private_key: string;
  alipay_public_key: string;
  alipay_gateway_url: string;
  alipay_subject_prefix: string;
  wechat_mch_id: string;
  wechat_api_v3_key: string;
  smtp_host: string;
  smtp_port: number;
  smtp_username: string;
  smtp_password: string;
  smtp_from_email: string;
  smtp_from_name: string;
  smtp_use_ssl: boolean;
  smtp_use_starttls: boolean;
}

const DEFAULT_FORM: FormState = {
  retry_max_attempts: 2,
  retry_base_delay_ms: 800,
  retry_timeout_seconds: 300,
  tolerance_circuit_failures: 3,
  tolerance_circuit_cooldown_seconds: 300,
  provider_routes: defaultProviderRoutes(),
  proxy_global_enabled: false,
  proxy_selection_mode: 'fixed',
  proxy_global_id: 0,
  oauth_refresh_before_hours: 6,
  storage_history_retention_days: 180,
  storage_result_retention_days: 30,
  storage_result_cache_driver: 'local',
  oss_enabled: false,
  oss_provider: 'aliyun',
  oss_endpoint: '',
  oss_region: '',
  oss_bucket: '',
  oss_access_key_id: '',
  oss_access_key_secret: '',
  oss_public_base_url: '',
  oss_path_prefix: 'uploads/{yyyy}/{mm}/{dd}',
  payment_enabled: false,
  payment_provider: 'alipay',
  payment_notify_url: '',
  alipay_app_id: '',
  alipay_seller_id: '',
  alipay_private_key: '',
  alipay_public_key: '',
  alipay_gateway_url: '',
  alipay_subject_prefix: 'DAPO达波显影-',
  wechat_mch_id: '',
  wechat_api_v3_key: '',
  smtp_host: 'smtp.qiye.aliyun.com',
  smtp_port: 465,
  smtp_username: '',
  smtp_password: '',
  smtp_from_email: '',
  smtp_from_name: 'DAPO达波显影',
  smtp_use_ssl: true,
  smtp_use_starttls: false,
};

const DEFAULT_ROUTE_TEST: ProviderRouteTestReq = {
  kind: 'image',
  model_code: 'gpt-image-2',
  fallback_provider: '',
};

const asBool = (v: unknown, fallback = false) => (v == null ? fallback : Boolean(v));
const asNum = (v: unknown, fallback: number) => {
  const n = Number(v);
  return Number.isFinite(n) ? n : fallback;
};
const asStr = (v: unknown, fallback = '') => (typeof v === 'string' ? v : fallback);

function fromSettings(s: SystemSettings | undefined): FormState {
  if (!s) return DEFAULT_FORM;
  return {
    retry_max_attempts: asNum(s['retry.max_attempts'], 2),
    retry_base_delay_ms: asNum(s['retry.base_delay_ms'], 800),
    retry_timeout_seconds: asNum(s['retry.timeout_seconds'], 300),
    tolerance_circuit_failures: asNum(s['tolerance.circuit_failures'], 3),
    tolerance_circuit_cooldown_seconds: asNum(s['tolerance.circuit_cooldown_seconds'], 300),
    provider_routes: providerRoutesValue(s['provider.routes']),
    proxy_global_enabled: asBool(s['proxy.global_enabled']),
    proxy_selection_mode: asStr(s['proxy.selection_mode'], 'fixed') === 'random' ? 'random' : 'fixed',
    proxy_global_id: asNum(s['proxy.global_id'], 0),
    oauth_refresh_before_hours: asNum(s['oauth.refresh_before_hours'], 6),
    storage_history_retention_days: asNum(s['storage.history_retention_days'], 180),
    storage_result_retention_days: asNum(s['storage.result_retention_days'], 30),
    storage_result_cache_driver: asStr(s['storage.result_cache_driver'], 'local'),
    oss_enabled: asBool(s['oss.enabled']),
    oss_provider: asStr(s['oss.provider'], 'aliyun'),
    oss_endpoint: asStr(s['oss.endpoint']),
    oss_region: asStr(s['oss.region']),
    oss_bucket: asStr(s['oss.bucket']),
    oss_access_key_id: asStr(s['oss.access_key_id']),
    oss_access_key_secret: asStr(s['oss.access_key_secret']),
    oss_public_base_url: asStr(s['oss.public_base_url']),
    oss_path_prefix: asStr(s['oss.path_prefix'], 'uploads/{yyyy}/{mm}/{dd}'),
    payment_enabled: asBool(s['payment.enabled']),
    payment_provider: asStr(s['payment.provider'], 'alipay'),
    payment_notify_url: asStr(s['payment.notify_url']),
    alipay_app_id: asStr(s['payment.alipay_app_id']),
    alipay_seller_id: asStr(s['payment.alipay_seller_id']),
    alipay_private_key: asStr(s['payment.alipay_private_key']),
    alipay_public_key: asStr(s['payment.alipay_public_key']),
    alipay_gateway_url: asStr(s['payment.alipay_gateway_url']),
    alipay_subject_prefix: asStr(s['payment.alipay_subject_prefix'], 'DAPO达波显影-'),
    wechat_mch_id: asStr(s['payment.wechat_mch_id']),
    wechat_api_v3_key: asStr(s['payment.wechat_api_v3_key']),
    smtp_host: asStr(s['smtp.host'], 'smtp.qiye.aliyun.com'),
    smtp_port: asNum(s['smtp.port'], 465),
    smtp_username: asStr(s['smtp.username']),
    smtp_password: asStr(s['smtp.password']),
    smtp_from_email: asStr(s['smtp.from_email']),
    smtp_from_name: asStr(s['smtp.from_name'], 'DAPO达波显影'),
    smtp_use_ssl: asBool(s['smtp.use_ssl'], true),
    smtp_use_starttls: asBool(s['smtp.use_starttls']),
  };
}

function toPayload(f: FormState): Partial<SystemSettings> {
  return {
    'retry.max_attempts': Number(f.retry_max_attempts) || 0,
    'retry.base_delay_ms': Number(f.retry_base_delay_ms) || 0,
    'retry.timeout_seconds': Number(f.retry_timeout_seconds) || 0,
    'tolerance.circuit_failures': Number(f.tolerance_circuit_failures) || 0,
    'tolerance.circuit_cooldown_seconds': Number(f.tolerance_circuit_cooldown_seconds) || 0,
    'provider.routes': normalizeProviderRoutes(f.provider_routes),
    'proxy.global_enabled': f.proxy_global_enabled,
    'proxy.selection_mode': f.proxy_selection_mode,
    'proxy.global_id': Number(f.proxy_global_id) || 0,
    'oauth.refresh_before_hours': Number(f.oauth_refresh_before_hours) || 6,
    'storage.history_retention_days': Number(f.storage_history_retention_days) || 0,
    'storage.result_retention_days': Number(f.storage_result_retention_days) || 0,
    'storage.result_cache_driver': f.storage_result_cache_driver,
    'oss.enabled': f.oss_enabled,
    'oss.provider': f.oss_provider.trim(),
    'oss.endpoint': f.oss_endpoint.trim(),
    'oss.region': f.oss_region.trim(),
    'oss.bucket': f.oss_bucket.trim(),
    'oss.access_key_id': f.oss_access_key_id.trim(),
    'oss.access_key_secret': f.oss_access_key_secret.trim(),
    'oss.public_base_url': f.oss_public_base_url.trim(),
    'oss.path_prefix': f.oss_path_prefix.trim(),
    'payment.enabled': f.payment_enabled,
    'payment.provider': f.payment_provider.trim(),
    'payment.notify_url': f.payment_notify_url.trim(),
    'payment.alipay_app_id': f.alipay_app_id.trim(),
    'payment.alipay_seller_id': f.alipay_seller_id.trim(),
    'payment.alipay_private_key': f.alipay_private_key.trim(),
    'payment.alipay_public_key': f.alipay_public_key.trim(),
    'payment.alipay_gateway_url': f.alipay_gateway_url.trim(),
    'payment.alipay_subject_prefix': f.alipay_subject_prefix.trim(),
    'payment.wechat_mch_id': f.wechat_mch_id.trim(),
    'payment.wechat_api_v3_key': f.wechat_api_v3_key.trim(),
    'smtp.host': f.smtp_host.trim(),
    'smtp.port': Number(f.smtp_port) || 465,
    'smtp.username': f.smtp_username.trim(),
    'smtp.password': f.smtp_password.trim(),
    'smtp.from_email': f.smtp_from_email.trim(),
    'smtp.from_name': f.smtp_from_name.trim(),
    'smtp.use_ssl': f.smtp_use_ssl,
    'smtp.use_starttls': f.smtp_use_starttls,
  };
}

export default function ConfigPage() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ['admin', 'system', 'settings'], queryFn: () => systemApi.get() });
  const readiness = useQuery({ queryKey: ['admin', 'system', 'readiness'], queryFn: () => systemApi.readiness() });
  const cacheStats = useQuery({ queryKey: ['admin', 'system', 'cache'], queryFn: () => systemApi.cacheStats() });
  const proxies = useQuery({
    queryKey: ['admin', 'proxies', 'options'],
    queryFn: () => proxiesApi.list({ page: 1, page_size: 200, status: 1 }),
  });
  const providerHealth = useQuery({
    queryKey: ['admin', 'provider-routes', 'health'],
    queryFn: () => providerRoutesApi.health(),
  });
  const [form, setForm] = useState<FormState>(DEFAULT_FORM);
  const [dirty, setDirty] = useState(false);
  const [routeTest, setRouteTest] = useState<ProviderRouteTestReq>(DEFAULT_ROUTE_TEST);

  useEffect(() => {
    if (settings.data) {
      setForm(fromSettings(settings.data));
      setDirty(false);
    }
  }, [settings.data]);

  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => {
    setForm((f) => ({ ...f, [k]: v }));
    setDirty(true);
  };

  const save = useMutation({
    mutationFn: () => systemApi.update(toPayload(form)),
    onSuccess: () => {
      toast.success('已保存');
      setDirty(false);
      qc.invalidateQueries({ queryKey: ['admin', 'system'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const cleanCache = useMutation({
    mutationFn: (body: { days?: number; all?: boolean }) => systemApi.cleanCache(body),
    onSuccess: (r) => {
      toast.success(`已清理 ${formatBytes(r.deleted_bytes)} / ${r.deleted_files} 个缓存文件`);
      qc.invalidateQueries({ queryKey: ['admin', 'system', 'cache'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const testRoute = useMutation({
    mutationFn: () => providerRoutesApi.test({
      ...routeTest,
      model_code: routeTest.model_code.trim(),
      fallback_provider: routeTest.fallback_provider || undefined,
    }),
    onSuccess: () => toast.success('路由测试完成'),
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const proxyOptions: ProxyItem[] = proxies.data?.list ?? [];

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title">系统配置</h1>
          <p className="page-subtitle">维护运行容错、邮箱验证码、刷新存储、OSS 和支付通道基础参数。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="btn btn-outline btn-md" onClick={() => settings.refetch()} disabled={settings.isFetching}>
            <RefreshCw size={16} className={settings.isFetching ? 'animate-spin' : ''} /> 重新加载
          </button>
          <button className="btn btn-primary btn-md" onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
            <Save size={16} /> {save.isPending ? '保存中...' : dirty ? '保存修改' : '已是最新'}
          </button>
        </div>
      </header>

      {settings.isLoading ? (
        <div className="card card-section text-center text-text-tertiary py-10">加载中...</div>
      ) : (
        <>
        <ReadinessPanel
          data={readiness.data}
          loading={readiness.isLoading}
          fetching={readiness.isFetching}
          onRefresh={() => readiness.refetch()}
        />
        <div className="grid gap-4 xl:grid-cols-2">
          <Section icon={<Mail size={18} />} title="邮箱验证码" desc="配置注册和找回密码验证码的 SMTP 发信参数。">
            <div className="grid gap-3 md:grid-cols-2">
              <TextField label="SMTP Host" value={form.smtp_host} onChange={(v) => set('smtp_host', v)} placeholder="smtp.qiye.aliyun.com" />
              <NumberField label="SMTP Port" value={form.smtp_port} min={1} max={65535} onChange={(v) => set('smtp_port', v)} />
              <TextField label="发件账号" value={form.smtp_username} onChange={(v) => set('smtp_username', v)} placeholder="sender@example.com" />
              <TextField label="邮箱三方密码" value={form.smtp_password} onChange={(v) => set('smtp_password', v)} type="password" />
              <TextField label="发件邮箱" value={form.smtp_from_email} onChange={(v) => set('smtp_from_email', v)} placeholder="sender@example.com" />
              <TextField label="发件名称" value={form.smtp_from_name} onChange={(v) => set('smtp_from_name', v)} placeholder="DAPO达波显影" />
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <Toggle label="启用 SSL" checked={form.smtp_use_ssl} onChange={(v) => set('smtp_use_ssl', v)} />
              <Toggle label="启用 STARTTLS" checked={form.smtp_use_starttls} onChange={(v) => set('smtp_use_starttls', v)} />
            </div>
          </Section>

          <Section icon={<ShieldAlert size={18} />} title="重试与容错" desc="控制生成请求失败后的重试次数、超时和账号熔断策略。">
            <NumberField label="最大重试次数" value={form.retry_max_attempts} min={0} max={10} onChange={(v) => set('retry_max_attempts', v)} />
            <NumberField label="重试基础延迟（毫秒）" value={form.retry_base_delay_ms} min={0} onChange={(v) => set('retry_base_delay_ms', v)} />
            <NumberField label="请求超时（秒）" value={form.retry_timeout_seconds} min={30} onChange={(v) => set('retry_timeout_seconds', v)} />
            <NumberField label="熔断失败次数" value={form.tolerance_circuit_failures} min={1} onChange={(v) => set('tolerance_circuit_failures', v)} />
            <NumberField label="熔断冷却时间（秒）" value={form.tolerance_circuit_cooldown_seconds} min={30} onChange={(v) => set('tolerance_circuit_cooldown_seconds', v)} />
          </Section>

          <Section icon={<GitBranch size={18} />} title="模型路由" desc="配置图片、文字、视频模型进入哪个上游账号池，并控制轮询策略和认证类型。">
            <ProviderRoutesEditor value={form.provider_routes} onChange={(v) => set('provider_routes', v)} />
            <ProviderRouteDryRunPanel
              value={routeTest}
              result={testRoute.data}
              loading={testRoute.isPending}
              onChange={setRouteTest}
              onSubmit={() => testRoute.mutate()}
            />
            <ProviderHealthPanel
              providers={providerHealth.data?.providers ?? []}
              loading={providerHealth.isLoading}
              fetching={providerHealth.isFetching}
              refreshedAt={providerHealth.data?.refreshed_at}
              onRefresh={() => providerHealth.refetch()}
            />
          </Section>

          <Section icon={<Database size={18} />} title="刷新与存储" desc="控制 OAuth 刷新窗口、全局代理和生成历史保留周期。">
            <Toggle label="启用全局代理" checked={form.proxy_global_enabled} onChange={(v) => set('proxy_global_enabled', v)} />
            <Field label="全局代理模式">
              <select
                className="select"
                value={form.proxy_selection_mode}
                onChange={(e) => set('proxy_selection_mode', e.target.value === 'random' ? 'random' : 'fixed')}
                disabled={!form.proxy_global_enabled}
              >
                <option value="fixed">固定代理</option>
                <option value="random">随机代理池</option>
              </select>
            </Field>
            <Field label="全局默认代理">
              <select
                className="select"
                value={form.proxy_global_id}
                onChange={(e) => set('proxy_global_id', Number(e.target.value) || 0)}
                disabled={!form.proxy_global_enabled || form.proxy_selection_mode === 'random'}
              >
                <option value={0}>不指定</option>
                {proxyOptions.map((p) => <option key={p.id} value={p.id}>[{p.protocol}] {p.name} - {p.host}:{p.port}</option>)}
              </select>
            </Field>
            {form.proxy_global_enabled && form.proxy_selection_mode === 'random' && (
              <div className="rounded-md border border-border bg-surface-2 p-3 text-small text-text-tertiary">
                每次任务启动时，会从当前已启用代理中随机挑选一个；账号单独绑定的代理仍然优先。
              </div>
            )}
            <NumberField label="OAuth 提前刷新窗口（小时）" value={form.oauth_refresh_before_hours} min={1} max={48} onChange={(v) => set('oauth_refresh_before_hours', v)} />
            <NumberField label="生成历史保留（天）" value={form.storage_history_retention_days} min={0} onChange={(v) => set('storage_history_retention_days', v)} />
            <NumberField label="生成结果文件保留（天）" value={form.storage_result_retention_days} min={0} onChange={(v) => set('storage_result_retention_days', v)} />
            <Field label="生成结果缓存位置">
              <select className="select" value={form.storage_result_cache_driver} onChange={(e) => set('storage_result_cache_driver', e.target.value)}>
                <option value="local">本地缓存</option>
                <option value="oss">OSS 存储</option>
                <option value="off">不缓存</option>
              </select>
            </Field>
          </Section>

          <Section icon={<Trash2 size={18} />} title="缓存清理" desc="查看并清理本地生成结果缓存，清理后旧作品可能无法继续预览原文件。">
            <div className="grid gap-3 md:grid-cols-3">
              <div className="rounded-md border border-border bg-surface-2 p-3">
                <div className="text-small text-text-tertiary">缓存大小</div>
                <div className="mt-1 text-h4 text-text-primary">{formatBytes(cacheStats.data?.bytes ?? 0)}</div>
              </div>
              <div className="rounded-md border border-border bg-surface-2 p-3">
                <div className="text-small text-text-tertiary">文件数量</div>
                <div className="mt-1 text-h4 text-text-primary">{cacheStats.data?.files ?? 0}</div>
              </div>
              <div className="rounded-md border border-border bg-surface-2 p-3">
                <div className="text-small text-text-tertiary">缓存目录</div>
                <div className="mt-1 truncate text-small text-text-secondary" title={cacheStats.data?.root}>{cacheStats.data?.root || '-'}</div>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              <button className="btn btn-outline btn-sm" disabled={cleanCache.isPending} onClick={() => cacheStats.refetch()}>
                <RefreshCw size={14} className={cacheStats.isFetching ? 'animate-spin' : ''} /> 刷新占用
              </button>
              <button className="btn btn-outline btn-sm" disabled={cleanCache.isPending} onClick={() => cleanCache.mutate({ days: 7 })}>
                清理 7 天前
              </button>
              <button className="btn btn-outline btn-sm" disabled={cleanCache.isPending} onClick={() => cleanCache.mutate({ days: 3 })}>
                清理 3 天前
              </button>
              <button
                className="btn btn-danger btn-sm"
                disabled={cleanCache.isPending}
                onClick={() => {
                  if (window.confirm('确定清空全部生成缓存吗？旧作品可能无法继续预览原文件。')) cleanCache.mutate({ all: true });
                }}
              >
                <Trash2 size={14} /> 清空全部缓存
              </button>
            </div>
          </Section>

          <Section icon={<Cloud size={18} />} title="OSS 存储" desc="配置图片、视频和用户上传素材的对象存储位置。">
            <Toggle label="启用 OSS 存储" checked={form.oss_enabled} onChange={(v) => set('oss_enabled', v)} />
            <div className="grid gap-3 md:grid-cols-2">
              <TextField label="服务商" value={form.oss_provider} onChange={(v) => set('oss_provider', v)} placeholder="aliyun / s3 / cos" />
              <TextField label="Region" value={form.oss_region} onChange={(v) => set('oss_region', v)} />
              <TextField label="Endpoint" value={form.oss_endpoint} onChange={(v) => set('oss_endpoint', v)} />
              <TextField label="Bucket" value={form.oss_bucket} onChange={(v) => set('oss_bucket', v)} />
              <TextField label="AccessKey ID" value={form.oss_access_key_id} onChange={(v) => set('oss_access_key_id', v)} />
              <TextField label="AccessKey Secret" value={form.oss_access_key_secret} onChange={(v) => set('oss_access_key_secret', v)} type="password" />
            </div>
            <TextField label="公开访问域名" value={form.oss_public_base_url} onChange={(v) => set('oss_public_base_url', v)} placeholder="https://cdn.example.com" />
            <TextField label="存储路径前缀" value={form.oss_path_prefix} onChange={(v) => set('oss_path_prefix', v)} />
          </Section>

          <Section icon={<CreditCard size={18} />} title="支付配置" desc="保存支付通道基础参数，后续充值下单与回调会读取这些配置。">
            <Toggle label="启用在线支付" checked={form.payment_enabled} onChange={(v) => set('payment_enabled', v)} />
            <div className="grid gap-3 md:grid-cols-2">
              <TextField label="默认支付通道" value={form.payment_provider} onChange={(v) => set('payment_provider', v)} placeholder="alipay / wechat" />
              <TextField label="支付回调地址" value={form.payment_notify_url} onChange={(v) => set('payment_notify_url', v)} />
              <TextField label="支付宝 AppID" value={form.alipay_app_id} onChange={(v) => set('alipay_app_id', v)} />
              <TextField label="支付宝 Seller ID" value={form.alipay_seller_id} onChange={(v) => set('alipay_seller_id', v)} placeholder="用于校验回调收款方" />
              <TextField label="支付宝网关" value={form.alipay_gateway_url} onChange={(v) => set('alipay_gateway_url', v)} placeholder="默认官方正式网关" />
              <TextField label="订单标题前缀" value={form.alipay_subject_prefix} onChange={(v) => set('alipay_subject_prefix', v)} />
              <TextField label="微信商户号" value={form.wechat_mch_id} onChange={(v) => set('wechat_mch_id', v)} />
            </div>
            <Field label="支付宝私钥"><textarea className="input font-mono text-small min-h-[96px]" value={form.alipay_private_key} onChange={(e) => set('alipay_private_key', e.target.value)} /></Field>
            <Field label="支付宝公钥"><textarea className="input font-mono text-small min-h-[96px]" value={form.alipay_public_key} onChange={(e) => set('alipay_public_key', e.target.value)} /></Field>
            <TextField label="微信 API v3 Key" value={form.wechat_api_v3_key} onChange={(v) => set('wechat_api_v3_key', v)} type="password" />
          </Section>
        </div>
        </>
      )}
    </div>
  );
}

function ReadinessPanel({
  data,
  loading,
  fetching,
  onRefresh,
}: {
  data?: AdminSystemReadinessResp;
  loading: boolean;
  fetching: boolean;
  onRefresh: () => void;
}) {
  const groups = groupReadinessChecks(data?.checks ?? []);
  const overall = data?.overall ?? 'warn';
  const title = overall === 'error' ? '存在阻断项' : overall === 'warn' ? '存在待确认项' : '配置体检通过';
  return (
    <section className="card card-section space-y-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <span className={`grid h-9 w-9 place-items-center rounded-md ${overall === 'error' ? 'bg-danger/10 text-danger' : overall === 'warn' ? 'bg-warning/10 text-warning' : 'bg-success/10 text-success'}`}>
            {overall === 'error' ? <XCircle size={18} /> : overall === 'warn' ? <AlertTriangle size={18} /> : <CheckCircle2 size={18} />}
          </span>
          <div>
            <h2 className="text-h5 font-semibold text-text-primary">上线配置体检 · {title}</h2>
            <p className="text-small text-text-tertiary mt-0.5">
              只读检查 SMTP、支付宝、Provider 路由和存储配置，不会发送邮件、不会请求支付网关、不会暴露密钥。
              {data?.refreshed_at ? ` 刷新于 ${formatUnix(data.refreshed_at)}` : ''}
            </p>
          </div>
        </div>
        <button type="button" className="btn btn-outline btn-sm" disabled={fetching} onClick={onRefresh}>
          <RefreshCw size={14} className={fetching ? 'animate-spin' : ''} /> 刷新体检
        </button>
      </header>
      {loading ? (
        <div className="rounded-md border border-border bg-surface-2 p-4 text-center text-small text-text-tertiary">体检中...</div>
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            <ReadinessCount label="通过" value={data?.summary.ok ?? 0} tone="ok" />
            <ReadinessCount label="提醒" value={data?.summary.warn ?? 0} tone="warn" />
            <ReadinessCount label="阻断" value={data?.summary.error ?? 0} tone="error" />
          </div>
          <div className="grid gap-3 xl:grid-cols-2">
            {Object.entries(groups).map(([category, checks]) => (
              <div key={category} className="rounded-md border border-border bg-surface-2 p-3">
                <div className="mb-2 text-small font-semibold text-text-primary">{readinessCategoryLabel(category)}</div>
                <div className="grid gap-2">
                  {checks.map((check) => (
                    <ReadinessRow key={`${check.category}-${check.key}`} check={check} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}

function ReadinessCount({ label, value, tone }: { label: string; value: number; tone: 'ok' | 'warn' | 'error' }) {
  const cls = tone === 'error' ? 'text-danger bg-danger/10 border-danger/20' : tone === 'warn' ? 'text-warning bg-warning/10 border-warning/20' : 'text-success bg-success/10 border-success/20';
  return (
    <div className={`rounded-md border p-3 ${cls}`}>
      <div className="text-small opacity-80">{label}</div>
      <div className="mt-1 text-h4 font-semibold">{value}</div>
    </div>
  );
}

function ReadinessRow({ check }: { check: AdminSystemReadinessCheck }) {
  const icon = check.status === 'error' ? <XCircle size={15} /> : check.status === 'warn' ? <AlertTriangle size={15} /> : <CheckCircle2 size={15} />;
  const tone = check.status === 'error' ? 'text-danger' : check.status === 'warn' ? 'text-warning' : 'text-success';
  return (
    <div className="rounded-md border border-border bg-surface p-2">
      <div className="flex items-start gap-2">
        <span className={`mt-0.5 ${tone}`}>{icon}</span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-small font-semibold text-text-primary">{check.label}</span>
            {check.required && <span className="badge badge-outline">必需</span>}
            {check.source && <span className="text-tiny text-text-tertiary">来源 {check.source}</span>}
          </div>
          <p className="mt-1 break-words text-small text-text-tertiary">{check.message}</p>
        </div>
      </div>
    </div>
  );
}

function groupReadinessChecks(checks: AdminSystemReadinessCheck[]) {
  return checks.reduce<Record<string, AdminSystemReadinessCheck[]>>((acc, check) => {
    const next = acc[check.category] ?? [];
    next.push(check);
    acc[check.category] = next;
    return acc;
  }, {});
}

function readinessCategoryLabel(category: string) {
  switch (category) {
    case 'runtime':
      return '基础运行';
    case 'smtp':
      return '邮箱验证码';
    case 'payment':
      return '支付宝支付';
    case 'provider_routes':
      return 'Provider 路由';
    case 'storage':
      return '存储';
    default:
      return category;
  }
}

function Section({ icon, title, desc, children }: { icon: ReactNode; title: string; desc: string; children: ReactNode }) {
  return (
    <section className="card card-section space-y-4">
      <header className="flex items-start gap-3">
        <span className="grid place-items-center w-9 h-9 rounded-md bg-info-soft text-klein-500">{icon}</span>
        <div>
          <h2 className="text-h5 font-semibold text-text-primary">{title}</h2>
          <p className="text-small text-text-tertiary mt-0.5">{desc}</p>
        </div>
      </header>
      <div className="grid gap-3">{children}</div>
    </section>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="field"><span className="field-label">{label}</span>{children}</label>;
}

function TextField({ label, value, onChange, placeholder, type = 'text' }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string; type?: string }) {
  return <Field label={label}><input className="input" type={type} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} /></Field>;
}

function NumberField({ label, value, min, max, onChange }: { label: string; value: number; min?: number; max?: number; onChange: (v: number) => void }) {
  return <Field label={label}><input type="number" className="input" min={min} max={max} value={value} onChange={(e) => onChange(Number(e.target.value) || 0)} /></Field>;
}

function ProviderRouteDryRunPanel({
  value,
  result,
  loading,
  onChange,
  onSubmit,
}: {
  value: ProviderRouteTestReq;
  result?: ProviderRouteTestResp;
  loading: boolean;
  onChange: (v: ProviderRouteTestReq) => void;
  onSubmit: () => void;
}) {
  const setTest = <K extends keyof ProviderRouteTestReq>(key: K, next: ProviderRouteTestReq[K]) => {
    onChange({ ...value, [key]: next });
  };
  return (
    <div className="rounded-md border border-border bg-surface-2 p-3">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-small font-semibold text-text-primary">路由测试</div>
          <div className="text-small text-text-tertiary">Dry-run 当前模型会命中的 provider、上游模型和账号池可承接数量。</div>
        </div>
        <button type="button" className="btn btn-outline btn-sm" disabled={loading || !value.model_code.trim()} onClick={onSubmit}>
          <RefreshCw size={14} className={loading ? 'animate-spin' : ''} /> {loading ? '测试中...' : '测试路由'}
        </button>
      </div>
      <div className="grid gap-3 lg:grid-cols-[120px_minmax(180px,1fr)_140px]">
        <label className="field">
          <span className="field-label">入口</span>
          <select className="select" value={value.kind} onChange={(e) => setTest('kind', e.target.value as ProviderRouteTestReq['kind'])}>
            <option value="image">图片</option>
            <option value="text">文字</option>
            <option value="video">视频</option>
            <option value="chat">对话</option>
          </select>
        </label>
        <label className="field">
          <span className="field-label">模型编码</span>
          <input className="input" value={value.model_code} placeholder="gpt-image-2 / grok-4.20-fast" onChange={(e) => setTest('model_code', e.target.value)} />
        </label>
        <label className="field">
          <span className="field-label">兜底账号池</span>
          <select className="select" value={value.fallback_provider || ''} onChange={(e) => setTest('fallback_provider', e.target.value as ProviderRouteTestReq['fallback_provider'])}>
            <option value="">自动判断</option>
            <option value="gpt">GPT</option>
            <option value="grok">Grok</option>
          </select>
        </label>
      </div>
      {result && (
        <div className="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <RouteResultStat label="命中账号池" value={result.provider || '-'} />
          <RouteResultStat label="上游模型" value={result.upstream_model || '-'} />
          <RouteResultStat label="策略 / 认证" value={`${result.strategy || '-'}${result.auth_type ? ` / ${result.auth_type}` : ''}`} />
          <RouteResultStat label="可用账号" value={`${result.available_accounts}/${result.candidate_accounts}`} />
          <div className="md:col-span-2 xl:col-span-4 rounded-md border border-border bg-surface p-3 text-small text-text-tertiary">
            {result.matched_config ? (
              <span>已命中配置：{result.matched_kind || '-'} / {result.matched_model_code || '-'}</span>
            ) : (
              <span>未命中配置，使用兜底：{result.fallback_reason || '默认路由'}</span>
            )}
            {result.warning && <span className="ml-2 text-warning">{result.warning}</span>}
          </div>
          {result.candidates && result.candidates.length > 0 && (
            <div className="md:col-span-2 xl:col-span-4 rounded-md border border-border bg-surface p-3">
              <div className="mb-2 text-small font-semibold text-text-primary">候选路线链</div>
              <div className="grid gap-2">
                {result.candidates.map((route) => (
                  <div key={`${route.index}-${route.provider}-${route.upstream_model}`} className="grid gap-2 rounded-md border border-border bg-surface-2 p-2 text-small md:grid-cols-[52px_1fr_1fr_1fr]">
                    <div className="font-semibold text-text-primary">#{route.index}</div>
                    <div className="text-text-secondary">{route.provider || '-'} / {route.upstream_model || '-'}</div>
                    <div className="text-text-tertiary">{route.strategy || '-'}{route.auth_type ? ` / ${route.auth_type}` : ''}</div>
                    <div className={route.warning ? 'text-warning' : 'text-text-secondary'}>
                      可用 {route.available_accounts}/{route.candidate_accounts}{route.warning ? ` · ${route.warning}` : ''}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ProviderHealthPanel({
  providers,
  loading,
  fetching,
  refreshedAt,
  onRefresh,
}: {
  providers: ProviderHealthProviderItem[];
  loading: boolean;
  fetching: boolean;
  refreshedAt?: number;
  onRefresh: () => void;
}) {
  return (
    <div className="rounded-md border border-border bg-surface-2 p-3">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-small font-semibold text-text-primary">Provider 健康</div>
          <div className="text-small text-text-tertiary">
            只读汇总账号池状态，不会请求上游。{refreshedAt ? `刷新于 ${formatUnix(refreshedAt)}` : ''}
          </div>
        </div>
        <button type="button" className="btn btn-outline btn-sm" disabled={fetching} onClick={onRefresh}>
          <RefreshCw size={14} className={fetching ? 'animate-spin' : ''} /> 刷新健康
        </button>
      </div>
      {loading ? (
        <div className="rounded-md border border-border bg-surface p-4 text-center text-small text-text-tertiary">加载中...</div>
      ) : providers.length === 0 ? (
        <div className="rounded-md border border-border bg-surface p-4 text-center text-small text-text-tertiary">暂无账号池数据</div>
      ) : (
        <div className="grid gap-3">
          {providers.map((p) => {
            const risk = p.cooldown_active + p.token_expired + p.last_test_fail + p.quota_zero;
            return (
              <section key={p.provider} className="rounded-md border border-border bg-surface p-3">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-small font-semibold uppercase text-text-primary">{p.provider}</span>
                      {risk > 0 && <span className="badge badge-warning"><AlertTriangle size={12} /> {risk} 项风险</span>}
                    </div>
                    <div className="mt-1 text-tiny text-text-tertiary">成功 {p.success_count} · 失败计数 {p.error_count}</div>
                  </div>
                  <div className="grid grid-cols-2 gap-2 text-small sm:grid-cols-4">
                    <HealthStat label="可用/总数" value={`${p.available}/${p.total}`} />
                    <HealthStat label="熔断/冷却" value={`${p.broken}/${p.cooldown_active}`} />
                    <HealthStat label="测试失败" value={String(p.last_test_fail)} />
                    <HealthStat label="额度归零" value={String(p.quota_zero)} />
                  </div>
                </div>
                {p.auth_types.length > 0 && (
                  <div className="mt-3 flex flex-wrap gap-2">
                    {p.auth_types.map((a) => (
                      <span key={`${p.provider}-${a.auth_type}`} className="rounded-md border border-border bg-surface-2 px-2 py-1 text-tiny text-text-secondary">
                        {a.auth_type || '未标注'} · 可用 {a.available}/{a.total} · 测试失败 {a.last_test_fail}
                      </span>
                    ))}
                  </div>
                )}
                {p.recent_errors.length > 0 && (
                  <div className="mt-3 grid gap-2">
                    {p.recent_errors.map((e) => (
                      <div key={`${p.provider}-${e.account_id}`} className="rounded-md border border-warning/30 bg-warning/5 p-2 text-tiny">
                        <div className="flex flex-wrap justify-between gap-2 text-text-secondary">
                          <span>#{e.account_id} {e.name} · {e.auth_type} · 状态 {e.status}</span>
                          <span>{formatUnix(e.updated_at)}</span>
                        </div>
                        <div className="mt-1 line-clamp-2 break-words text-warning">{e.last_error || e.last_test_error || `失败计数 ${e.error_count}`}</div>
                      </div>
                    ))}
                  </div>
                )}
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}

function HealthStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-[86px] rounded-md border border-border bg-surface-2 px-2 py-1.5">
      <div className="text-tiny text-text-tertiary">{label}</div>
      <div className="mt-0.5 font-semibold text-text-primary">{value}</div>
    </div>
  );
}

function RouteResultStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-surface p-3">
      <div className="text-small text-text-tertiary">{label}</div>
      <div className="mt-1 break-words text-small font-semibold text-text-primary">{value}</div>
    </div>
  );
}

function formatUnix(value?: number) {
  if (!value) return '-';
  return new Date(value * 1000).toLocaleString();
}

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-md border border-border bg-surface-2 p-3">
      <div className="text-small font-medium text-text-primary">{label}</div>
      <button type="button" role="switch" aria-checked={checked} onClick={() => onChange(!checked)} className={'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition ' + (checked ? 'bg-klein-500' : 'bg-surface-3')}>
        <span className={'inline-block h-5 w-5 rounded-full bg-white shadow transition transform ' + (checked ? 'translate-x-5' : 'translate-x-0.5')} />
      </button>
    </div>
  );
}
