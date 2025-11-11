import { useAuthStore } from '@/store/useAuth';
import type { ApiResult } from '@/types';

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

interface RequestOptions extends Omit<RequestInit, 'body' | 'method'> {
  method?: HttpMethod;
  body?: unknown;
}

const API_BASE_URL = '/api';

function resolveUrl(url: string) {
  if (url.startsWith('http')) {
    return url;
  }
  if (url.startsWith('/auth')) {
    return url;
  }
  return `${API_BASE_URL}${url}`;
}

async function execute<T>(url: string, options: RequestOptions = {}): Promise<ApiResult<T>> {
  const { method = 'GET', headers: optionHeaders, ...rest } = options;
  const headers = new Headers(optionHeaders ?? {});
  if (!headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  const token = useAuthStore.getState().token;
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }

  try {
    const response = await fetch(resolveUrl(url), {
      ...rest,
      method,
      headers,
      body: rest.body !== undefined ? JSON.stringify(rest.body) : undefined,
    });

    const contentType = response.headers.get('Content-Type') ?? '';
    const isJson = contentType.includes('application/json');
    const payload = isJson ? await response.json() : null;

    if (!response.ok) {
      return {
        error: {
          message: payload?.message ?? '请求失败',
          status: response.status,
        },
      };
    }

    return { data: payload as T };
  } catch (error) {
    const message = error instanceof Error ? error.message : '网络异常';
    return { error: { message } };
  }
}

export const http = {
  get: <T>(url: string, options?: RequestOptions) => execute<T>(url, { ...options, method: 'GET' }),
  post: <T>(url: string, body?: unknown, options?: RequestOptions) =>
    execute<T>(url, { ...options, method: 'POST', body }),
  put: <T>(url: string, body?: unknown, options?: RequestOptions) =>
    execute<T>(url, { ...options, method: 'PUT', body }),
  patch: <T>(url: string, body?: unknown, options?: RequestOptions) =>
    execute<T>(url, { ...options, method: 'PATCH', body }),
  delete: <T>(url: string, options?: RequestOptions) =>
    execute<T>(url, { ...options, method: 'DELETE' }),
};
