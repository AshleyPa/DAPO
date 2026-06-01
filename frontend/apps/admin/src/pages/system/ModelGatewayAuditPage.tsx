import { useQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight, ClipboardList, RefreshCw, Search } from 'lucide-react';
import { Fragment, useMemo, useState } from 'react';

import { fmtPoints, fmtTime } from '../../lib/format';
import { logsApi, modelGatewayApi, type ModelGatewayAuditListQuery } from '../../lib/services';
import type { AdminGenerationUpstreamLogItem, ModelGatewayAuditItem } from '../../lib/types';

const pageSize = 20;
type AuditType = 'all' | 'route' | 'pricing' | 'output' | 'output_missing' | 'video' | 'video_missing';

function statusLabel(s: number) {
  switch (s) {
    case 0:
      return { text: '待处理', cls: 'badge badge-outline' };
    case 1:
      return { text: '生成中', cls: 'badge badge-warning' };
    case 2:
      return { text: '成功', cls: 'badge badge-success' };
    case 3:
      return { text: '失败', cls: 'badge badge-danger' };
    case 4:
      return { text: '已退款', cls: 'badge badge-warning' };
    default:
      return { text: String(s), cls: 'badge badge-outline' };
  }
}

function kindLabel(kind: string) {
  if (kind === 'image') return '图片';
  if (kind === 'video') return '视频';
  if (kind === 'chat' || kind === 'text') return '文字';
  return kind || '-';
}

function sourceLabel(row: ModelGatewayAuditItem) {
  const name = row.selected_source_name || row.selected_source_code || row.selected_provider || '-';
  const type = row.selected_source_type || '-';
  const adapter = row.selected_adapter ? ` · ${row.selected_adapter}` : '';
  return `${type} / ${name}${adapter}`;
}

function settlementLabel(v?: string) {
  switch (v) {
    case 'actual_usage':
      return '按实际 usage';
    case 'estimated_only':
      return '按预估';
    case 'full_refund':
      return '全额退款';
    case 'partial_refund':
      return '退差额';
    case 'extra_charge':
      return '补扣';
    default:
      return v || '-';
  }
}

function pricingSourceLabel(v?: string) {
  switch (v) {
    case 'model_catalog':
      return '模型库';
    case 'system_config':
      return '系统配置';
    case 'default':
      return '默认';
    default:
      return v || '-';
  }
}

function outputLabel(row: ModelGatewayAuditItem) {
  if (row.kind === 'image' || row.kind === 'video') {
    return row.preview_url ? '有产出' : '缺产出';
  }
  if (row.kind === 'chat' || row.kind === 'text') {
    return row.output_snapshot?.output_present ? '有回复' : '缺回复';
  }
  return '-';
}

export default function ModelGatewayAuditPage() {
  const [keyword, setKeyword] = useState('');
  const [auditType, setAuditType] = useState<AuditType>('all');
  const [kind, setKind] = useState<'' | 'image' | 'video' | 'chat' | 'text'>('');
  const [status, setStatus] = useState<'' | '0' | '1' | '2' | '3' | '4'>('');
  const [modelCode, setModelCode] = useState('');
  const [sourceCode, setSourceCode] = useState('');
  const [skipReason, setSkipReason] = useState('');
  const [pricingSource, setPricingSource] = useState('');
  const [settlement, setSettlement] = useState('');
  const [page, setPage] = useState(1);
  const [expanded, setExpanded] = useState<string | null>(null);

  const query = useMemo<ModelGatewayAuditListQuery>(() => ({
    keyword: keyword.trim() || undefined,
    audit_type: auditType,
    kind: kind || undefined,
    status: status === '' ? undefined : (Number(status) as 0 | 1 | 2 | 3 | 4),
    model_code: modelCode.trim() || undefined,
    source_code: sourceCode.trim() || undefined,
    skip_reason: skipReason.trim() || undefined,
    pricing_source: pricingSource || undefined,
    settlement: settlement || undefined,
    page,
    page_size: pageSize,
  }), [auditType, kind, keyword, modelCode, page, pricingSource, settlement, skipReason, sourceCode, status]);

  const audit = useQuery({
    queryKey: ['admin', 'model-gateway', 'audit', query],
    queryFn: () => modelGatewayApi.audit(query),
  });
  const billingProof = useQuery({
    queryKey: ['admin', 'model-gateway', 'audit-billing', expanded],
    queryFn: () => logsApi.generationBilling(expanded || ''),
    enabled: Boolean(expanded),
  });
  const upstreamLogs = useQuery({
    queryKey: ['admin', 'model-gateway', 'audit-upstream', expanded],
    queryFn: () => logsApi.generationUpstream(expanded || ''),
    enabled: Boolean(expanded),
  });

  const rows = audit.data?.list ?? [];
  const total = audit.data?.total ?? 0;
  const lastPage = Math.max(1, Math.ceil(total / pageSize));

  const resetPage = () => setPage(1);

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title">模型审计</h1>
          <p className="page-subtitle">按模型、渠道、跳过原因和扣费结果查询 Model Gateway 任务快照。</p>
        </div>
        <button className="btn btn-outline btn-md" onClick={() => audit.refetch()} disabled={audit.isFetching}>
          <RefreshCw size={16} className={audit.isFetching ? 'animate-spin' : ''} /> 刷新
        </button>
      </header>

      <section className="card card-section grid gap-2 xl:grid-cols-[minmax(260px,1.2fr)_repeat(4,minmax(120px,0.6fr))]">
        <div className="relative">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-tertiary" />
          <input
            className="input h-11 pl-8"
            placeholder="搜索 task_id / 用户 / 模型 / params"
            value={keyword}
            onChange={(e) => {
              setKeyword(e.target.value);
              resetPage();
            }}
          />
        </div>
        <select className="input h-11" value={auditType} onChange={(e) => { setAuditType(e.target.value as typeof auditType); resetPage(); }}>
          <option value="all">全部快照</option>
          <option value="route">路由快照</option>
          <option value="pricing">扣费快照</option>
          <option value="output">有产出/回复</option>
          <option value="output_missing">缺产出/回复</option>
          <option value="video">视频任务快照</option>
          <option value="video_missing">缺视频任务快照</option>
        </select>
        <select className="input h-11" value={kind} onChange={(e) => { setKind(e.target.value as typeof kind); resetPage(); }}>
          <option value="">全部入口</option>
          <option value="chat">文字</option>
          <option value="text">文字兼容</option>
          <option value="image">图片</option>
          <option value="video">视频</option>
        </select>
        <select className="input h-11" value={status} onChange={(e) => { setStatus(e.target.value as typeof status); resetPage(); }}>
          <option value="">全部状态</option>
          <option value="0">待处理</option>
          <option value="1">生成中</option>
          <option value="2">成功</option>
          <option value="3">失败</option>
          <option value="4">已退款</option>
        </select>
        <select className="input h-11" value={pricingSource} onChange={(e) => { setPricingSource(e.target.value); resetPage(); }}>
          <option value="">全部计价</option>
          <option value="model_catalog">模型库</option>
          <option value="system_config">系统配置</option>
          <option value="default">默认</option>
        </select>
      </section>

      <section className="card card-section grid gap-2 xl:grid-cols-4">
        <input
          className="input h-10"
          placeholder="模型编码"
          value={modelCode}
          onChange={(e) => {
            setModelCode(e.target.value);
            resetPage();
          }}
        />
        <input
          className="input h-10"
          placeholder="渠道 / 账号池编码"
          value={sourceCode}
          onChange={(e) => {
            setSourceCode(e.target.value);
            resetPage();
          }}
        />
        <input
          className="input h-10"
          placeholder="跳过原因"
          value={skipReason}
          onChange={(e) => {
            setSkipReason(e.target.value);
            resetPage();
          }}
        />
        <select className="input h-10" value={settlement} onChange={(e) => { setSettlement(e.target.value); resetPage(); }}>
          <option value="">全部结算</option>
          <option value="actual_usage">按实际 usage</option>
          <option value="estimated_only">按预估</option>
          <option value="partial_refund">退差额</option>
          <option value="full_refund">全额退款</option>
          <option value="extra_charge">补扣</option>
        </select>
      </section>

      <section className="card table-wrap overflow-hidden">
        <table className="data-table table-fixed text-small">
          <thead>
            <tr>
              <th className="w-[42px]" />
              <th className="w-[150px]">时间</th>
              <th className="w-[170px]">模型</th>
              <th className="w-[220px]">命中来源</th>
              <th className="w-[160px]">跳过原因</th>
              <th className="w-[145px]">扣费</th>
              <th className="w-[120px]">结算</th>
              <th className="w-[90px]">产出</th>
              <th className="w-[90px]">状态</th>
            </tr>
          </thead>
          <tbody>
            {audit.isLoading && (
              <tr><td colSpan={9} className="py-10 text-center text-text-tertiary">加载中...</td></tr>
            )}
            {!audit.isLoading && rows.length === 0 && (
              <tr><td colSpan={9} className="py-10 text-center text-text-tertiary">暂无审计快照</td></tr>
            )}
            {rows.map((row) => {
              const open = expanded === row.task_id;
              const st = statusLabel(row.status);
              const skipText = row.skip_reasons?.length ? row.skip_reasons.join(' / ') : '-';
              return (
                <Fragment key={row.task_id}>
                  <tr>
                    <td>
                      <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setExpanded(open ? null : row.task_id)}>
                        {open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                      </button>
                    </td>
                    <td className="whitespace-nowrap">
                      <div>{fmtTime(row.created_at)}</div>
                      <div className="truncate text-tiny text-text-tertiary">{row.task_id}</div>
                    </td>
                    <td>
                      <div className="flex min-w-0 items-center gap-1.5">
                        <ClipboardList size={14} className="shrink-0 text-text-tertiary" />
                        <span className="truncate" title={row.model_code}>{row.model_code}</span>
                      </div>
                      <div className="text-tiny text-text-tertiary">{kindLabel(row.kind)} · UID {row.user_id}</div>
                    </td>
                    <td>
                      <div className="truncate" title={sourceLabel(row)}>{sourceLabel(row)}</div>
                      <div className="text-tiny text-text-tertiary">{row.selected_upstream_model || '-'}</div>
                    </td>
                    <td>
                      <div className="truncate" title={skipText}>{skipText}</div>
                      <div className="text-tiny text-text-tertiary">候选 {row.candidate_count ?? 0} · 跳过 {row.skipped_count ?? 0}</div>
                    </td>
                    <td>
                      <div>{fmtPoints(row.actual_points || row.cost_points)}</div>
                      <div className="text-tiny text-text-tertiary">
                        {pricingSourceLabel(row.pricing_source)} / {row.pricing_mode || '-'}
                      </div>
                    </td>
                    <td>{settlementLabel(row.settlement)}</td>
                    <td>
                      {row.preview_url ? (
                        <a className="text-primary hover:underline" href={row.preview_url} target="_blank" rel="noreferrer">查看</a>
                      ) : (
                        <span className={(row.kind === 'image' || row.kind === 'video' || row.kind === 'chat' || row.kind === 'text') && outputLabel(row).startsWith('缺') ? 'text-danger' : 'text-text-tertiary'}>{outputLabel(row)}</span>
                      )}
                    </td>
                    <td><span className={st.cls}>{st.text}</span></td>
                  </tr>
                  {open && (
                    <tr>
                      <td colSpan={9} className="bg-surface-2/60 p-0">
                        <div className="grid gap-3 p-4 xl:grid-cols-6">
                          <SnapshotBlock title="路由快照" value={row.model_gateway_route_snapshot} />
                          <UpstreamAttemptsBlock
                            loading={upstreamLogs.isFetching && expanded === row.task_id}
                            error={upstreamLogs.isError}
                            rows={upstreamLogs.data ?? []}
                          />
                          <SnapshotBlock title="扣费快照" value={row.pricing_snapshot} />
                          <SnapshotBlock title="产出证明" value={row.output_snapshot} />
                          <SnapshotBlock title="视频任务快照" value={row.video_job_snapshot} />
                          <SnapshotBlock
                            title="计费证明"
                            value={
                              billingProof.isFetching && expanded === row.task_id
                                ? '加载中...'
                                : billingProof.isError
                                  ? '读取失败'
                                  : billingProof.data
                            }
                          />
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </section>

      <div className="flex items-center justify-between text-small text-text-tertiary">
        <span>共 {total} 条审计记录</span>
        <div className="inline-flex items-center gap-2">
          <button className="btn btn-outline btn-sm" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>上一页</button>
          <span>{page} / {lastPage}</span>
          <button className="btn btn-outline btn-sm" disabled={page >= lastPage} onClick={() => setPage((p) => Math.min(lastPage, p + 1))}>下一页</button>
        </div>
      </div>
    </div>
  );
}

function UpstreamAttemptsBlock({ rows, loading, error }: { rows: AdminGenerationUpstreamLogItem[]; loading: boolean; error: boolean }) {
  return (
    <div className="rounded-xl border border-border bg-surface-1 p-3">
      <div className="mb-2 text-tiny font-semibold text-text-tertiary">上游尝试</div>
      {loading && <div className="py-8 text-center text-tiny text-text-tertiary">加载中...</div>}
      {!loading && error && <div className="py-8 text-center text-tiny text-danger">读取失败</div>}
      {!loading && !error && rows.length === 0 && <div className="py-8 text-center text-tiny text-text-tertiary">暂无上游日志</div>}
      {!loading && !error && rows.length > 0 && (
        <div className="max-h-72 space-y-2 overflow-auto">
          {rows.map((row, index) => (
            <div key={row.id} className="rounded-lg border border-border bg-surface-2 p-2 text-tiny">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="badge badge-outline">#{index + 1}</span>
                <span className="font-medium text-text-primary">{row.stage}</span>
                <span className="text-text-tertiary">{row.provider}</span>
                {row.account_id ? <span className="text-text-tertiary">账号 #{row.account_id}</span> : null}
              </div>
              <div className="mt-1 flex flex-wrap gap-2 text-text-tertiary">
                {row.status_code > 0 ? <span>HTTP {row.status_code}</span> : <span>未返回 HTTP</span>}
                {row.duration_ms > 0 ? <span>{row.duration_ms}ms</span> : null}
                <span>{fmtTime(row.created_at)}</span>
              </div>
              {row.error && <pre className="mt-2 max-h-20 overflow-auto whitespace-pre-wrap break-words text-danger">{row.error}</pre>}
              {row.meta && <pre className="mt-2 max-h-20 overflow-auto whitespace-pre-wrap break-words text-text-secondary">{prettyJSONString(row.meta)}</pre>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function SnapshotBlock({ title, value }: { title: string; value?: unknown }) {
  const text = typeof value === 'string' ? value : value ? JSON.stringify(value, null, 2) : '无快照';
  return (
    <div className="rounded-xl border border-border bg-surface-1 p-3">
      <div className="mb-2 text-tiny font-semibold text-text-tertiary">{title}</div>
      <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words text-tiny leading-relaxed text-text-secondary">{text}</pre>
    </div>
  );
}

function prettyJSONString(value: string) {
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}
