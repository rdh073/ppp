import { requestJson } from '../../../shared/http/client';

export interface SavedMacro {
  id: string;
  deviceId: string;
  workflowName: string;
  source: 'manual' | 'ai';
  actionCount: number;
  durationMs?: number;
  steps?: number;
  done?: boolean;
  reason?: string;
  script: string;
  createdAt: string;
}

export interface PromoteResult {
  workflowName: string;
  path: string;
}

export function listMacros(): Promise<SavedMacro[]> {
  return requestJson('/macros');
}

export function deleteMacro(id: string): Promise<void> {
  return requestJson(`/macros/${id}`, { method: 'DELETE' });
}

export function promoteMacro(id: string, workflowName?: string): Promise<PromoteResult> {
  return requestJson(`/macros/${id}/promote`, {
    method: 'POST',
    body: workflowName ? { workflowName } : {},
  });
}
