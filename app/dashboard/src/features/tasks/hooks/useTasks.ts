import { useCallback, useState } from 'react';
import { createTask, getTask, cancelTask, listTasks } from '../api/tasks';
import type { TaskCreateRequest } from '../../../types';
import { useTaskStore } from '../store/tasks';
import { TASK_POLL_MS } from '../../../config';
import { usePolling } from '../../../shared/react/usePolling';

interface UseTaskOptions {
  limit?: number;
  offset?: number;
  autoRefresh?: boolean;
  status?: string;
  deviceId?: string;
  workflowName?: string;
}

export function useTasks(options: UseTaskOptions = {}) {
  const {
    limit = 20,
    offset = 0,
    autoRefresh = true,
    status,
    deviceId,
    workflowName,
  } = options;
  const tasks = useTaskStore((state) => state.tasks);
  const trackedIds = useTaskStore((state) => state.trackedIds);
  const loading = useTaskStore((state) => state.loading);
  const loadingById = useTaskStore((state) => state.loadingById);
  const error = useTaskStore((state) => state.error);
  const setTasks = useTaskStore((state) => state.setTasks);
  const setTask = useTaskStore((state) => state.setTask);
  const setTasksLoading = useTaskStore((state) => state.setTasksLoading);
  const setLoadingById = useTaskStore((state) => state.setLoadingById);
  const setError = useTaskStore((state) => state.setError);
  const upsertTrackedId = useTaskStore((state) => state.upsertTrackedId);
  const [hasMore, setHasMore] = useState(false);
  const create = useCallback(async (body: TaskCreateRequest) => {
    setTasksLoading(true);
    try {
      const task = await createTask(body);
      setTask(task);
      upsertTrackedId(task.id);
      return task.id;
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to create task';
      setError(message);
      throw raw;
    }
  }, [setError, setTask, setTasksLoading, upsertTrackedId]);

  const refreshById = useCallback(async (id: string) => {
    setLoadingById(id, true);
    try {
      const task = await getTask(id);
      setTask(task);
      upsertTrackedId(id);
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to load task';
      setError(message);
    } finally {
      setLoadingById(id, false);
    }
  }, [setError, setLoadingById, setTask, upsertTrackedId]);

  const cancel = useCallback(async (id: string) => {
    setLoadingById(id, true);
    try {
      await cancelTask(id);
      const task = tasks[id];
      if (task) {
        setTask({ ...task, status: 'cancelled' });
      }
      await refreshById(id);
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to cancel task';
      setError(message);
      setLoadingById(id, false);
      throw raw;
    }
  }, [refreshById, setError, setLoadingById, setTask, tasks]);

  const listTracked = trackedIds
    .map((id) => tasks[id])
    .filter((task): task is NonNullable<typeof task> => task !== undefined);

  const refreshList = useCallback(async (silent = false) => {
    if (!silent || trackedIds.length === 0) {
      setTasksLoading(true);
    }
    try {
      const fetchLimit = Math.min(500, Math.max(1, limit + 1));
      const list = await listTasks({
        status: status?.trim() || undefined,
        deviceId: deviceId?.trim() || undefined,
        workflowName: workflowName?.trim() || undefined,
        limit: fetchLimit,
        offset: Math.max(0, offset),
      });
      const pageItems = list.slice(0, limit);
      setTasks(pageItems);
      pageItems.forEach((task) => upsertTrackedId(task.id));
      setHasMore(list.length > limit);
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to list tasks';
      setError(message);
    }
  }, [deviceId, limit, offset, setError, setTasks, setTasksLoading, status, trackedIds.length, upsertTrackedId, workflowName]);

  const refreshListSilent = useCallback(async () => {
    await refreshList(true);
  }, [refreshList]);

  usePolling(refreshListSilent, TASK_POLL_MS, {
    enabled: autoRefresh,
    immediate: true,
    pauseWhenHidden: true,
  });

  return {
    tasks: listTracked,
    loading,
    loadingById,
    error,
    create,
    refreshById,
    refreshList: () => refreshList(false),
    cancel,
    upsertTrackedId,
    hasMore,
  };
}
