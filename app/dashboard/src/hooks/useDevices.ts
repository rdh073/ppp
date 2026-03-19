import { useCallback } from 'react';
import { listDevices } from '../api/devices';
import { useDeviceStore } from '../store/devices';
import { POLL_MS } from '../config';
import { usePolling } from './usePolling';

export function useDevices(intervalMs: number = POLL_MS) {
  const devices = useDeviceStore((state) => state.devices);
  const loading = useDeviceStore((state) => state.loading);
  const error = useDeviceStore((state) => state.error);
  const setDevices = useDeviceStore((state) => state.setDevices);
  const setLoading = useDeviceStore((state) => state.setLoading);
  const setError = useDeviceStore((state) => state.setError);

  const fetchDevices = useCallback(async () => {
    setLoading(true);
    try {
      const payload = await listDevices();
      setDevices(payload);
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to load devices';
      setError(message);
    }
  }, [setError, setDevices, setLoading]);

  const refresh = usePolling(fetchDevices, intervalMs, true);

  return { devices, loading, error, refresh };
}
