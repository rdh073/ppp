import { create } from 'zustand';
import type { Device } from '../types';
import { createLoadableActions } from './helpers';

interface DeviceState {
  devices: Device[];
  loading: boolean;
  error: string | null;
  setDevices: (devices: Device[]) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
}

export const useDeviceStore = create<DeviceState>((set) => ({
  ...createLoadableActions<DeviceState>(set),
  devices: [],
  loading: false,
  error: null,
  setDevices: (devices) =>
    set({
      devices,
      loading: false,
      error: null,
    }),
}));
