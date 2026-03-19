const DEFAULT_API_URL = 'http://localhost:3000';
const DEFAULT_POLL_MS = 5000;

function normalizeUrl(value: string): string {
  return value.replace(/\/+$/, '');
}

function parsePollMs(value: unknown): number {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return DEFAULT_POLL_MS;
  }
  return Math.floor(parsed);
}

export const API_URL = normalizeUrl(
  (typeof import.meta !== 'undefined' && import.meta.env?.VITE_API_URL) || DEFAULT_API_URL,
);

export const POLL_MS = parsePollMs(
  typeof import.meta !== 'undefined' ? import.meta.env?.VITE_POLL_MS : undefined,
);

export const METRICS_URL =
  (typeof import.meta !== 'undefined' && import.meta.env?.VITE_METRICS_URL) ||
  `${API_URL}/metrics`;

