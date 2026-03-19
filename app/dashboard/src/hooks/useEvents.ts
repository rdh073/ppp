import { useCallback, useMemo } from 'react';
import { listAcceptedEvents } from '../api/events';
import type { DeviceQueryParams } from '../types';
import { useEventStore } from '../store/events';
import { POLL_MS } from '../config';
import { usePolling } from './usePolling';

export function useEvents(intervalMs: number = POLL_MS, params: DeviceQueryParams = {}) {
  const entries = useEventStore((state) => state.entries);
  const loading = useEventStore((state) => state.loading);
  const error = useEventStore((state) => state.error);
  const pagination = useEventStore((state) => state.pagination);
  const setList = useEventStore((state) => state.setList);
  const appendPage = useEventStore((state) => state.appendPage);
  const setLoading = useEventStore((state) => state.setLoading);
  const setError = useEventStore((state) => state.setError);

  const {
    limit,
    offset,
    order,
    cursor,
    from,
    to,
    kind,
    source,
    deviceId,
    includePayload,
  } = params;

  const loadEvents = useCallback(
    async (append = false) => {
      setLoading(true);
      const baseOffset =
        offset ??
        (append && pagination
          ? pagination.offset + pagination.limit
          : 0);
      try {
        const payload = await listAcceptedEvents({
          limit,
          offset: baseOffset,
          order,
          cursor,
          from,
          to,
          kind,
          source,
          deviceId,
          includePayload,
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
    [appendPage, cursor, deviceId, from, includePayload, kind, limit, offset, order, pagination, setError, setList, setLoading, source, to],
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
