import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import QRCode from 'qrcode';
import { CheckCircle2, Copy, Gift, QrCode, RefreshCw, Sparkles, Wallet } from 'lucide-react';
import clsx from 'clsx';

import { ApiError } from '../../lib/api';
import { fmtBiz, fmtPoints, fmtTime, pointsClass } from '../../lib/format';
import { billingApi } from '../../lib/services';
import type { RechargeOrder, RechargePackage } from '../../lib/types';
import { useAuthStore } from '../../stores/auth';
import { toast } from '../../stores/toast';

export default function BillingPage() {
  const me = useAuthStore((s) => s.me);
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const qc = useQueryClient();

  const [page, setPage] = useState(1);
  const [code, setCode] = useState('');
  const [selectedPackageID, setSelectedPackageID] = useState('');
  const [activeOrderNo, setActiveOrderNo] = useState<string | null>(null);
  const [settledOrderNo, setSettledOrderNo] = useState<string | null>(null);
  const [qrDataUrl, setQrDataUrl] = useState('');

  const packagesQ = useQuery({
    queryKey: ['billing.recharge.packages'],
    queryFn: billingApi.packages,
  });

  const logsQ = useQuery({
    queryKey: ['billing.logs', page],
    queryFn: () => billingApi.logs(page, 20),
  });

  const orderQ = useQuery({
    queryKey: ['billing.recharge.order', activeOrderNo],
    queryFn: () => billingApi.rechargeOrder(activeOrderNo || ''),
    enabled: !!activeOrderNo,
    refetchInterval: (q) => (q.state.data?.status === 0 ? 3000 : false),
  });

  const createOrder = useMutation({
    mutationFn: (pkg: RechargePackage) => billingApi.createRechargeOrder({ package_id: pkg.id, channel: 'alipay' }),
    onSuccess: (order) => {
      setActiveOrderNo(order.order_no);
      setSettledOrderNo(null);
      toast.success('支付宝订单已创建');
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : '下单失败'),
  });

  const redeemMut = useMutation({
    mutationFn: () => billingApi.redeemCDK(code.trim()),
    onSuccess: async (resp) => {
      toast.success(`兑换成功 +${fmtPoints(resp.points)} 点`);
      setCode('');
      await refreshMe();
      await qc.invalidateQueries({ queryKey: ['billing.logs'] });
      setPage(1);
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : '兑换失败'),
  });

  const packages = packagesQ.data ?? [];
  const selectedPackage = useMemo(
    () => packages.find((p) => p.id === selectedPackageID) ?? packages[0],
    [packages, selectedPackageID],
  );
  const activeOrder = orderQ.data ?? (createOrder.data?.order_no === activeOrderNo ? createOrder.data : undefined);

  useEffect(() => {
    if (!selectedPackageID && packages[0]) {
      setSelectedPackageID(packages[0].id);
    }
  }, [packages, selectedPackageID]);

  useEffect(() => {
    let cancelled = false;
    setQrDataUrl('');
    if (!activeOrder?.qr_code) return;
    QRCode.toDataURL(activeOrder.qr_code, {
      errorCorrectionLevel: 'M',
      margin: 1,
      width: 248,
      color: { dark: '#111111', light: '#ffffff' },
    }).then((url) => {
      if (!cancelled) setQrDataUrl(url);
    }).catch(() => {
      if (!cancelled) setQrDataUrl('');
    });
    return () => {
      cancelled = true;
    };
  }, [activeOrder?.qr_code]);

  useEffect(() => {
    if (!activeOrder || activeOrder.status !== 1 || settledOrderNo === activeOrder.order_no) return;
    setSettledOrderNo(activeOrder.order_no);
    toast.success('支付成功，点数已到账');
    refreshMe();
    qc.invalidateQueries({ queryKey: ['billing.logs'] });
  }, [activeOrder, qc, refreshMe, settledOrderNo]);

  const stats = [
    { label: '可用点数', value: fmtPoints(me?.points ?? 0), accent: true },
    { label: '冻结点数', value: fmtPoints(me?.frozen_points ?? 0) },
    { label: '当前套餐', value: me?.plan_code?.toUpperCase() ?? 'FREE' },
    { label: '邀请码', value: me?.invite_code ?? '—' },
  ];

  const logs = logsQ.data?.list ?? [];
  const total = logsQ.data?.total ?? 0;
  const pageSize = logsQ.data?.page_size ?? 20;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="page">
      <header className="page-header">
        <div>
          <h1 className="page-title">余额明细</h1>
          <p className="page-subtitle">点数变动、兑换码、支付宝充值都在这里管理。</p>
        </div>
      </header>

      <div className="stat-grid mb-6">
        {stats.map((s) => (
          <div key={s.label} className={`stat-tile ${s.accent ? 'stat-tile-accent' : ''}`}>
            <p className="stat-label">{s.label}</p>
            <p className="stat-value">{s.value}</p>
          </div>
        ))}
      </div>

      <section className="grid gap-4 mb-6 xl:grid-cols-[minmax(0,1fr)_380px]">
        <div className="card card-section">
          <header className="section-header mb-4">
            <span className="section-title">
              <Sparkles size={18} className="text-klein-500" />
              支付宝充值
            </span>
            <button className="btn btn-outline btn-sm" type="button" onClick={() => packagesQ.refetch()} disabled={packagesQ.isFetching}>
              <RefreshCw size={14} className={packagesQ.isFetching ? 'animate-spin' : ''} />
              刷新套餐
            </button>
          </header>

          {packagesQ.isLoading && <p className="text-small text-text-tertiary py-8 text-center">正在读取充值套餐...</p>}
          {!packagesQ.isLoading && packages.length === 0 && (
            <div className="empty-state">
              <span className="empty-state-icon">
                <Wallet size={22} />
              </span>
              <p className="empty-state-title">暂无可用套餐</p>
              <p className="empty-state-desc">请先在管理后台配置并启用充值套餐。</p>
            </div>
          )}
          {packages.length > 0 && (
            <>
              <div className="grid gap-3 md:grid-cols-2">
                {packages.map((pkg) => (
                  <button
                    key={pkg.id}
                    type="button"
                    onClick={() => setSelectedPackageID(pkg.id)}
                    className={clsx(
                      'rounded-lg border p-4 text-left transition hover:-translate-y-0.5 hover:shadow-md',
                      selectedPackage?.id === pkg.id
                        ? 'border-klein-500 bg-klein-50 shadow-sm'
                        : 'border-border bg-white',
                    )}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <p className="text-h5 text-text-primary">{pkg.name}</p>
                        <p className="mt-1 text-small text-text-tertiary">{pkg.remark || '支付宝扫码支付，成功后自动到账。'}</p>
                      </div>
                      {pkg.badge && <span className="badge badge-klein">{pkg.badge}</span>}
                    </div>
                    <div className="mt-4 flex items-end justify-between gap-3">
                      <p className="text-[28px] font-semibold text-text-primary">{fmtMoney(pkg.amount)}</p>
                      <p className="text-small text-text-secondary">
                        {fmtPoints(pkg.points)} 点
                        {pkg.bonus_points > 0 ? ` + 赠 ${fmtPoints(pkg.bonus_points)}` : ''}
                      </p>
                    </div>
                  </button>
                ))}
              </div>

              <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between rounded-lg border border-border bg-surface-2 p-4">
                <div>
                  <p className="text-small text-text-tertiary">当前选择</p>
                  <p className="mt-1 font-semibold text-text-primary">
                    {selectedPackage?.name ?? '请选择套餐'} · {selectedPackage ? fmtMoney(selectedPackage.amount) : '—'}
                  </p>
                </div>
                <button
                  className="btn btn-primary btn-lg"
                  type="button"
                  disabled={!selectedPackage || createOrder.isPending}
                  onClick={() => selectedPackage && createOrder.mutate(selectedPackage)}
                >
                  <QrCode size={18} />
                  {createOrder.isPending ? '生成订单中...' : '支付宝扫码充值'}
                </button>
              </div>
            </>
          )}
        </div>

        <PaymentPanel order={activeOrder} qrDataUrl={qrDataUrl} isFetching={orderQ.isFetching} />
      </section>

      <section className="grid gap-4 mb-6 lg:grid-cols-2">
        <div className="card card-section">
          <header className="section-header mb-3">
            <span className="section-title">
              <Gift size={18} className="text-klein-500" />
              兑换码 CDK
            </span>
          </header>
          <p className="text-small text-text-secondary mb-4 leading-loose">
            输入活动码或邀请码即可立刻到账点数；同一个兑换码不可重复使用。
          </p>
          <div className="flex flex-col sm:flex-row gap-2">
            <input
              className="input"
              placeholder="例如：GPT2API-2026-WELCOME"
              value={code}
              onChange={(e) => setCode(e.target.value.toUpperCase())}
              maxLength={32}
            />
            <button
              className="btn btn-primary btn-lg whitespace-nowrap"
              disabled={code.trim().length < 4 || redeemMut.isPending}
              onClick={() => redeemMut.mutate()}
              type="button"
            >
              {redeemMut.isPending ? '兑换中…' : '立即兑换'}
            </button>
          </div>
        </div>

        <div className="card-tinted card-section">
          <header className="section-header mb-3">
            <span className="section-title">
              <CheckCircle2 size={18} className="text-klein-500" />
              支付说明
            </span>
          </header>
          <p className="text-small text-text-secondary leading-loose">
            支付宝扫码完成后，页面会自动轮询订单状态；支付成功后点数会即时入账，并写入最近交易流水。
          </p>
          <p className="mt-3 text-small text-text-tertiary leading-loose">
            冻结点数不会按时间自动释放，它只会在任务成功结算时转为已消费，或在任务失败、超时后自动退款解冻。
          </p>
        </div>
      </section>

      <section className="card overflow-hidden">
        <div className="px-5 py-3.5 border-b border-border flex items-center justify-between">
          <span className="section-title">
            <Wallet size={16} className="text-text-tertiary" />
            最近交易
          </span>
          <span className="text-small text-text-tertiary">共 {total} 条</span>
        </div>
        <div className="divide-y divide-border">
          {logsQ.isLoading && (
            <p className="px-5 py-10 text-center text-text-tertiary text-small">加载中...</p>
          )}
          {!logsQ.isLoading && logs.length === 0 && (
            <div className="empty-state">
              <span className="empty-state-icon">
                <Wallet size={22} />
              </span>
              <p className="empty-state-title">暂无流水记录</p>
              <p className="empty-state-desc">兑换 CDK、生成图片或视频后，相关账单会在此呈现。</p>
            </div>
          )}
          {logs.map((l) => (
            <div key={l.id} className="list-row">
              <div className="min-w-0">
                <p className="font-medium text-text-primary truncate">
                  {fmtBiz(l.biz_type)}
                  {l.remark ? ` · ${l.remark}` : ''}
                </p>
                <p className="text-small text-text-tertiary mt-0.5">{fmtTime(l.created_at)}</p>
              </div>
              <p className={`font-bold whitespace-nowrap ${pointsClass(l.direction)}`}>
                {l.direction > 0 ? '+' : '-'} {fmtPoints(Math.abs(l.points))} 点
              </p>
            </div>
          ))}
        </div>
        <div className="flex items-center justify-between gap-3 border-t border-border px-5 py-4 text-sm">
          <span className="text-text-tertiary">
            第 {page} / {totalPages} 页，共 {total} 条
          </span>
          <div className="flex items-center gap-2">
            <button
              className="btn btn-outline btn-md"
              disabled={page <= 1 || logsQ.isFetching}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              type="button"
            >
              上一页
            </button>
            <button
              className="btn btn-outline btn-md"
              disabled={page >= totalPages || logsQ.isFetching}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              type="button"
            >
              下一页
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}

function PaymentPanel({ order, qrDataUrl, isFetching }: { order?: RechargeOrder; qrDataUrl: string; isFetching: boolean }) {
  if (!order) {
    return (
      <aside className="card card-section flex min-h-[360px] flex-col items-center justify-center text-center">
        <span className="grid h-14 w-14 place-items-center rounded-full bg-surface-2 text-text-tertiary">
          <QrCode size={24} />
        </span>
        <p className="mt-4 text-h5 text-text-primary">等待创建支付订单</p>
        <p className="mt-2 max-w-[260px] text-small leading-loose text-text-tertiary">
          选择套餐后生成支付宝二维码，扫码完成后点数会自动到账。
        </p>
      </aside>
    );
  }
  const paid = order.status === 1;
  return (
    <aside className="card card-section">
      <header className="section-header mb-4">
        <span className="section-title">
          <QrCode size={18} className="text-klein-500" />
          支付订单
        </span>
        <span className={clsx('badge', paid ? 'badge-success' : 'badge-klein')}>
          {statusText(order.status)}
        </span>
      </header>

      <div className="grid place-items-center rounded-xl border border-border bg-white p-4">
        {paid ? (
          <div className="grid min-h-[248px] place-items-center text-center text-success">
            <div>
              <CheckCircle2 size={54} className="mx-auto" />
              <p className="mt-3 font-semibold">支付成功</p>
            </div>
          </div>
        ) : qrDataUrl ? (
          <img src={qrDataUrl} alt="支付宝支付二维码" className="h-[248px] w-[248px]" />
        ) : (
          <div className="grid min-h-[248px] place-items-center text-small text-text-tertiary">二维码生成中...</div>
        )}
      </div>

      <div className="mt-4 space-y-2 text-small">
        <InfoRow label="订单号" value={order.order_no} copyable />
        <InfoRow label="支付金额" value={fmtMoney(order.amount)} />
        <InfoRow label="到账点数" value={`${fmtPoints(order.total_points)} 点`} />
        <InfoRow label="创建时间" value={fmtTime(order.created_at)} />
      </div>

      {!paid && (
        <p className="mt-4 text-small text-text-tertiary">
          {isFetching ? '正在确认支付状态...' : '支付后会自动刷新状态，也可以稍等几秒查看最近交易。'}
        </p>
      )}
    </aside>
  );
}

function InfoRow({ label, value, copyable = false }: { label: string; value: string; copyable?: boolean }) {
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success('已复制');
    } catch {
      toast.error('复制失败');
    }
  };
  return (
    <div className="flex items-center justify-between gap-3 rounded-md bg-surface-2 px-3 py-2">
      <span className="text-text-tertiary">{label}</span>
      <span className="flex min-w-0 items-center gap-2 font-medium text-text-primary">
        <span className="truncate">{value}</span>
        {copyable && (
          <button type="button" className="text-text-tertiary transition hover:text-text-primary" onClick={copy} aria-label="复制">
            <Copy size={14} />
          </button>
        )}
      </span>
    </div>
  );
}

function statusText(status: number) {
  switch (status) {
    case 0:
      return '待支付';
    case 1:
      return '已支付';
    case 2:
      return '已过期';
    case 3:
      return '已取消';
    case 4:
      return '失败';
    default:
      return '处理中';
  }
}

const moneyFmt = new Intl.NumberFormat('zh-CN', {
  style: 'currency',
  currency: 'CNY',
  minimumFractionDigits: 2,
});

function fmtMoney(cents: number | undefined | null) {
  return moneyFmt.format((cents ?? 0) / 100);
}
