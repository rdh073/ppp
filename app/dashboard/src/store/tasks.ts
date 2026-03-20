import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import type { Task } from '../types';

interface TaskState {
  tasks: Record<string, Task>;
  trackedIds: string[];
  loading: boolean;
  loadingById: Record<string, boolean>;
  error: string | null;
  setTasks: (tasks: Task[]) => void;
  setTask: (task: Task) => void;
  setTasksLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setLoadingById: (id: string, loading: boolean) => void;
  upsertTrackedId: (id: string) => void;
  removeTrackedId: (id: string) => void;
}

export const useTaskStore = create<TaskState>()(
  persist(
    (set) => ({
      tasks: {},
      trackedIds: [],
      loading: false,
      loadingById: {},
      error: null,
      setTasks: (tasks) =>
        set(() => {
          const mapped: Record<string, Task> = {};
          const ids: string[] = [];
          tasks.forEach((task) => {
            mapped[task.id] = task;
            ids.push(task.id);
          });
          return {
            tasks: mapped,
            trackedIds: ids,
            loading: false,
            error: null,
          };
        }),
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
            trackedIds: [id, ...state.trackedIds].slice(0, 30),
          };
        }),
      removeTrackedId: (id) =>
        set((state) => ({
          trackedIds: state.trackedIds.filter((value) => value !== id),
        })),
    }),
    {
      name: 'ppp-dashboard-task-store',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        tasks: state.tasks,
        trackedIds: state.trackedIds,
      }),
    },
  ),
);
