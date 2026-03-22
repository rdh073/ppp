import { requestJson } from './client';

export type CampaignStatus = 'running' | 'done' | 'failed';
export type CampaignPhase = 'google' | 'instagram';

export interface Campaign {
  id: string;
  kind: string;
  deviceId: string;
  personaId?: string;
  status: CampaignStatus;
  phase?: CampaignPhase;
  googleTaskId?: string;
  instagramTaskId?: string;
  googleAccountId?: string;
  error?: string;
  createdAt: string;
  updatedAt: string;
}

export interface StartCampaignRequest {
  kind?: 'google+instagram' | 'google';
  deviceId: string;
  personaId?: string;
  phoneNumber?: string;
  captchaEndpoint?: string;
}

export function listCampaigns(): Promise<Campaign[]> {
  return requestJson('/campaigns');
}

export function startCampaign(req: StartCampaignRequest): Promise<{ campaignId: string }> {
  return requestJson('/campaigns', { method: 'POST', body: req });
}

export function getCampaign(id: string): Promise<Campaign> {
  return requestJson(`/campaigns/${id}`);
}
