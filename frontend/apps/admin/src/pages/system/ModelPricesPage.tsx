import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, ClipboardList, GitBranch, Globe2, Plus, RefreshCw, Save, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import { ApiError } from '../../lib/api';
import { systemApi } from '../../lib/services';
import { toast } from '../../stores/toast';

interface PriceRow {
  model_code: string;
  name: string;
  kind: 'text' | 'image' | 'video';
  provider: 'gpt' | 'grok' | string;
  upstream_model: string;
  unit_points: number;
  input_unit_points?: number;
  output_unit_points?: number;
  enabled: boolean;
}

type ImageResolution = '1K' | '2K' | '4K';

interface ImagePriceRuleRow {
  model_code: string;
  mode: 't2i' | 'i2i';
  ratio_group: 'standard' | 'extended';
  ratios: string[];
  resolution: ImageResolution;
  unit_points: number;
  enabled: boolean;
}

const DEFAULT_ROWS: PriceRow[] = [
  { model_code: 'gpt-4o-mini', name: '文字对话', kind: 'text', provider: 'gpt', upstream_model: 'gpt-4o-mini', unit_points: 0, input_unit_points: 1, output_unit_points: 3, enabled: true },
  { model_code: 'gpt-image-2', name: 'GPT Image 2', kind: 'image', provider: 'gpt', upstream_model: 'gpt-image-2', unit_points: 4, enabled: true },
  { model_code: 'img-v3', name: '通用图片', kind: 'image', provider: 'gpt', upstream_model: 'gpt-image', unit_points: 4, enabled: true },
  { model_code: 'img-real', name: '真实图片', kind: 'image', provider: 'gpt', upstream_model: 'gpt-image-real', unit_points: 4, enabled: true },
  { model_code: 'img-anime', name: '动漫图片', kind: 'image', provider: 'gpt', upstream_model: 'gpt-image-anime', unit_points: 3, enabled: true },
  { model_code: 'img-3d', name: '3D 图片', kind: 'image', provider: 'gpt', upstream_model: 'gpt-image-3d', unit_points: 5, enabled: true },
  { model_code: 'vid-v1', name: '视频生成', kind: 'video', provider: 'grok', upstream_model: 'grok-video', unit_points: 15, enabled: true },
  { model_code: 'vid-i2v', name: '图生视频', kind: 'video', provider: 'grok', upstream_model: 'grok-i2v', unit_points: 20, enabled: true },
];

const STANDARD_IMAGE_RATIOS = ['1:1', '16:9', '9:16', '4:3', '3:4', '5:4', '4:5'];
const EXTENDED_IMAGE_RATIOS = ['3:2', '2:3', '21:9'];
const IMAGE_RESOLUTIONS: ImageResolution[] = ['1K', '2K', '4K'];

const DEFAULT_IMAGE_PRICE_RULES: ImagePriceRuleRow[] = [
  { model_code: 'gpt-image-2', mode: 't2i', ratio_group: 'standard', ratios: STANDARD_IMAGE_RATIOS, resolution: '1K', unit_points: 4, enabled: true },
  { model_code: 'gpt-image-2', mode: 't2i', ratio_group: 'standard', ratios: STANDARD_IMAGE_RATIOS, resolution: '2K', unit_points: 6, enabled: true },
  { model_code: 'gpt-image-2', mode: 't2i', ratio_group: 'standard', ratios: STANDARD_IMAGE_RATIOS, resolution: '4K', unit_points: 8, enabled: true },
  { model_code: 'gpt-image-2', mode: 't2i', ratio_group: 'extended', ratios: EXTENDED_IMAGE_RATIOS, resolution: '1K', unit_points: 5, enabled: true },
  { model_code: 'gpt-image-2', mode: 't2i', ratio_group: 'extended', ratios: EXTENDED_IMAGE_RATIOS, resolution: '2K', unit_points: 7, enabled: true },
  { model_code: 'gpt-image-2', mode: 't2i', ratio_group: 'extended', ratios: EXTENDED_IMAGE_RATIOS, resolution: '4K', unit_points: 9, enabled: true },
  { model_code: 'gpt-image-2', mode: 'i2i', ratio_group: 'standard', ratios: STANDARD_IMAGE_RATIOS, resolution: '1K', unit_points: 6, enabled: true },
  { model_code: 'gpt-image-2', mode: 'i2i', ratio_group: 'standard', ratios: STANDARD_IMAGE_RATIOS, resolution: '2K', unit_points: 8, enabled: true },
  { model_code: 'gpt-image-2', mode: 'i2i', ratio_group: 'standard', ratios: STANDARD_IMAGE_RATIOS, resolution: '4K', unit_points: 10, enabled: true },
  { model_code: 'gpt-image-2', mode: 'i2i', ratio_group: 'extended', ratios: EXTENDED_IMAGE_RATIOS, resolution: '1K', unit_points: 7, enabled: true },
  { model_code: 'gpt-image-2', mode: 'i2i', ratio_group: 'extended', ratios: EXTENDED_IMAGE_RATIOS, resolution: '2K', unit_points: 9, enabled: true },
  { model_code: 'gpt-image-2', mode: 'i2i', ratio_group: 'extended', ratios: EXTENDED_IMAGE_RATIOS, resolution: '4K', unit_points: 11, enabled: true },
];

function fromValue(v: unknown): PriceRow[] {
  const withRequiredRows = (rows: PriceRow[]) => {
    const seen = new Set(rows.map((row) => row.model_code));
    const missing = DEFAULT_ROWS.filter((row) => row.model_code === 'gpt-image-2' && !seen.has(row.model_code));
    return missing.length > 0 ? [...rows, ...missing] : rows;
  };
  if (Array.isArray(v)) {
    return withRequiredRows(v.map((r) => {
      const row = r as Partial<PriceRow>;
      return {
        model_code: String(row.model_code || ''),
        name: String(row.name || ''),
        kind: row.kind === 'text' ? 'text' : row.kind === 'video' ? 'video' : 'image',
        provider: String(row.provider || 'gpt'),
        upstream_model: String(row.upstream_model || ''),
        unit_points: Number(row.unit_points || 0) / 100,
        input_unit_points: Number(row.input_unit_points || 0) / 100,
        output_unit_points: Number(row.output_unit_points || 0) / 100,
        enabled: row.enabled !== false,
      };
    }));
  }
  if (v && typeof v === 'object') {
    return withRequiredRows(Object.entries(v as Record<string, number>).map(([model_code, price]) => ({
      model_code,
      name: model_code,
      kind: model_code.startsWith('vid') ? 'video' : model_code.startsWith('gpt') ? 'text' : 'image',
      provider: model_code.startsWith('vid') ? 'grok' : 'gpt',
      upstream_model: model_code,
      unit_points: Number(price || 0) / 100,
      input_unit_points: model_code.startsWith('gpt') ? Number(price || 0) / 100 : 0,
      output_unit_points: model_code.startsWith('gpt') ? Number(price || 0) / 100 : 0,
      enabled: true,
    })));
  }
  return DEFAULT_ROWS;
}

function imageRulesFromValue(v: unknown): ImagePriceRuleRow[] {
  if (!Array.isArray(v)) return DEFAULT_IMAGE_PRICE_RULES;
  const rows: ImagePriceRuleRow[] = v.map((r): ImagePriceRuleRow => {
    const row = r as Partial<ImagePriceRuleRow> & { unit_points?: number };
    const ratioGroup: ImagePriceRuleRow['ratio_group'] = row.ratio_group === 'extended' ? 'extended' : 'standard';
    const fallbackRatios = ratioGroup === 'extended' ? EXTENDED_IMAGE_RATIOS : STANDARD_IMAGE_RATIOS;
    const resolution: ImagePriceRuleRow['resolution'] = row.resolution === '2K' || row.resolution === '4K' ? row.resolution : '1K';
    const mode: ImagePriceRuleRow['mode'] = row.mode === 'i2i' ? 'i2i' : 't2i';
    return {
      model_code: String(row.model_code || 'gpt-image-2'),
      mode,
      ratio_group: ratioGroup,
      ratios: Array.isArray(row.ratios) && row.ratios.length ? row.ratios.map(String) : fallbackRatios,
      resolution,
      unit_points: Number(row.unit_points || 0) / 100,
      enabled: row.enabled !== false,
    };
  });
  return rows.length ? rows : DEFAULT_IMAGE_PRICE_RULES;
}

interface ImageMatrixGroup {
  key: string;
  model_code: string;
  mode: ImagePriceRuleRow['mode'];
  ratio_group: ImagePriceRuleRow['ratio_group'];
  ratios: string[];
  prices: Partial<Record<ImageResolution, number>>;
  enabled: boolean;
}

function buildDefaultImageMatrix(modelCode: string) {
  return DEFAULT_IMAGE_PRICE_RULES.map((row) => ({ ...row, model_code: modelCode }));
}

function buildImageMatrixGroups(rows: ImagePriceRuleRow[]): ImageMatrixGroup[] {
  const groups = new Map<string, ImageMatrixGroup>();
  rows.forEach((row) => {
    const key = [
      row.model_code.trim(),
      row.mode,
      row.ratio_group,
    ].join('|');
    const fallbackRatios = row.ratio_group === 'extended' ? EXTENDED_IMAGE_RATIOS : STANDARD_IMAGE_RATIOS;
    const group = groups.get(key) ?? {
      key,
      model_code: row.model_code,
      mode: row.mode,
      ratio_group: row.ratio_group,
      ratios: row.ratios.length ? row.ratios : fallbackRatios,
      prices: {},
      enabled: false,
    };
    group.prices[row.resolution] = row.unit_points;
    group.enabled = group.enabled || row.enabled;
    groups.set(key, group);
  });
  return Array.from(groups.values()).sort((a, b) => {
    const modelCompare = a.model_code.localeCompare(b.model_code);
    if (modelCompare !== 0) return modelCompare;
    const modeCompare = a.mode.localeCompare(b.mode);
    if (modeCompare !== 0) return modeCompare;
    return a.ratio_group.localeCompare(b.ratio_group);
  });
}

function imageModeLabel(mode: ImagePriceRuleRow['mode']) {
  return mode === 'i2i' ? '图生图' : '文生图';
}

function ratioGroupLabel(group: ImagePriceRuleRow['ratio_group']) {
  return group === 'extended' ? '扩展比例' : '常规比例';
}

export default function ModelPricesPage() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ['admin', 'system', 'settings'], queryFn: () => systemApi.get() });
  const [rows, setRows] = useState<PriceRow[]>(DEFAULT_ROWS);
  const [imageRules, setImageRules] = useState<ImagePriceRuleRow[]>(DEFAULT_IMAGE_PRICE_RULES);
  const [imageMatrixModelCode, setImageMatrixModelCode] = useState('gpt-image-2');
  const [dirty, setDirty] = useState(false);
  const imageMatrixGroups = useMemo(() => buildImageMatrixGroups(imageRules), [imageRules]);
  const gatewayLikeRows = useMemo(() => rows.filter(isGatewayLikeLegacyPriceRow), [rows]);

  useEffect(() => {
    if (settings.data) {
      setRows(fromValue(settings.data['billing.model_prices']));
      setImageRules(imageRulesFromValue(settings.data['billing.image_price_rules']));
      setDirty(false);
    }
  }, [settings.data]);

  const update = (idx: number, patch: Partial<PriceRow>) => {
    setRows((old) => old.map((row, i) => (i === idx ? { ...row, ...patch } : row)));
    setDirty(true);
  };

  const updateImageRule = (idx: number, patch: Partial<ImagePriceRuleRow>) => {
    setImageRules((old) => old.map((row, i) => {
      if (i !== idx) return row;
      const next = { ...row, ...patch };
      if (patch.ratio_group && !patch.ratios) {
        next.ratios = patch.ratio_group === 'extended' ? EXTENDED_IMAGE_RATIOS : STANDARD_IMAGE_RATIOS;
      }
      return next;
    }));
    setDirty(true);
  };

  const setImageMatrixPrice = (group: ImageMatrixGroup, resolution: ImageResolution, unitPoints: number) => {
    setImageRules((old) => {
      const idx = old.findIndex((row) => (
        row.model_code === group.model_code
        && row.mode === group.mode
        && row.ratio_group === group.ratio_group
        && row.resolution === resolution
      ));
      if (idx >= 0) {
        return old.map((row, i) => (i === idx ? { ...row, unit_points: unitPoints } : row));
      }
      return [...old, {
        model_code: group.model_code,
        mode: group.mode,
        ratio_group: group.ratio_group,
        ratios: group.ratios,
        resolution,
        unit_points: unitPoints,
        enabled: true,
      }];
    });
    setDirty(true);
  };

  const applyDefaultImageMatrix = () => {
    const modelCode = imageMatrixModelCode.trim() || 'gpt-image-2';
    setImageRules((old) => [
      ...old.filter((row) => row.model_code.trim().toLowerCase() !== modelCode.toLowerCase()),
      ...buildDefaultImageMatrix(modelCode),
    ]);
    setDirty(true);
  };

  const save = useMutation({
    mutationFn: () => systemApi.update({
      'billing.model_prices': rows.map((row) => ({
        ...row,
        model_code: row.model_code.trim(),
        name: row.name.trim(),
        provider: row.provider.trim(),
        upstream_model: row.upstream_model.trim(),
        unit_points: Math.round((Number(row.unit_points) || 0) * 100),
        input_unit_points: Math.round((Number(row.input_unit_points) || 0) * 100),
        output_unit_points: Math.round((Number(row.output_unit_points) || 0) * 100),
      })),
      'billing.image_price_rules': imageRules.map((row) => ({
        ...row,
        model_code: row.model_code.trim(),
        ratios: row.ratios.map((ratio) => ratio.trim()).filter(Boolean),
        unit_points: Math.round((Number(row.unit_points) || 0) * 100),
      })),
    }),
    onSuccess: () => {
      toast.success('兼容价格已保存');
      setDirty(false);
      qc.invalidateQueries({ queryKey: ['admin', 'system'] });
    },
    onError: (e: ApiError) => toast.error(e.message),
  });

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title">兼容模型价格</h1>
          <p className="page-subtitle">仅维护旧系统配置 billing.model_prices / billing.image_price_rules；新模型的展示、路由和价格请优先在模型库配置。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="btn btn-outline btn-md" onClick={() => settings.refetch()} disabled={settings.isFetching}>
            <RefreshCw size={16} className={settings.isFetching ? 'animate-spin' : ''} /> 重新加载
          </button>
          <button className="btn btn-primary btn-md" onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
            <Save size={16} /> {save.isPending ? '保存中…' : dirty ? '保存修改' : '已是最新'}
          </button>
        </div>
      </header>

      <LegacyPriceNotice gatewayLikeRows={gatewayLikeRows} />

      <div className="card table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>旧模型编码</th>
              <th>显示名称</th>
              <th>类型</th>
              <th>旧供应商</th>
              <th>旧上游模型映射</th>
              <th>单价（点）</th>
              <th>输入/输出（点/千Token）</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, idx) => (
              <tr key={`${row.model_code}-${idx}`}>
                <td><input className="input min-w-[130px]" value={row.model_code} onChange={(e) => update(idx, { model_code: e.target.value })} /></td>
                <td><input className="input min-w-[130px]" value={row.name} onChange={(e) => update(idx, { name: e.target.value })} /></td>
                <td>
                  <select className="input min-w-[96px]" value={row.kind} onChange={(e) => update(idx, { kind: e.target.value as PriceRow['kind'] })}>
                    <option value="text">文字</option>
                    <option value="image">图片</option>
                    <option value="video">视频</option>
                  </select>
                </td>
                <td><input className="input min-w-[90px]" value={row.provider} onChange={(e) => update(idx, { provider: e.target.value })} /></td>
                <td><input className="input min-w-[150px]" value={row.upstream_model} onChange={(e) => update(idx, { upstream_model: e.target.value })} /></td>
                <td><input className="input w-[100px]" type="number" min={0} value={row.unit_points} onChange={(e) => update(idx, { unit_points: Number(e.target.value) || 0 })} disabled={row.kind === 'text'} /></td>
                <td>
                  {row.kind === 'text' ? (
                    <div className="flex gap-2">
                      <input className="input w-[90px]" type="number" min={0} value={row.input_unit_points || 0} onChange={(e) => update(idx, { input_unit_points: Number(e.target.value) || 0 })} placeholder="输入" />
                      <input className="input w-[90px]" type="number" min={0} value={row.output_unit_points || 0} onChange={(e) => update(idx, { output_unit_points: Number(e.target.value) || 0 })} placeholder="输出" />
                    </div>
                  ) : (
                    <span className="text-muted">-</span>
                  )}
                </td>
                <td>
                  <button className={row.enabled ? 'btn btn-outline btn-sm' : 'btn btn-ghost btn-sm'} onClick={() => update(idx, { enabled: !row.enabled })}>
                    {row.enabled ? '启用' : '停用'}
                  </button>
                </td>
                <td>
                  <button className="btn btn-danger-ghost btn-icon btn-sm" onClick={() => { setRows((old) => old.filter((_, i) => i !== idx)); setDirty(true); }}>
                    <Trash2 size={14} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="card space-y-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-h4 font-semibold text-text-primary">兼容图片价格矩阵</h2>
            <p className="mt-1 text-small text-text-tertiary">仅作为模型库没有有效图片矩阵时的兜底。新图片模型请在模型库的价格矩阵维护。</p>
          </div>
          <button
            className="btn btn-outline btn-sm"
            onClick={() => {
              setImageRules(DEFAULT_IMAGE_PRICE_RULES);
              setDirty(true);
            }}
          >
            恢复默认梯度
          </button>
        </div>
        <div className="flex flex-wrap items-end gap-2 rounded-md border border-border bg-surface-2 p-3">
          <label className="field min-w-[240px]">
            <span className="field-label">快速覆盖模型</span>
            <input className="input" value={imageMatrixModelCode} onChange={(e) => setImageMatrixModelCode(e.target.value)} placeholder="gpt-image-2" />
          </label>
          <button className="btn btn-outline btn-md" onClick={applyDefaultImageMatrix}>
            <Plus size={16} /> 生成完整图片梯度
          </button>
          <div className="text-small text-text-tertiary">会覆盖同模型现有图片规则，并一次生成文生图/图生图 × 常规/扩展 × 1K/2K/4K。</div>
        </div>
        <div className="table-wrap">
          <table className="data-table min-w-[820px]">
            <thead>
              <tr>
                <th>模型 / 场景</th>
                <th>覆盖比例</th>
                {IMAGE_RESOLUTIONS.map((resolution) => <th key={resolution}>{resolution}（点）</th>)}
                <th>组状态</th>
              </tr>
            </thead>
            <tbody>
              {imageMatrixGroups.map((group) => (
                <tr key={group.key}>
                  <td>
                    <div className="font-semibold text-text-primary">{group.model_code}</div>
                    <div className="mt-1 text-tiny text-text-tertiary">{imageModeLabel(group.mode)} · {ratioGroupLabel(group.ratio_group)}</div>
                  </td>
                  <td className="max-w-[280px] text-small text-text-secondary">{group.ratios.join(', ')}</td>
                  {IMAGE_RESOLUTIONS.map((resolution) => (
                    <td key={resolution}>
                      <input
                        className="input w-[96px]"
                        type="number"
                        min={0}
                        step={0.5}
                        value={group.prices[resolution] ?? ''}
                        onChange={(e) => setImageMatrixPrice(group, resolution, Number(e.target.value) || 0)}
                      />
                    </td>
                  ))}
                  <td>{group.enabled ? '有启用规则' : '全部停用'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>模型编码</th>
                <th>模式</th>
                <th>比例组</th>
                <th>覆盖比例</th>
                <th>分辨率</th>
                <th>单张扣费（点）</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {imageRules.map((row, idx) => (
                <tr key={`${row.model_code}-${row.mode}-${row.ratio_group}-${row.resolution}-${idx}`}>
                  <td><input className="input min-w-[140px]" value={row.model_code} onChange={(e) => updateImageRule(idx, { model_code: e.target.value })} /></td>
                  <td>
                    <select className="input min-w-[110px]" value={row.mode} onChange={(e) => updateImageRule(idx, { mode: e.target.value as ImagePriceRuleRow['mode'] })}>
                      <option value="t2i">文生图</option>
                      <option value="i2i">图生图</option>
                    </select>
                  </td>
                  <td>
                    <select className="input min-w-[120px]" value={row.ratio_group} onChange={(e) => updateImageRule(idx, { ratio_group: e.target.value as ImagePriceRuleRow['ratio_group'] })}>
                      <option value="standard">常规比例</option>
                      <option value="extended">扩展比例</option>
                    </select>
                  </td>
                  <td>
                    <input
                      className="input min-w-[250px]"
                      value={row.ratios.join(', ')}
                      onChange={(e) => updateImageRule(idx, { ratios: e.target.value.split(',').map((item) => item.trim()).filter(Boolean) })}
                    />
                  </td>
                  <td>
                    <select className="input min-w-[90px]" value={row.resolution} onChange={(e) => updateImageRule(idx, { resolution: e.target.value as ImagePriceRuleRow['resolution'] })}>
                      <option value="1K">1K</option>
                      <option value="2K">2K</option>
                      <option value="4K">4K</option>
                    </select>
                  </td>
                  <td><input className="input w-[110px]" type="number" min={0} step={0.5} value={row.unit_points} onChange={(e) => updateImageRule(idx, { unit_points: Number(e.target.value) || 0 })} /></td>
                  <td>
                    <button className={row.enabled ? 'btn btn-outline btn-sm' : 'btn btn-ghost btn-sm'} onClick={() => updateImageRule(idx, { enabled: !row.enabled })}>
                      {row.enabled ? '启用' : '停用'}
                    </button>
                  </td>
                  <td>
                    <button className="btn btn-danger-ghost btn-icon btn-sm" onClick={() => { setImageRules((old) => old.filter((_, i) => i !== idx)); setDirty(true); }}>
                      <Trash2 size={14} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <button
          className="btn btn-outline btn-md"
          onClick={() => {
            setImageRules((old) => [...old, { model_code: 'gpt-image-2', mode: 't2i', ratio_group: 'standard', ratios: STANDARD_IMAGE_RATIOS, resolution: '1K', unit_points: 4, enabled: true }]);
            setDirty(true);
          }}
        >
          <Plus size={16} /> 添加图片价格规则
        </button>
      </div>

      <button
        className="btn btn-outline btn-md"
        onClick={() => {
          setRows((old) => [...old, { model_code: '', name: '', kind: 'image', provider: 'gpt', upstream_model: '', unit_points: 0, input_unit_points: 0, output_unit_points: 0, enabled: true }]);
          setDirty(true);
        }}
      >
        <Plus size={16} /> 添加模型
      </button>
    </div>
  );
}

function LegacyPriceNotice({ gatewayLikeRows }: { gatewayLikeRows: PriceRow[] }) {
  return (
    <section className="rounded-md border border-amber-200 bg-amber-50 p-4 text-amber-950">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 flex-1 items-start gap-3">
          <AlertTriangle size={18} className="mt-0.5 shrink-0" />
          <div className="min-w-0">
            <div className="text-small font-semibold">这是旧价格兼容页，不负责 Model Gateway 路由</div>
            <p className="mt-1 text-small leading-relaxed text-amber-900">
              在这里新增 MiMo、DeepSeek 或其他 OpenAI-compatible 模型，只会影响旧价格兜底；不会创建 API 渠道、不会绑定来源映射，也不会阻止系统继续尝试错误账号池。新模型请走“API 渠道、模型库、模型审计”。
            </p>
            {gatewayLikeRows.length > 0 && (
              <div className="mt-3 rounded-md border border-amber-200 bg-white/70 p-3 text-small">
                <div className="font-semibold">检测到疑似新网关模型仍在旧价格表中</div>
                <div className="mt-1 break-words font-mono text-tiny text-amber-800">
                  {gatewayLikeRows.slice(0, 8).map((row) => row.model_code || row.name || row.upstream_model).join(', ')}
                  {gatewayLikeRows.length > 8 ? ` +${gatewayLikeRows.length - 8}` : ''}
                </div>
              </div>
            )}
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <Link className="btn btn-outline btn-sm bg-white/80" to="/api-channels">
            <Globe2 size={14} /> API 渠道
          </Link>
          <Link className="btn btn-outline btn-sm bg-white/80" to="/model-gateway">
            <GitBranch size={14} /> 模型库
          </Link>
          <Link className="btn btn-outline btn-sm bg-white/80" to="/model-gateway-audit">
            <ClipboardList size={14} /> 模型审计
          </Link>
        </div>
      </div>
    </section>
  );
}

function isGatewayLikeLegacyPriceRow(row: PriceRow) {
  const provider = row.provider.trim().toLowerCase();
  const text = `${row.model_code} ${row.name} ${row.upstream_model} ${provider}`.toLowerCase();
  if (/(mimo|deepseek|openai-compatible|openai_compatible|official|api channel|api_channel)/.test(text)) {
    return true;
  }
  return row.kind === 'text' && provider !== '' && provider !== 'gpt' && provider !== 'grok';
}
