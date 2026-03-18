import { useCallback, useMemo } from 'react';
import { listAcceptedEvents } from '../api/events';
import type { DeviceQueryParams } from '../types';
import { useEventStore } from '../store/events';
import { usePolling } from './usePolling';

export function useEvents(intervalMs: number = 5000, params: DeviceQueryParams = {}) {
  const entries = useEventStore((state) => state.entries);
  const loading = useEventStore((state) => state.loading);
  const error = useEventStore((state) => state.error);
  const pagination = useEventStore((state) => state.pagination);
  const setList = useEventStore((state) => state.setList);
  const appendPage = useEventStore((state) => state.appendPage);
  const setLoading = useEventStore((state) => state.setLoading);
  const setError = useEventStore((state) => state.setError);

  const loadEvents = useCallback(
    async (append = false) => {
      setLoading(true);
      const baseOffset =
        params.offset ??
        (append && pagination
          ? pagination.offset + pagination.limit
          : 0);
      try {
        const payload = await listAcceptedEvents({
          limit: params.limit,
          offset: baseOffset,
          order: params.order,
          cursor: params.cursor,
          from: params.from,
          to: params.to,
          kind: params.kind,
          source: params.source,
          deviceId: params.deviceId,
        });
        if (append) {
          appendPage(payload);
        } else {
          setList(payload);
        }
      } catch (raw) {
        const message = raw instanceof Error ? raw.message : 'Failed to load events';
        setError(message);
      }
    },
    [appendPage, params, pagination, setError, setList, setLoading],
  );

  const refresh = useCallback(
    () => loadEvents(false),
    [loadEvents],
  );

  const loadMore = useCallback(() => loadEvents(true), [loadEvents]);

  const hasMore = useMemo(() => {
    return pagination?.hasMore ?? false;
  }, [pagination?.hasMore, entries.length]);

  return {
    entries,
    loading,
    error,
    refresh: usePolling(refresh, intervalMs, true),
    loadMore,
    hasMore,
  };
}
