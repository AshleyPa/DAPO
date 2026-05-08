import { Plus, Trash2 } from 'lucide-react';

import type {
  ProviderRouteAuthType,
  ProviderRouteKind,
  ProviderRouteOption,
  ProviderRouteRule,
  ProviderRouteStrategy,
} from '../../lib/types';

const KIND_OPTIONS: Array<{ value: ProviderRouteKind; label: string }> = [
  { value: 'image', label: '图片' },
  { value: 'text', label: '文字' },
  { value: 'video', label: '视频' },
  { value: 'chat', label: '对话' },
  { value: '*', label: '全部' },
];

const STRATEGY_OPTIONS: Array<{ value: ProviderRouteStrategy; label: string }> = [
  { value: 'round_robin', label: '顺序轮询' },
  { value: 'weighted_rr', label: '权重优先' },
];

const AUTH_TYPE_OPTIONS: Array<{ value: ProviderRouteAuthType; label: string }> = [
  { value: '', label: '不限认证' },
  { value: 'api_key', label: 'API Key' },
  { value: 'cookie', label: 'Cookie' },
  { value: 'oauth', label: 'OAuth' },
];

const PROVIDER_OPTIONS = [
  { value: 'gpt', label: 'GPT 账号池' },
  { value: 'grok', label: 'Grok 账号池' },
];

const DEFAULT_ENABLED = true;

export function defaultProviderRoutes(): ProviderRouteRule[] {
  return cloneRoutes([
    {
      kind: 'image',
      model_code: 'gpt-image-2',
      enabled: true,
      strategy: 'weighted_rr',
      routes: [
        { provider: 'gpt', upstream_model: 'gpt-image-2', auth_type: 'oauth', priority: 1, weight: 100, enabled: true },
      ],
    },
    {
      kind: 'text',
      model_code: '*',
      enabled: true,
      strategy: 'weighted_rr',
      routes: [
        { provider: 'grok', upstream_model: '', auth_type: '', priority: 1, weight: 100, enabled: true },
      ],
    },
    {
      kind: 'video',
      model_code: '*',
      enabled: true,
      strategy: 'weighted_rr',
      routes: [
        { provider: 'grok', upstream_model: '', auth_type: '', priority: 1, weight: 100, enabled: true },
      ],
    },
  ]);
}

export function providerRoutesValue(v: unknown): ProviderRouteRule[] {
  if (Array.isArray(v)) return normalizeProviderRoutesLenient(v);
  if (typeof v === 'string' && v.trim()) {
    try {
      const parsed = JSON.parse(v);
      if (Array.isArray(parsed)) return normalizeProviderRoutesLenient(parsed);
    } catch {
      return defaultProviderRoutes();
    }
  }
  return defaultProviderRoutes();
}

export function normalizeProviderRoutes(value: ProviderRouteRule[]): ProviderRouteRule[] {
  if (!Array.isArray(value)) {
    throw new Error('模型路由配置必须是规则数组');
  }
  const seen = new Set<string>();
  return value.map((rule, index) => {
    const kind = normalizeKind(rule.kind, index);
    const modelCode = trimOrError(rule.model_code, `第 ${index + 1} 条规则的模型编码不能为空，可填写 * 表示通配`);
    const strategy = normalizeStrategy(rule.strategy, `第 ${index + 1} 条规则`);
    const enabled = rule.enabled ?? DEFAULT_ENABLED;
    const routes = normalizeRouteOptions(rule.routes, index, strategy, enabled);
    const duplicateKey = `${kind}:${modelCode.toLowerCase()}`;
    if (enabled) {
      if (seen.has(duplicateKey)) {
        throw new Error(`启用规则重复：${kind} / ${modelCode}`);
      }
      seen.add(duplicateKey);
    }
    return { kind, model_code: modelCode, enabled, strategy, routes };
  });
}

export default function ProviderRoutesEditor({
  value,
  onChange,
}: {
  value: ProviderRouteRule[];
  onChange: (value: ProviderRouteRule[]) => void;
}) {
  const updateRule = (index: number, patch: Partial<ProviderRouteRule>) => {
    onChange(value.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)));
  };

  const removeRule = (index: number) => {
    onChange(value.filter((_, i) => i !== index));
  };

  const addRule = () => {
    onChange([...value, createProviderRouteRule()]);
  };

  const updateRoute = (ruleIndex: number, routeIndex: number, patch: Partial<ProviderRouteOption>) => {
    onChange(value.map((rule, i) => {
      if (i !== ruleIndex) return rule;
      return {
        ...rule,
        routes: rule.routes.map((route, j) => (j === routeIndex ? { ...route, ...patch } : route)),
      };
    }));
  };

  const addRoute = (ruleIndex: number) => {
    onChange(value.map((rule, i) => (i === ruleIndex ? { ...rule, routes: [...rule.routes, createProviderRouteOption()] } : rule)));
  };

  const removeRoute = (ruleIndex: number, routeIndex: number) => {
    onChange(value.map((rule, i) => {
      if (i !== ruleIndex) return rule;
      return { ...rule, routes: rule.routes.filter((_, j) => j !== routeIndex) };
    }));
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-surface-2 p-3">
        <div className="text-small text-text-tertiary">
          按入口和模型编码配置上游账号池。保存前会校验字段，留空上游模型时默认使用用户请求的模型编码。
        </div>
        <button type="button" className="btn btn-outline btn-sm" onClick={addRule}>
          <Plus size={14} /> 新增规则
        </button>
      </div>

      {value.length === 0 ? (
        <div className="rounded-md border border-dashed border-border bg-surface-2 p-8 text-center text-small text-text-tertiary">
          暂无模型路由规则。未配置时，生成服务会回退到原有账号池选择逻辑。
        </div>
      ) : (
        value.map((rule, ruleIndex) => (
          <div key={ruleIndex} className="rounded-md border border-border bg-surface p-3 shadow-soft">
            <div className="grid gap-3 xl:grid-cols-[120px_minmax(180px,1fr)_150px_110px_auto]">
              <label className="field">
                <span className="field-label">入口</span>
                <select
                  className="select"
                  value={rule.kind}
                  onChange={(e) => updateRule(ruleIndex, { kind: e.target.value as ProviderRouteKind })}
                >
                  {KIND_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                </select>
              </label>
              <label className="field">
                <span className="field-label">模型编码</span>
                <input
                  className="input"
                  value={rule.model_code}
                  placeholder="gpt-image-2 或 *"
                  onChange={(e) => updateRule(ruleIndex, { model_code: e.target.value })}
                />
              </label>
              <label className="field">
                <span className="field-label">策略</span>
                <select
                  className="select"
                  value={rule.strategy || 'round_robin'}
                  onChange={(e) => updateRule(ruleIndex, { strategy: e.target.value as ProviderRouteStrategy })}
                >
                  {STRATEGY_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                </select>
              </label>
              <label className="field">
                <span className="field-label">状态</span>
                <button
                  type="button"
                  className={rule.enabled === false ? 'btn btn-ghost btn-sm justify-center' : 'btn btn-outline btn-sm justify-center'}
                  onClick={() => updateRule(ruleIndex, { enabled: !(rule.enabled ?? true) })}
                >
                  {rule.enabled === false ? '停用' : '启用'}
                </button>
              </label>
              <div className="flex items-end gap-2">
                <button type="button" className="btn btn-outline btn-sm" onClick={() => addRoute(ruleIndex)}>
                  <Plus size={14} /> 新增上游
                </button>
                <button type="button" className="btn btn-danger-ghost btn-icon btn-sm" onClick={() => removeRule(ruleIndex)} title="删除规则">
                  <Trash2 size={14} />
                </button>
              </div>
            </div>

            <div className="mt-3 overflow-x-auto rounded-md border border-border">
              <table className="data-table min-w-[860px] text-small">
                <thead>
                  <tr>
                    <th>上游账号池</th>
                    <th>上游模型</th>
                    <th>认证类型</th>
                    <th>优先级</th>
                    <th>权重</th>
                    <th>状态</th>
                    <th className="w-[72px]">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {rule.routes.length === 0 ? (
                    <tr>
                      <td colSpan={7} className="text-center text-text-tertiary">此规则还没有上游路线。</td>
                    </tr>
                  ) : (
                    rule.routes.map((route, routeIndex) => (
                      <tr key={routeIndex}>
                        <td>
                          <select
                            className="select"
                            value={route.provider}
                            onChange={(e) => updateRoute(ruleIndex, routeIndex, { provider: e.target.value })}
                          >
                            {PROVIDER_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                          </select>
                        </td>
                        <td>
                          <input
                            className="input"
                            value={route.upstream_model || ''}
                            placeholder="留空则同模型编码"
                            onChange={(e) => updateRoute(ruleIndex, routeIndex, { upstream_model: e.target.value })}
                          />
                        </td>
                        <td>
                          <select
                            className="select"
                            value={route.auth_type || ''}
                            onChange={(e) => updateRoute(ruleIndex, routeIndex, { auth_type: e.target.value as ProviderRouteAuthType })}
                          >
                            {AUTH_TYPE_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                          </select>
                        </td>
                        <td>
                          <input
                            className="input"
                            type="number"
                            min={0}
                            max={10000}
                            value={route.priority ?? 1}
                            onChange={(e) => updateRoute(ruleIndex, routeIndex, { priority: Number(e.target.value) || 0 })}
                          />
                        </td>
                        <td>
                          <input
                            className="input"
                            type="number"
                            min={1}
                            max={10000}
                            value={route.weight ?? 100}
                            onChange={(e) => updateRoute(ruleIndex, routeIndex, { weight: Number(e.target.value) || 1 })}
                          />
                        </td>
                        <td>
                          <button
                            type="button"
                            className={route.enabled === false ? 'btn btn-ghost btn-sm' : 'btn btn-outline btn-sm'}
                            onClick={() => updateRoute(ruleIndex, routeIndex, { enabled: !(route.enabled ?? true) })}
                          >
                            {route.enabled === false ? '停用' : '启用'}
                          </button>
                        </td>
                        <td>
                          <button
                            type="button"
                            className="btn btn-danger-ghost btn-icon btn-sm"
                            onClick={() => removeRoute(ruleIndex, routeIndex)}
                            title="删除上游路线"
                          >
                            <Trash2 size={14} />
                          </button>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ))
      )}

      <details className="rounded-md border border-border bg-surface-2 p-3 text-small text-text-tertiary">
        <summary className="cursor-pointer select-none text-text-secondary">查看当前 JSON 预览</summary>
        <pre className="mt-3 max-h-[240px] overflow-auto rounded-md bg-surface p-3 font-mono text-[12px] leading-5 text-text-secondary">
          {JSON.stringify(value, null, 2)}
        </pre>
      </details>
    </div>
  );
}

function normalizeProviderRoutesLenient(value: unknown[]): ProviderRouteRule[] {
  try {
    return normalizeProviderRoutes(value as ProviderRouteRule[]);
  } catch {
    return defaultProviderRoutes();
  }
}

function createProviderRouteRule(): ProviderRouteRule {
  return {
    kind: 'image',
    model_code: '*',
    enabled: true,
    strategy: 'weighted_rr',
    routes: [createProviderRouteOption()],
  };
}

function createProviderRouteOption(): ProviderRouteOption {
  return { provider: 'gpt', upstream_model: '', auth_type: '', priority: 1, weight: 100, enabled: true };
}

function normalizeRouteOptions(
  routes: ProviderRouteOption[] | undefined,
  ruleIndex: number,
  strategy: ProviderRouteStrategy,
  ruleEnabled: boolean,
): ProviderRouteOption[] {
  if (!Array.isArray(routes)) {
    if (!ruleEnabled) return [];
    throw new Error(`第 ${ruleIndex + 1} 条规则必须至少配置 1 条上游路线`);
  }
  const normalized = routes.map((route, routeIndex) => {
    const provider = trimOrError(route.provider, `第 ${ruleIndex + 1} 条规则第 ${routeIndex + 1} 条上游账号池不能为空`);
    if (!['gpt', 'grok'].includes(provider)) {
      throw new Error(`第 ${ruleIndex + 1} 条规则第 ${routeIndex + 1} 条上游账号池只能是 gpt 或 grok`);
    }
    const priority = normalizeInteger(route.priority ?? 1, 0, 10000, `第 ${ruleIndex + 1} 条规则第 ${routeIndex + 1} 条优先级`);
    const weight = normalizeInteger(route.weight ?? 100, 1, 10000, `第 ${ruleIndex + 1} 条规则第 ${routeIndex + 1} 条权重`);
    if (strategy === 'weighted_rr' && weight <= 0) {
      throw new Error(`第 ${ruleIndex + 1} 条规则第 ${routeIndex + 1} 条权重必须大于 0`);
    }
    const authType = normalizeAuthType(route.auth_type, `第 ${ruleIndex + 1} 条规则第 ${routeIndex + 1} 条认证类型`);
    return {
      provider,
      upstream_model: (route.upstream_model || '').trim(),
      auth_type: authType,
      priority,
      weight,
      enabled: route.enabled ?? DEFAULT_ENABLED,
    };
  });
  if (ruleEnabled && !normalized.some((route) => route.enabled !== false)) {
    throw new Error(`第 ${ruleIndex + 1} 条启用规则必须至少保留 1 条启用上游路线`);
  }
  return normalized;
}

function normalizeKind(value: unknown, index: number): ProviderRouteKind {
  const kind = String(value || '').trim().toLowerCase();
  if (KIND_OPTIONS.some((item) => item.value === kind)) return kind as ProviderRouteKind;
  throw new Error(`第 ${index + 1} 条规则入口只能是 image/text/video/chat/*`);
}

function normalizeStrategy(value: unknown, label: string): ProviderRouteStrategy {
  const strategy = String(value || 'round_robin').trim().toLowerCase();
  if (strategy === 'round_robin') return 'round_robin';
  if (['weighted', 'weighted_rr', 'weight', 'weight_rr'].includes(strategy)) return 'weighted_rr';
  throw new Error(`${label} 的策略只能是 round_robin 或 weighted_rr`);
}

function normalizeAuthType(value: unknown, label: string): ProviderRouteAuthType {
  const authType = String(value || '').trim().toLowerCase();
  if (AUTH_TYPE_OPTIONS.some((item) => item.value === authType)) return authType as ProviderRouteAuthType;
  throw new Error(`${label} 只能是 api_key/cookie/oauth 或留空`);
}

function normalizeInteger(value: unknown, min: number, max: number, label: string) {
  const n = Number(value);
  if (!Number.isInteger(n) || n < min || n > max) {
    throw new Error(`${label}必须是 ${min}-${max} 的整数`);
  }
  return n;
}

function trimOrError(value: unknown, message: string) {
  const text = String(value || '').trim();
  if (!text) throw new Error(message);
  return text;
}

function cloneRoutes(value: ProviderRouteRule[]) {
  return JSON.parse(JSON.stringify(value)) as ProviderRouteRule[];
}
