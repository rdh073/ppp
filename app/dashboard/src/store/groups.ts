import { create } from 'zustand';

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

const STORAGE_KEY = 'ppp.dashboard.deviceGroups';

function loadGroups(): DeviceGroup[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as DeviceGroup[]) : [];
  } catch {
    return [];
  }
}

function saveGroups(groups: DeviceGroup[]): void {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(groups));
}

export const useGroupStore = create<GroupState>((set) => ({
  groups: loadGroups(),
  addGroup: (group) =>
    set((state) => {
      const next = [...state.groups, group];
      saveGroups(next);
      return { groups: next };
    }),
  removeGroup: (id) =>
    set((state) => {
      const next = state.groups.filter((g) => g.id !== id);
      saveGroups(next);
      return { groups: next };
    }),
}));
