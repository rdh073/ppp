import { requestJson } from '../../../shared/http/client';

export interface RecordStatus {
  active: boolean;
  deviceId: string;
  seq: number;
}

export interface RecordResult {
  workflowName: string;
  deviceId: string;
  actionCount: number;
  script: string;
}

export interface LLMRunResult {
  workflowName: string;
  deviceId: string;
  done: boolean;
  reason: string;
  steps: number;
  durationMs: number;
  actionCount: number;
  script: string;
}

export function recordStart(deviceId: string, workflowName?: string): Promise<RecordResult> {
  return requestJson(`/devices/${encodeURIComponent(deviceId)}/record/start`, {
    method: 'POST',
    body: workflowName ? { workflowName } : {},
  });
}

export function recordStop(deviceId: string, workflowName?: string): Promise<RecordResult> {
  return requestJson(`/devices/${encodeURIComponent(deviceId)}/record/stop`, {
    method: 'POST',
    body: workflowName ? { workflowName } : {},
  });
}

export function recordStatus(deviceId: string): Promise<RecordStatus> {
  return requestJson(`/devices/${encodeURIComponent(deviceId)}/record/status`);
}

export function llmRun(
  deviceId: string,
  opts: { goal: string; maxSteps?: number; workflowName?: string; timeout?: number },
): Promise<LLMRunResult> {
  return requestJson(`/devices/${encodeURIComponent(deviceId)}/record/llm-run`, {
    method: 'POST',
    body: opts,
    timeoutMs: (opts.timeout ?? 300_000) + 15_000, // server timeout + buffer
  });
}
