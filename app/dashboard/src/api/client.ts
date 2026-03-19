import { API_URL } from '../config';

export function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  body?: unknown;
  timeoutMs?: number;
  headers?: HeadersInit;
  query?: Record<string, string | number | undefined | null>;
}

function encodeQuery(query?: Record<string, string | number | undefined | null>): string {
  if (!query) return '';
  const params = new URLSearchParams();

  Object.entries(query).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return;
    params.set(key, String(value));
  });

  const qs = params.toString();
  return qs ? `?${qs}` : '';
}

export async function requestJson<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const {
    method = 'GET',
    body,
    timeoutMs = 30_000,
    headers,
    query,
  } = options;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort('request-timeout'), timeoutMs);

  try {
    const hasBody = body !== undefined && body !== null;
    const payload = hasBody
      ? body instanceof FormData || typeof body === 'string' || body instanceof Blob || body instanceof ArrayBuffer
        ? body
        : JSON.stringify(body)
      : undefined;

    const response = await fetch(`${API_URL}${path}${encodeQuery(query)}`, {
      method,
      body: payload,
      headers: {
        Accept: 'application/json',
        ...(hasBody && body instanceof FormData ? {} : { 'Content-Type': 'application/json' }),
        ...headers,
      },
      signal: controller.signal,
    });

    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `HTTP ${response.status}`);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    const contentType = response.headers.get('content-type') || '';
    if (!contentType.includes('application/json')) {
      const text = await response.text();
      throw new Error(`Unexpected response format: ${text.slice(0, 80)}`);
    }

    return (await response.json()) as T;
  } catch (error) {
    if (isAbortError(error)) {
      throw new Error(`Request timed out after ${timeoutMs}ms (${method} ${path})`);
    }
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}
