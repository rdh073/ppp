import { API_URL } from '../../../config';
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

export async function streamCaptionPreview(
  prompt: string,
  onChunk: (token: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  const resp = await fetch(`${API_URL}/campaigns/posts/preview/caption/stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ prompt }),
    signal,
  });
  if (!resp.ok) throw new Error(await resp.text());

  const reader = resp.body!.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const lines = buf.split('\n');
      buf = lines.pop() ?? '';
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue;
        const tok = line.slice(6);
        if (tok === '[DONE]') return;
        if (tok.startsWith('[ERROR] ')) throw new Error(tok.slice(8));
        onChunk(tok);
      }
    }
  } finally {
    reader.cancel();
  }
}
