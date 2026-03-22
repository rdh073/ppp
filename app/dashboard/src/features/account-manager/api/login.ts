import { requestJson } from '../../../shared/http/client';

export type LoginRunStatus = 'running' | 'done' | 'failed';

export interface LoginRun {
  id: string;
  platform: 'google' | 'instagram';
  accountId: string;
  deviceId: string;
  taskId?: string;
  status: LoginRunStatus;
  error?: string;
  createdAt: string;
  updatedAt: string;
}

export interface StartLoginRequest {
  /** Use an existing persisted account */
  accountId?: string;
  /** Or provide credentials directly (will be persisted as deactive) */
  email?: string;
  password?: string;
  deviceId: string;
}

export interface LoginPlatformApi {
  list: () => Promise<LoginRun[]>;
  start: (req: StartLoginRequest) => Promise<{ loginRunId: string; accountId: string }>;
}

export function createLoginPlatformApi(platform: 'google' | 'instagram'): LoginPlatformApi {
  const base = `/account-manager/logins/${platform}`;
  return {
    list: () => requestJson(base),
    start: (req) => requestJson(base, { method: 'POST', body: req }),
  };
}

// Named exports for backward compatibility
export function listLoginRuns(): Promise<LoginRun[]> {
  return createLoginPlatformApi('google').list();
}

export function startLogin(req: StartLoginRequest): Promise<{ loginRunId: string; accountId: string }> {
  return createLoginPlatformApi('google').start(req);
}

export function listInstagramLoginRuns(): Promise<LoginRun[]> {
  return createLoginPlatformApi('instagram').list();
}

export function startInstagramLogin(req: StartLoginRequest): Promise<{ loginRunId: string; accountId: string }> {
  return createLoginPlatformApi('instagram').start(req);
}
