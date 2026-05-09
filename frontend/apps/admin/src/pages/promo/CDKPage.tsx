import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, CheckCircle2, Copy, Download, RefreshCw, Search, Ticket } from 'lucide-react';
import { useMemo, useState, type FormEvent, type ReactNode } from 'react';

import { ApiError } from '../../lib/api';
import { cdkApi } from '../../lib/services';
import type { CDKBatchItem, CDKCodeItem, CDKCreateBatchBody, CDKCreateBatchResp } from '../../lib/types';
import { fmtNumber, fmtPoints, fmtTime } from '../../lib/format';
import { toast } from '../../stores/toast';

const BATCH_PAGE_SIZE = 20;
const CODE_PAGE_SIZE = 200;

export default function CDKPage() {
  const qc = useQueryClient();
  const [body, setBody] = useState<CDKCreateBatchBody>({
    batch_no: '',
    name: '',
    points: 1000,
    qty: 100,
    per_user_limit: 1,
    expire_at: 0,
  });
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState<'' | '0' | '1' | '2'>('');
  const [page, setPage] = useState(1);
  const [selectedBatchId, setSelectedBatchId] = useState<number | null>(null);
  const [codesPage, setCodesPage] = useState(1);
  const [last, setLast] = useState<CDKCreateBatchResp | null>(null);

  const batchesQuery = useQuery({
    queryKey: ['admin', 'cdk', 'batches', keyword, status, page],
    queryFn: () => cdkApi.listBatches({
      keyword: keyword.trim() || undefined,
      status: status === '' ? '' : Number(status) as 0 | 1 | 2,
      page,
      page_size: BATCH_PAGE_SIZE,
    }),
  });

  const batches = batchesQuery.data?.list ?? [];
  const total = batchesQuery.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / BATCH_PAGE_SIZE));
  const selectedBatch = useMemo(
    () => batches.find((row) => row.id === selectedBatchId) ?? null,
    [batches, selectedBatchId],
  );

  const codesQuery = useQuery({
    queryKey: ['admin', 'cdk', 'codes', selectedBatchId, codesPage],
    queryFn: () => cdkApi.listCodes(selectedBatchId ?? 0, { page: codesPage, page_size: CODE_PAGE_SIZE }),
    enabled: Boolean(selectedBatchId),
  });

  const codes = codesQuery.data?.list ?? [];
  const codesTotal = codesQuery.data?.total ?? 0;
  const codesPages = Math.max(1, Math.ceil(codesTotal / CODE_PAGE_SIZE));

  const create = useMutation({
    mutationFn: (b: CDKCreateBatchBody) => cdkApi.createBatch(b),
    onSuccess: (r) => {
      toast.success(`已生成批次 ${r.batch_no}（共 ${r.total_qty} 张）`);
      setLast(r);
      setSelectedBatchId(r.id);
      setCodesPage(1);
      qc.invalidateQueries({ queryKey: ['admin', 'cdk'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!body.batch_no.trim() || !body.name.trim()) {
      toast.error('请填写批次号和名称');
      return;
    }
    if (body.points <= 0 || body.qty <= 0) {
      toast.error('点数和数量必须 > 0');
      return;
    }
    create.mutate({
      ...body,
      batch_no: body.batch_no.trim().toUpperCase(),
      name: body.name.trim(),
      per_user_limit: body.per_user_limit || 0,
      expire_at: body.expire_at || undefined,
    });
  };

  const copyCodes = async () => {
    if (codes.length === 0) return;
    await copyText(codes.map((row) => row.code).join('\n'));
    toast.success('已复制当前页 CDK');
  };

  const exportCodes = () => {
    if (codes.length === 0 || !selectedBatchId) return;
    const csv = [
      ['code', 'status', 'used_by', 'used_at', 'created_at'],
      ...codes.map((row) => [
        row.code,
        codeStatusLabel(row.status),
        row.used_by ? String(row.used_by) : '',
        row.used_at ? fmtTime(row.used_at) : '',
        fmtTime(row.created_at),
      ]),
    ].map((row) => row.map(csvCell).join(',')).join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `cdk-batch-${selectedBatchId}-page-${codesPage}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="page page-wide space-y-6">
      <header className="page-header">
        <div>
          <h1 className="page-title flex items-center gap-2">
            <Ticket className="text-klein-500" size={26} />
            兑换码 CDK
          </h1>
          <p className="page-subtitle">按批次生成、查看、复制和导出兑换码，兑换后会写入用户钱包流水。</p>
        </div>
        <button className="btn btn-outline btn-md" onClick={() => batchesQuery.refetch()} disabled={batchesQuery.isFetching}>
          <RefreshCw size={16} className={batchesQuery.isFetching ? 'animate-spin' : ''} /> 刷新
        </button>
      </header>

      <form onSubmit={submit} className="card card-section grid w-full gap-5 lg:grid-cols-2">
        <Field label="批次号" hint="同批次唯一，如 SPRING2026-A">
          <input
            className="input"
            value={body.batch_no}
            onChange={(e) => setBody((s) => ({ ...s, batch_no: e.target.value.toUpperCase() }))}
            placeholder="SPRING2026-A"
          />
        </Field>

        <Field label="批次名称" hint="展示给运营 / 客服的友好名称">
          <input
            className="input"
            value={body.name}
            onChange={(e) => setBody((s) => ({ ...s, name: e.target.value }))}
            placeholder="春节活动 100 点"
          />
        </Field>

        <Field label="单码点数（×100 储存）" hint={`输入 1000 = 实际 10.00 点；当前等价：${fmtPoints(body.points)} 点`}>
          <input
            type="number"
            min={1}
            className="input"
            value={body.points}
            onChange={(e) => setBody((s) => ({ ...s, points: Math.max(1, Number(e.target.value) || 0) }))}
          />
        </Field>

        <Field label="生成数量" hint="单批次最多 100,000 张">
          <input
            type="number"
            min={1}
            max={100_000}
            className="input"
            value={body.qty}
            onChange={(e) => setBody((s) => ({ ...s, qty: Math.max(1, Number(e.target.value) || 0) }))}
          />
        </Field>

        <Field label="每用户限领次数" hint="0 表示不限制；建议 1">
          <input
            type="number"
            min={0}
            className="input"
            value={body.per_user_limit ?? 0}
            onChange={(e) => setBody((s) => ({ ...s, per_user_limit: Number(e.target.value) || 0 }))}
          />
        </Field>

        <Field label="过期时间（可选）" hint="留空表示永久有效">
          <input
            type="datetime-local"
            className="input"
            onChange={(e) => {
              const v = e.target.value;
              setBody((s) => ({ ...s, expire_at: v ? Math.floor(new Date(v).getTime() / 1000) : 0 }));
            }}
          />
        </Field>

        <div className="lg:col-span-2 flex flex-col items-stretch justify-between gap-3 rounded-md bg-klein-gradient-soft p-4 md:flex-row md:items-center">
          <div className="flex flex-wrap items-center gap-2 text-small text-text-secondary">
            <AlertCircle size={16} className="text-klein-500" />
            <span>预计生成</span>
            <strong className="text-text-primary">{fmtNumber(body.qty)}</strong>
            <span>张，单码价值</span>
            <strong className="text-text-primary">{fmtPoints(body.points)} 点</strong>
            <span>，合计</span>
            <strong className="text-klein-500">{fmtPoints(body.points * body.qty)} 点</strong>
          </div>
          <button type="submit" className="btn btn-primary btn-md md:shrink-0" disabled={create.isPending}>
            {create.isPending ? '生成中...' : '生成批次'}
          </button>
        </div>
      </form>

      {last && (
        <div className="card card-section flex w-full items-start gap-3 border-success/40">
          <CheckCircle2 className="text-success shrink-0 mt-0.5" size={20} />
          <div className="flex-1 space-y-1">
            <p className="text-text-primary font-medium">最新生成成功</p>
            <p className="text-small text-text-secondary">
              批次 ID #{last.id} · 批次号 <code className="kbd mx-1">{last.batch_no}</code> · 共 {fmtNumber(last.total_qty)} 张
            </p>
          </div>
        </div>
      )}

      <section className="card card-section space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[220px] flex-1">
            <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-tertiary" />
            <input className="input pl-9" value={keyword} onChange={(e) => { setKeyword(e.target.value); setPage(1); }} placeholder="搜索批次号、名称、ID" />
          </div>
          <select className="select select-sm min-w-[120px]" value={status} onChange={(e) => { setStatus(e.target.value as typeof status); setPage(1); }}>
            <option value="">全部状态</option>
            <option value="1">启用</option>
            <option value="0">停用</option>
            <option value="2">作废</option>
          </select>
        </div>

        <div className="table-wrap">
          <table className="data-table min-w-[1040px]">
            <thead>
              <tr>
                <th>批次</th>
                <th>单码点数</th>
                <th>使用量</th>
                <th>每用户</th>
                <th>有效期</th>
                <th>状态</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {batches.map((row) => (
                <tr key={row.id} className={row.id === selectedBatchId ? 'bg-klein-50/60' : undefined}>
                  <td>
                    <div className="font-semibold text-text-primary">{row.batch_no}</div>
                    <div className="text-tiny text-text-tertiary">#{row.id} · {row.name}</div>
                  </td>
                  <td>{fmtPoints(row.points)} 点</td>
                  <td>{fmtNumber(row.used_qty)} / {fmtNumber(row.total_qty)}</td>
                  <td>{row.per_user_limit > 0 ? `${row.per_user_limit} 次` : '不限'}</td>
                  <td className="whitespace-nowrap">{row.expire_at ? fmtTime(row.expire_at) : '永久'}</td>
                  <td><span className="badge badge-outline">{batchStatusLabel(row.status)}</span></td>
                  <td className="whitespace-nowrap">{fmtTime(row.created_at)}</td>
                  <td>
                    <button
                      className="btn btn-outline btn-sm"
                      onClick={() => {
                        setSelectedBatchId(row.id);
                        setCodesPage(1);
                      }}
                    >
                      查看码
                    </button>
                  </td>
                </tr>
              ))}
              {!batchesQuery.isLoading && batches.length === 0 && (
                <tr><td colSpan={8} className="py-10 text-center text-text-tertiary">暂无 CDK 批次</td></tr>
              )}
            </tbody>
          </table>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className="text-small text-text-tertiary">第 {page} / {pages} 页，共 {fmtNumber(total)} 条</span>
          <div className="flex gap-2">
            <button className="btn btn-outline btn-sm" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>上一页</button>
            <button className="btn btn-outline btn-sm" disabled={page >= pages} onClick={() => setPage((p) => p + 1)}>下一页</button>
          </div>
        </div>
      </section>

      {selectedBatchId && (
        <section className="card card-section space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-h4 font-semibold text-text-primary">CDK 明细</h2>
              <p className="text-small text-text-tertiary">
                {selectedBatch ? `${selectedBatch.batch_no} · ${selectedBatch.name}` : `批次 #${selectedBatchId}`}
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <button className="btn btn-outline btn-md" onClick={() => codesQuery.refetch()} disabled={codesQuery.isFetching}>
                <RefreshCw size={16} className={codesQuery.isFetching ? 'animate-spin' : ''} /> 刷新
              </button>
              <button className="btn btn-outline btn-md" onClick={copyCodes} disabled={codes.length === 0}>
                <Copy size={16} /> 复制本页
              </button>
              <button className="btn btn-outline btn-md" onClick={exportCodes} disabled={codes.length === 0}>
                <Download size={16} /> 导出本页
              </button>
            </div>
          </div>

          <div className="table-wrap">
            <table className="data-table min-w-[860px]">
              <thead>
                <tr>
                  <th>兑换码</th>
                  <th>状态</th>
                  <th>使用用户</th>
                  <th>使用时间</th>
                  <th>创建时间</th>
                </tr>
              </thead>
              <tbody>
                {codes.map((row) => (
                  <tr key={row.id}>
                    <td><code className="kbd">{row.code}</code></td>
                    <td><span className="badge badge-outline">{codeStatusLabel(row.status)}</span></td>
                    <td>{row.used_by ? `#${row.used_by}` : '—'}</td>
                    <td className="whitespace-nowrap">{fmtTime(row.used_at)}</td>
                    <td className="whitespace-nowrap">{fmtTime(row.created_at)}</td>
                  </tr>
                ))}
                {!codesQuery.isLoading && codes.length === 0 && (
                  <tr><td colSpan={5} className="py-10 text-center text-text-tertiary">暂无 CDK 明细</td></tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-small text-text-tertiary">第 {codesPage} / {codesPages} 页，共 {fmtNumber(codesTotal)} 条</span>
            <div className="flex gap-2">
              <button className="btn btn-outline btn-sm" disabled={codesPage <= 1} onClick={() => setCodesPage((p) => Math.max(1, p - 1))}>上一页</button>
              <button className="btn btn-outline btn-sm" disabled={codesPage >= codesPages} onClick={() => setCodesPage((p) => p + 1)}>下一页</button>
            </div>
          </div>
        </section>
      )}
    </div>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

function batchStatusLabel(status: CDKBatchItem['status']) {
  if (status === 1) return '启用';
  if (status === 0) return '停用';
  if (status === 2) return '作废';
  return String(status);
}

function codeStatusLabel(status: CDKCodeItem['status']) {
  if (status === 0) return '未使用';
  if (status === 1) return '已使用';
  if (status === 2) return '作废';
  return String(status);
}

async function copyText(text: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  document.execCommand('copy');
  ta.remove();
}

function csvCell(v: string) {
  return `"${v.replace(/"/g, '""')}"`;
}
