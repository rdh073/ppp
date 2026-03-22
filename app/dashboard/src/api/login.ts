import { requestJson } from './client';

export type LoginCampaignStatus = 'running' | 'done' | 'failed';

export interface LoginCampaign {
  id: string;
  accountId: string;
  deviceId: string;
  taskId?: string;
  status: LoginCampaignStatus;
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
  list: () => Promise<LoginCampaign[]>;
  start: (req: StartLoginRequest) => Promise<{ loginCampaignId: string; accountId: string }>;
}

export function createLoginPlatformApi(platform: 'google' | 'instagram'): LoginPlatformApi {
  const base = `/login/${platform}`;
  return {
    list: () => requestJson(base),
    start: (req) => requestJson(base, { method: 'POST', body: req }),
  };
}

// Named exports for backward compatibility
export function listLoginCampaigns(): Promise<LoginCampaign[]> {
  return createLoginPlatformApi('google').list();
}

export function startLogin(req: StartLoginRequest): Promise<{ loginCampaignId: string; accountId: string }> {
  return createLoginPlatformApi('google').start(req);
}

export function listInstagramLoginCampaigns(): Promise<LoginCampaign[]> {
  return createLoginPlatformApi('instagram').list();
}

export function startInstagramLogin(req: StartLoginRequest): Promise<{ loginCampaignId: string; accountId: string }> {
  return createLoginPlatformApi('instagram').start(req);
}
