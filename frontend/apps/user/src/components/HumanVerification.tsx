import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';

import { authApi } from '../lib/services';
import type { HumanVerificationAction } from '../lib/types';

declare global {
  interface Window {
    turnstile?: {
      render: (
        container: HTMLElement,
        options: Record<string, unknown>,
      ) => string;
      reset: (widgetId?: string) => void;
      remove: (widgetId: string) => void;
    };
  }
}

const TURNSTILE_SCRIPT_ID = 'cf-turnstile-script';
let turnstileScriptPromise: Promise<void> | null = null;

export function useHumanVerification(action: HumanVerificationAction): {
  element: ReactNode;
  isLoading: boolean;
  isRequired: boolean;
  isSatisfied: boolean;
  token: string;
  reset: () => void;
} {
  const [enabled, setEnabled] = useState(false);
  const [siteKey, setSiteKey] = useState('');
  const [configLoading, setConfigLoading] = useState(true);
  const [configError, setConfigError] = useState('');
  const [token, setToken] = useState('');
  const [resetSignal, setResetSignal] = useState(0);

  useEffect(() => {
    let alive = true;
    setConfigLoading(true);
    authApi.humanVerificationConfig()
      .then((cfg) => {
        if (!alive) return;
        const turnstile = cfg?.turnstile;
        setEnabled(Boolean(turnstile?.enabled));
        setSiteKey(turnstile?.site_key ?? '');
        setConfigError('');
      })
      .catch(() => {
        if (!alive) return;
        setEnabled(true);
        setSiteKey('');
        setConfigError('人机验证配置读取失败，请刷新页面');
      })
      .finally(() => {
        if (alive) setConfigLoading(false);
      });
    return () => {
      alive = false;
    };
  }, []);

  const reset = useCallback(() => {
    setToken('');
    setResetSignal((v) => v + 1);
  }, []);

  const element = useMemo(() => {
    if (configLoading || !enabled) return null;
    return (
      <HumanVerificationWidget
        action={action}
        error={configError}
        resetSignal={resetSignal}
        siteKey={siteKey}
        onToken={setToken}
      />
    );
  }, [action, configError, configLoading, enabled, resetSignal, siteKey]);

  return {
    element,
    isLoading: configLoading,
    isRequired: enabled,
    isSatisfied: !enabled || Boolean(token),
    token,
    reset,
  };
}

function HumanVerificationWidget({
  action,
  error,
  resetSignal,
  siteKey,
  onToken,
}: {
  action: HumanVerificationAction;
  error: string;
  resetSignal: number;
  siteKey: string;
  onToken: (token: string) => void;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const widgetIdRef = useRef<string | null>(null);
  const [runtimeError, setRuntimeError] = useState('');

  useEffect(() => {
    if (!siteKey || error) return;
    let alive = true;
    setRuntimeError('');
    loadTurnstileScript()
      .then(() => {
        if (!alive || !containerRef.current || !window.turnstile || widgetIdRef.current) return;
        widgetIdRef.current = window.turnstile.render(containerRef.current, {
          sitekey: siteKey,
          action,
          theme: 'auto',
          size: 'flexible',
          callback: (value: string) => {
            setRuntimeError('');
            onToken(value);
          },
          'expired-callback': () => {
            onToken('');
            setRuntimeError('人机验证已过期，请重新验证');
          },
          'error-callback': () => {
            onToken('');
            setRuntimeError('人机验证加载失败，请刷新页面');
          },
        });
      })
      .catch(() => {
        if (!alive) return;
        onToken('');
        setRuntimeError('人机验证脚本加载失败，请检查网络');
      });
    return () => {
      alive = false;
      if (widgetIdRef.current && window.turnstile) {
        window.turnstile.remove(widgetIdRef.current);
      }
      widgetIdRef.current = null;
      onToken('');
    };
  }, [action, error, onToken, siteKey]);

  useEffect(() => {
    if (!widgetIdRef.current || !window.turnstile) return;
    window.turnstile.reset(widgetIdRef.current);
    onToken('');
    setRuntimeError('');
  }, [onToken, resetSignal]);

  if (error || !siteKey) {
    return (
      <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-small text-amber-800">
        {error || '人机验证未配置，请联系管理员'}
      </div>
    );
  }

  return (
    <div className="space-y-2">
      <div ref={containerRef} className="min-h-[65px] overflow-hidden rounded-2xl" />
      {runtimeError && <p className="field-error">{runtimeError}</p>}
    </div>
  );
}

function loadTurnstileScript() {
  if (window.turnstile) return Promise.resolve();
  if (turnstileScriptPromise) return turnstileScriptPromise;

  turnstileScriptPromise = new Promise<void>((resolve, reject) => {
    const existing = document.getElementById(TURNSTILE_SCRIPT_ID) as HTMLScriptElement | null;
    if (existing) {
      existing.addEventListener('load', () => resolve(), { once: true });
      existing.addEventListener('error', () => reject(new Error('turnstile script failed')), { once: true });
      return;
    }
    const script = document.createElement('script');
    script.id = TURNSTILE_SCRIPT_ID;
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
    script.async = true;
    script.defer = true;
    script.onload = () => resolve();
    script.onerror = () => {
      turnstileScriptPromise = null;
      reject(new Error('turnstile script failed'));
    };
    document.head.appendChild(script);
  });

  return turnstileScriptPromise;
}
