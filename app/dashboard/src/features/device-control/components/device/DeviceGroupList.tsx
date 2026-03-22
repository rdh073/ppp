import type { DeviceGroup } from '../../store/groups';

interface DeviceGroupListProps {
  groups: DeviceGroup[];
  onMirrorGroup: (group: DeviceGroup) => void;
  onRemoveGroup: (id: string) => void;
}

export function DeviceGroupList({ groups, onMirrorGroup, onRemoveGroup }: DeviceGroupListProps) {
  if (groups.length === 0) return null;
  return (
    <div className="mt-3 flex flex-wrap gap-2">
      {groups.map((group) => (
        <div
          key={group.id}
          className="flex items-center gap-2 rounded-xl border px-3 py-1.5"
          style={{ borderColor: 'var(--border)', background: 'var(--surface)' }}
        >
          <span className="text-xs font-medium" style={{ color: 'var(--text)' }}>
            {group.name}
          </span>
          <span className="text-[0.65rem]" style={{ color: 'var(--muted)' }}>
            {1 + group.slaveDeviceIds.length} devices
          </span>
          <button
            type="button"
            className="btn-secondary"
            style={{ minHeight: '22px', fontSize: '0.65rem', padding: '0.1rem 0.4rem' }}
            onClick={() => onMirrorGroup(group)}
          >
            Mirror
          </button>
          <button
            type="button"
            className="btn-secondary"
            style={{ minHeight: '22px', fontSize: '0.65rem', padding: '0.1rem 0.4rem' }}
            onClick={() => onRemoveGroup(group.id)}
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}
