import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Info, KeyRound, Plus, RefreshCw, Save, Search, Trash2, X } from 'lucide-react';
import { useEffect, useMemo, useState, type ReactNode } from 'react';

import { ApiError } from '../../lib/api';
import { apiChannelsApi, proxiesApi } from '../../lib/services';
import type { APIChannelAdapter, APIChannelBody, APIChannelItem, APIChannelKeyBody, APIChannelKeyItem, ProxyItem } from '../../lib/types';
import { toast } from '../../stores/toast';

const ADAPTER_OPTIONS: Array<{ value: APIChannelAdapter; label: string }> = [
  { value: 'openai_compatible_chat', label: 'OpenAI 兼容 Chat' },
  { value: 'openai_compatible_images', label: 'OpenAI 兼容 Images' },
  { value: 'openai_compatible_video', label: 'OpenAI 兼容 Video' },
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'nova_async', label: 'Nova 异步' },
  { value: 'pic2api_images', label: 'Pic2API Images' },
];

const CAPABILITY_OPTIONS = [
  { value: 'chat', label: '文字' },
  { value: 'image', label: '图片' },
  { value: 'video', label: '视频' },
  { value: 'audio', label: '音频' },
  { value: 'embedding', label: 'Embedding' },
];

interface ChannelForm {
  code: string;
  name: string;
  provider_name: string;
  adapter: APIChannelAdapter;
  base_url: string;
  api_key: string;
  clear_api_key: boolean;
  models_text: string;
  capabilities: string[];
  proxy_id: number;
  priority: number;
  weight: number;
  rpm_limit: number;
  tpm_limit: number;
  timeout_seconds: number;
  status: 0 | 1;
  remark: string;
}

interface ChannelKeyForm {
  name: string;
  api_key: string;
  priority: number;
  weight: number;
  rpm_limit: number;
  tpm_limit: number;
  status: 0 | 1;
}

const DEFAULT_FORM: ChannelForm = {
  code: '',
  name: '',
  provider_name: '',
  adapter: 'openai_compatible_chat',
  base_url: '',
  api_key: '',
  clear_api_key: false,
  models_text: '',
  capabilities: ['chat'],
  proxy_id: 0,
  priority: 100,
  weight: 100,
  rpm_limit: 0,
  tpm_limit: 0,
  timeout_seconds: 300,
  status: 1,
  remark: '',
};

const DEFAULT_KEY_FORM: ChannelKeyForm = {
  name: '',
  api_key: '',
  priority: 100,
  weight: 100,
  rpm_limit: 0,
  tpm_limit: 0,
  status: 1,
};

const CHANNEL_PRESETS: Array<{ label: string; description: string; values: Partial<ChannelForm> }> = [
  {
    label: 'MiMo 文字 API',
    description: 'OpenAI 兼容 Chat；只预填地址和模型，不保存密钥。',
    values: {
      code: 'mimo-official',
      name: 'MiMo 官方 API',
      provider_name: 'mimo',
      adapter: 'openai_compatible_chat',
      base_url: 'https://token-plan-cn.xiaomimimo.com/v1',
      models_text: 'mimo-v2.5-pro',
      capabilities: ['chat'],
      timeout_seconds: 300,
      priority: 100,
      weight: 100,
      status: 1,
    },
  },
  {
    label: 'DeepSeek 文字 API',
    description: 'OpenAI 兼容 Chat；适合 deepseek-chat / deepseek-reasoner。',
    values: {
      code: 'deepseek-official',
      name: 'DeepSeek 官方 API',
      provider_name: 'deepseek',
      adapter: 'openai_compatible_chat',
      base_url: 'https://api.deepseek.com/v1',
      models_text: 'deepseek-chat, deepseek-reasoner',
      capabilities: ['chat'],
      timeout_seconds: 300,
      priority: 100,
      weight: 100,
      status: 1,
    },
  },
];

export default function APIChannelsPage() {
  const qc = useQueryClient();
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState<'' | 0 | 1>('');
  const [adapter, setAdapter] = useState('');
  const [editing, setEditing] = useState<APIChannelItem | null>(null);
  const [keyChannel, setKeyChannel] = useState<APIChannelItem | null>(null);
  const [creating, setCreating] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [form, setForm] = useState<ChannelForm>(DEFAULT_FORM);

  const query = useQuery({
    queryKey: ['admin', 'api-channels', keyword, status, adapter],
    queryFn: () => apiChannelsApi.list({
      keyword: keyword.trim() || undefined,
      status: status === '' ? undefined : status,
      adapter: adapter || undefined,
      page: 1,
      page_size: 100,
    }),
  });
  const proxies = useQuery({
    queryKey: ['admin', 'proxies', 'api-channel-options'],
    queryFn: () => proxiesApi.list({ page: 1, page_size: 200, status: 1 }),
  });

  useEffect(() => {
    if (editing) {
      setForm(formFromChannel(editing));
    } else if (creating) {
      setForm(DEFAULT_FORM);
    }
  }, [editing, creating]);

  const closeDialog = () => {
    setEditing(null);
    setCreating(false);
  };

  const save = useMutation({
    mutationFn: async () => {
      const body = bodyFromForm(form, !editing);
      if (editing) {
        await apiChannelsApi.update(editing.id, body);
        return;
      }
      await apiChannelsApi.create(body as APIChannelBody);
    },
    onSuccess: () => {
      toast.success(editing ? 'API 渠道已更新' : 'API 渠道已新增');
      closeDialog();
      qc.invalidateQueries({ queryKey: ['admin', 'api-channels'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const remove = useMutation({
    mutationFn: (id: number) => apiChannelsApi.remove(id),
    onSuccess: () => {
      toast.success('API 渠道已删除');
      qc.invalidateQueries({ queryKey: ['admin', 'api-channels'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const test = useMutation({
    mutationFn: async (id: number) => {
      setTestingId(id);
      return apiChannelsApi.test(id);
    },
    onSuccess: (res) => {
      if (res.ok) {
        toast.success(`渠道测试通过，${res.latency_ms}ms${credentialSourceSuffix(res)}`);
      } else {
        toast.error(res.error || '渠道测试失败');
      }
      qc.invalidateQueries({ queryKey: ['admin', 'api-channels'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
    onSettled: () => setTestingId(null),
  });

  const proxyOptions: ProxyItem[] = proxies.data?.list ?? [];
  const dialogOpen = creating || editing;

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title">API 渠道</h1>
          <p className="page-subtitle">独立管理官方 API / OpenAI 兼容接口，后续模型路由会从这里选择可用渠道。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="btn btn-outline btn-md" onClick={() => query.refetch()} disabled={query.isFetching}>
            <RefreshCw size={16} className={query.isFetching ? 'animate-spin' : ''} /> 刷新
          </button>
          <button className="btn btn-primary btn-md" onClick={() => setCreating(true)}>
            <Plus size={16} /> 新增渠道
          </button>
        </div>
      </header>

      <div className="card flex flex-wrap items-center gap-3 p-3">
        <div className="relative min-w-[260px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-tertiary" size={16} />
          <input
            className="input pl-9"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索名称 / 编码 / Provider / Base URL"
          />
        </div>
        <select className="select w-[160px]" value={adapter} onChange={(e) => setAdapter(e.target.value)}>
          <option value="">全部协议</option>
          {ADAPTER_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
        </select>
        <select className="select w-[140px]" value={status} onChange={(e) => setStatus(e.target.value === '' ? '' : Number(e.target.value) as 0 | 1)}>
          <option value="">全部状态</option>
          <option value={1}>启用</option>
          <option value={0}>停用</option>
        </select>
      </div>

      <div className="card table-wrap">
        <table className="data-table min-w-[1240px]">
          <thead>
            <tr>
              <th>渠道</th>
              <th>协议</th>
              <th>Base URL</th>
              <th>模型</th>
              <th>能力</th>
              <th>优先级 / 权重</th>
              <th>限流</th>
              <th>最近测试</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {query.isLoading ? (
              <tr><td colSpan={10} className="text-center text-text-tertiary">加载中...</td></tr>
            ) : (query.data?.list.length ?? 0) === 0 ? (
              <tr><td colSpan={10} className="text-center text-text-tertiary">暂无 API 渠道</td></tr>
            ) : (
              query.data?.list.map((item) => (
                <tr key={item.id}>
                  <td>
                    <div className="font-semibold text-text-primary">{item.name}</div>
                    <div className="mt-1 text-tiny text-text-tertiary">{item.code} · {item.provider_name || '未标注'}</div>
                  </td>
                  <td>{adapterLabel(item.adapter)}</td>
                  <td className="max-w-[280px] truncate" title={item.base_url}>{item.base_url}</td>
                  <td className="max-w-[220px]">
                    <TagList items={item.models} empty="未限制" />
                  </td>
                  <td><TagList items={item.capabilities} empty="-" /></td>
                  <td>{item.priority} / {item.weight}</td>
                  <td>
                    <div>{item.rpm_limit || '-'} RPM</div>
                    <div className="text-tiny text-text-tertiary">{item.tpm_limit || '-'} TPM</div>
                    <div className="mt-1 text-tiny text-text-tertiary">Keys {item.enabled_key_count || 0}/{item.key_count || 0}</div>
                    <div className={item.has_api_key ? 'mt-1 text-tiny text-amber-700' : 'mt-1 text-tiny text-text-tertiary'}>
                      Legacy {item.has_api_key ? '已配置，建议迁移' : '未配置'}
                    </div>
                  </td>
                  <td><ChannelHealth item={item} /></td>
                  <td>
                    <button className={item.status === 1 ? 'btn btn-outline btn-sm' : 'btn btn-ghost btn-sm'} onClick={() => setEditing(item)}>
                      {item.status === 1 ? '启用' : '停用'}
                    </button>
                  </td>
                  <td>
                    <div className="flex gap-2">
                      <button className="btn btn-outline btn-sm" onClick={() => test.mutate(item.id)} disabled={testingId === item.id}>
                        <RefreshCw size={14} className={testingId === item.id ? 'animate-spin' : ''} />
                        测试
                      </button>
                      <button className="btn btn-outline btn-sm" onClick={() => setKeyChannel(item)}>
                        <KeyRound size={14} />
                        Key 池
                      </button>
                      <button className="btn btn-outline btn-sm" onClick={() => setEditing(item)}>编辑</button>
                      <button
                        className="btn btn-danger-ghost btn-icon btn-sm"
                        onClick={() => {
                          if (window.confirm(`确定删除 API 渠道「${item.name}」吗？`)) remove.mutate(item.id);
                        }}
                        title="删除"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {dialogOpen && (
        <ChannelDialog
          form={form}
          editing={Boolean(editing)}
          proxyOptions={proxyOptions}
          saving={save.isPending}
          onChange={setForm}
          onClose={closeDialog}
          onSubmit={() => save.mutate()}
        />
      )}
      {keyChannel && <ChannelKeysDialog channel={keyChannel} onClose={() => setKeyChannel(null)} />}
    </div>
  );
}

function ChannelDialog({
  form,
  editing,
  proxyOptions,
  saving,
  onChange,
  onClose,
  onSubmit,
}: {
  form: ChannelForm;
  editing: boolean;
  proxyOptions: ProxyItem[];
  saving: boolean;
  onChange: (form: ChannelForm) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const set = <K extends keyof ChannelForm>(key: K, value: ChannelForm[K]) => onChange({ ...form, [key]: value });
  const selectedCaps = useMemo(() => new Set(form.capabilities), [form.capabilities]);
  const warnings = useMemo(() => channelFormWarnings(form, editing), [form, editing]);
  const applyPreset = (values: Partial<ChannelForm>) => {
    onChange({ ...form, ...values, api_key: form.api_key });
  };
  const toggleCap = (cap: string) => {
    const next = selectedCaps.has(cap)
      ? form.capabilities.filter((item) => item !== cap)
      : [...form.capabilities, cap];
    set('capabilities', next);
  };

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-surface-overlay p-4">
      <div className="w-full max-w-4xl rounded-lg border border-border bg-surface-1 shadow-4">
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div className="flex items-center gap-2">
            <KeyRound size={18} className="text-klein-blue" />
            <h2 className="text-h4 text-text-primary">{editing ? '编辑 API 渠道' : '新增 API 渠道'}</h2>
          </div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}><X size={16} /></button>
        </div>
        <div className="grid max-h-[72vh] gap-4 overflow-y-auto p-5 md:grid-cols-2">
          {!editing && (
            <div className="md:col-span-2 rounded-md border border-border bg-surface-2 p-3">
              <div className="flex items-center gap-2 text-small font-semibold text-text-primary">
                <Info size={15} className="text-klein-blue" />
                常用文字 API 模板
              </div>
              <div className="mt-2 grid gap-2 md:grid-cols-2">
                {CHANNEL_PRESETS.map((preset) => (
                  <button key={preset.label} type="button" className="btn btn-outline btn-sm justify-start" onClick={() => applyPreset(preset.values)}>
                    <KeyRound size={14} />
                    <span className="text-left">
                      <span className="block">{preset.label}</span>
                      <span className="block text-tiny font-normal text-text-tertiary">{preset.description}</span>
                    </span>
                  </button>
                ))}
              </div>
            </div>
          )}
          {warnings.length > 0 && (
            <div className="md:col-span-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-small text-amber-800">
              {warnings.map((warning) => <div key={warning}>{warning}</div>)}
            </div>
          )}
          <TextField label="渠道编码" value={form.code} onChange={(v) => set('code', v)} placeholder="mimo-official" />
          <TextField label="渠道名称" value={form.name} onChange={(v) => set('name', v)} placeholder="MiMo 官方 API" />
          <TextField label="Provider 标识" value={form.provider_name} onChange={(v) => set('provider_name', v)} placeholder="mimo / deepseek / openai" />
          <Field label="协议适配">
            <select className="select" value={form.adapter} onChange={(e) => set('adapter', e.target.value as APIChannelAdapter)}>
              {ADAPTER_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </Field>
          <TextField className="md:col-span-2" label="Base URL" value={form.base_url} onChange={(v) => set('base_url', v)} placeholder="https://api.xiaomimimo.com/v1" />
          <TextField
            className="md:col-span-2"
            label={editing ? 'Legacy API Key（留空则不修改）' : 'Legacy API Key（可选）'}
            value={form.api_key}
            onChange={(v) => set('api_key', v)}
            type="password"
          />
          {editing && (
            <label className="md:col-span-2 flex items-start gap-2 rounded-md border border-border bg-surface-2 p-3 text-small text-text-secondary">
              <input
                className="mt-1"
                type="checkbox"
                checked={form.clear_api_key}
                onChange={(e) => set('clear_api_key', e.target.checked)}
              />
              <span>
                清除 Legacy API Key
                <span className="mt-1 block text-tiny text-text-tertiary">
                  保存后会清空渠道级旧密钥并重置健康检测；正式运行建议只使用 Key 池。
                </span>
              </span>
            </label>
          )}
          <Field className="md:col-span-2" label="可服务模型">
            <textarea
              className="input min-h-[84px] font-mono text-small"
              value={form.models_text}
              onChange={(e) => set('models_text', e.target.value)}
              placeholder="mimo-v2.5-pro, deepseek-chat"
            />
          </Field>
          <Field className="md:col-span-2" label="能力">
            <div className="flex flex-wrap gap-2">
              {CAPABILITY_OPTIONS.map((item) => (
                <button
                  key={item.value}
                  type="button"
                  className={selectedCaps.has(item.value) ? 'btn btn-outline btn-sm' : 'btn btn-ghost btn-sm'}
                  onClick={() => toggleCap(item.value)}
                >
                  {item.label}
                </button>
              ))}
            </div>
          </Field>
          <Field label="代理">
            <select className="select" value={form.proxy_id} onChange={(e) => set('proxy_id', Number(e.target.value) || 0)}>
              <option value={0}>不指定</option>
              {proxyOptions.map((p) => <option key={p.id} value={p.id}>[{p.protocol}] {p.name}</option>)}
            </select>
          </Field>
          <Field label="状态">
            <select className="select" value={form.status} onChange={(e) => set('status', Number(e.target.value) === 1 ? 1 : 0)}>
              <option value={1}>启用</option>
              <option value={0}>停用</option>
            </select>
          </Field>
          <NumberField label="优先级" value={form.priority} min={0} max={10000} onChange={(v) => set('priority', v)} />
          <NumberField label="权重" value={form.weight} min={1} max={10000} onChange={(v) => set('weight', v)} />
          <NumberField label="RPM 限制" value={form.rpm_limit} min={0} onChange={(v) => set('rpm_limit', v)} />
          <NumberField label="TPM 限制" value={form.tpm_limit} min={0} onChange={(v) => set('tpm_limit', v)} />
          <NumberField label="超时（秒）" value={form.timeout_seconds} min={5} max={1800} onChange={(v) => set('timeout_seconds', v)} />
          <TextField label="备注" value={form.remark} onChange={(v) => set('remark', v)} />
        </div>
        <div className="flex justify-end gap-2 border-t border-border px-5 py-4">
          <button className="btn btn-outline btn-md" onClick={onClose}>取消</button>
          <button className="btn btn-primary btn-md" onClick={onSubmit} disabled={saving}>
            <Save size={16} /> {saving ? '保存中...' : '保存'}
          </button>
        </div>
      </div>
    </div>
  );
}

function ChannelKeysDialog({ channel, onClose }: { channel: APIChannelItem; onClose: () => void }) {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<APIChannelKeyItem | null>(null);
  const [form, setForm] = useState<ChannelKeyForm>(DEFAULT_KEY_FORM);
  const creating = !editing;
  const q = useQuery({
    queryKey: ['admin', 'api-channels', channel.id, 'keys'],
    queryFn: () => apiChannelsApi.keys(channel.id),
  });

  const set = <K extends keyof ChannelKeyForm>(key: K, value: ChannelKeyForm[K]) => setForm({ ...form, [key]: value });

  const resetCreate = () => {
    setEditing(null);
    setForm(DEFAULT_KEY_FORM);
  };

  const save = useMutation({
    mutationFn: async () => {
      const body = keyBodyFromForm(form, creating);
      if (editing) {
        await apiChannelsApi.updateKey(channel.id, editing.id, body);
        return;
      }
      await apiChannelsApi.createKey(channel.id, body as APIChannelKeyBody);
    },
    onSuccess: () => {
      toast.success(editing ? 'API Key 已更新' : 'API Key 已新增');
      resetCreate();
      qc.invalidateQueries({ queryKey: ['admin', 'api-channels', channel.id, 'keys'] });
      qc.invalidateQueries({ queryKey: ['admin', 'api-channels'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const remove = useMutation({
    mutationFn: (keyID: number) => apiChannelsApi.removeKey(channel.id, keyID),
    onSuccess: () => {
      toast.success('API Key 已删除');
      resetCreate();
      qc.invalidateQueries({ queryKey: ['admin', 'api-channels', channel.id, 'keys'] });
      qc.invalidateQueries({ queryKey: ['admin', 'api-channels'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const editKey = (item: APIChannelKeyItem) => {
    setEditing(item);
    setForm({
      name: item.name || '',
      api_key: '',
      priority: item.priority || 100,
      weight: item.weight || 100,
      rpm_limit: item.rpm_limit || 0,
      tpm_limit: item.tpm_limit || 0,
      status: item.status === 1 ? 1 : 0,
    });
  };

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-surface-overlay p-4">
      <div className="w-full max-w-5xl rounded-lg border border-border bg-surface-1 shadow-4">
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-h4 text-text-primary">
              <KeyRound size={18} className="text-klein-blue" /> API Key 池
            </div>
            <div className="mt-1 truncate text-small text-text-tertiary">{channel.name} · {channel.code}</div>
          </div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}><X size={16} /></button>
        </div>
        <div className="grid max-h-[76vh] gap-4 overflow-y-auto p-5 lg:grid-cols-[1fr_340px]">
          <div className="table-wrap rounded-md border border-border">
            <table className="data-table min-w-[760px]">
              <thead>
                <tr>
                  <th>名称</th>
                  <th>优先级 / 权重</th>
                  <th>限流</th>
                  <th>最近使用</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {q.isLoading ? (
                  <tr><td colSpan={6} className="text-center text-text-tertiary">加载中...</td></tr>
                ) : (q.data?.length ?? 0) === 0 ? (
                  <tr><td colSpan={6} className="text-center text-text-tertiary">暂无 Key，可在右侧新增</td></tr>
                ) : (
                  q.data?.map((item) => (
                    <tr key={item.id}>
                      <td>
                        <div className="font-semibold text-text-primary">{item.name || `Key #${item.id}`}</div>
                        <div className="mt-1 text-tiny text-text-tertiary">{item.has_api_key ? '已加密保存' : '未配置密钥'}</div>
                        {item.last_error && <div className="mt-1 max-w-[220px] truncate text-tiny text-red-600" title={item.last_error}>{item.last_error}</div>}
                      </td>
                      <td>{item.priority} / {item.weight}</td>
                      <td>
                        <div>{item.rpm_limit || '-'} RPM</div>
                        <div className="text-tiny text-text-tertiary">{item.tpm_limit || '-'} TPM</div>
                      </td>
                      <td>{item.last_used_at ? formatTime(item.last_used_at) : '-'}</td>
                      <td>{item.status === 1 ? '启用' : '停用'}</td>
                      <td>
                        <div className="flex gap-2">
                          <button className="btn btn-outline btn-sm" onClick={() => editKey(item)}>编辑</button>
                          <button
                            className="btn btn-danger-ghost btn-icon btn-sm"
                            title="删除"
                            onClick={() => {
                              if (window.confirm(`确定删除 API Key「${item.name || item.id}」吗？`)) remove.mutate(item.id);
                            }}
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          <div className="rounded-md border border-border bg-surface-2 p-4">
            <div className="flex items-center justify-between gap-2">
              <h3 className="text-small font-semibold text-text-primary">{editing ? '编辑 Key' : '新增 Key'}</h3>
              {editing && <button className="btn btn-ghost btn-sm" onClick={resetCreate}>新增</button>}
            </div>
            <div className="mt-4 grid gap-3">
              <TextField label="名称" value={form.name} onChange={(v) => set('name', v)} placeholder="primary / backup" />
              <TextField
                label={editing ? 'API Key（留空则不修改）' : 'API Key'}
                value={form.api_key}
                onChange={(v) => set('api_key', v)}
                type="password"
              />
              <NumberField label="优先级" value={form.priority} min={0} max={10000} onChange={(v) => set('priority', v)} />
              <NumberField label="权重" value={form.weight} min={1} max={10000} onChange={(v) => set('weight', v)} />
              <NumberField label="RPM 限制" value={form.rpm_limit} min={0} onChange={(v) => set('rpm_limit', v)} />
              <NumberField label="TPM 限制" value={form.tpm_limit} min={0} onChange={(v) => set('tpm_limit', v)} />
              <Field label="状态">
                <select className="select" value={form.status} onChange={(e) => set('status', Number(e.target.value) === 1 ? 1 : 0)}>
                  <option value={1}>启用</option>
                  <option value={0}>停用</option>
                </select>
              </Field>
              <button className="btn btn-primary btn-md" onClick={() => save.mutate()} disabled={save.isPending || (creating && !form.api_key.trim())}>
                <Save size={16} /> {save.isPending ? '保存中...' : '保存 Key'}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function Field({ label, children, className = '' }: { label: string; children: ReactNode; className?: string }) {
  return (
    <label className={`field ${className}`}>
      <span className="field-label">{label}</span>
      {children}
    </label>
  );
}

function TextField({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
  className = '',
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  className?: string;
}) {
  return (
    <Field label={label} className={className}>
      <input className="input" type={type} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
    </Field>
  );
}

function NumberField({ label, value, min, max, onChange }: { label: string; value: number; min?: number; max?: number; onChange: (value: number) => void }) {
  return (
    <Field label={label}>
      <input className="input" type="number" min={min} max={max} value={value} onChange={(e) => onChange(Number(e.target.value) || 0)} />
    </Field>
  );
}

function TagList({ items, empty }: { items: string[]; empty: string }) {
  if (!items.length) return <span className="text-text-tertiary">{empty}</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {items.slice(0, 6).map((item) => (
        <span key={item} className="rounded-full border border-border bg-surface-2 px-2 py-0.5 text-tiny text-text-secondary">{item}</span>
      ))}
      {items.length > 6 && <span className="text-tiny text-text-tertiary">+{items.length - 6}</span>}
    </div>
  );
}

function ChannelHealth({ item }: { item: APIChannelItem }) {
  const status = Number(item.last_test_status || 0);
  const label = status === 1 ? 'OK' : status === 2 ? 'FAIL' : '未测';
  const badgeClass = status === 1
    ? 'border-green-200 bg-green-50 text-green-700'
    : status === 2
      ? 'border-red-200 bg-red-50 text-red-700'
      : 'border-border bg-surface-2 text-text-tertiary';
  return (
    <div className="max-w-[220px]">
      <span className={`inline-flex rounded-full border px-2 py-0.5 text-tiny font-semibold ${badgeClass}`}>{label}</span>
      {item.last_test_at ? <div className="mt-1 text-tiny text-text-tertiary">{formatTime(item.last_test_at)}</div> : null}
      {item.last_test_error ? <div className="mt-1 truncate text-tiny text-red-600" title={item.last_test_error}>{item.last_test_error}</div> : null}
    </div>
  );
}

function adapterLabel(adapter: string) {
  return ADAPTER_OPTIONS.find((item) => item.value === adapter)?.label ?? adapter;
}

function formatTime(value: number) {
  if (!value) return '';
  return new Date(value * 1000).toLocaleString('zh-CN', { hour12: false });
}

function credentialSourceSuffix(res: { credential_source?: string; key_id?: number; key_name?: string }) {
  if (res.credential_source === 'key_pool') {
    return ` · Key Pool${res.key_name ? ` ${res.key_name}` : res.key_id ? ` #${res.key_id}` : ''}`;
  }
  if (res.credential_source === 'channel_legacy') {
    return ' · 单 Key';
  }
  return '';
}

function channelFormWarnings(form: ChannelForm, editing: boolean) {
  const warnings: string[] = [];
  const capabilities = new Set(form.capabilities.map((item) => item.toLowerCase()));
  if (!editing && !form.api_key.trim()) {
    warnings.push('可先不填 Legacy API Key；保存后请在 Key 池新增密钥，否则健康检测和模型路由会判定无可用凭证。');
  }
  if (form.api_key.trim()) {
    warnings.push('Legacy API Key 仅用于兼容旧单 Key；正式接入建议保存渠道后改用 Key 池，并在 Key 池测试通过后清除 Legacy。');
  }
  if (editing && form.clear_api_key) {
    warnings.push('保存后会清除渠道级 Legacy API Key，并把最近健康检测重置为未测；请确认 Key 池有可用密钥后重新测试渠道。');
  }
  if (!form.provider_name.trim()) {
    warnings.push('建议填写 Provider 标识，例如 mimo 或 deepseek，后续排查和筛选会更清楚。');
  }
  if (form.adapter === 'openai_compatible_chat' && !capabilities.has('chat') && !capabilities.has('text')) {
    warnings.push('OpenAI 兼容 Chat 渠道应至少勾选“文字”能力，否则模型路由会判定能力不匹配。');
  }
  if (form.adapter === 'openai_compatible_video' && !capabilities.has('video')) {
    warnings.push('OpenAI 兼容 Video 渠道应至少勾选“视频”能力，否则模型路由会判定能力不匹配。');
  }
  if (!splitList(form.models_text).length) {
    warnings.push('可服务模型为空时表示不限制模型，建议为 MiMo/DeepSeek 这类官方 API 显式填写模型白名单。');
  }
  const baseURL = form.base_url.trim();
  if (baseURL && !/\/v1\/?$/.test(baseURL)) {
    warnings.push('OpenAI 兼容接口通常以 /v1 结尾；如果上游文档不是这样，请确认适配器是否正确。');
  }
  return warnings;
}

function formFromChannel(item: APIChannelItem): ChannelForm {
  return {
    code: item.code,
    name: item.name,
    provider_name: item.provider_name || '',
    adapter: item.adapter,
    base_url: item.base_url,
    api_key: '',
    clear_api_key: false,
    models_text: item.models.join(', '),
    capabilities: item.capabilities.length ? item.capabilities : ['chat'],
    proxy_id: item.proxy_id || 0,
    priority: item.priority,
    weight: item.weight,
    rpm_limit: item.rpm_limit,
    tpm_limit: item.tpm_limit,
    timeout_seconds: item.timeout_seconds,
    status: item.status === 1 ? 1 : 0,
    remark: item.remark || '',
  };
}

function bodyFromForm(form: ChannelForm, creating: boolean): Partial<APIChannelBody> {
  const body: Partial<APIChannelBody> = {
    code: form.code.trim(),
    name: form.name.trim(),
    provider_name: form.provider_name.trim(),
    adapter: form.adapter,
    base_url: form.base_url.trim(),
    models: splitList(form.models_text),
    capabilities: form.capabilities,
    priority: Number(form.priority) || 100,
    weight: Number(form.weight) || 100,
    rpm_limit: Number(form.rpm_limit) || 0,
    tpm_limit: Number(form.tpm_limit) || 0,
    timeout_seconds: Number(form.timeout_seconds) || 300,
    status: form.status,
    remark: form.remark.trim(),
  };
  if (form.proxy_id > 0) {
    body.proxy_id = form.proxy_id;
  } else if (!creating) {
    body.clear_proxy = true;
  }
  if (!creating && form.clear_api_key) {
    body.clear_api_key = true;
  } else if (form.api_key.trim()) {
    body.api_key = form.api_key.trim();
  }
  return body;
}

function keyBodyFromForm(form: ChannelKeyForm, creating: boolean): Partial<APIChannelKeyBody> {
  const body: Partial<APIChannelKeyBody> = {
    name: form.name.trim(),
    priority: Number(form.priority) || 100,
    weight: Number(form.weight) || 100,
    rpm_limit: Number(form.rpm_limit) || 0,
    tpm_limit: Number(form.tpm_limit) || 0,
    status: form.status,
  };
  if (creating || form.api_key.trim()) {
    body.api_key = form.api_key.trim();
  }
  return body;
}

function splitList(value: string) {
  const seen = new Set<string>();
  return value
    .split(/[\n,，]/)
    .map((item) => item.trim())
    .filter((item) => {
      if (!item || seen.has(item.toLowerCase())) return false;
      seen.add(item.toLowerCase());
      return true;
    });
}
