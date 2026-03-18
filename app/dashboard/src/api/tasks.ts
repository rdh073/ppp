import { requestJson } from './client';
import type { Task, TaskCreateRequest } from '../types';

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

export function cancelTask(taskId: string): Promise<void> {
  return requestJson(`/tasks/${encodeURIComponent(taskId)}`, {
    method: 'DELETE',
  });
}
