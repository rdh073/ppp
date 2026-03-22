import { useCallback } from 'react';
import { listDevices } from '../api/devices';
import { useDeviceStore } from '../store/devices';
import { DEVICE_POLL_MS } from '../../../config';
import { usePolling } from '../../../shared/react/usePolling';

export function useDevices(intervalMs: number = DEVICE_POLL_MS) {
  const devices = useDeviceStore((state) => state.devices);
  const loading = useDeviceStore((state) => state.loading);
  const error = useDeviceStore((state) => state.error);
  const setDevices = useDeviceStore((state) => state.setDevices);
  const setLoading = useDeviceStore((state) => state.setLoading);
  const setError = useDeviceStore((state) => state.setError);

  const fetchDevices = useCallback(async (silent = false) => {
    if (!silent || devices.length === 0) {
      setLoading(true);
    }
    try {
      const payload = await listDevices();
      setDevices(payload);
    } catch (raw) {
      const message = raw instanceof Error ? raw.message : 'Failed to load devices';
      setError(message);
    }
  }, [devices.length, setError, setDevices, setLoading]);

  const refresh = useCallback(() => {
    void fetchDevices(false);
  }, [fetchDevices]);

  const refreshSilent = useCallback(async () => {
    await fetchDevices(true);
  }, [fetchDevices]);

  usePolling(refreshSilent, intervalMs, {
    enabled: true,
    immediate: true,
    pauseWhenHidden: true,
  });

  return { devices, loading, error, refresh };
}
