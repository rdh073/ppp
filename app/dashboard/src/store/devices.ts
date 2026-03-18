import { create } from 'zustand';
import type { Device } from '../types';

interface DeviceState {
  devices: Device[];
  loading: boolean;
  error: string | null;
  setDevices: (devices: Device[]) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
}

export const useDeviceStore = create<DeviceState>((set) => ({
  devices: [],
  loading: false,
  error: null,
  setDevices: (devices) =>
    set({
      devices,
      loading: false,
      error: null,
    }),
  setLoading: (loading) => set({ loading }),
  setError: (error) =>
    set({
      loading: false,
      error,
    }),
}));
