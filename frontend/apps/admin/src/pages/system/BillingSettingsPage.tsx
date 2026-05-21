import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ReceiptText, RefreshCw, Save, Sparkles, Users } from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';

import { ApiError } from '../../lib/api';
import { systemApi } from '../../lib/services';
import type { SystemSettings } from '../../lib/types';
import { toast } from '../../stores/toast';

interface FormState {
  refund_on_failure: boolean;
  free_initial_points: number;
  invite_enabled: boolean;
  invite_new_user_points: number;
  invite_inviter_register_reward: number;
  invite_first_recharge_reward: number;
  invite_lifetime_share_pct: number;
}

const DEFAULT_FORM: FormState = {
  refund_on_failure: true,
  free_initial_points: 0,
  invite_enabled: false,
  invite_new_user_points: 0,
  invite_inviter_register_reward: 0,
  invite_first_recharge_reward: 50,
  invite_lifetime_share_pct: 5,
};

const asBool = (v: unknown, fallback = false) => (v == null ? fallback : Boolean(v));
const asNum = (v: unknown, fallback: number) => {
  const n = Number(v);
  return Number.isFinite(n) ? n : fallback;
};

function fromSettings(s?: SystemSettings): FormState {
  if (!s) return DEFAULT_FORM;
  return {
    refund_on_failure: asBool(s['billing.refund_on_failure'], true),
    free_initial_points: asNum(s['billing.free_initial_points'], 0) / 100,
    invite_enabled: asBool(s['invite.enabled'], false),
    invite_new_user_points: asNum(s['invite.new_user_points'], 0) / 100,
    invite_inviter_register_reward: asNum(s['invite.inviter_register_reward'], 0) / 100,
    invite_first_recharge_reward: asNum(s['invite.first_recharge_reward'], 5000) / 100,
    invite_lifetime_share_pct: asNum(s['invite.lifetime_share_pct'], 5),
  };
}

function toPayload(form: FormState): Partial<SystemSettings> {
  return {
    'billing.refund_on_failure': form.refund_on_failure,
    'billing.free_initial_points': Math.round((Number(form.free_initial_points) || 0) * 100),
    'invite.enabled': form.invite_enabled,
    'invite.new_user_points': Math.round((Number(form.invite_new_user_points) || 0) * 100),
    'invite.inviter_register_reward': Math.round((Number(form.invite_inviter_register_reward) || 0) * 100),
    'invite.first_recharge_reward': Math.round((Number(form.invite_first_recharge_reward) || 0) * 100),
    'invite.lifetime_share_pct': Math.max(0, Math.min(100, Math.round(Number(form.invite_lifetime_share_pct) || 0))),
  };
}

export default function BillingSettingsPage() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ['admin', 'system', 'settings'], queryFn: () => systemApi.get() });
  const [form, setForm] = useState<FormState>(DEFAULT_FORM);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (settings.data) {
      setForm(fromSettings(settings.data));
      setDirty(false);
    }
  }, [settings.data]);

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((old) => ({ ...old, [key]: value }));
    setDirty(true);
  };

  const save = useMutation({
    mutationFn: () => systemApi.update(toPayload(form)),
    onSuccess: () => {
      toast.success('扣费设置已保存');
      setDirty(false);
      qc.invalidateQueries({ queryKey: ['admin', 'system'] });
    },
    onError: (e: ApiError | Error) => toast.error(e.message),
  });

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title">扣费设置</h1>
          <p className="page-subtitle">维护通用扣费规则。模型单价与模型映射请在“模型价格”维护，充值商品请在“充值套餐”维护。</p>
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
        <div className="grid gap-4 lg:grid-cols-[1.1fr_0.9fr]">
          <section className="card card-section space-y-5">
            <SectionTitle icon={<ReceiptText size={18} />} title="失败退款" desc="生成任务失败时是否自动返还本次预扣积分。" />
            <div className="rounded-md border border-border bg-surface-2 p-4 flex items-center justify-between gap-4">
              <div>
                <div className="text-base font-semibold text-text-primary">失败自动退款</div>
                <div className="text-small text-text-tertiary mt-1">建议开启。关闭后失败任务不会返还已扣积分。</div>
              </div>
              <Switch checked={form.refund_on_failure} onChange={(v) => set('refund_on_failure', v)} />
            </div>
          </section>

          <section className="card card-section space-y-5">
            <SectionTitle icon={<Sparkles size={18} />} title="注册赠送" desc="新用户注册成功后自动赠送的初始积分。" />
            <label className="field">
              <span className="field-label">赠送积分（点）</span>
              <input
                className="input text-[26px] font-semibold tabular-nums"
                type="number"
                min={0}
                value={form.free_initial_points}
                onChange={(e) => set('free_initial_points', Number(e.target.value) || 0)}
              />
            </label>
            <div className="rounded-md border border-border bg-surface-2 p-3 text-small text-text-tertiary">
              保存后会以系统内部积分单位入库，页面填写仍然按“点”显示。
            </div>
          </section>

          <section className="card card-section space-y-5 lg:col-span-2">
            <SectionTitle icon={<Users size={18} />} title="邀请奖励" desc="按邀请码绑定关系，控制注册奖励、好友首充奖励和长期充值分润。" />
            <div className="rounded-md border border-border bg-surface-2 p-4 flex items-center justify-between gap-4">
              <div>
                <div className="text-base font-semibold text-text-primary">启用邀请奖励</div>
                <div className="text-small text-text-tertiary mt-1">关闭时仍会绑定邀请关系，但不会自动发放邀请奖励。</div>
              </div>
              <Switch checked={form.invite_enabled} onChange={(v) => set('invite_enabled', v)} />
            </div>
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <NumberField
                label="被邀请人注册赠点"
                value={form.invite_new_user_points}
                onChange={(v) => set('invite_new_user_points', v)}
              />
              <NumberField
                label="邀请人注册奖励"
                value={form.invite_inviter_register_reward}
                onChange={(v) => set('invite_inviter_register_reward', v)}
              />
              <NumberField
                label="好友首充固定奖励"
                value={form.invite_first_recharge_reward}
                onChange={(v) => set('invite_first_recharge_reward', v)}
              />
              <NumberField
                label="好友充值分润比例 %"
                value={form.invite_lifetime_share_pct}
                max={100}
                onChange={(v) => set('invite_lifetime_share_pct', v)}
              />
            </div>
            <div className="rounded-md border border-border bg-surface-2 p-3 text-small text-text-tertiary">
              首充奖励只在好友第一笔充值成功时发放一次；分润按实际到账点数乘以比例取整，所有奖励都会写入钱包流水。
            </div>
          </section>
        </div>
      )}
    </div>
  );
}

function SectionTitle({ icon, title, desc }: { icon: ReactNode; title: string; desc: string }) {
  return (
    <header className="flex items-start gap-3">
      <span className="grid place-items-center w-9 h-9 rounded-md bg-info-soft text-klein-500">{icon}</span>
      <div>
        <h2 className="text-h5 font-semibold text-text-primary">{title}</h2>
        <p className="text-small text-text-tertiary mt-0.5">{desc}</p>
      </div>
    </header>
  );
}

function NumberField({ label, value, max, onChange }: { label: string; value: number; max?: number; onChange: (v: number) => void }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      <input
        className="input text-[22px] font-semibold tabular-nums"
        type="number"
        min={0}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
      />
    </label>
  );
}

function Switch({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button type="button" role="switch" aria-checked={checked} onClick={() => onChange(!checked)} className={'relative inline-flex h-8 w-14 shrink-0 items-center rounded-full transition ' + (checked ? 'bg-klein-500' : 'bg-surface-3')}>
      <span className={'inline-block h-7 w-7 rounded-full bg-white shadow transition transform ' + (checked ? 'translate-x-6' : 'translate-x-0.5')} />
    </button>
  );
}
