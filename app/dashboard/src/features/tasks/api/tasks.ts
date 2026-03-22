import { requestJson } from '../../../shared/http/client';
import type { Task, TaskCreateRequest } from '../../../types';

export function createTask(body: TaskCreateRequest): Promise<Task> {
  return requestJson('/tasks', {
    method: 'POST',
    body,
  });
}

export function getTask(taskId: string): Promise<Task> {
  return requestJson(`/tasks/${encodeURIComponent(taskId)}`, {
    method: 'GET',
  });
}

export interface ListTaskParams {
  status?: string;
  deviceId?: string;
  workflowName?: string;
  limit?: number;
  offset?: number;
}

export function listTasks(params: ListTaskParams = {}): Promise<Task[]> {
  return requestJson('/tasks', {
    method: 'GET',
    query: {
      status: params.status,
      deviceId: params.deviceId,
      workflowName: params.workflowName,
      limit: params.limit,
      offset: params.offset,
    },
  });
}

export function cancelTask(taskId: string): Promise<void> {
  return requestJson(`/tasks/${encodeURIComponent(taskId)}`, {
    method: 'DELETE',
  });
}
