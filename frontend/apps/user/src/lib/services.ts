// 高层 API 服务：分领域封装，UI 层只看 services.* 不直接 import axios。
import { request } from './api';
import type {
  APIKey,
  APIKeyCreateBody,
  APIKeyCreated,
  CreateImageBody,
  CreateTextBody,
  CreateVideoBody,
  GenerationTask,
  LoginResp,
  MeResp,
  MessageResp,
  PageData,
  PromptGalleryItem,
  PublicModel,
  RedeemCDKResp,
  RechargeOrder,
  RechargePackage,
  RegisterResp,
  TextGenerationResp,
  TokenPair,
  WalletLog,
} from './types';

export const authApi = {
  register: (body: { account: string; password: string; code: string; invite_code?: string }) =>
    request<RegisterResp>({ method: 'POST', url: '/auth/register', data: body }),
  login: (body: { account: string; password: string }) =>
    request<LoginResp>({ method: 'POST', url: '/auth/login', data: body }),
  sendEmailCode: (body: { email: string; scene: 'register' | 'reset_password' }) =>
    request<MessageResp>({ method: 'POST', url: '/auth/email/code', data: body }),
  resetPassword: (body: { email: string; code: string; password: string }) =>
    request<MessageResp>({ method: 'POST', url: '/auth/password/reset', data: body }),
  refresh: (refresh_token: string) =>
    request<TokenPair>({ method: 'POST', url: '/auth/refresh', data: { refresh_token } }),
  logout: () => request<null>({ method: 'POST', url: '/auth/logout' }),
  me: () => request<MeResp>({ method: 'GET', url: '/users/me' }),
  changePassword: (body: { old_password: string; new_password: string }) =>
    request<null>({ method: 'POST', url: '/users/password', data: body }),
};

export const keysApi = {
  list: async () => {
    const r = await request<{ list: APIKey[] } | APIKey[] | null>({ method: 'GET', url: '/keys' });
    if (Array.isArray(r)) return r;
    return r?.list ?? [];
  },
  create: (body: APIKeyCreateBody) =>
    request<APIKeyCreated>({ method: 'POST', url: '/keys', data: body }),
  toggle: ({ id, enable }: { id: number; enable: boolean }) =>
    request<null>({
      method: 'POST',
      url: `/keys/${id}/toggle`,
      params: { enable: enable ? 1 : 0 },
    }),
  remove: (id: number) => request<null>({ method: 'DELETE', url: `/keys/${id}` }),
};

export const billingApi = {
  packages: async () => {
    const r = await request<{ list: RechargePackage[] } | RechargePackage[] | null>({
      method: 'GET',
      url: '/billing/recharge/packages',
    });
    if (Array.isArray(r)) return r;
    return r?.list ?? [];
  },
  createRechargeOrder: (body: { package_id: string; channel?: 'alipay' }, idemKey = clientIdemKey()) =>
    request<RechargeOrder>({
      method: 'POST',
      url: '/billing/recharge/orders',
      data: body,
      headers: { 'Idempotency-Key': idemKey },
    }),
  rechargeOrders: (page = 1, pageSize = 20) =>
    request<PageData<RechargeOrder>>({
      method: 'GET',
      url: '/billing/recharge/orders',
      params: { page, page_size: pageSize },
    }),
  rechargeOrder: (orderNo: string) =>
    request<RechargeOrder>({ method: 'GET', url: `/billing/recharge/orders/${orderNo}` }),
  cancelRechargeOrder: (orderNo: string) =>
    request<RechargeOrder>({ method: 'POST', url: `/billing/recharge/orders/${orderNo}/cancel` }),
  logs: (page = 1, pageSize = 20) =>
    request<PageData<WalletLog>>({
      method: 'GET',
      url: '/billing/logs',
      params: { page, page_size: pageSize },
    }),
  redeemCDK: (code: string, idemKey = clientIdemKey()) =>
    request<RedeemCDKResp>({
      method: 'POST',
      url: '/billing/cdk/redeem',
      data: { code },
      headers: { 'Idempotency-Key': idemKey },
    }),
};

function clientIdemKey() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export const genApi = {
  models: async () => {
    const r = await request<{ list: PublicModel[] } | PublicModel[] | null>({ method: 'GET', url: '/models' });
    if (Array.isArray(r)) return r;
    return r?.list ?? [];
  },
  createText: (body: CreateTextBody, idemKey?: string) =>
    request<TextGenerationResp>({
      method: 'POST',
      url: '/gen/text',
      data: body,
      headers: idemKey ? { 'Idempotency-Key': idemKey } : undefined,
    }),
  createImage: (body: CreateImageBody, idemKey?: string) =>
    request<GenerationTask>({
      method: 'POST',
      url: '/gen/image',
      data: body,
      headers: idemKey ? { 'Idempotency-Key': idemKey } : undefined,
    }),
  createVideo: (body: CreateVideoBody, idemKey?: string) =>
    request<GenerationTask>({
      method: 'POST',
      url: '/gen/video',
      data: body,
      headers: idemKey ? { 'Idempotency-Key': idemKey } : undefined,
    }),
  getTask: (taskId: string) =>
    request<GenerationTask>({ method: 'GET', url: `/gen/tasks/${taskId}` }),
  history: (params: { kind?: 'image' | 'video' | 'media'; page?: number; page_size?: number } = {}) =>
    request<PageData<GenerationTask>>({
      method: 'GET',
      url: '/gen/history',
      params: {
        kind: params.kind,
        page: params.page ?? 1,
        page_size: params.page_size ?? 20,
      },
    }),
  deleteHistory: (scope: 'before_3d' | 'before_7d' | 'failed' | 'all') =>
    request<{ deleted: number }>({ method: 'DELETE', url: '/gen/history', params: { scope } }),
};

export const promptGalleryApi = {
  list: async (modality: 'image' | 'text' | 'video') => {
    const r = await request<{ list: PromptGalleryItem[] } | PromptGalleryItem[] | null>({
      method: 'GET',
      url: '/public/prompt-gallery',
      params: { modality },
    });
    if (Array.isArray(r)) return r;
    return r?.list ?? [];
  },
};
