import { useState } from 'react';
import { useGroupStore, type DeviceGroup } from '../store/groups';
import { executeDeviceAction } from '../api/devices';

export interface GroupManagement {
  groups: DeviceGroup[];
  removeGroup: (id: string) => void;
  activeMirrorGroup: DeviceGroup | null;
  setActiveMirrorGroup: (group: DeviceGroup | null) => void;
  groupCreating: boolean;
  groupName: string;
  groupMaster: string;
  setGroupName: (name: string) => void;
  setGroupMaster: (id: string) => void;
  startGroupCreation: (selectedIds: string[]) => void;
  confirmGroupCreation: (selectedIds: string[]) => void;
  cancelGroupCreation: () => void;
  makeTouchFanout: (deviceId: string) => ((type: 'down' | 'move' | 'up', x: number, y: number) => void) | undefined;
}

export function useGroupManagement(clearSelection: () => void): GroupManagement {
  const { groups, addGroup, removeGroup } = useGroupStore();
  const [activeMirrorGroup, setActiveMirrorGroup] = useState<DeviceGroup | null>(null);
  const [groupCreating, setGroupCreating] = useState(false);
  const [groupName, setGroupName] = useState('');
  const [groupMaster, setGroupMaster] = useState('');

  function startGroupCreation(selectedIds: string[]) {
    setGroupMaster(selectedIds[0]);
    setGroupName('Group ' + (groups.length + 1));
    setGroupCreating(true);
  }

  function confirmGroupCreation(selectedIds: string[]) {
    const slaveIds = selectedIds.filter((id) => id !== groupMaster);
    if (!groupMaster || slaveIds.length === 0) return;
    addGroup({
      id: `group-${Date.now()}`,
      name: groupName.trim() || 'Group ' + (groups.length + 1),
      masterDeviceId: groupMaster,
      slaveDeviceIds: slaveIds,
    });
    setGroupCreating(false);
    setGroupName('');
    setGroupMaster('');
    clearSelection();
  }

  function cancelGroupCreation() {
    setGroupCreating(false);
    setGroupName('');
    setGroupMaster('');
  }

  /** Fan out taps from master device to all slave devices in its group. */
  function makeTouchFanout(deviceId: string): ((type: 'down' | 'move' | 'up', x: number, y: number) => void) | undefined {
    const group = groups.find((g) => g.masterDeviceId === deviceId);
    if (!group || group.slaveDeviceIds.length === 0) return undefined;
    return (type, x, y) => {
      if (type !== 'down') return;
      const value = `${x},${y}`;
      for (const slaveId of group.slaveDeviceIds) {
        void executeDeviceAction(slaveId, { kind: 'click', target: { kind: 'coordinate', value } }).catch(() => {});
      }
    };
  }

  return {
    groups,
    removeGroup,
    activeMirrorGroup,
    setActiveMirrorGroup,
    groupCreating,
    groupName,
    groupMaster,
    setGroupName,
    setGroupMaster,
    startGroupCreation,
    confirmGroupCreation,
    cancelGroupCreation,
    makeTouchFanout,
  };
}
