import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import type { Device } from '../../../types';
import { deviceStatus, relativeTime } from './utils';
import { StatusDot, StatusBadge, CapabilityChip } from './DeviceStatusBadge';
import type { DeviceCardProps } from './DeviceCard';

export function DeviceRow({ device, selected, onSelect, onToggleScrcpy, isScrcpyActive, onRecord, isRecording }: DeviceCardProps) {
  const status = deviceStatus(device);
  const identity = getAndroidIdentity(device);
  return (
    <tr
      className="cursor-pointer transition-colors duration-150"
      style={{ background: selected ? 'rgba(34,197,94,0.06)' : undefined }}
      onClick={() => onSelect(device.deviceId, !selected)}
    >
      <td style={{ width: '36px' }}>
        <input
          type="checkbox"
          checked={selected}
          onChange={(e) => { e.stopPropagation(); onSelect(device.deviceId, e.target.checked); }}
          onClick={(e) => e.stopPropagation()}
          className="cursor-pointer"
          style={{ accentColor: 'var(--accent)', width: '14px', height: '14px' }}
          aria-label={`Select ${identity}`}
        />
      </td>
      <td>
        <div className="flex items-center gap-2">
          <StatusDot status={status} />
          <span className="font-medium truncate max-w-[160px]">{identity}</span>
        </div>
      </td>
      <td><StatusBadge status={status} /></td>
      <td className="font-mono text-[0.72rem]">{device.adbSerial || '—'}</td>
      <td className="font-mono text-[0.72rem]">{device.sessionId ? device.sessionId.slice(0, 10) + '…' : '—'}</td>
      <td>{relativeTime(device.lastHeartbeatAt)}</td>
      <td>
        <div className="flex flex-wrap gap-1">
          {device.capabilities.slice(0, 3).map((c) => <CapabilityChip key={c.name} name={c.name} />)}
          {device.capabilities.length > 3 && <span style={{ color: 'var(--muted)' }} className="text-[0.65rem]">+{device.capabilities.length - 3}</span>}
        </div>
      </td>
      <td onClick={(e) => e.stopPropagation()}>
        <div className="flex gap-1.5">
          <button type="button" className="btn-secondary gap-1.5"
            style={{ minHeight: '28px', fontSize: '0.72rem', padding: '0.2rem 0.5rem' }}
            onClick={() => onToggleScrcpy(device)}>
            <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <rect x="2" y="3" width="20" height="14" rx="2" />
              <path d="M8 21h8M12 17v4" />
            </svg>
            {isScrcpyActive ? 'Close Mirror' : 'Mirror'}
          </button>
          <button type="button" className="btn-secondary gap-1.5"
            style={{
              minHeight: '28px', fontSize: '0.72rem', padding: '0.2rem 0.5rem',
              ...(isRecording ? { borderColor: 'var(--accent)', color: 'var(--accent)' } : {}),
            }}
            onClick={() => onRecord(device)}>
            <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="8" />
              <circle cx="12" cy="12" r="3" fill="currentColor" stroke="none" />
            </svg>
            {isRecording ? 'Close Rec' : 'Record'}
          </button>
        </div>
      </td>
    </tr>
  );
}
