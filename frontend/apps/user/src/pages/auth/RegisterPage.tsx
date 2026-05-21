import { useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { z } from 'zod';
import { zodResolver } from '@hookform/resolvers/zod';
import clsx from 'clsx';

import { useHumanVerification } from '../../components/HumanVerification';
import { ApiError } from '../../lib/api';
import { authApi } from '../../lib/services';
import { useAuthStore } from '../../stores/auth';
import { toast } from '../../stores/toast';

const schema = z
  .object({
    account: z.string().email('请输入有效邮箱').max(128, '邮箱过长'),
    code: z.string().regex(/^\d{6}$/, '请输入 6 位验证码'),
    password: z
      .string()
      .min(8, '密码至少 8 位')
      .max(64, '密码过长')
      .regex(/[A-Za-z]/, '密码需包含字母')
      .regex(/[0-9]/, '密码需包含数字'),
    confirm: z.string(),
    invite_code: z.string().max(16).optional().or(z.literal('')),
  })
  .refine((d) => d.password === d.confirm, {
    message: '两次密码不一致',
    path: ['confirm'],
  });

type FormValues = z.infer<typeof schema>;

export default function RegisterPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const inviteFromUrl = (searchParams.get('invite') || searchParams.get('invite_code') || '').trim().toUpperCase();
  const setToken = useAuthStore((s) => s.setToken);
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const human = useHumanVerification('auth');

  const {
    register,
    handleSubmit,
    getValues,
    setValue,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { account: '', code: '', password: '', confirm: '', invite_code: inviteFromUrl },
  });
  const [sendingCode, setSendingCode] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const emailValue = watch('account');

  useEffect(() => {
    if (cooldown <= 0) return;
    const t = window.setTimeout(() => setCooldown((v) => Math.max(0, v - 1)), 1000);
    return () => window.clearTimeout(t);
  }, [cooldown]);

  useEffect(() => {
    if (inviteFromUrl) {
      setValue('invite_code', inviteFromUrl, { shouldDirty: true });
    }
  }, [inviteFromUrl, setValue]);

  const sendCode = async () => {
    const email = getValues('account').trim();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      toast.error('请先填写有效邮箱');
      return;
    }
    if (human.isRequired && !human.token) {
      toast.error('请先完成人机验证');
      return;
    }
    try {
      setSendingCode(true);
      await authApi.sendEmailCode({ email, scene: 'register', turnstile_token: human.token || undefined });
      setCooldown(60);
      toast.success('验证码已发送，请查看邮箱');
      human.reset();
    } catch (err) {
      human.reset();
      const msg = err instanceof ApiError ? err.message : '验证码发送失败';
      toast.error(msg);
    } finally {
      setSendingCode(false);
    }
  };

  const onSubmit = async (values: FormValues) => {
    if (human.isRequired && !human.token) {
      toast.error('请先完成人机验证');
      return;
    }
    try {
      const resp = await authApi.register({
        account: values.account,
        password: values.password,
        code: values.code,
        invite_code: values.invite_code || undefined,
        turnstile_token: human.token || undefined,
      });
      setToken(resp.token);
      await refreshMe();
      toast.success('注册成功，已为你登录');
      navigate('/create/image', { replace: true });
    } catch (err) {
      human.reset();
      const msg = err instanceof ApiError ? err.message : '注册失败，请重试';
      toast.error(msg);
    }
  };

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-h1 text-text-primary">注册账号</h1>
        <p className="text-body text-text-secondary">创建账号开启你的 AIGC 之旅</p>
      </header>

      <form className="space-y-4" onSubmit={handleSubmit(onSubmit)} noValidate>
        <div className="field">
          <label className="field-label">邮箱</label>
          <input
            className={clsx('input', errors.account && 'input-error')}
            placeholder="用于登录和找回密码"
            autoComplete="username"
            {...register('account')}
          />
          {errors.account && <p className="field-error">{errors.account.message}</p>}
        </div>

        <div className="field">
          <label className="field-label">邮箱验证码</label>
          <div className="flex gap-2">
            <input
              className={clsx('input', errors.code && 'input-error')}
              placeholder="6 位验证码"
              inputMode="numeric"
              autoComplete="one-time-code"
              {...register('code')}
            />
            <button className="btn btn-outline btn-lg shrink-0" type="button" disabled={sendingCode || cooldown > 0 || !emailValue || human.isLoading || !human.isSatisfied} onClick={sendCode}>
              {cooldown > 0 ? `${cooldown}s` : sendingCode ? '发送中…' : '获取验证码'}
            </button>
          </div>
          {errors.code && <p className="field-error">{errors.code.message}</p>}
        </div>

        {human.element}

        <div className="field">
          <label className="field-label">设置密码</label>
          <input
            className={clsx('input', errors.password && 'input-error')}
            type="password"
            placeholder="≥ 8 位，含字母与数字"
            autoComplete="new-password"
            {...register('password')}
          />
          {errors.password && <p className="field-error">{errors.password.message}</p>}
        </div>

        <div className="field">
          <label className="field-label">确认密码</label>
          <input
            className={clsx('input', errors.confirm && 'input-error')}
            type="password"
            placeholder="再次输入密码"
            autoComplete="new-password"
            {...register('confirm')}
          />
          {errors.confirm && <p className="field-error">{errors.confirm.message}</p>}
        </div>

        <div className="field">
          <label className="field-label">邀请码（选填）</label>
          <input className="input" placeholder="填写以获得额外点数" {...register('invite_code')} />
          <p className="field-hint">{inviteFromUrl ? '已从邀请链接自动填入，可直接注册。' : '使用邀请码注册可获得额外赠点。'}</p>
        </div>

        <button className="btn btn-primary btn-lg btn-block" type="submit" disabled={isSubmitting || human.isLoading || !human.isSatisfied}>
          {isSubmitting ? '创建中…' : '创 建 账 号'}
        </button>

        <p className="text-small text-text-tertiary text-center">
          注册即代表同意 <a className="text-klein-500">服务条款</a> 与 <a className="text-klein-500">隐私政策</a>
        </p>
      </form>

      <p className="text-small text-text-secondary text-center">
        已有账号？
        <Link to="/login" className="text-klein-500 hover:underline ml-1">立即登录</Link>
      </p>
    </div>
  );
}
