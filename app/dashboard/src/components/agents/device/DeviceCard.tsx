import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import type { Device } from '../../../types';
import { deviceStatus, relativeTime } from './utils';
import { StatusBadge, CapabilityChip } from './DeviceStatusBadge';

export interface DeviceCardProps {
  device: Device;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
  isScrcpyActive: boolean;
  onToggleScrcpy: (device: Device) => void;
  onRecord: (device: Device) => void;
  isRecording: boolean;
}

export function DeviceCard({ device, selected, onSelect, onToggleScrcpy, isScrcpyActive, onRecord, isRecording }: DeviceCardProps) {
  const status = deviceStatus(device);
  const identity = getAndroidIdentity(device);
  const version = device.deviceMetadata?.androidVersion
    ? `Android ${device.deviceMetadata.androidVersion}`
    : device.deviceMetadata?.sdkInt
      ? `API ${device.deviceMetadata.sdkInt}`
      : null;

  return (
    <div
      className="relative rounded-2xl border p-4 transition-all duration-200 cursor-pointer"
      style={{
        borderColor: selected
          ? 'var(--accent)'
          : status === 'online'
            ? 'var(--state-success-border)'
            : 'var(--border)',
        background: selected
          ? 'linear-gradient(135deg, rgba(34,197,94,0.08), var(--surface))'
          : 'var(--surface)',
        boxShadow: selected
          ? '0 0 0 1px var(--accent), 0 8px 24px rgba(34,197,94,0.1)'
          : undefined,
      }}
      onClick={() => onSelect(device.deviceId, !selected)}
    >
      {/* selection checkbox */}
      <input
        type="checkbox"
        checked={selected}
        onChange={(e) => { e.stopPropagation(); onSelect(device.deviceId, e.target.checked); }}
        onClick={(e) => e.stopPropagation()}
        className="absolute top-3.5 right-3.5 w-4 h-4 cursor-pointer"
        style={{ accentColor: 'var(--accent)', width: '1rem', height: '1rem' }}
        aria-label={`Select ${identity}`}
      />

      {/* header */}
      <div className="flex items-start gap-3 pr-6 mb-3">
        <div className="shrink-0 flex items-center justify-center w-10 h-10 rounded-xl"
          style={{ background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.18)' }}>
          <svg viewBox="0 0 24 24" className="w-5 h-5" fill="none" stroke="var(--accent)" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
            <rect x="5" y="2" width="14" height="20" rx="2" />
            <circle cx="12" cy="17" r="1" />
          </svg>
        </div>
        <div className="min-w-0">
          <div className="font-semibold text-sm leading-tight truncate" style={{ color: 'var(--text)' }}>{identity}</div>
          {version && <div className="text-[0.7rem] mt-0.5" style={{ color: 'var(--muted)' }}>{version}</div>}
        </div>
      </div>

      <StatusBadge status={status} />

      {/* meta rows */}
      <dl className="mt-3 grid gap-1.5">
        {device.adbSerial && (
          <div className="flex items-center gap-2">
            <dt className="text-[0.65rem] uppercase tracking-wide w-16 shrink-0" style={{ color: 'var(--muted)' }}>ADB</dt>
            <dd className="text-[0.72rem] font-mono truncate" style={{ color: 'var(--text)' }}>{device.adbSerial}</dd>
          </div>
        )}
        {device.sessionId && (
          <div className="flex items-center gap-2">
            <dt className="text-[0.65rem] uppercase tracking-wide w-16 shrink-0" style={{ color: 'var(--muted)' }}>Session</dt>
            <dd className="text-[0.72rem] font-mono truncate" style={{ color: 'var(--text)' }}>{device.sessionId.slice(0, 12)}…</dd>
          </div>
        )}
        <div className="flex items-center gap-2">
          <dt className="text-[0.65rem] uppercase tracking-wide w-16 shrink-0" style={{ color: 'var(--muted)' }}>Heartbeat</dt>
          <dd className="text-[0.72rem]" style={{ color: status === 'stale' ? '#fbbf24' : 'var(--muted)' }}>
            {relativeTime(device.lastHeartbeatAt)}
          </dd>
        </div>
      </dl>

      {/* capabilities */}
      {device.capabilities.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1">
          {device.capabilities.slice(0, 4).map((c) => (
            <CapabilityChip key={c.name} name={c.name} />
          ))}
          {device.capabilities.length > 4 && (
            <span className="text-[0.62rem]" style={{ color: 'var(--muted)' }}>+{device.capabilities.length - 4}</span>
          )}
        </div>
      )}

      {/* actions */}
      <div className="mt-4 flex gap-2" onClick={(e) => e.stopPropagation()}>
        <button
          type="button"
          className="btn-secondary flex-1 gap-1.5"
          style={{ minHeight: '32px', fontSize: '0.73rem', padding: '0.3rem 0.6rem' }}
          onClick={() => onToggleScrcpy(device)}
          aria-label={`${isScrcpyActive ? 'Close' : 'Open'} Scrcpy for ${identity}`}
          data-active={isScrcpyActive || undefined}
        >
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <rect x="2" y="3" width="20" height="14" rx="2" />
            <path d="M8 21h8M12 17v4" />
          </svg>
          {isScrcpyActive ? 'Close Mirror' : 'Mirror'}
        </button>
        <button
          type="button"
          className="btn-secondary flex-1 gap-1.5"
          style={{
            minHeight: '32px', fontSize: '0.73rem', padding: '0.3rem 0.6rem',
            ...(isRecording ? {
              background: 'linear-gradient(135deg, rgba(34,197,94,0.18), rgba(8,15,31,0.92))',
              borderColor: 'var(--accent)',
              color: 'var(--accent)',
            } : {}),
          }}
          onClick={() => onRecord(device)}
          aria-label={`${isRecording ? 'Close' : 'Open'} recorder for ${identity}`}
          data-active={isRecording || undefined}
        >
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="8" />
            <circle cx="12" cy="12" r="3" fill="currentColor" stroke="none" />
          </svg>
          {isRecording ? 'Close Rec' : 'Record'}
        </button>
      </div>
    </div>
  );
}
