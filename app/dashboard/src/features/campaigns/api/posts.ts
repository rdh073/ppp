import { requestJson } from '../../../shared/http/client';

export type PostJobStatus = 'pending' | 'running' | 'done' | 'failed';

export interface PostJob {
  id: string;
  accountId: string;
  deviceId: string;
  taskId?: string;
  status: PostJobStatus;
  error?: string;
}

export interface PostCampaign {
  id: string;
  imageSource: 'manual' | 'ai';
  textSource: 'manual' | 'ai';
  caption: string;
  jobs: PostJob[];
  status: 'running' | 'done' | 'failed';
  error?: string;
  createdAt: string;
  updatedAt: string;
}

export interface PostCampaignAICapability {
  available: boolean;
  reason?: string;
}

export interface PostCampaignCapabilities {
  imageAI: PostCampaignAICapability;
  textAI: PostCampaignAICapability;
}

export interface StartPostRequest {
  accountIds: string[];
  imageSource: 'manual' | 'ai';
  imageBase64?: string;
  imagePrompt?: string;
  textSource: 'manual' | 'ai';
  textContent?: string;
  textPrompt?: string;
}

export function listPostCampaigns(): Promise<PostCampaign[]> {
  return requestJson('/campaigns/posts');
}

export function getPostCampaignCapabilities(): Promise<PostCampaignCapabilities> {
  return requestJson('/campaigns/posts/capabilities');
}

export function startPostCampaign(req: StartPostRequest): Promise<{ campaignId: string }> {
  return requestJson('/campaigns/posts', { method: 'POST', body: req });
}

export function generateImagePreview(prompt: string): Promise<{ imageBase64: string }> {
  return requestJson('/campaigns/posts/preview/image', { method: 'POST', body: { prompt } });
}

export function generateCaptionPreview(prompt: string): Promise<{ caption: string }> {
  return requestJson('/campaigns/posts/preview/caption', { method: 'POST', body: { prompt } });
}
