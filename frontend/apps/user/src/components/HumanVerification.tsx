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
const TURNSTILE_WEBVIEW_TIMEOUT_MS = 15000;
const TURNSTILE_DEFAULT_TIMEOUT_MS = 24000;
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
  const timeoutRef = useRef<number | null>(null);
  const solvedRef = useRef(false);
  const isWeChat = useMemo(isWeChatWebView, []);
  const [runtimeError, setRuntimeError] = useState('');

  const clearWatchdog = useCallback(() => {
    if (timeoutRef.current) {
      window.clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
  }, []);

  const startWatchdog = useCallback(() => {
    clearWatchdog();
    solvedRef.current = false;
    timeoutRef.current = window.setTimeout(() => {
      if (solvedRef.current) return;
      onToken('');
      setRuntimeError(turnstileMessage('timeout', undefined, isWeChat));
    }, isWeChat ? TURNSTILE_WEBVIEW_TIMEOUT_MS : TURNSTILE_DEFAULT_TIMEOUT_MS);
  }, [clearWatchdog, isWeChat, onToken]);

  const resetWidget = useCallback(() => {
    onToken('');
    setRuntimeError('');
    if (widgetIdRef.current && window.turnstile) {
      window.turnstile.reset(widgetIdRef.current);
      startWatchdog();
    }
  }, [onToken, startWatchdog]);

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
            solvedRef.current = true;
            clearWatchdog();
            setRuntimeError('');
            onToken(value);
          },
          'expired-callback': () => {
            clearWatchdog();
            onToken('');
            setRuntimeError('人机验证已过期，请重新验证');
          },
          'error-callback': (code?: string) => {
            clearWatchdog();
            onToken('');
            setRuntimeError(turnstileMessage('error', code, isWeChat));
          },
          'timeout-callback': () => {
            clearWatchdog();
            onToken('');
            setRuntimeError(turnstileMessage('timeout', undefined, isWeChat));
          },
          'unsupported-callback': () => {
            clearWatchdog();
            onToken('');
            setRuntimeError(turnstileMessage('unsupported', undefined, isWeChat));
          },
        });
        startWatchdog();
      })
      .catch(() => {
        if (!alive) return;
        clearWatchdog();
        onToken('');
        setRuntimeError(turnstileMessage('script', undefined, isWeChat));
      });
    return () => {
      alive = false;
      clearWatchdog();
      if (widgetIdRef.current && window.turnstile) {
        window.turnstile.remove(widgetIdRef.current);
      }
      widgetIdRef.current = null;
      onToken('');
    };
  }, [action, clearWatchdog, error, isWeChat, onToken, siteKey, startWatchdog]);

  useEffect(() => {
    resetWidget();
  }, [resetSignal, resetWidget]);

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
      {runtimeError && (
        <div className="space-y-2">
          <p className="field-error">{runtimeError}</p>
          <button
            type="button"
            onClick={resetWidget}
            className="inline-flex h-9 items-center rounded-full border border-neutral-200 px-3 text-xs text-neutral-700 transition hover:border-neutral-300 hover:bg-neutral-50"
          >
            重新验证
          </button>
        </div>
      )}
    </div>
  );
}

function isWeChatWebView() {
  if (typeof navigator === 'undefined') return false;
  return /MicroMessenger/i.test(navigator.userAgent);
}

function turnstileMessage(kind: 'error' | 'timeout' | 'unsupported' | 'script', code?: string, isWeChat = false) {
  const suffix = isWeChat ? '微信内置浏览器可能限制了验证组件，请重试；如果仍失败，请从右上角菜单选择在浏览器中打开。' : '请重试，或刷新页面后再登录。';
  if (kind === 'timeout') return `人机验证超时。${suffix}`;
  if (kind === 'unsupported') return `当前浏览器环境不支持人机验证。${suffix}`;
  if (kind === 'script') return `人机验证脚本加载失败。${suffix}`;
  const codeText = code ? `（错误码：${code}）` : '';
  return `人机验证加载失败${codeText}。${suffix}`;
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
