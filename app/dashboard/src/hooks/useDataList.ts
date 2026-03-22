import { useCallback, useEffect, useState } from 'react';
import { usePolling } from './usePolling';

export interface UseDataListOptions<T> {
  /** When true, polling runs. Useful when data contains in-progress items. */
  pollWhile?: (item: T) => boolean;
  /** Polling interval in ms. Default: 3000. */
  pollInterval?: number;
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
  const { pollWhile, pollInterval = 3000 } = opts;

  const [data, setData] = useState<T[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

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

  // Conditional polling: run while any item matches pollWhile
  const shouldPoll = pollWhile != null && data.some(pollWhile);
  usePolling(load, pollInterval, { enabled: shouldPoll, immediate: false });

  return { data, loading, error, reload: load };
}
