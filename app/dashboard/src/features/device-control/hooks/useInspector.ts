import { useCallback, useEffect, useRef, useState } from 'react';
import { usePolling } from '../../../shared/react/usePolling';
import { observeDevice } from '../api/inspect';
import type { UiSnapshot, UiTarget } from '../types/inspector';

const AUTO_REFRESH_INTERVAL = 3_000;

interface UseInspectorResult {
  snapshot: UiSnapshot | null;
  targets: UiTarget[];
  loading: boolean;
  error: string;
  selectedTarget: UiTarget | null;
  hoveredTarget: UiTarget | null;
  autoRefresh: boolean;
  setSelectedTarget: (t: UiTarget | null) => void;
  setHoveredTarget: (t: UiTarget | null) => void;
  toggleAutoRefresh: () => void;
  refresh: () => void;
}

export function useInspector(deviceId: string | undefined, enabled: boolean): UseInspectorResult {
  const [snapshot, setSnapshot] = useState<UiSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [selectedTarget, setSelectedTarget] = useState<UiTarget | null>(null);
  const [hoveredTarget, setHoveredTarget] = useState<UiTarget | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;

  const fetchSnapshot = useCallback(async () => {
    if (!deviceId || !enabledRef.current) return;
    setLoading(true);
    setError('');
    try {
      const snap = await observeDevice(deviceId);
      if (enabledRef.current) {
        setSnapshot(snap);
      }
    } catch (e) {
      if (enabledRef.current) {
        setError(e instanceof Error ? e.message : 'Observe failed');
      }
    } finally {
      setLoading(false);
    }
  }, [deviceId]);

  // Initial fetch when enabled
  useEffect(() => {
    if (enabled && deviceId) {
      void fetchSnapshot();
    }
    if (!enabled) {
      setSnapshot(null);
      setSelectedTarget(null);
      setHoveredTarget(null);
      setError('');
    }
  }, [enabled, deviceId, fetchSnapshot]);

  // Auto-refresh polling
  usePolling(fetchSnapshot, AUTO_REFRESH_INTERVAL, {
    enabled: enabled && autoRefresh && !!deviceId,
    immediate: false,
  });

  const toggleAutoRefresh = useCallback(() => {
    setAutoRefresh((v) => !v);
  }, []);

  return {
    snapshot,
    targets: snapshot?.targets ?? [],
    loading,
    error,
    selectedTarget,
    hoveredTarget,
    autoRefresh,
    setSelectedTarget,
    setHoveredTarget,
    toggleAutoRefresh,
    refresh: fetchSnapshot,
  };
}
