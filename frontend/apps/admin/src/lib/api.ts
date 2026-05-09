// 后台管理 axios 客户端。与用户端独立：
//   - baseURL: /admin/api/v1
//   - token 存 localStorage(key: klein:admin:token)
//   - 401 → 清 token，跳转 /login
import axios, {
  AxiosError,
  type AxiosInstance,
  type AxiosRequestConfig,
  type InternalAxiosRequestConfig,
} from 'axios';

import type { ApiBody, AdminLoginResp } from './types';

const TOKEN_KEY = 'klein:admin:token';

export interface StoredToken {
  access: string;
  refresh: string;
  type: string;
  accessExpireAt: number;
  refreshExpireAt: number;
}

export function loadToken(): StoredToken | null {
  try {
    const raw = localStorage.getItem(TOKEN_KEY);
    if (!raw) return null;
    const tok = JSON.parse(raw) as StoredToken;
    if (!tok?.access || tok.accessExpireAt <= Date.now()) {
      clearToken();
      return null;
    }
    return tok;
  } catch {
    clearToken();
    return null;
  }
}

export function saveToken(tok: AdminLoginResp['token']): StoredToken {
  const now = Date.now();
  const v: StoredToken = {
    access: tok.access_token,
    refresh: tok.refresh_token,
    type: tok.token_type || 'Bearer',
    accessExpireAt: now + tok.access_expire_in * 1000,
    refreshExpireAt: now + tok.refresh_expire_in * 1000,
  };
  localStorage.setItem(TOKEN_KEY, JSON.stringify(v));
  return v;
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  code: number;
  httpStatus?: number;
  traceId?: string;
  rawMessage: string;
  constructor(msg: string, code: number, opts?: { httpStatus?: number; traceId?: string }) {
    const suffix = opts?.traceId ? `（错误编号：${opts.traceId}）` : '';
    super(`${msg}${suffix}`);
    this.rawMessage = msg;
    this.code = code;
    this.httpStatus = opts?.httpStatus;
    this.traceId = opts?.traceId;
  }
}

function headerValue(headers: unknown, key: string): string | undefined {
  if (!headers || typeof headers !== 'object') return undefined;
  const maybeGet = (headers as { get?: (name: string) => unknown }).get;
  if (typeof maybeGet === 'function') {
    const v = maybeGet.call(headers, key);
    return typeof v === 'string' && v.trim() ? v : undefined;
  }
  const plain = headers as Record<string, unknown>;
  const v = plain[key] ?? plain[key.toLowerCase()] ?? plain[key.toUpperCase()];
  return typeof v === 'string' && v.trim() ? v : undefined;
}

function parseApiBody(data: unknown): Partial<ApiBody<unknown>> | null {
  if (!data) return null;
  if (typeof data === 'object') return data as Partial<ApiBody<unknown>>;
  if (typeof data !== 'string') return null;
  try {
    const parsed = JSON.parse(data);
    return parsed && typeof parsed === 'object' ? (parsed as Partial<ApiBody<unknown>>) : null;
  } catch {
    return null;
  }
}

function fallbackHttpMessage(status?: number, message?: string): string {
  if (status === 403) {
    return '访问被拒绝，可能是账号权限不足、后台访问白名单或部署网关拦截';
  }
  if (status === 401) return '登录已过期，请重新登录';
  if (status === 429) return '操作过于频繁，请稍后再试';
  if (status && status >= 500) return '服务暂时不可用，请稍后再试';
  return message || '网络异常';
}

const baseURL =
  (import.meta.env.VITE_ADMIN_BASE_URL as string | undefined)?.replace(/\/+$/, '') ??
  '/admin/api/v1';

export const api: AxiosInstance = axios.create({
  baseURL,
  timeout: 30_000,
  headers: { Accept: 'application/json' },
});

api.interceptors.request.use((cfg: InternalAxiosRequestConfig) => {
  const tok = loadToken();
  if (tok && cfg.headers) {
    cfg.headers.set?.('Authorization', `${tok.type} ${tok.access}`);
  }
  return cfg;
});

let unauthorizedHandler: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  unauthorizedHandler = fn;
}

api.interceptors.response.use(
  (res) => {
    const body = res.data as ApiBody<unknown>;
    const traceId = body?.trace_id || headerValue(res.headers, 'x-request-id');
    if (body && typeof body === 'object' && 'code' in body && body.code !== 0) {
      throw new ApiError(body.msg || '请求失败', body.code, {
        httpStatus: res.status,
        traceId,
      });
    }
    return res;
  },
  (err: AxiosError<unknown>) => {
    const status = err.response?.status;
    const body = parseApiBody(err.response?.data);
    const traceId =
      (typeof body?.trace_id === 'string' && body.trace_id) ||
      headerValue(err.response?.headers, 'x-request-id');
    const msg =
      (typeof body?.msg === 'string' && body.msg.trim()) ||
      fallbackHttpMessage(status, err.message);
    const code = typeof body?.code === 'number' ? body.code : status ?? -1;
    if (status === 401) {
      clearToken();
      unauthorizedHandler?.();
    }
    return Promise.reject(
      new ApiError(msg, code, { httpStatus: status, traceId }),
    );
  },
);

/** 统一请求并解构 data，抹平 axios 返回结构 */
export async function request<T = unknown>(cfg: AxiosRequestConfig): Promise<T> {
  const res = await api.request<ApiBody<T>>(cfg);
  return (res.data?.data ?? (undefined as unknown)) as T;
}
