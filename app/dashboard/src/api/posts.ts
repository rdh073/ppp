import { requestJson } from './client';

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
  return requestJson('/posts/instagram');
}

export function startPostCampaign(req: StartPostRequest): Promise<{ postCampaignId: string }> {
  return requestJson('/posts/instagram', { method: 'POST', body: req });
}
