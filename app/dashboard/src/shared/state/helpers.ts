import { createJSONStorage, type PersistOptions } from 'zustand/middleware';

type LoadableState = {
  loading: boolean;
  error: string | null;
};

export function createLoadableActions<T extends LoadableState>(
  set: (partial: Partial<T>) => void,
) {
  return {
    setLoading: (loading: boolean) => set({ loading } as Partial<T>),
    setError: (error: string | null) => set({ loading: false, error } as Partial<T>),
  };
}

export function createPersistOptions<T>(
  name: string,
  partialize?: (state: T) => Partial<T>,
): PersistOptions<T> {
  return {
    name,
    storage: createJSONStorage(() => localStorage),
    partialize,
  } as unknown as PersistOptions<T>;
}
