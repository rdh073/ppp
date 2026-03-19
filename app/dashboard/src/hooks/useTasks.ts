import { useCallback, useMemo } from 'react';
import { createTask, getTask, cancelTask } from '../api/tasks';
import type { TaskCreateRequest } from '../types';
import { useTaskStore } from '../store/tasks';
import { POLL_MS } from '../config';
import { usePolling } from './usePolling';

export function useTasks() {
  const tasks = useTaskStore((state) => state.tasks);
  const trackedIds = useTaskStore((state) => state.trackedIds);
  const loading = useTaskStore((state) => state.loading);
  const loadingById = useTaskStore((state) => state.loadingById);
  const error = useTaskStore((state) => state.error);
  const setTask = useTaskStore((state) => state.setTask);
  const setTasksLoading = useTaskStore((state) => state.setTasksLoading);
  const setLoadingById = useTaskStore((state) => state.setLoadingById);
  const setError = useTaskStore((state) => state.setError);
  const upsertTrackedId = useTaskStore((state) => state.upsertTrackedId);
  const activeTrackedIds = useMemo(() => {
    return trackedIds.filter((id) => {
      const status = tasks[id]?.status;
      return status !== 'completed' && status !== 'failed' && status !== 'cancelled';
    });
  }, [tasks, trackedIds]);

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

  const refreshActive = useCallback(async () => {
    await Promise.all(activeTrackedIds.map(async (id) => {
      setLoadingById(id, true);
      try {
        const task = await getTask(id);
        setTask(task);
      } catch (raw) {
        const message = raw instanceof Error ? raw.message : 'Failed to refresh active tasks';
        setError(message);
      } finally {
        setLoadingById(id, false);
      }
    }));
  }, [activeTrackedIds, setError, setLoadingById, setTask]);

  usePolling(refreshActive, POLL_MS, activeTrackedIds.length > 0);

  return {
    tasks: listTracked,
    loading,
    loadingById,
    error,
    create,
    refreshById,
    cancel,
    upsertTrackedId,
  };
}
