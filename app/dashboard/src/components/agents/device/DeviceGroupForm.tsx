import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import type { Device } from '../../../types';

interface DeviceGroupFormProps {
  groupName: string;
  groupMaster: string;
  selectedIds: string[];
  devices: Device[];
  onGroupNameChange: (name: string) => void;
  onGroupMasterChange: (id: string) => void;
  onConfirm: () => void;
  onCancel: () => void;
}

export function DeviceGroupForm({
  groupName,
  groupMaster,
  selectedIds,
  devices,
  onGroupNameChange,
  onGroupMasterChange,
  onConfirm,
  onCancel,
}: DeviceGroupFormProps) {
  return (
    <div
      className="flex flex-wrap items-center gap-3 rounded-xl border px-4 py-3 mt-2"
      style={{ borderColor: 'var(--border)', background: 'var(--surface-strong)' }}
    >
      <span className="text-xs font-semibold uppercase tracking-wide" style={{ color: 'var(--muted)' }}>
        New group
      </span>
      <input
        type="text"
        placeholder="Group name"
        value={groupName}
        onChange={(e) => onGroupNameChange(e.target.value)}
        style={{ width: '160px' }}
        aria-label="Group name"
      />
      <label htmlFor="group-master-select" className="flex items-center gap-2 text-xs" style={{ color: 'var(--muted)' }}>
        Master:
        <select
          id="group-master-select"
          value={groupMaster}
          onChange={(e) => onGroupMasterChange(e.target.value)}
          style={{ fontSize: '0.75rem', padding: '0.2rem 0.4rem' }}
        >
          {selectedIds.map((id) => {
            const d = devices.find((x) => x.deviceId === id);
            return (
              <option key={id} value={id}>
                {d ? getAndroidIdentity(d) || id : id}
              </option>
            );
          })}
        </select>
      </label>
      <div className="flex gap-2 ml-auto">
        <button
          type="button"
          className="topbar-link"
          style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.8rem' }}
          onClick={onConfirm}
        >
          Save group
        </button>
        <button
          type="button"
          className="btn-secondary"
          style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.6rem' }}
          onClick={onCancel}
        >
          Cancel
        </button>
      </div>
    </div>
  );
}
