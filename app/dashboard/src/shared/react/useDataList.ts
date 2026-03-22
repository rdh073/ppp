import { useCallback, useEffect, useState } from 'react';
import { openEventStream, type StreamEnvelope } from '../http/client';
import { usePolling } from './usePolling';

export interface UseDataListOptions<T> {
  /** When true, polling runs. Useful when data contains in-progress items. */
  pollWhile?: (item: T) => boolean;
  /** Polling interval in ms. Default: 3000. */
  pollInterval?: number;
  /** Optional SSE stream topics for incremental read-model updates. */
  stream?: {
    topics: string[];
    getKey: (item: T) => string;
    applyEvent?: (current: T[], event: StreamEnvelope<T>) => T[];
  };
}

export interface UseDataListResult<T> {
  data: T[];
  loading: boolean;
  error: string;
  reload: () => void;
}

/**
 * Loads a list from an async fetcher, manages loading/error state,
 * and optionally polls while `pollWhile` returns true for any item.
 */
export function useDataList<T>(
  fetcher: () => Promise<T[]>,
  opts: UseDataListOptions<T> = {},
): UseDataListResult<T> {
  const { pollWhile, pollInterval = 3000, stream } = opts;

  const [data, setData] = useState<T[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [streamVersion, setStreamVersion] = useState(0);

  const load = useCallback(async () => {
    try {
      setData(await fetcher());
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Load failed');
    } finally {
      setLoading(false);
    }
  }, [fetcher]);

  // Initial load
  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!stream || typeof EventSource === 'undefined') {
      return;
    }

    let disposed = false;
    let source: EventSource | null = null;
    source = openEventStream<T>(
      stream.topics,
      (event) => {
        setData((current) => {
          if (stream.applyEvent) {
            return stream.applyEvent(current, event);
          }
          return upsertItem(current, event.payload, stream.getKey);
        });
        setError('');
        setLoading(false);
      },
      () => {
        if (disposed) return;
        source?.close();
        void load();
        setStreamVersion((value) => value + 1);
      },
    );

    source.onerror = () => {
      // EventSource reconnects automatically; keep the last known read model.
    };

    return () => {
      disposed = true;
      source?.close();
    };
  }, [load, stream, streamVersion]);

  const shouldPoll = stream == null && pollWhile != null && data.some(pollWhile);
  usePolling(load, pollInterval, { enabled: shouldPoll, immediate: false });

  return { data, loading, error, reload: load };
}

function upsertItem<T>(items: T[], item: T, getKey: (item: T) => string): T[] {
  const key = getKey(item);
  const index = items.findIndex((current) => getKey(current) === key);
  if (index === -1) {
    return [...items, item];
  }
  const next = items.slice();
  next[index] = item;
  return next;
}
