import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, Database, GitBranch, Info, Plus, RefreshCw, Save, Search, Trash2, X } from 'lucide-react';
import { useEffect, useMemo, useState, type ReactNode } from 'react';

import { ApiError } from '../../lib/api';
import { apiChannelsApi, modelGatewayApi } from '../../lib/services';
import type {
  APIChannelItem,
  ModelCatalogBody,
  ModelCatalogItem,
  ModelCatalogKind,
  ModelGatewayDryRunResp,
  ModelPricingMode,
  ModelSourceBody,
  ModelSourceConflictItem,
  ModelSourceItem,
  ModelSourceType,
} from '../../lib/types';
import { toast } from '../../stores/toast';

const KIND_OPTIONS: Array<{ value: ModelCatalogKind; label: string }> = [
  { value: 'text', label: '文字' },
  { value: 'image', label: '图片' },
  { value: 'video', label: '视频' },
  { value: 'chat', label: '对话' },
];

const PRICING_OPTIONS: Array<{ value: ModelPricingMode; label: string }> = [
  { value: 'fixed', label: '固定单价' },
  { value: 'token', label: 'Token 计价' },
  { value: 'char', label: '字符计价' },
  { value: 'matrix', label: '矩阵计价' },
  { value: 'manual', label: '手动/外部' },
];

function pricingOptionsForKind(kind: ModelCatalogKind) {
  if (kind === 'image' || kind === 'video') {
    return PRICING_OPTIONS.filter((item) => item.value === 'fixed' || item.value === 'matrix' || item.value === 'manual');
  }
  return PRICING_OPTIONS.filter((item) => item.value !== 'matrix');
}

function normalizePricingModeForKind(kind: ModelCatalogKind, mode: ModelPricingMode): ModelPricingMode {
  return pricingOptionsForKind(kind).some((item) => item.value === mode)
    ? mode
    : kind === 'image' || kind === 'video'
      ? 'fixed'
      : 'token';
}

function canUsePriceRules(kind: ModelCatalogKind, mode: ModelPricingMode) {
  return mode === 'matrix' && (kind === 'image' || kind === 'video');
}

const CAPABILITY_OPTIONS = [
  { value: 'chat', label: '文字' },
  { value: 'image', label: '图片' },
  { value: 'edit', label: '修图' },
  { value: 'video', label: '视频' },
  { value: 'vision', label: '视觉' },
  { value: 'embedding', label: 'Embedding' },
];

const ACCOUNT_POOL_OPTIONS = [
  { value: 'gpt', label: 'GPT 账号池' },
  { value: 'grok', label: 'Grok 账号池' },
];

const IMAGE_API_MODE_OPTIONS = [
  { value: '', label: '自动' },
  { value: 'openai_images', label: 'OpenAI / 银河 Images' },
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'pic2api', label: 'Pic2API 修图' },
  { value: 'nova_async', label: 'Nova 异步' },
];

type ImageResolution = '1K' | '2K' | '4K';

interface CatalogImagePriceRuleRow {
  model_code: string;
  mode: 't2i' | 'i2i';
  ratio_group: 'standard' | 'extended';
  ratios: string[];
  resolution: ImageResolution;
  quality: string;
  unit_points: number;
  enabled: boolean;
}

interface CatalogImageMatrixGroup {
  key: string;
  model_code: string;
  mode: CatalogImagePriceRuleRow['mode'];
  ratio_group: CatalogImagePriceRuleRow['ratio_group'];
  quality: string;
  ratios: string[];
  prices: Partial<Record<ImageResolution, number>>;
  enabled: boolean;
}

interface CatalogVideoPriceRuleRow {
  model_code: string;
  mode: 't2v' | 'i2v';
  duration_sec: number;
  quality: string;
  resolution: string;
  unit_points: number;
  enabled: boolean;
}

const IMAGE_RESOLUTIONS: ImageResolution[] = ['1K', '2K', '4K'];
const STANDARD_IMAGE_RATIOS = ['1:1', '16:9', '9:16', '4:3', '3:4', '5:4', '4:5'];
const EXTENDED_IMAGE_RATIOS = ['3:2', '2:3', '21:9'];
const DEFAULT_TEXT_PARAMETERS_SCHEMA = JSON.stringify({
  controls: [
    { key: 'temperature', label: '温度', type: 'number', min: 0, max: 2, step: 0.1, default: 0.7 },
    { key: 'max_tokens', label: '最大输出 Token', type: 'number', min: 1, max: 8192, step: 1, default: 1200 },
  ],
}, null, 2);

interface ModelForm {
  model_code: string;
  display_name: string;
  entry_kind: ModelCatalogKind;
  provider_hint: string;
  upstream_default_model: string;
  capabilities: string[];
  parameters_schema_text: string;
  pricing_mode: ModelPricingMode;
  unit_points: number;
  input_unit_points: number;
  output_unit_points: number;
  price_rules_text: string;
  min_plan: string;
  tags_text: string;
  description: string;
  sort_order: number;
  visible: 0 | 1;
  status: 0 | 1;
}

interface SourceForm {
  model_code: string;
  source_type: ModelSourceType;
  source_code: string;
  upstream_model: string;
  adapter: string;
  auth_type: string;
  image_api_mode: string;
  strategy: 'round_robin' | 'weighted_rr';
  priority: number;
  weight: number;
  status: 0 | 1;
  remark: string;
}

const DEFAULT_MODEL_FORM: ModelForm = {
  model_code: '',
  display_name: '',
  entry_kind: 'image',
  provider_hint: '',
  upstream_default_model: '',
  capabilities: ['image'],
  parameters_schema_text: '',
  pricing_mode: 'fixed',
  unit_points: 4,
  input_unit_points: 0,
  output_unit_points: 0,
  price_rules_text: '',
  min_plan: 'free',
  tags_text: '',
  description: '',
  sort_order: 100,
  visible: 1,
  status: 1,
};

const MODEL_PRESETS: Array<{ label: string; description: string; values: Partial<ModelForm> }> = [
  {
    label: 'MiMo 文字模型',
    description: '公开模型编码与上游模型保持一致，适合挂到 MiMo API 渠道。',
    values: {
      model_code: 'mimo-v2.5-pro',
      display_name: 'MiMo V2.5 Pro',
      entry_kind: 'text',
      provider_hint: 'mimo',
      upstream_default_model: 'mimo-v2.5-pro',
      capabilities: ['chat'],
      parameters_schema_text: DEFAULT_TEXT_PARAMETERS_SCHEMA,
      pricing_mode: 'token',
      unit_points: 0,
      input_unit_points: 1,
      output_unit_points: 3,
      tags_text: '文字, MiMo',
      description: 'MiMo 官方 API 文字模型，来源映射应选择 MiMo API 渠道。',
    },
  },
  {
    label: 'DeepSeek 文字模型',
    description: '默认 deepseek-chat，可按需改成 deepseek-reasoner。',
    values: {
      model_code: 'deepseek-chat',
      display_name: 'DeepSeek Chat',
      entry_kind: 'text',
      provider_hint: 'deepseek',
      upstream_default_model: 'deepseek-chat',
      capabilities: ['chat'],
      parameters_schema_text: DEFAULT_TEXT_PARAMETERS_SCHEMA,
      pricing_mode: 'token',
      unit_points: 0,
      input_unit_points: 1,
      output_unit_points: 3,
      tags_text: '文字, DeepSeek',
      description: 'DeepSeek 官方 API 文字模型，来源映射应选择 DeepSeek API 渠道。',
    },
  },
];

function defaultSourceForm(modelCode: string): SourceForm {
  return {
    model_code: modelCode,
    source_type: 'api_channel',
    source_code: '',
    upstream_model: '',
    adapter: '',
    auth_type: '',
    image_api_mode: '',
    strategy: 'round_robin',
    priority: 100,
    weight: 100,
    status: 1,
    remark: '',
  };
}

export default function ModelGatewayPage() {
  const qc = useQueryClient();
  const [keyword, setKeyword] = useState('');
  const [entryKind, setEntryKind] = useState('');
  const [status, setStatus] = useState<'' | 0 | 1>('');
  const [selected, setSelected] = useState<ModelCatalogItem | null>(null);
  const [modelEditing, setModelEditing] = useState<ModelCatalogItem | null>(null);
  const [modelCreating, setModelCreating] = useState(false);
  const [modelForm, setModelForm] = useState<ModelForm>(DEFAULT_MODEL_FORM);
  const [sourceEditing, setSourceEditing] = useState<ModelSourceItem | null>(null);
  const [sourceCreating, setSourceCreating] = useState(false);
  const [sourceForm, setSourceForm] = useState<SourceForm>(defaultSourceForm(''));
  const [dryRunResult, setDryRunResult] = useState<ModelGatewayDryRunResp | null>(null);

  const models = useQuery({
    queryKey: ['admin', 'model-gateway', 'models', keyword, entryKind, status],
    queryFn: () => modelGatewayApi.models({
      keyword: keyword.trim() || undefined,
      entry_kind: entryKind || undefined,
      status: status === '' ? undefined : status,
      page: 1,
      page_size: 100,
    }),
  });
  const apiChannels = useQuery({
    queryKey: ['admin', 'api-channels', 'model-gateway-options'],
    queryFn: () => apiChannelsApi.list({ page: 1, page_size: 200, status: 1 }),
  });
  const sources = useQuery({
    queryKey: ['admin', 'model-gateway', 'sources', selected?.model_code],
    queryFn: () => modelGatewayApi.sources({ model_code: selected?.model_code, page: 1, page_size: 200 }),
    enabled: Boolean(selected?.model_code),
  });
  const conflicts = useQuery({
    queryKey: ['admin', 'model-gateway', 'source-conflicts'],
    queryFn: () => modelGatewayApi.sourceConflicts(),
  });

  const modelItems = models.data?.list ?? [];

  useEffect(() => {
    const first = modelItems[0];
    if (!selected && first) {
      setSelected(first);
      return;
    }
    if (selected && first) {
      const latest = modelItems.find((item) => item.id === selected.id);
      if (latest && latest !== selected) setSelected(latest);
      if (!latest) setSelected(first);
    }
  }, [modelItems, selected]);

  useEffect(() => {
    if (modelEditing) {
      setModelForm(modelFormFromItem(modelEditing));
    } else if (modelCreating) {
      setModelForm(DEFAULT_MODEL_FORM);
    }
  }, [modelEditing, modelCreating]);

  useEffect(() => {
    if (sourceEditing) {
      setSourceForm(sourceFormFromItem(sourceEditing));
    } else if (sourceCreating) {
      setSourceForm(defaultSourceForm(selected?.model_code ?? ''));
    }
  }, [sourceEditing, sourceCreating, selected?.model_code]);

  const saveModel = useMutation({
    mutationFn: async () => {
      const body = bodyFromModelForm(modelForm, Boolean(modelEditing));
      if (modelEditing) {
        await modelGatewayApi.updateModel(modelEditing.id, body);
        return;
      }
      await modelGatewayApi.createModel(body as ModelCatalogBody);
    },
    onSuccess: () => {
      toast.success(modelEditing ? '模型已更新' : '模型已新增');
      setModelEditing(null);
      setModelCreating(false);
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'models'] });
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'source-conflicts'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const removeModel = useMutation({
    mutationFn: (id: number) => modelGatewayApi.removeModel(id),
    onSuccess: () => {
      toast.success('模型已删除');
      setSelected(null);
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'models'] });
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'source-conflicts'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const saveSource = useMutation({
    mutationFn: async () => {
      const body = bodyFromSourceForm(sourceForm);
      if (sourceEditing) {
        await modelGatewayApi.updateSource(sourceEditing.id, body);
        return;
      }
      await modelGatewayApi.createSource(body as ModelSourceBody);
    },
    onSuccess: () => {
      toast.success(sourceEditing ? '来源映射已更新' : '来源映射已新增');
      setSourceEditing(null);
      setSourceCreating(false);
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'sources'] });
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'source-conflicts'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const removeSource = useMutation({
    mutationFn: (id: number) => modelGatewayApi.removeSource(id),
    onSuccess: () => {
      toast.success('来源映射已删除');
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'sources'] });
      qc.invalidateQueries({ queryKey: ['admin', 'model-gateway', 'source-conflicts'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const dryRun = useMutation({
    mutationFn: () => modelGatewayApi.dryRun({
      model_code: selected?.model_code ?? '',
      entry_kind: selected?.entry_kind ?? '',
    }),
    onSuccess: (resp) => {
      setDryRunResult(resp);
      toast.success('路由测试完成');
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title">模型库</h1>
          <p className="page-subtitle">管理公开模型、计价基础和可承接来源映射；文字、图片和视频请求已优先按模型库候选路由，旧账号池路由仅作为未接管模型的兼容兜底。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="btn btn-outline btn-md" onClick={() => models.refetch()} disabled={models.isFetching}>
            <RefreshCw size={16} className={models.isFetching ? 'animate-spin' : ''} /> 刷新
          </button>
          <button className="btn btn-primary btn-md" onClick={() => setModelCreating(true)}>
            <Plus size={16} /> 新增模型
          </button>
        </div>
      </header>

      <div className="card flex flex-wrap items-center gap-3 p-3">
        <div className="relative min-w-[260px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-tertiary" size={16} />
          <input className="input pl-9" value={keyword} onChange={(e) => setKeyword(e.target.value)} placeholder="搜索模型编码 / 名称 / Provider" />
        </div>
        <select className="select w-[140px]" value={entryKind} onChange={(e) => setEntryKind(e.target.value)}>
          <option value="">全部入口</option>
          {KIND_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
        </select>
        <select className="select w-[140px]" value={status} onChange={(e) => setStatus(e.target.value === '' ? '' : Number(e.target.value) as 0 | 1)}>
          <option value="">全部状态</option>
          <option value={1}>启用</option>
          <option value={0}>停用</option>
        </select>
      </div>

      <SourceConflictPanel
        conflicts={conflicts.data ?? []}
        loading={conflicts.isLoading}
        refreshing={conflicts.isFetching}
        onRefresh={() => conflicts.refetch()}
      />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(420px,0.75fr)]">
        <div className="card table-wrap">
          <table className="data-table min-w-[980px]">
            <thead>
              <tr>
                <th>模型</th>
                <th>入口</th>
                <th>默认上游</th>
                <th>能力</th>
                <th>计价</th>
                <th>前台</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {models.isLoading ? (
                <tr><td colSpan={8} className="text-center text-text-tertiary">加载中...</td></tr>
              ) : modelItems.length === 0 ? (
                <tr><td colSpan={8} className="text-center text-text-tertiary">暂无模型</td></tr>
              ) : modelItems.map((item) => (
                <tr key={item.id} className={selected?.id === item.id ? 'bg-surface-2/70' : ''}>
                  <td>
                    <button className="text-left" onClick={() => setSelected(item)}>
                      <div className="font-semibold text-text-primary">{item.display_name}</div>
                      <div className="mt-1 font-mono text-tiny text-text-tertiary">{item.model_code}</div>
                    </button>
                  </td>
                  <td>{kindLabel(item.entry_kind)}</td>
                  <td>
                    <div className="font-mono text-small text-text-secondary">{item.upstream_default_model || '-'}</div>
                    <div className="text-tiny text-text-tertiary">{item.provider_hint || '未标注'}</div>
                  </td>
                  <td><TagList items={item.capabilities} empty="-" /></td>
                  <td>{pricingSummary(item)}</td>
                  <td>{item.visible === 1 ? '可见' : '隐藏'}</td>
                  <td>{item.status === 1 ? '启用' : '停用'}</td>
                  <td>
                    <div className="flex gap-2">
                      <button className="btn btn-outline btn-sm" onClick={() => setModelEditing(item)}>编辑</button>
                      <button
                        className="btn btn-danger-ghost btn-icon btn-sm"
                        onClick={() => {
                          if (window.confirm(`确定删除模型「${item.display_name}」吗？有来源映射时后端会拒绝删除。`)) removeModel.mutate(item.id);
                        }}
                        title="删除模型"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="card space-y-3 p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 className="text-h4 font-semibold text-text-primary">来源映射</h2>
              <div className="mt-1 font-mono text-tiny text-text-tertiary">{selected?.model_code || '未选择模型'}</div>
            </div>
            <div className="flex gap-2">
              <button className="btn btn-outline btn-sm" disabled={!selected || dryRun.isPending} onClick={() => dryRun.mutate()}>
                <GitBranch size={14} /> {dryRun.isPending ? '测试中' : '测试路由'}
              </button>
              <button className="btn btn-outline btn-sm" disabled={!selected} onClick={() => setSourceCreating(true)}>
                <Plus size={14} /> 新增来源
              </button>
            </div>
          </div>

          {!selected ? (
            <div className="rounded-md border border-dashed border-border p-6 text-center text-small text-text-tertiary">选择左侧模型后配置来源</div>
          ) : sources.isLoading ? (
            <div className="text-small text-text-tertiary">加载来源...</div>
          ) : (sources.data?.list.length ?? 0) === 0 ? (
            <div className="rounded-md border border-dashed border-border p-6 text-center text-small text-text-tertiary">暂无来源映射</div>
          ) : (
            <div className="space-y-2">
              {sources.data?.list.map((item) => (
                <div key={item.id} className="rounded-md border border-border bg-surface-1 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="font-semibold text-text-primary">{sourceTypeLabel(item.source_type)} · {item.source_code}</div>
                      <div className="mt-1 font-mono text-tiny text-text-tertiary">{item.upstream_model || selected.upstream_default_model || selected.model_code}</div>
                    </div>
                    <div className="flex gap-2">
                      <button className="btn btn-outline btn-sm" onClick={() => setSourceEditing(item)}>编辑</button>
                      <button
                        className="btn btn-danger-ghost btn-icon btn-sm"
                        onClick={() => {
                          if (window.confirm('确定删除这条来源映射吗？')) removeSource.mutate(item.id);
                        }}
                        title="删除来源"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </div>
                  <div className="mt-3 grid grid-cols-2 gap-2 text-tiny text-text-tertiary">
                    <div>策略：{item.strategy}</div>
                    <div>优先级 / 权重：{item.priority} / {item.weight}</div>
                    <div>认证：{item.auth_type || '-'}</div>
                    <div>图片调用：{item.image_api_mode || '-'}</div>
                    <div>协议：{item.adapter || '-'}</div>
                    <div>状态：{item.status === 1 ? '启用' : '停用'}</div>
                  </div>
                  {item.remark && <div className="mt-2 text-small text-text-secondary">{item.remark}</div>}
                </div>
              ))}
            </div>
          )}

          {dryRunResult && dryRunResult.model_code === selected?.model_code && (
            <DryRunPanel result={dryRunResult} />
          )}
        </div>
      </div>

      {(modelCreating || modelEditing) && (
        <ModelDialog
          form={modelForm}
          editing={Boolean(modelEditing)}
          saving={saveModel.isPending}
          onChange={setModelForm}
          onClose={() => { setModelCreating(false); setModelEditing(null); }}
          onSubmit={() => saveModel.mutate()}
        />
      )}

      {(sourceCreating || sourceEditing) && (
        <SourceDialog
          form={sourceForm}
          model={selected}
          apiChannels={apiChannels.data?.list ?? []}
          saving={saveSource.isPending}
          onChange={setSourceForm}
          onClose={() => { setSourceCreating(false); setSourceEditing(null); }}
          onSubmit={() => saveSource.mutate()}
        />
      )}
    </div>
  );
}

function SourceConflictPanel({
  conflicts,
  loading,
  refreshing,
  onRefresh,
}: {
  conflicts: ModelSourceConflictItem[];
  loading: boolean;
  refreshing: boolean;
  onRefresh: () => void;
}) {
  if (loading) {
    return (
      <div className="rounded-md border border-border bg-surface-1 p-3 text-small text-text-tertiary">
        正在检查账号池错配...
      </div>
    );
  }
  if (conflicts.length === 0) {
    return null;
  }
  return (
    <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-amber-900">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-start gap-2">
          <AlertTriangle size={17} className="mt-0.5 shrink-0" />
          <div>
            <div className="text-small font-semibold">账号池错配 {conflicts.length} 条</div>
            <div className="mt-1 text-tiny text-amber-800">这些来源会在 dry-run 和非流式文字运行时被跳过。</div>
          </div>
        </div>
        <button className="btn btn-outline btn-sm bg-white/70" onClick={onRefresh} disabled={refreshing}>
          <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} /> 复查
        </button>
      </div>
      <div className="mt-3 grid gap-2 md:grid-cols-2">
        {conflicts.slice(0, 6).map((item) => (
          <div key={item.id} className="rounded-md border border-amber-200 bg-white/70 p-2">
            <div className="flex items-start justify-between gap-2">
              <div>
                <div className="font-mono text-tiny text-amber-950">{item.model_code}</div>
                <div className="mt-1 text-tiny text-amber-800">
                  {sourceTypeLabel(item.source_type)} · {item.source_code} · {item.upstream_model || '-'}
                </div>
              </div>
              <span className="rounded-full bg-amber-100 px-2 py-1 text-tiny">{item.status === 1 ? '启用' : '停用'}</span>
            </div>
            <div className="mt-2 text-small">{item.reason}</div>
          </div>
        ))}
      </div>
      {conflicts.length > 6 && <div className="mt-2 text-tiny text-amber-800">还有 {conflicts.length - 6} 条，请用筛选或命令行审计继续处理。</div>}
    </div>
  );
}

function DryRunPanel({ result }: { result: ModelGatewayDryRunResp }) {
  return (
    <div className="rounded-md border border-border bg-surface-2 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <div className="text-small font-semibold text-text-primary">Dry-run v2</div>
          <div className="mt-1 text-tiny text-text-tertiary">
            可用 {result.available_count} / 候选 {result.candidate_count}
            {result.selected_index > 0 ? ` · 首选 #${result.selected_index}` : ''}
          </div>
        </div>
        {result.warning && <span className="rounded-full bg-amber-50 px-2 py-1 text-tiny text-amber-700">{result.warning}</span>}
      </div>
      <div className="mt-3 space-y-2">
        {result.candidates.length === 0 ? (
          <div className="text-small text-text-tertiary">没有候选来源</div>
        ) : result.candidates.map((item) => (
          <div key={item.index} className="rounded-md border border-border bg-surface-1 p-2">
            <div className="flex items-start justify-between gap-2">
              <div>
                <div className="font-semibold text-text-primary">
                  #{item.index} {sourceTypeLabel(item.source_type)} · {item.source_name || item.source_code}
                </div>
                <div className="mt-1 font-mono text-tiny text-text-tertiary">{item.upstream_model}</div>
              </div>
              <span className={item.available ? 'rounded-full bg-emerald-50 px-2 py-1 text-tiny text-emerald-700' : 'rounded-full bg-red-50 px-2 py-1 text-tiny text-red-700'}>
                {item.available ? '可用' : '跳过'}
              </span>
            </div>
            <div className="mt-2 grid grid-cols-2 gap-1 text-tiny text-text-tertiary">
              <div>协议：{item.adapter || '-'}</div>
              <div>认证：{item.auth_type || '-'}</div>
              <div>优先级/权重：{item.priority} / {item.weight}</div>
              <div>账号：{item.available_accounts ?? 0} / {item.candidate_accounts ?? 0}</div>
            </div>
            {item.skip_reason && <div className="mt-2 text-small text-red-600">{item.skip_reason}</div>}
          </div>
        ))}
      </div>
    </div>
  );
}

function PriceRulesEditor({
  form,
  onChange,
}: {
  form: ModelForm;
  onChange: (form: ModelForm) => void;
}) {
  const parsed = useMemo(() => parsePriceRuleArray(form.price_rules_text), [form.price_rules_text]);
  const modelCode = form.model_code.trim() || form.upstream_default_model.trim() || 'model';
  const imageRules = useMemo(
    () => catalogImageRulesFromArray(parsed.rules, modelCode, false),
    [form.entry_kind, form.pricing_mode, modelCode, parsed.rules],
  );
  const videoRules = useMemo(
    () => catalogVideoRulesFromArray(parsed.rules, modelCode, form.unit_points, false),
    [form.entry_kind, form.pricing_mode, modelCode, parsed.rules, form.unit_points],
  );
  const setImageRules = (rows: CatalogImagePriceRuleRow[]) => onChange({ ...form, price_rules_text: imageRulesToText(rows) });
  const setVideoRules = (rows: CatalogVideoPriceRuleRow[]) => onChange({ ...form, price_rules_text: videoRulesToText(rows) });

  if (!canUsePriceRules(form.entry_kind, form.pricing_mode)) {
    return null;
  }

  return (
    <Field className="md:col-span-2" label="价格矩阵">
      <div className="space-y-3 rounded-md border border-border bg-surface-2 p-3">
        {parsed.error ? (
          <div className="rounded-md border border-red-200 bg-red-50 p-3 text-small text-red-700">
            JSON 格式错误，矩阵暂不可编辑：{parsed.error}
          </div>
        ) : form.entry_kind === 'image' ? (
          <ImageRulesMatrix modelCode={modelCode} rows={imageRules} onChange={setImageRules} />
        ) : form.entry_kind === 'video' ? (
          <VideoRulesMatrix modelCode={modelCode} rows={videoRules} basePoints={form.unit_points} onChange={setVideoRules} />
        ) : (
          <div className="text-small text-text-tertiary">文字模型使用输入/输出单价字段计价。</div>
        )}
        <details>
          <summary className="cursor-pointer text-small font-semibold text-text-primary">JSON 兼容入口</summary>
          <textarea className="input mt-3 min-h-[110px] font-mono text-small" value={form.price_rules_text} onChange={(e) => onChange({ ...form, price_rules_text: e.target.value })} placeholder='[{"mode":"t2i","resolution":"1K","unit_points":400}]' />
        </details>
      </div>
    </Field>
  );
}

function ImageRulesMatrix({
  modelCode,
  rows,
  onChange,
}: {
  modelCode: string;
  rows: CatalogImagePriceRuleRow[];
  onChange: (rows: CatalogImagePriceRuleRow[]) => void;
}) {
  const groups = useMemo(() => buildCatalogImageMatrixGroups(rows), [rows]);
  const setPrice = (group: CatalogImageMatrixGroup, resolution: ImageResolution, unitPoints: number) => {
    const idx = rows.findIndex((row) => (
      row.model_code === group.model_code
      && row.mode === group.mode
      && row.ratio_group === group.ratio_group
      && row.quality === group.quality
      && row.resolution === resolution
    ));
    if (idx >= 0) {
      onChange(rows.map((row, i) => (i === idx ? { ...row, unit_points: unitPoints } : row)));
      return;
    }
    onChange([...rows, {
      model_code: group.model_code,
      mode: group.mode,
      ratio_group: group.ratio_group,
      quality: group.quality,
      ratios: group.ratios,
      resolution,
      unit_points: unitPoints,
      enabled: true,
    }]);
  };
  const setGroupEnabled = (group: CatalogImageMatrixGroup, enabled: boolean) => {
    onChange(rows.map((row) => (
      row.model_code === group.model_code
      && row.mode === group.mode
      && row.ratio_group === group.ratio_group
      && row.quality === group.quality
        ? { ...row, enabled }
        : row
    )));
  };
  const setGroupQuality = (group: CatalogImageMatrixGroup, quality: string) => {
    onChange(rows.map((row) => (
      row.model_code === group.model_code
      && row.mode === group.mode
      && row.ratio_group === group.ratio_group
      && row.quality === group.quality
        ? { ...row, quality }
        : row
    )));
  };
  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <button type="button" className="btn btn-outline btn-sm" onClick={() => onChange(defaultCatalogImageRules(modelCode))}>
          生成默认图片梯度
        </button>
      </div>
      <div className="overflow-x-auto rounded-md border border-border bg-surface-1">
        <table className="data-table min-w-[860px]">
          <thead>
            <tr>
              <th>模式</th>
              <th>清晰度</th>
              <th>比例组</th>
              <th>比例</th>
              {IMAGE_RESOLUTIONS.map((resolution) => <th key={resolution}>{resolution}</th>)}
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {groups.length === 0 ? (
              <tr>
                <td colSpan={7} className="text-center text-text-tertiary">暂无价格规则</td>
              </tr>
            ) : groups.map((group) => (
              <tr key={group.key}>
                <td>{imageModeLabel(group.mode)}</td>
                <td>
                  <input className="input min-w-[110px]" value={group.quality} onChange={(e) => setGroupQuality(group, e.target.value)} placeholder="high / 留空自动" />
                </td>
                <td>{ratioGroupLabel(group.ratio_group)}</td>
                <td className="font-mono text-tiny text-text-tertiary">{group.ratios.join(', ')}</td>
                {IMAGE_RESOLUTIONS.map((resolution) => (
                  <td key={resolution}>
                    <input
                      className="input w-[90px]"
                      type="number"
                      min={0}
                      step={0.01}
                      value={group.prices[resolution] ?? 0}
                      onChange={(e) => setPrice(group, resolution, Number(e.target.value) || 0)}
                    />
                  </td>
                ))}
                <td>
                  <button type="button" className={group.enabled ? 'btn btn-outline btn-sm' : 'btn btn-ghost btn-sm'} onClick={() => setGroupEnabled(group, !group.enabled)}>
                    {group.enabled ? '启用' : '停用'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function VideoRulesMatrix({
  modelCode,
  rows,
  basePoints,
  onChange,
}: {
  modelCode: string;
  rows: CatalogVideoPriceRuleRow[];
  basePoints: number;
  onChange: (rows: CatalogVideoPriceRuleRow[]) => void;
}) {
  const update = (idx: number, patch: Partial<CatalogVideoPriceRuleRow>) => {
    onChange(rows.map((row, i) => (i === idx ? { ...row, ...patch } : row)));
  };
  const addRow = () => {
    onChange([...rows, {
      model_code: modelCode,
      mode: 't2v',
      duration_sec: 6,
      quality: 'standard',
      resolution: '',
      unit_points: basePoints > 0 ? basePoints : 15,
      enabled: true,
    }]);
  };
  return (
    <div className="space-y-3">
      <div className="flex justify-end gap-2">
        <button type="button" className="btn btn-outline btn-sm" onClick={() => onChange(defaultCatalogVideoRules(modelCode, basePoints))}>
          生成默认视频梯度
        </button>
        <button type="button" className="btn btn-outline btn-sm" onClick={addRow}>
          <Plus size={14} /> 新增视频梯度
        </button>
      </div>
      <div className="overflow-x-auto rounded-md border border-border bg-surface-1">
        <table className="data-table min-w-[840px]">
          <thead>
            <tr>
              <th>模式</th>
              <th>时长（秒）</th>
              <th>清晰度</th>
              <th>分辨率</th>
              <th>单价（点）</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={7} className="text-center text-text-tertiary">暂无价格规则</td>
              </tr>
            ) : rows.map((row, idx) => (
              <tr key={`${row.mode}-${row.duration_sec}-${idx}`}>
                <td>
                  <select className="select min-w-[100px]" value={row.mode} onChange={(e) => update(idx, { mode: e.target.value === 'i2v' ? 'i2v' : 't2v' })}>
                    <option value="t2v">文生视频</option>
                    <option value="i2v">图生视频</option>
                  </select>
                </td>
                <td><input className="input w-[110px]" type="number" min={1} value={row.duration_sec} onChange={(e) => update(idx, { duration_sec: Number(e.target.value) || 1 })} /></td>
                <td><input className="input min-w-[120px]" value={row.quality} onChange={(e) => update(idx, { quality: e.target.value })} placeholder="standard" /></td>
                <td><input className="input min-w-[120px]" value={row.resolution} onChange={(e) => update(idx, { resolution: e.target.value })} placeholder="留空" /></td>
                <td><input className="input w-[110px]" type="number" min={0} step={0.01} value={row.unit_points} onChange={(e) => update(idx, { unit_points: Number(e.target.value) || 0 })} /></td>
                <td>
                  <button type="button" className={row.enabled ? 'btn btn-outline btn-sm' : 'btn btn-ghost btn-sm'} onClick={() => update(idx, { enabled: !row.enabled })}>
                    {row.enabled ? '启用' : '停用'}
                  </button>
                </td>
                <td>
                  <button type="button" className="btn btn-danger-ghost btn-icon btn-sm" onClick={() => onChange(rows.filter((_, i) => i !== idx))}>
                    <Trash2 size={14} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ModelDialog({
  form,
  editing,
  saving,
  onChange,
  onClose,
  onSubmit,
}: {
  form: ModelForm;
  editing: boolean;
  saving: boolean;
  onChange: (form: ModelForm) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const set = <K extends keyof ModelForm>(key: K, value: ModelForm[K]) => onChange({ ...form, [key]: value });
  const selectedCaps = useMemo(() => new Set(form.capabilities), [form.capabilities]);
  const pricingOptions = useMemo(() => pricingOptionsForKind(form.entry_kind), [form.entry_kind]);
  const applyPreset = (values: Partial<ModelForm>) => onChange({ ...form, ...values });
  const setEntryKind = (kind: ModelCatalogKind) => {
    const pricingMode = normalizePricingModeForKind(kind, form.pricing_mode);
    onChange({
      ...form,
      entry_kind: kind,
      pricing_mode: pricingMode,
      price_rules_text: canUsePriceRules(kind, pricingMode) ? form.price_rules_text : '',
    });
  };
  const setPricingMode = (pricingMode: ModelPricingMode) => {
    const next: ModelForm = { ...form, pricing_mode: pricingMode };
    if (!canUsePriceRules(form.entry_kind, pricingMode)) {
      next.price_rules_text = '';
    } else if (!form.price_rules_text.trim()) {
      next.price_rules_text = form.entry_kind === 'image'
        ? imageRulesToText(defaultCatalogImageRules(form.model_code.trim() || form.upstream_default_model.trim() || 'model'))
        : videoRulesToText(defaultCatalogVideoRules(form.model_code.trim() || form.upstream_default_model.trim() || 'model', form.unit_points));
    }
    onChange(next);
  };
  const toggleCap = (cap: string) => {
    const next = selectedCaps.has(cap) ? form.capabilities.filter((item) => item !== cap) : [...form.capabilities, cap];
    set('capabilities', next);
  };

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-surface-overlay p-4">
      <div className="w-full max-w-5xl rounded-lg border border-border bg-surface-1 shadow-4">
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div className="flex items-center gap-2">
            <Database size={18} className="text-klein-blue" />
            <h2 className="text-h4 text-text-primary">{editing ? '编辑模型' : '新增模型'}</h2>
          </div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}><X size={16} /></button>
        </div>
        <div className="grid max-h-[72vh] gap-4 overflow-y-auto p-5 md:grid-cols-2">
          {!editing && (
            <div className="md:col-span-2 rounded-md border border-border bg-surface-2 p-3">
              <div className="flex items-center gap-2 text-small font-semibold text-text-primary">
                <Info size={15} className="text-klein-blue" />
                常用文字模型模板
              </div>
              <div className="mt-2 grid gap-2 md:grid-cols-2">
                {MODEL_PRESETS.map((preset) => (
                  <button key={preset.label} type="button" className="btn btn-outline btn-sm justify-start" onClick={() => applyPreset(preset.values)}>
                    <Database size={14} />
                    <span className="text-left">
                      <span className="block">{preset.label}</span>
                      <span className="block text-tiny font-normal text-text-tertiary">{preset.description}</span>
                    </span>
                  </button>
                ))}
              </div>
            </div>
          )}
          <TextField label="模型编码" value={form.model_code} onChange={(v) => set('model_code', v)} placeholder="mimo-v2.5-pro" />
          <TextField label="显示名称" value={form.display_name} onChange={(v) => set('display_name', v)} placeholder="MiMo V2.5 Pro" />
          <Field label="入口类型">
            <select className="select" value={form.entry_kind} onChange={(e) => setEntryKind(e.target.value as ModelCatalogKind)}>
              {KIND_OPTIONS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </Field>
          <TextField label="Provider 提示" value={form.provider_hint} onChange={(v) => set('provider_hint', v)} placeholder="mimo / deepseek / gpt" />
          <TextField className="md:col-span-2" label="默认上游模型" value={form.upstream_default_model} onChange={(v) => set('upstream_default_model', v)} placeholder="留空时后续可默认同模型编码" />
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
          <Field className="md:col-span-2" label="参数 Schema">
            <textarea className="input min-h-[120px] font-mono text-small" value={form.parameters_schema_text} onChange={(e) => set('parameters_schema_text', e.target.value)} placeholder='{"controls":[{"key":"temperature","type":"number","min":0,"max":2}]}' />
          </Field>
          <Field label="计价方式">
            <select className="select" value={form.pricing_mode} onChange={(e) => setPricingMode(e.target.value as ModelPricingMode)}>
              {pricingOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </Field>
          <TextField label="套餐门槛" value={form.min_plan} onChange={(v) => set('min_plan', v)} />
          <NumberField label="固定单价（点）" value={form.unit_points} min={0} step={0.01} onChange={(v) => set('unit_points', v)} />
          <NumberField label={form.pricing_mode === 'char' ? '输入单价（点/千字符）' : '输入单价（点/千Token）'} value={form.input_unit_points} min={0} step={0.01} onChange={(v) => set('input_unit_points', v)} />
          <NumberField label={form.pricing_mode === 'char' ? '输出单价（点/千字符）' : '输出单价（点/千Token）'} value={form.output_unit_points} min={0} step={0.01} onChange={(v) => set('output_unit_points', v)} />
          <NumberField label="排序" value={form.sort_order} min={0} onChange={(v) => set('sort_order', v)} />
          <Field label="前台可见">
            <select className="select" value={form.visible} onChange={(e) => set('visible', Number(e.target.value) === 1 ? 1 : 0)}>
              <option value={1}>可见</option>
              <option value={0}>隐藏</option>
            </select>
          </Field>
          <Field label="状态">
            <select className="select" value={form.status} onChange={(e) => set('status', Number(e.target.value) === 1 ? 1 : 0)}>
              <option value={1}>启用</option>
              <option value={0}>停用</option>
            </select>
          </Field>
          <TextField className="md:col-span-2" label="标签" value={form.tags_text} onChange={(v) => set('tags_text', v)} placeholder="图片, 修图, 海报" />
          <PriceRulesEditor form={form} onChange={onChange} />
          <Field className="md:col-span-2" label="说明">
            <textarea className="input min-h-[88px]" value={form.description} onChange={(e) => set('description', e.target.value)} />
          </Field>
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

function SourceDialog({
  form,
  model,
  apiChannels,
  saving,
  onChange,
  onClose,
  onSubmit,
}: {
  form: SourceForm;
  model: ModelCatalogItem | null;
  apiChannels: APIChannelItem[];
  saving: boolean;
  onChange: (form: SourceForm) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const set = <K extends keyof SourceForm>(key: K, value: SourceForm[K]) => onChange({ ...form, [key]: value });
  const sourceOptions = useMemo(
    () => form.source_type === 'api_channel'
      ? apiChannels.map((item) => ({ value: item.code, label: `${item.name} (${item.code})` }))
      : ACCOUNT_POOL_OPTIONS,
    [apiChannels, form.source_type],
  );
  const selectedChannel = useMemo(
    () => apiChannels.find((item) => item.code === form.source_code) ?? null,
    [apiChannels, form.source_code],
  );
  const warnings = useMemo(
    () => sourceFormWarnings(form, model, selectedChannel),
    [form, model, selectedChannel],
  );

  useEffect(() => {
    const first = sourceOptions[0];
    if (!form.source_code && first) {
      onChange({ ...form, source_code: first.value });
    }
  }, [form, form.source_code, onChange, sourceOptions]);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-surface-overlay p-4">
      <div className="w-full max-w-4xl rounded-lg border border-border bg-surface-1 shadow-4">
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div className="flex items-center gap-2">
            <GitBranch size={18} className="text-klein-blue" />
            <h2 className="text-h4 text-text-primary">来源映射</h2>
          </div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}><X size={16} /></button>
        </div>
        <div className="grid max-h-[72vh] gap-4 overflow-y-auto p-5 md:grid-cols-2">
          {warnings.length > 0 && (
            <div className="md:col-span-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-small text-amber-800">
              {warnings.map((warning) => <div key={warning}>{warning}</div>)}
            </div>
          )}
          <TextField label="模型编码" value={form.model_code} onChange={(v) => set('model_code', v)} />
          <Field label="来源类型">
            <select
              className="select"
              value={form.source_type}
              onChange={(e) => onChange({ ...form, source_type: e.target.value as ModelSourceType, source_code: '' })}
            >
              <option value="api_channel">API 渠道</option>
              <option value="account_pool">账号池</option>
            </select>
          </Field>
          <Field label="来源">
            <select className="select" value={form.source_code} onChange={(e) => set('source_code', e.target.value)}>
              {sourceOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </Field>
          <TextField label="上游模型" value={form.upstream_model} onChange={(v) => set('upstream_model', v)} placeholder="留空后续可使用模型默认上游" />
          <TextField label="协议适配" value={form.adapter} onChange={(v) => set('adapter', v)} placeholder="API 渠道可留空自动继承" />
          <Field label="认证类型">
            <select className="select" value={form.auth_type} onChange={(e) => set('auth_type', e.target.value)}>
              <option value="">不限制</option>
              <option value="api_key">API Key</option>
              <option value="oauth">OAuth</option>
              <option value="cookie">Cookie</option>
            </select>
          </Field>
          <Field label="图片调用">
            <select className="select" value={form.image_api_mode} onChange={(e) => set('image_api_mode', e.target.value)}>
              {IMAGE_API_MODE_OPTIONS.map((item) => <option key={item.value || 'auto'} value={item.value}>{item.label}</option>)}
            </select>
          </Field>
          <Field label="策略">
            <select className="select" value={form.strategy} onChange={(e) => set('strategy', e.target.value as SourceForm['strategy'])}>
              <option value="round_robin">顺序轮询</option>
              <option value="weighted_rr">权重优先</option>
            </select>
          </Field>
          <NumberField label="优先级" value={form.priority} min={0} max={10000} onChange={(v) => set('priority', v)} />
          <NumberField label="权重" value={form.weight} min={1} max={10000} onChange={(v) => set('weight', v)} />
          <Field label="状态">
            <select className="select" value={form.status} onChange={(e) => set('status', Number(e.target.value) === 1 ? 1 : 0)}>
              <option value={1}>启用</option>
              <option value={0}>停用</option>
            </select>
          </Field>
          <TextField label="备注" value={form.remark} onChange={(v) => set('remark', v)} />
        </div>
        <div className="flex justify-end gap-2 border-t border-border px-5 py-4">
          <button className="btn btn-outline btn-md" onClick={onClose}>取消</button>
          <button className="btn btn-primary btn-md" onClick={onSubmit} disabled={saving || !form.source_code}>
            <Save size={16} /> {saving ? '保存中...' : '保存'}
          </button>
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
  className = '',
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <Field label={label} className={className}>
      <input className="input" value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
    </Field>
  );
}

function NumberField({
  label,
  value,
  min,
  max,
  step = 1,
  onChange,
}: {
  label: string;
  value: number;
  min?: number;
  max?: number;
  step?: number;
  onChange: (value: number) => void;
}) {
  return (
    <Field label={label}>
      <input className="input" type="number" min={min} max={max} step={step} value={value} onChange={(e) => onChange(Number(e.target.value) || 0)} />
    </Field>
  );
}

function TagList({ items, empty }: { items: string[]; empty: string }) {
  if (!items.length) return <span className="text-text-tertiary">{empty}</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {items.slice(0, 5).map((item) => (
        <span key={item} className="rounded-full border border-border bg-surface-2 px-2 py-0.5 text-tiny text-text-secondary">{item}</span>
      ))}
      {items.length > 5 && <span className="text-tiny text-text-tertiary">+{items.length - 5}</span>}
    </div>
  );
}

function kindLabel(kind: string) {
  return KIND_OPTIONS.find((item) => item.value === kind)?.label ?? kind;
}

function sourceTypeLabel(sourceType: string) {
  return sourceType === 'api_channel' ? 'API 渠道' : sourceType === 'account_pool' ? '账号池' : sourceType;
}

function sourceFormWarnings(form: SourceForm, model: ModelCatalogItem | null, channel: APIChannelItem | null) {
  const warnings: string[] = [];
  const entryKind = model?.entry_kind || '';
  const publicModel = form.model_code.trim() || model?.model_code || '';
  const upstreamModel = form.upstream_model.trim() || model?.upstream_default_model || publicModel;
  if (form.source_type === 'api_channel') {
    if (!channel) {
      warnings.push('请选择一个已启用的 API 渠道；API 渠道和账号池不要混放。');
      return warnings;
    }
    const adapter = form.adapter.trim() || channel.adapter;
    if (isTextLikeKind(entryKind) && adapter !== 'openai_compatible_chat') {
      warnings.push('文字/对话模型目前应使用 OpenAI 兼容 Chat 协议，否则运行时不会按文字链路调用。');
    }
    if (entryKind === 'video' && adapter !== 'openai_compatible_video') {
      warnings.push('视频模型目前应使用 OpenAI 兼容 Video 协议；上游接单后会锁定该渠道并进入轮询。');
    }
    if (!listAllows(channel.models, publicModel, upstreamModel)) {
      warnings.push('该 API 渠道的模型白名单不包含当前模型或上游模型，保存后后台会拒绝这条映射。');
    }
    if (!capabilityAllows(channel.capabilities, entryKind)) {
      warnings.push('该 API 渠道能力与当前模型入口不匹配，例如文字模型需要“文字/chat”能力。');
    }
    return warnings;
  }
  if (
    form.source_type === 'account_pool'
    && (
      !accountPoolMatchesProviderHint(model?.provider_hint || '', form.source_code)
      || modelLooksLikeStandaloneAPIModel(model, upstreamModel)
    )
  ) {
    warnings.push('当前模型 Provider 与账号池不匹配；MiMo/DeepSeek 这类官方 API 应选择 API 渠道。');
  }
  return warnings;
}

function listAllows(list: string[], publicModel: string, upstreamModel: string) {
  if (!list.length) return true;
  const allowed = new Set(list.map((item) => item.trim().toLowerCase()).filter(Boolean));
  if (!allowed.size || allowed.has('*')) return true;
  return Boolean(
    (publicModel && allowed.has(publicModel.trim().toLowerCase()))
    || (upstreamModel && allowed.has(upstreamModel.trim().toLowerCase())),
  );
}

function capabilityAllows(capabilities: string[], entryKind: string) {
  if (!capabilities.length || !entryKind) return true;
  const allowed = new Set(capabilities.map((item) => item.trim().toLowerCase()).filter(Boolean));
  if (entryKind === 'image') return allowed.has('image') || allowed.has('edit');
  if (entryKind === 'video') return allowed.has('video');
  if (entryKind === 'text' || entryKind === 'chat') return allowed.has('chat') || allowed.has('text');
  return true;
}

function isTextLikeKind(kind: string) {
  return kind === 'text' || kind === 'chat';
}

function accountPoolMatchesProviderHint(providerHint: string, sourceCode: string) {
  const hint = providerHint.trim().toLowerCase();
  if (!hint) return true;
  const source = sourceCode.trim().toLowerCase();
  if (source === 'gpt') return ['gpt', 'openai', 'chatgpt'].includes(hint);
  if (source === 'grok') return ['grok', 'xai'].includes(hint);
  return true;
}

function modelLooksLikeStandaloneAPIModel(model: ModelCatalogItem | null, upstreamModel: string) {
  const candidates = [
    model?.provider_hint,
    model?.model_code,
    model?.upstream_default_model,
    upstreamModel,
  ];
  return candidates.some((candidate) => {
    const value = (candidate || '').trim().toLowerCase();
    return value.startsWith('mimo') || value.startsWith('deepseek');
  });
}

function pricingSummary(item: ModelCatalogItem) {
  if (item.pricing_mode === 'token') {
    return `Token · ${points(item.input_unit_points)} / ${points(item.output_unit_points)}`;
  }
  if (item.pricing_mode === 'char') {
    return `字符 · ${points(item.input_unit_points)} / ${points(item.output_unit_points)}`;
  }
  if (item.pricing_mode === 'matrix') {
    return `矩阵 · 基础 ${points(item.unit_points)}`;
  }
  return `${item.pricing_mode} · ${points(item.unit_points)}`;
}

function points(raw: number) {
  return `${((Number(raw) || 0) / 100).toFixed(2).replace(/\.00$/, '')} 点`;
}

function modelFormFromItem(item: ModelCatalogItem): ModelForm {
  const pricingMode = normalizePricingModeForKind(item.entry_kind, item.pricing_mode);
  return {
    model_code: item.model_code,
    display_name: item.display_name,
    entry_kind: item.entry_kind,
    provider_hint: item.provider_hint || '',
    upstream_default_model: item.upstream_default_model || '',
    capabilities: item.capabilities.length ? item.capabilities : [item.entry_kind === 'image' ? 'image' : item.entry_kind],
    parameters_schema_text: item.parameters_schema ? JSON.stringify(item.parameters_schema, null, 2) : '',
    pricing_mode: pricingMode,
    unit_points: (item.unit_points || 0) / 100,
    input_unit_points: (item.input_unit_points || 0) / 100,
    output_unit_points: (item.output_unit_points || 0) / 100,
    price_rules_text: canUsePriceRules(item.entry_kind, pricingMode) && item.price_rules ? JSON.stringify(item.price_rules, null, 2) : '',
    min_plan: item.min_plan || 'free',
    tags_text: item.tags.join(', '),
    description: item.description || '',
    sort_order: item.sort_order || 0,
    visible: item.visible === 1 ? 1 : 0,
    status: item.status === 1 ? 1 : 0,
  };
}

function sourceFormFromItem(item: ModelSourceItem): SourceForm {
  return {
    model_code: item.model_code,
    source_type: item.source_type,
    source_code: item.source_code,
    upstream_model: item.upstream_model || '',
    adapter: item.adapter || '',
    auth_type: item.auth_type || '',
    image_api_mode: item.image_api_mode || '',
    strategy: item.strategy === 'weighted_rr' ? 'weighted_rr' : 'round_robin',
    priority: item.priority || 100,
    weight: item.weight || 100,
    status: item.status === 1 ? 1 : 0,
    remark: item.remark || '',
  };
}

function bodyFromModelForm(form: ModelForm, editing: boolean): Partial<ModelCatalogBody> {
  const body: Partial<ModelCatalogBody> = {
    model_code: form.model_code.trim(),
    display_name: form.display_name.trim(),
    entry_kind: form.entry_kind,
    provider_hint: form.provider_hint.trim(),
    upstream_default_model: form.upstream_default_model.trim(),
    capabilities: form.capabilities,
    pricing_mode: form.pricing_mode,
    unit_points: toStoredPoints(form.unit_points),
    input_unit_points: toStoredPoints(form.input_unit_points),
    output_unit_points: toStoredPoints(form.output_unit_points),
    min_plan: form.min_plan.trim() || 'free',
    tags: splitList(form.tags_text),
    description: form.description.trim(),
    sort_order: Number(form.sort_order) || 0,
    visible: form.visible,
    status: form.status,
  };
  const priceRules = canUsePriceRules(form.entry_kind, form.pricing_mode) ? form.price_rules_text.trim() : '';
  if (priceRules) {
    body.price_rules = JSON.parse(priceRules);
  } else if (editing) {
    body.clear_price_rules = true;
  }
  const parametersSchema = form.parameters_schema_text.trim();
  if (parametersSchema) {
    body.parameters_schema = JSON.parse(parametersSchema);
  } else if (editing) {
    body.clear_parameters_schema = true;
  }
  return body;
}

function parsePriceRuleArray(value: string): { rules: unknown[]; error?: string } {
  const raw = value.trim();
  if (!raw) return { rules: [] };
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return { rules: [], error: '必须是数组' };
    }
    return { rules: parsed };
  } catch (err) {
    return { rules: [], error: err instanceof Error ? err.message : '无法解析' };
  }
}

function catalogImageRulesFromArray(rows: unknown[], modelCode: string, withDefault: boolean): CatalogImagePriceRuleRow[] {
  if (!rows.length) return withDefault ? defaultCatalogImageRules(modelCode) : [];
  const out = rows.map((item): CatalogImagePriceRuleRow => {
    const row = item as Partial<CatalogImagePriceRuleRow>;
    const ratioGroup: CatalogImagePriceRuleRow['ratio_group'] = row.ratio_group === 'extended' ? 'extended' : 'standard';
    const resolution: ImageResolution = row.resolution === '2K' || row.resolution === '4K' ? row.resolution : '1K';
    const mode: CatalogImagePriceRuleRow['mode'] = row.mode === 'i2i' ? 'i2i' : 't2i';
    return {
      model_code: String(row.model_code || modelCode),
      mode,
      ratio_group: ratioGroup,
      quality: String(row.quality || ''),
      ratios: Array.isArray(row.ratios) && row.ratios.length ? row.ratios.map(String) : defaultRatiosForGroup(ratioGroup),
      resolution,
      unit_points: Number(row.unit_points || 0) / 100,
      enabled: row.enabled !== false,
    };
  });
  return out.length ? out : withDefault ? defaultCatalogImageRules(modelCode) : [];
}

function catalogVideoRulesFromArray(rows: unknown[], modelCode: string, basePoints: number, withDefault: boolean): CatalogVideoPriceRuleRow[] {
  if (!rows.length) return withDefault ? defaultCatalogVideoRules(modelCode, basePoints) : [];
  const out = rows.map((item): CatalogVideoPriceRuleRow => {
    const row = item as Partial<CatalogVideoPriceRuleRow>;
    return {
      model_code: String(row.model_code || modelCode),
      mode: row.mode === 'i2v' ? 'i2v' : 't2v',
      duration_sec: Math.max(1, Number(row.duration_sec || 6) || 6),
      quality: String(row.quality || ''),
      resolution: String(row.resolution || ''),
      unit_points: Number(row.unit_points || 0) / 100,
      enabled: row.enabled !== false,
    };
  });
  return out.length ? out : withDefault ? defaultCatalogVideoRules(modelCode, basePoints) : [];
}

function imageRulesToText(rows: CatalogImagePriceRuleRow[]) {
  return JSON.stringify(rows.map((row) => ({
    model_code: row.model_code.trim(),
    mode: row.mode,
    ratio_group: row.ratio_group,
    quality: row.quality.trim(),
    ratios: row.ratios.map((ratio) => ratio.trim()).filter(Boolean),
    resolution: row.resolution,
    unit_points: toStoredPoints(row.unit_points),
    enabled: row.enabled,
  })), null, 2);
}

function videoRulesToText(rows: CatalogVideoPriceRuleRow[]) {
  return JSON.stringify(rows.map((row) => ({
    model_code: row.model_code.trim(),
    mode: row.mode,
    duration_sec: Math.max(1, Number(row.duration_sec) || 1),
    quality: row.quality.trim(),
    resolution: row.resolution.trim(),
    unit_points: toStoredPoints(row.unit_points),
    enabled: row.enabled,
  })), null, 2);
}

function defaultCatalogImageRules(modelCode: string): CatalogImagePriceRuleRow[] {
  const code = modelCode.trim() || 'model';
  const specs: Array<Pick<CatalogImagePriceRuleRow, 'mode' | 'ratio_group' | 'quality' | 'resolution' | 'unit_points'>> = [
    { mode: 't2i', ratio_group: 'standard', quality: 'high', resolution: '1K', unit_points: 4 },
    { mode: 't2i', ratio_group: 'standard', quality: 'high', resolution: '2K', unit_points: 6 },
    { mode: 't2i', ratio_group: 'standard', quality: 'high', resolution: '4K', unit_points: 8 },
    { mode: 't2i', ratio_group: 'extended', quality: 'high', resolution: '1K', unit_points: 5 },
    { mode: 't2i', ratio_group: 'extended', quality: 'high', resolution: '2K', unit_points: 7 },
    { mode: 't2i', ratio_group: 'extended', quality: 'high', resolution: '4K', unit_points: 9 },
    { mode: 'i2i', ratio_group: 'standard', quality: 'high', resolution: '1K', unit_points: 6 },
    { mode: 'i2i', ratio_group: 'standard', quality: 'high', resolution: '2K', unit_points: 8 },
    { mode: 'i2i', ratio_group: 'standard', quality: 'high', resolution: '4K', unit_points: 10 },
    { mode: 'i2i', ratio_group: 'extended', quality: 'high', resolution: '1K', unit_points: 7 },
    { mode: 'i2i', ratio_group: 'extended', quality: 'high', resolution: '2K', unit_points: 9 },
    { mode: 'i2i', ratio_group: 'extended', quality: 'high', resolution: '4K', unit_points: 11 },
  ];
  return specs.map((row) => ({
    ...row,
    model_code: code,
    ratios: defaultRatiosForGroup(row.ratio_group),
    enabled: true,
  }));
}

function defaultCatalogVideoRules(modelCode: string, basePoints: number): CatalogVideoPriceRuleRow[] {
  const code = modelCode.trim() || 'model';
  const base = basePoints > 0 ? basePoints : 15;
  const durations = [
    { duration_sec: 6, unit_points: base },
    { duration_sec: 10, unit_points: Math.round(base * 10 / 6 * 100) / 100 },
  ];
  return (['t2v', 'i2v'] as const).flatMap((mode) => durations.map((item) => ({
    model_code: code,
    mode,
    duration_sec: item.duration_sec,
    quality: 'standard',
    resolution: '',
    unit_points: item.unit_points,
    enabled: true,
  })));
}

function buildCatalogImageMatrixGroups(rows: CatalogImagePriceRuleRow[]): CatalogImageMatrixGroup[] {
  const groups = new Map<string, CatalogImageMatrixGroup>();
  rows.forEach((row) => {
    const key = [row.model_code.trim(), row.mode, row.quality.trim(), row.ratio_group].join('|');
    const group = groups.get(key) ?? {
      key,
      model_code: row.model_code,
      mode: row.mode,
      ratio_group: row.ratio_group,
      quality: row.quality.trim(),
      ratios: row.ratios.length ? row.ratios : defaultRatiosForGroup(row.ratio_group),
      prices: {},
      enabled: false,
    };
    group.prices[row.resolution] = row.unit_points;
    group.enabled = group.enabled || row.enabled;
    groups.set(key, group);
  });
  return Array.from(groups.values()).sort((a, b) => {
    const modeCompare = a.mode.localeCompare(b.mode);
    if (modeCompare !== 0) return modeCompare;
    const qualityCompare = a.quality.localeCompare(b.quality);
    if (qualityCompare !== 0) return qualityCompare;
    return a.ratio_group.localeCompare(b.ratio_group);
  });
}

function defaultRatiosForGroup(group: CatalogImagePriceRuleRow['ratio_group']) {
  return group === 'extended' ? EXTENDED_IMAGE_RATIOS : STANDARD_IMAGE_RATIOS;
}

function imageModeLabel(mode: CatalogImagePriceRuleRow['mode']) {
  return mode === 'i2i' ? '图生图' : '文生图';
}

function ratioGroupLabel(group: CatalogImagePriceRuleRow['ratio_group']) {
  return group === 'extended' ? '扩展比例' : '常规比例';
}

function bodyFromSourceForm(form: SourceForm): Partial<ModelSourceBody> {
  return {
    model_code: form.model_code.trim(),
    source_type: form.source_type,
    source_code: form.source_code.trim(),
    upstream_model: form.upstream_model.trim(),
    adapter: form.adapter.trim(),
    auth_type: form.auth_type.trim(),
    image_api_mode: form.image_api_mode.trim(),
    strategy: form.strategy,
    priority: Number(form.priority) || 100,
    weight: Number(form.weight) || 100,
    status: form.status,
    remark: form.remark.trim(),
  };
}

function toStoredPoints(value: number) {
  return Math.round((Number(value) || 0) * 100);
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
