import { create } from 'zustand';
import type { EventRecord, PagedEventsResponse } from '../types';
import { createLoadableActions } from './helpers';

interface EventState {
  entries: EventRecord[];
  pagination: Omit<PagedEventsResponse, 'items'> | null;
  loading: boolean;
  error: string | null;
  selected: EventRecord | null;
  setList: (page: PagedEventsResponse) => void;
  appendPage: (page: PagedEventsResponse) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setSelected: (entry: EventRecord | null) => void;
}

export const useEventStore = create<EventState>((set) => ({
  ...createLoadableActions<EventState>(set),
  entries: [],
  pagination: null,
  loading: false,
  error: null,
  selected: null,
  setList: (page) =>
    set({
      entries: page.items,
      pagination: {
        total: page.total,
        offset: page.offset,
        limit: page.limit,
        hasMore: page.hasMore,
        nextCursor: page.nextCursor,
      },
      loading: false,
      error: null,
    }),
  appendPage: (page) =>
    set((state) => ({
      entries: [...state.entries, ...page.items],
      pagination: {
        total: page.total,
        offset: page.offset,
        limit: page.limit,
        hasMore: page.hasMore,
        nextCursor: page.nextCursor,
      },
      loading: false,
      error: null,
    })),
  setSelected: (selected) => set({ selected }),
}));
