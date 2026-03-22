import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { createPersistOptions } from '../../../shared/state/helpers';

export interface DeviceGroup {
  id: string;
  name: string;
  masterDeviceId: string;
  slaveDeviceIds: string[];
}

interface GroupState {
  groups: DeviceGroup[];
  addGroup: (group: DeviceGroup) => void;
  removeGroup: (id: string) => void;
}

export const useGroupStore = create<GroupState>()(
  persist(
    (set) => ({
      groups: [],
      addGroup: (group) => set((state) => ({ groups: [...state.groups, group] })),
      removeGroup: (id) => set((state) => ({ groups: state.groups.filter((g) => g.id !== id) })),
    }),
    createPersistOptions<GroupState>('ppp.dashboard.deviceGroups'),
  ),
);
