import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Edit3, Images, Plus, RefreshCw, Search, Sparkles, Trash2, Upload } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import { ApiError } from '../../lib/api';
import { fmtNumber, fmtTime } from '../../lib/format';
import { promptGalleryApi } from '../../lib/services';
import type {
  AdminPromptGalleryBody,
  AdminPromptGalleryItem,
  PromptGalleryModality,
} from '../../lib/types';
import { toast } from '../../stores/toast';

interface FormState {
  id?: number;
  modality: PromptGalleryModality;
  category: string;
  title: string;
  subtitle: string;
  cover_url: string;
  prompt: string;
  tags: string;
  variables_schema: string;
  sort_order: number;
  status: 0 | 1;
  locale: string;
}

const DEFAULT_FORM: FormState = {
  modality: 'image',
  category: 'default',
  title: '',
  subtitle: '',
  cover_url: '',
  prompt: '',
  tags: '',
  variables_schema: '{}',
  sort_order: 0,
  status: 1,
  locale: 'zh-CN',
};

export default function PromptGalleryPage() {
  const qc = useQueryClient();
  const [keyword, setKeyword] = useState('');
  const [modality, setModality] = useState<PromptGalleryModality | ''>('');
  const [status, setStatus] = useState<'' | '0' | '1'>('');
  const [category, setCategory] = useState('');
  const [page, setPage] = useState(1);
  const [form, setForm] = useState<FormState | null>(null);
  const pageSize = 20;

  const query = useQuery({
    queryKey: ['admin', 'prompt-gallery', keyword, modality, status, category, page],
    queryFn: () =>
      promptGalleryApi.list({
        keyword: keyword.trim() || undefined,
        modality,
        category: category.trim() || undefined,
        status: status === '' ? '' : (Number(status) as 0 | 1),
        page,
        page_size: pageSize,
      }),
  });

  const rows = query.data?.list ?? [];
  const total = query.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / pageSize));

  const save = useMutation({
    mutationFn: (f: FormState) => {
      const body = formToBody(f);
      return f.id ? promptGalleryApi.update(f.id, body) : promptGalleryApi.create(body).then(() => undefined);
    },
    onSuccess: () => {
      toast.success('快捷提示词已保存');
      setForm(null);
      qc.invalidateQueries({ queryKey: ['admin', 'prompt-gallery'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const remove = useMutation({
    mutationFn: (id: number) => promptGalleryApi.remove(id),
    onSuccess: () => {
      toast.success('快捷提示词已删除');
      qc.invalidateQueries({ queryKey: ['admin', 'prompt-gallery'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const toggle = useMutation({
    mutationFn: (row: AdminPromptGalleryItem) =>
      promptGalleryApi.update(row.id, { status: row.status === 1 ? 0 : 1 }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'prompt-gallery'] }),
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const normalizeCurrentPageSort = useMutation({
    mutationFn: () =>
      promptGalleryApi.reorder({
        items: rows.map((row, index) => ({ id: row.id, sort_order: (page - 1) * pageSize + index + 1 })),
      }),
    onSuccess: () => {
      toast.success('当前页排序已重排');
      qc.invalidateQueries({ queryKey: ['admin', 'prompt-gallery'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const seedDefaults = useMutation({
    mutationFn: () => promptGalleryApi.seedDefaults(),
    onSuccess: (res) => {
      toast.success(res.inserted > 0 ? `已导入 ${res.inserted} 条前台默认案例` : '默认案例已经存在');
      qc.invalidateQueries({ queryKey: ['admin', 'prompt-gallery'] });
      setPage(1);
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title flex items-center gap-2">
            <Images className="text-klein-500" size={26} />快捷提示词
          </h1>
          <p className="page-subtitle">维护前台图片、文字、视频入口的快捷提示词卡片，控制封面、提示词、分类、排序和启停。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className="btn btn-outline btn-md" onClick={() => query.refetch()} disabled={query.isFetching}>
            <RefreshCw size={16} className={query.isFetching ? 'animate-spin' : ''} /> 刷新
          </button>
          <button className="btn btn-outline btn-md" onClick={() => normalizeCurrentPageSort.mutate()} disabled={rows.length === 0 || normalizeCurrentPageSort.isPending}>
            重排当前页
          </button>
          <button className="btn btn-outline btn-md" onClick={() => seedDefaults.mutate()} disabled={seedDefaults.isPending}>
            <Sparkles size={16} /> 导入默认案例
          </button>
          <button className="btn btn-primary btn-md" onClick={() => setForm(DEFAULT_FORM)}>
            <Plus size={16} /> 新增卡片
          </button>
        </div>
      </header>

      <div className="card card-section flex flex-wrap items-center gap-2 !py-3">
        <div className="relative min-w-[220px] flex-1">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-tertiary" />
          <input className="input pl-9" value={keyword} onChange={(e) => { setKeyword(e.target.value); setPage(1); }} placeholder="搜索标题、分类、提示词、ID" />
        </div>
        <select className="select select-sm min-w-[110px]" value={modality} onChange={(e) => { setModality(e.target.value as typeof modality); setPage(1); }}>
          <option value="">全部入口</option>
          <option value="image">图片</option>
          <option value="text">文字</option>
          <option value="video">视频</option>
        </select>
        <input className="input input-sm min-w-[160px]" value={category} onChange={(e) => { setCategory(e.target.value); setPage(1); }} placeholder="分类筛选" />
        <select className="select select-sm min-w-[110px]" value={status} onChange={(e) => { setStatus(e.target.value as typeof status); setPage(1); }}>
          <option value="">全部状态</option>
          <option value="1">启用</option>
          <option value="0">停用</option>
        </select>
      </div>

      <div className="card table-wrap">
        <table className="data-table min-w-[1180px]">
          <thead>
            <tr>
              <th>卡片</th>
              <th>入口</th>
              <th>分类 / 标签</th>
              <th>提示词</th>
              <th>排序</th>
              <th>状态</th>
              <th>更新时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id}>
                <td>
                  <div className="flex min-w-[260px] items-center gap-3">
                    <img src={row.cover_url} alt={row.title} className="h-16 w-14 rounded-md object-cover ring-1 ring-border" loading="lazy" />
                    <div>
                      <div className="font-semibold text-text-primary">{row.title}</div>
                      {row.subtitle && <div className="line-clamp-1 text-tiny text-text-tertiary">{row.subtitle}</div>}
                      <div className="text-tiny text-text-tertiary">ID {row.id} · {row.locale || 'zh-CN'}</div>
                    </div>
                  </div>
                </td>
                <td><span className="badge badge-outline">{modalityLabel(row.modality)}</span></td>
                <td>
                  <div className="text-small text-text-primary">{row.category || 'default'}</div>
                  <div className="mt-1 flex max-w-[220px] flex-wrap gap-1">
                    {row.tags.slice(0, 4).map((tag) => <span key={tag} className="badge badge-soft">{tag}</span>)}
                    {row.tags.length === 0 && <span className="text-tiny text-text-tertiary">无标签</span>}
                  </div>
                </td>
                <td><p className="line-clamp-3 max-w-[360px] text-small text-text-secondary">{row.prompt}</p></td>
                <td className="font-mono">{row.sort_order}</td>
                <td>
                  <button className={row.status === 1 ? 'btn btn-outline btn-sm' : 'btn btn-ghost btn-sm'} onClick={() => toggle.mutate(row)}>
                    {row.status === 1 ? '启用' : '停用'}
                  </button>
                </td>
                <td className="whitespace-nowrap">{fmtTime(row.updated_at)}</td>
                <td>
                  <div className="flex items-center gap-1">
                    <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setForm(rowToForm(row))} title="编辑"><Edit3 size={14} /></button>
                    <button className="btn btn-danger-ghost btn-icon btn-sm" onClick={() => { if (confirm(`删除快捷提示词「${row.title}」？`)) remove.mutate(row.id); }} title="删除"><Trash2 size={14} /></button>
                  </div>
                </td>
              </tr>
            ))}
            {!query.isLoading && rows.length === 0 && (
              <tr><td colSpan={8} className="py-10 text-center text-text-tertiary">暂无快捷提示词</td></tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="card card-section flex flex-wrap items-center justify-between gap-3 !py-2">
        <span className="text-small text-text-tertiary">第 {page} / {pages} 页，共 {fmtNumber(total)} 条</span>
        <div className="flex gap-2">
          <button className="btn btn-outline btn-sm" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>上一页</button>
          <button className="btn btn-outline btn-sm" disabled={page >= pages} onClick={() => setPage((p) => p + 1)}>下一页</button>
        </div>
      </div>

      {form && <PromptGalleryDialog form={form} setForm={setForm} saving={save.isPending} onClose={() => setForm(null)} onSave={() => save.mutate(form)} />}
    </div>
  );
}

function PromptGalleryDialog({ form, setForm, saving, onClose, onSave }: { form: FormState; setForm: (f: FormState | null) => void; saving: boolean; onClose: () => void; onSave: () => void }) {
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setForm({ ...form, [k]: v });
  const uploadCover = useMutation({
    mutationFn: (file: File) => promptGalleryApi.uploadCover(file),
    onSuccess: (res) => {
      set('cover_url', res.url);
      toast.success('封面已上传');
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-surface-overlay p-4">
      <div className="card card-section max-h-[92vh] w-full max-w-5xl space-y-4 overflow-y-auto">
        <header className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-h4 font-semibold text-text-primary">{form.id ? '编辑快捷提示词' : '新增快捷提示词'}</h2>
            <p className="text-small text-text-tertiary">封面可直接上传；提示词会填入前台输入框，不会自动生成。</p>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={onClose}>关闭</button>
        </header>

        <div className="grid gap-4 lg:grid-cols-[1fr_260px]">
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="入口类型">
              <select className="select" value={form.modality} onChange={(e) => set('modality', e.target.value as PromptGalleryModality)}>
                <option value="image">图片</option>
                <option value="text">文字</option>
                <option value="video">视频</option>
              </select>
            </Field>
            <Field label="状态">
              <select className="select" value={form.status} onChange={(e) => set('status', Number(e.target.value) as 0 | 1)}>
                <option value={1}>启用</option>
                <option value={0}>停用</option>
              </select>
            </Field>
            <Field label="标题"><input className="input" value={form.title} onChange={(e) => set('title', e.target.value)} placeholder="极简产品广告" /></Field>
            <Field label="副标题"><input className="input" value={form.subtitle} onChange={(e) => set('subtitle', e.target.value)} placeholder="可选，用于后台和后续前台扩展" /></Field>
            <Field label="分类"><input className="input" value={form.category} onChange={(e) => set('category', e.target.value)} placeholder="poster / product / social" /></Field>
            <Field label="标签"><input className="input" value={form.tags} onChange={(e) => set('tags', e.target.value)} placeholder="逗号分隔：广告,产品,极简" /></Field>
            <Field label="排序"><input className="input" type="number" value={form.sort_order} onChange={(e) => set('sort_order', Number(e.target.value) || 0)} /></Field>
            <Field label="语言"><input className="input" value={form.locale} onChange={(e) => set('locale', e.target.value)} placeholder="zh-CN" /></Field>
            <Field label="封面图">
              <div className="flex flex-col gap-2">
                <input className="input" value={form.cover_url} onChange={(e) => set('cover_url', e.target.value)} placeholder="/api/v1/gen/cached/... 或 https://..." />
                <label className="btn btn-outline btn-sm w-fit cursor-pointer">
                  <Upload size={14} />
                  {uploadCover.isPending ? '上传中...' : '上传图片'}
                  <input
                    type="file"
                    accept="image/png,image/jpeg,image/webp,image/avif,image/gif"
                    className="hidden"
                    disabled={uploadCover.isPending}
                    onChange={(e) => {
                      const file = e.target.files?.[0];
                      e.currentTarget.value = '';
                      if (file) uploadCover.mutate(file);
                    }}
                  />
                </label>
              </div>
            </Field>
            <Field label="变量 Schema JSON">
              <textarea className="textarea min-h-[88px] font-mono text-small" value={form.variables_schema} onChange={(e) => set('variables_schema', e.target.value)} placeholder='{"product":{"label":"产品","default":"咖啡"}}' />
            </Field>
            <div className="md:col-span-2">
              <Field label="提示词">
                <textarea className="textarea min-h-[220px] font-mono text-small leading-relaxed" value={form.prompt} onChange={(e) => set('prompt', e.target.value)} placeholder="写入点击卡片后要填入前台输入框的完整提示词" />
              </Field>
            </div>
          </div>

          <aside className="space-y-3">
            <div className="text-small font-semibold text-text-primary">卡片预览</div>
            <div className="relative aspect-[4/5] overflow-hidden rounded-lg bg-surface-2 ring-1 ring-border">
              {form.cover_url ? (
                <img src={form.cover_url} alt={form.title || '预览'} className="absolute inset-0 h-full w-full object-cover" />
              ) : (
                <div className="grid h-full place-items-center text-small text-text-tertiary">等待封面</div>
              )}
              <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-black/10 to-transparent" />
              <div className="absolute bottom-3 left-3 right-3">
                <span className="text-sm font-medium text-white">{form.title || '未命名卡片'}</span>
                {form.subtitle && <p className="mt-1 line-clamp-2 text-tiny text-white/75">{form.subtitle}</p>}
              </div>
            </div>
          </aside>
        </div>

        <div className="flex justify-end gap-2">
          <button className="btn btn-outline btn-md" onClick={onClose}>取消</button>
          <button className="btn btn-primary btn-md" disabled={saving} onClick={onSave}>{saving ? '保存中...' : '保存'}</button>
        </div>
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="field"><span className="field-label">{label}</span>{children}</label>;
}

function rowToForm(row: AdminPromptGalleryItem): FormState {
  return {
    id: row.id,
    modality: row.modality,
    category: row.category || 'default',
    title: row.title,
    subtitle: row.subtitle || '',
    cover_url: row.cover_url,
    prompt: row.prompt,
    tags: row.tags.join(', '),
    variables_schema: JSON.stringify(row.variables_schema || {}, null, 2),
    sort_order: row.sort_order,
    status: row.status === 1 ? 1 : 0,
    locale: row.locale || 'zh-CN',
  };
}

function formToBody(f: FormState): AdminPromptGalleryBody {
  const variables = parseVariables(f.variables_schema);
  return {
    modality: f.modality,
    category: f.category.trim() || 'default',
    title: f.title.trim(),
    subtitle: f.subtitle.trim(),
    cover_url: f.cover_url.trim(),
    prompt: f.prompt.trim(),
    tags: f.tags.split(',').map((tag) => tag.trim()).filter(Boolean),
    variables_schema: variables,
    sort_order: Number(f.sort_order) || 0,
    status: f.status,
    locale: f.locale.trim() || 'zh-CN',
  };
}

function parseVariables(raw: string): Record<string, unknown> {
  const text = raw.trim();
  if (!text) return {};
  const parsed = JSON.parse(text) as unknown;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('变量 Schema 必须是 JSON 对象');
  }
  return parsed as Record<string, unknown>;
}

function modalityLabel(v: string) {
  if (v === 'image') return '图片';
  if (v === 'text') return '文字';
  if (v === 'video') return '视频';
  return v;
}
