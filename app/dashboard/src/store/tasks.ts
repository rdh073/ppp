import { create } from 'zustand';
import type { Task } from '../types';

interface TaskState {
  tasks: Record<string, Task>;
  trackedIds: string[];
  loading: boolean;
  loadingById: Record<string, boolean>;
  error: string | null;
  setTask: (task: Task) => void;
  setTasksLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setLoadingById: (id: string, loading: boolean) => void;
  upsertTrackedId: (id: string) => void;
  removeTrackedId: (id: string) => void;
}

export const useTaskStore = create<TaskState>((set) => ({
  tasks: {},
  trackedIds: [],
  loading: false,
  loadingById: {},
  error: null,
  setTask: (task) =>
    set((state) => ({
      tasks: {
        ...state.tasks,
        [task.id]: task,
      },
      loading: false,
      error: null,
    })),
  setTasksLoading: (loading) => set({ loading }),
  setError: (error) =>
    set({
      loading: false,
      error,
    }),
  setLoadingById: (id, loading) =>
    set((state) => ({
      loadingById: {
        ...state.loadingById,
        [id]: loading,
      },
    })),
  upsertTrackedId: (id) =>
    set((state) => {
      const exists = state.trackedIds.includes(id);
      if (exists) {
        return state;
      }

      return {
        trackedIds: [id, ...state.trackedIds].slice(0, 12),
      };
    }),
  removeTrackedId: (id) =>
    set((state) => ({
      trackedIds: state.trackedIds.filter((value) => value !== id),
    })),
}));
