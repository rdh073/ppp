import { useState } from 'react';
import type { Device } from '../types';

export interface DeviceSelection {
  selected: Set<string>;
  allSelected: boolean;
  handleSelect: (id: string, checked: boolean) => void;
  handleSelectAll: (checked: boolean, devices: Device[]) => void;
  clearSelection: () => void;
}

export function useDeviceSelection(filteredDevices: Device[]): DeviceSelection {
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const allSelected = filteredDevices.length > 0 && filteredDevices.every((d) => selected.has(d.deviceId));

  function handleSelect(id: string, checked: boolean) {
    setSelected((prev) => {
      const next = new Set(prev);
      checked ? next.add(id) : next.delete(id);
      return next;
    });
  }

  function handleSelectAll(checked: boolean, devices: Device[]) {
    setSelected(checked ? new Set(devices.map((d) => d.deviceId)) : new Set());
  }

  function clearSelection() {
    setSelected(new Set());
  }

  return { selected, allSelected, handleSelect, handleSelectAll, clearSelection };
}
