import { useCallback, useEffect } from 'react';

export function usePolling(fetcher: () => Promise<void>, intervalMs: number, enabled: boolean): () => void {
  const run = useCallback(() => {
    void fetcher();
  }, [fetcher]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    run();
    const id = window.setInterval(run, intervalMs);
    return () => {
      window.clearInterval(id);
    };
  }, [run, intervalMs, enabled]);

  return run;
}
