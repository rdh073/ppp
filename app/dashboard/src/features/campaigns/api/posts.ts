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

export function startPostCampaign(req: StartPostRequest): Promise<{ campaignId: string }> {
  return requestJson('/campaigns/posts', { method: 'POST', body: req });
}
