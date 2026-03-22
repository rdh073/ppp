import { useCallback, useEffect, useRef } from 'react';

interface PollingOptions {
  enabled?: boolean;
  immediate?: boolean;
  pauseWhenHidden?: boolean;
}

export function usePolling(
  fetcher: () => Promise<void>,
  intervalMs: number,
  options: PollingOptions = {},
): () => void {
  const { enabled = true, immediate = true, pauseWhenHidden = true } = options;
  const inFlightRef = useRef(false);
  const timerRef = useRef<number | null>(null);

  const run = useCallback(() => {
    if (inFlightRef.current) {
      return;
    }
    if (pauseWhenHidden && document.visibilityState === 'hidden') {
      return;
    }
    inFlightRef.current = true;
    void fetcher().finally(() => {
      inFlightRef.current = false;
    });
  }, [fetcher, pauseWhenHidden]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    if (immediate) {
      run();
    }

    timerRef.current = window.setInterval(run, intervalMs);
    return () => {
      if (timerRef.current !== null) {
        window.clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [enabled, immediate, intervalMs, run]);

  return run;
}
