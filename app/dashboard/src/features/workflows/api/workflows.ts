import { requestJson } from '../../../shared/http/client';
import type { WorkflowDefinition } from '../../../types';

export function listWorkflows(): Promise<WorkflowDefinition[]> {
  return requestJson('/workflows', {
    method: 'GET',
  });
}

export function getWorkflow(name: string): Promise<WorkflowDefinition> {
  return requestJson(`/workflows/${encodeURIComponent(name)}`, {
    method: 'GET',
  });
}
