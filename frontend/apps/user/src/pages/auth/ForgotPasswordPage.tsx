import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { z } from 'zod';
import { zodResolver } from '@hookform/resolvers/zod';
import clsx from 'clsx';

import { useHumanVerification } from '../../components/HumanVerification';
import { ApiError } from '../../lib/api';
import { authApi } from '../../lib/services';
import { toast } from '../../stores/toast';

const schema = z
  .object({
    email: z.string().email('请输入有效邮箱').max(128, '邮箱过长'),
    code: z.string().regex(/^\d{6}$/, '请输入 6 位验证码'),
    password: z
      .string()
      .min(8, '密码至少 8 位')
      .max(64, '密码过长')
      .regex(/[A-Za-z]/, '密码需包含字母')
      .regex(/[0-9]/, '密码需包含数字'),
    confirm: z.string(),
  })
  .refine((d) => d.password === d.confirm, {
    message: '两次密码不一致',
    path: ['confirm'],
  });

type FormValues = z.infer<typeof schema>;

export default function ForgotPasswordPage() {
  const navigate = useNavigate();
  const human = useHumanVerification('auth');
  const {
    register,
    handleSubmit,
    getValues,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { email: '', code: '', password: '', confirm: '' },
  });
  const [sendingCode, setSendingCode] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const emailValue = watch('email');

  useEffect(() => {
    if (cooldown <= 0) return;
    const t = window.setTimeout(() => setCooldown((v) => Math.max(0, v - 1)), 1000);
    return () => window.clearTimeout(t);
  }, [cooldown]);

  const sendCode = async () => {
    const email = getValues('email').trim();
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
      await authApi.sendEmailCode({ email, scene: 'reset_password', turnstile_token: human.token || undefined });
      setCooldown(60);
      toast.success('验证码已发送，请查看邮箱');
      human.reset();
    } catch (err) {
      human.reset();
      toast.error(err instanceof ApiError ? err.message : '验证码发送失败');
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
      await authApi.resetPassword({
        email: values.email,
        code: values.code,
        password: values.password,
        turnstile_token: human.token || undefined,
      });
      toast.success('密码已重置，请重新登录');
      navigate('/login', { replace: true });
    } catch (err) {
      human.reset();
      toast.error(err instanceof ApiError ? err.message : '密码重置失败');
    }
  };

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-h1 text-text-primary">找回密码</h1>
        <p className="text-body text-text-secondary">通过注册邮箱验证码重置登录密码。</p>
      </header>

      <form className="space-y-4" onSubmit={handleSubmit(onSubmit)} noValidate>
        <div className="field">
          <label className="field-label">邮箱</label>
          <input
            className={clsx('input', errors.email && 'input-error')}
            placeholder="你的注册邮箱"
            autoComplete="email"
            {...register('email')}
          />
          {errors.email && <p className="field-error">{errors.email.message}</p>}
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
          <label className="field-label">新密码</label>
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
          <label className="field-label">确认新密码</label>
          <input
            className={clsx('input', errors.confirm && 'input-error')}
            type="password"
            placeholder="再次输入新密码"
            autoComplete="new-password"
            {...register('confirm')}
          />
          {errors.confirm && <p className="field-error">{errors.confirm.message}</p>}
        </div>

        <button className="btn btn-primary btn-lg btn-block" type="submit" disabled={isSubmitting || human.isLoading || !human.isSatisfied}>
          {isSubmitting ? '重置中…' : '重 置 密 码'}
        </button>
      </form>

      <p className="text-small text-text-secondary text-center">
        想起来了？
        <Link to="/login" className="text-klein-500 hover:underline ml-1">返回登录</Link>
      </p>
    </div>
  );
}
