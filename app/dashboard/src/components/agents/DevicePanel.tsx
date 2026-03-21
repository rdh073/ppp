import { useState } from 'react';
import { useDevices } from '../../hooks/useDevices';
import { getAndroidIdentity } from '../../utils/deviceIdentity';
import { ScrcpyView } from './ScrcpyView';
import { GroupMirrorView } from './GroupMirrorView';
import { useGroupStore, type DeviceGroup } from '../../store/groups';
import { executeDeviceAction } from '../../api/devices';
import type { Device } from '../../types';

const DEFAULT_MAX_SCRCPY_SESSIONS = 6;
const MIN_MAX_SCRCPY_SESSIONS = 1;
const MAX_MAX_SCRCPY_SESSIONS = 12;
const MAX_SCRCPY_SESSIONS_STORAGE_KEY = 'ppp.dashboard.maxScrcpySessions';

// ── helpers ────────────────────────────────────────────────────────────────

function relativeTime(value: string): string {
  const diff = Date.now() - new Date(value).getTime();
  if (Number.isNaN(diff)) return value;
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function isOnline(device: Device): boolean {
  return !!device.sessionId;
}

function heartbeatAge(device: Device): number {
  return Date.now() - new Date(device.lastHeartbeatAt).getTime();
}

function deviceStatus(device: Device): 'online' | 'stale' | 'offline' {
  if (!isOnline(device)) return 'offline';
  return heartbeatAge(device) < 90_000 ? 'online' : 'stale';
}

// ── sub-components ─────────────────────────────────────────────────────────

function StatusDot({ status }: { status: 'online' | 'stale' | 'offline' }) {
  const base = 'inline-block w-2 h-2 rounded-full shrink-0';
  if (status === 'online')
    return (
      <span className="relative inline-flex items-center justify-center w-3 h-3">
        <span className={`${base} bg-[var(--state-success)] animate-ping absolute opacity-60`} />
        <span className={`${base} bg-[var(--state-success)] relative`} />
      </span>
    );
  if (status === 'stale') return <span className={`${base} bg-[var(--state-warning)]`} />;
  if (status === 'offline') return <span className={`${base} bg-[var(--state-offline)]`} />;
  return null;
}

function StatusBadge({ status }: { status: 'online' | 'stale' | 'offline' }) {
  if (status === 'online')
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[0.65rem] font-semibold uppercase tracking-wide"
        style={{ background: 'var(--state-success-bg)', color: 'var(--state-success-text)', border: '1px solid var(--state-success-border)' }}>
        <StatusDot status="online" /> Online
      </span>
    );
  if (status === 'stale')
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[0.65rem] font-semibold uppercase tracking-wide"
        style={{ background: 'var(--state-warning-bg)', color: 'var(--state-warning-text)', border: '1px solid var(--state-warning-border)' }}>
        <StatusDot status="stale" /> Stale
      </span>
    );
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[0.65rem] font-semibold uppercase tracking-wide"
      style={{ background: 'var(--state-offline-bg)', color: 'var(--state-offline-text)', border: '1px solid var(--state-offline-border)' }}>
      <StatusDot status="offline" /> Offline
    </span>
  );
}

function CapabilityChip({ name }: { name: string }) {
  return (
    <span className="capability-chip inline-flex items-center rounded-md px-1.5 py-0.5 text-[0.62rem] font-medium">
      {name}
    </span>
  );
}

// ── device card ────────────────────────────────────────────────────────────

interface DeviceCardProps {
  device: Device;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
  isScrcpyActive: boolean;
  onToggleScrcpy: (device: Device) => void;
}

function DeviceCard({ device, selected, onSelect, onToggleScrcpy, isScrcpyActive }: DeviceCardProps) {
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
          ? 'linear-gradient(135deg, rgba(122,162,247,0.08), var(--surface))'
          : 'var(--surface)',
        boxShadow: selected
          ? '0 0 0 1px var(--accent), 0 8px 24px rgba(122,162,247,0.1)'
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
        {/* device icon */}
        <div className="shrink-0 flex items-center justify-center w-10 h-10 rounded-xl"
          style={{ background: 'rgba(122,162,247,0.1)', border: '1px solid rgba(122,162,247,0.18)' }}>
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
      </div>
    </div>
  );
}

// ── summary bar ────────────────────────────────────────────────────────────

function SummaryBar({ devices }: { devices: Device[] }) {
  const online = devices.filter((d) => deviceStatus(d) === 'online').length;
  const stale = devices.filter((d) => deviceStatus(d) === 'stale').length;
  const offline = devices.filter((d) => deviceStatus(d) === 'offline').length;

  return (
    <div className="flex flex-wrap gap-3">
      <Stat label="Total" value={devices.length} />
      <Stat label="Online" value={online} color="#4ade80" />
      {stale > 0 && <Stat label="Stale" value={stale} color="#fbbf24" />}
      {offline > 0 && <Stat label="Offline" value={offline} color="#64748b" />}
    </div>
  );
}

function Stat({ label, value, color }: { label: string; value: number; color?: string }) {
  return (
    <div className="flex items-center gap-2 rounded-xl border px-3 py-1.5"
      style={{ borderColor: 'var(--border)', background: 'var(--surface)' }}>
      <span className="text-lg font-bold leading-none tabular-nums" style={{ color: color ?? 'var(--text)' }}>{value}</span>
      <span className="text-[0.68rem] uppercase tracking-wide" style={{ color: 'var(--muted)' }}>{label}</span>
    </div>
  );
}

// ── bulk action bar ────────────────────────────────────────────────────────

interface BulkBarProps {
  count: number;
  onClear: () => void;
  onScrcpyAll: () => void;
  onCreateGroup?: () => void;
}

function BulkBar({ count, onClear, onScrcpyAll, onCreateGroup }: BulkBarProps) {
  if (count === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-xl border px-4 py-2.5 transition-all duration-200"
      style={{ borderColor: 'var(--accent)', background: 'rgba(122,162,247,0.08)' }}>
      <span className="text-sm font-semibold" style={{ color: 'var(--accent)' }}>
        {count} selected
      </span>
      <div className="flex gap-2 ml-auto">
        <button type="button" className="btn-secondary" style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.6rem' }}
          onClick={onScrcpyAll}>
          Mirror selected
        </button>
        {count >= 2 && onCreateGroup && (
          <button type="button" className="btn-secondary" style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.6rem' }}
            onClick={onCreateGroup}>
            Create group
          </button>
        )}
        <button type="button" className="btn-secondary" style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.6rem' }}
          onClick={onClear}>
          Deselect all
        </button>
      </div>
    </div>
  );
}

// ── view toggle ────────────────────────────────────────────────────────────

function ViewToggle({ view, onChange }: { view: 'grid' | 'list'; onChange: (v: 'grid' | 'list') => void }) {
  const btn = (v: 'grid' | 'list', icon: React.ReactNode) => (
    <button
      type="button"
      onClick={() => onChange(v)}
      aria-pressed={view === v}
      aria-label={`${v} view`}
      style={{
        minHeight: '32px',
        padding: '0.25rem 0.5rem',
        background: view === v
          ? 'linear-gradient(180deg, var(--accent), var(--accent-strong))'
          : 'rgba(47, 128, 237, 0.08)',
        borderColor: view === v ? 'var(--accent)' : 'rgba(47, 128, 237, 0.32)',
        color: view === v ? 'var(--on-accent)' : 'var(--accent-strong)',
      }}
    >
      {icon}
    </button>
  );
  return (
    <div className="flex gap-1">
      {btn('grid',
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="3" y="3" width="7" height="7" /><rect x="14" y="3" width="7" height="7" />
          <rect x="3" y="14" width="7" height="7" /><rect x="14" y="14" width="7" height="7" />
        </svg>
      )}
      {btn('list',
        <svg viewBox="0 0 24 24" className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <line x1="3" y1="6" x2="21" y2="6" /><line x1="3" y1="12" x2="21" y2="12" /><line x1="3" y1="18" x2="21" y2="18" />
        </svg>
      )}
    </div>
  );
}

// ── list view row ──────────────────────────────────────────────────────────

function DeviceRow({ device, selected, onSelect, onToggleScrcpy, isScrcpyActive }: DeviceCardProps) {
  const status = deviceStatus(device);
  const identity = getAndroidIdentity(device);
  const actionLabel = isScrcpyActive ? 'Close Mirror' : 'Mirror';
  return (
    <tr
      className="cursor-pointer transition-colors duration-150"
      style={{ background: selected ? 'rgba(122,162,247,0.07)' : undefined }}
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
        <button type="button" className="btn-secondary gap-1.5"
          style={{ minHeight: '28px', fontSize: '0.72rem', padding: '0.2rem 0.5rem' }}
          onClick={() => onToggleScrcpy(device)}>
          <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <rect x="2" y="3" width="20" height="14" rx="2" />
            <path d="M8 21h8M12 17v4" />
          </svg>
          {actionLabel}
        </button>
      </td>
    </tr>
  );
}

// ── empty state ────────────────────────────────────────────────────────────

function EmptyState({ loading }: { loading: boolean }) {
  if (loading) return null;
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3">
      <div className="flex items-center justify-center w-14 h-14 rounded-2xl"
        style={{ background: 'rgba(122,162,247,0.07)', border: '1px solid rgba(122,162,247,0.15)' }}>
        <svg viewBox="0 0 24 24" className="w-7 h-7" fill="none" stroke="var(--muted)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
          <rect x="5" y="2" width="14" height="20" rx="2" />
          <circle cx="12" cy="17" r="1" />
        </svg>
      </div>
      <p className="text-sm font-medium" style={{ color: 'var(--muted)' }}>No active device sessions</p>
      <p className="text-xs text-center max-w-[260px]" style={{ color: 'var(--muted)', opacity: 0.7 }}>
        Start the android-agent on a device to see it appear here.
      </p>
    </div>
  );
}

// ── search ─────────────────────────────────────────────────────────────────

function filterDevices(devices: Device[], query: string): Device[] {
  const q = query.toLowerCase().trim();
  if (!q) return devices;
  return devices.filter((d) => {
    const identity = getAndroidIdentity(d).toLowerCase();
    return (
      identity.includes(q) ||
      d.deviceId.toLowerCase().includes(q) ||
      (d.adbSerial ?? '').toLowerCase().includes(q) ||
      (d.deviceMetadata?.androidVersion ?? '').toLowerCase().includes(q) ||
      d.capabilities.some((c) => c.name.toLowerCase().includes(q))
    );
  });
}

type ScrcpySession = {
  id: string;
  deviceId: string;
  adbSerial?: string;
  deviceName: string;
};

function clampScrcpySessionLimit(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_MAX_SCRCPY_SESSIONS;
  return Math.max(MIN_MAX_SCRCPY_SESSIONS, Math.min(MAX_MAX_SCRCPY_SESSIONS, value));
}

function readStoredScrcpySessionLimit(): number {
  if (typeof window === 'undefined') return DEFAULT_MAX_SCRCPY_SESSIONS;
  const raw = window.localStorage.getItem(MAX_SCRCPY_SESSIONS_STORAGE_KEY);
  if (!raw) return DEFAULT_MAX_SCRCPY_SESSIONS;
  const parsed = Number.parseInt(raw, 10);
  if (Number.isNaN(parsed)) return DEFAULT_MAX_SCRCPY_SESSIONS;
  return clampScrcpySessionLimit(parsed);
}

function capScrcpySessions(sessions: ScrcpySession[], maxSessions: number): ScrcpySession[] {
  if (sessions.length <= maxSessions) return sessions;
  return sessions.slice(sessions.length - maxSessions);
}

// ── main panel ─────────────────────────────────────────────────────────────

export function DevicePanel() {
  const { devices, loading, error, refresh } = useDevices();
  const { groups, addGroup, removeGroup } = useGroupStore();
  const [view, setView] = useState<'grid' | 'list'>('grid');
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [scrcpySessions, setScrcpySessions] = useState<ScrcpySession[]>([]);
  const [maxScrcpySessions, setMaxScrcpySessions] = useState<number>(() => readStoredScrcpySessionLimit());
  const [groupCreating, setGroupCreating] = useState(false);
  const [groupName, setGroupName] = useState('');
  const [groupMaster, setGroupMaster] = useState('');
  const [activeMirrorGroup, setActiveMirrorGroup] = useState<DeviceGroup | null>(null);

  const filtered = filterDevices(devices, search);

  function handleSelect(id: string, checked: boolean) {
    setSelected((prev) => {
      const next = new Set(prev);
      checked ? next.add(id) : next.delete(id);
      return next;
    });
  }

  function handleSelectAll(checked: boolean) {
    setSelected(checked ? new Set(filtered.map((d) => d.deviceId)) : new Set());
  }

  function updateMaxScrcpySessions(value: number) {
    const next = clampScrcpySessionLimit(value);
    setMaxScrcpySessions(next);
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(MAX_SCRCPY_SESSIONS_STORAGE_KEY, String(next));
    }
    setScrcpySessions((prev) => capScrcpySessions(prev, next));
  }

  function openScrcpy(device: Device) {
    const identity = getAndroidIdentity(device);
    setScrcpySessions((prev) => {
      if (prev.some((session) => session.deviceId === device.deviceId)) {
        return prev;
      }
      return capScrcpySessions([
        ...prev,
        {
          id: `${device.deviceId}-${Date.now()}`,
          deviceId: device.deviceId,
          adbSerial: device.adbSerial ?? undefined,
          deviceName: identity,
        },
      ], maxScrcpySessions);
    });
  }

  function closeScrcpyBySessionId(id: string) {
    setScrcpySessions((prev) => prev.filter((s) => s.id !== id));
  }

  function closeScrcpyByDeviceId(deviceId: string) {
    setScrcpySessions((prev) => prev.filter((s) => s.deviceId !== deviceId));
  }

  function handleScrcpySelected() {
    const selectedDevices = devices.filter((device) => selected.has(device.deviceId));
    if (selectedDevices.length === 0) return;
    const sessionIds = new Set(scrcpySessions.map((session) => session.deviceId));
    const now = Date.now();
    const missing = selectedDevices.filter((device) => !sessionIds.has(device.deviceId));
    setScrcpySessions((prev) => {
      if (missing.length === 0) return prev;
      const sessionsToOpen = missing.map((device, index) => ({
          id: `${device.deviceId}-${now + index}`,
          deviceId: device.deviceId,
          adbSerial: device.adbSerial ?? undefined,
          deviceName: getAndroidIdentity(device),
        }));
      return capScrcpySessions([...prev, ...sessionsToOpen], maxScrcpySessions);
    });
  }

  function toggleScrcpy(device: Device) {
    const hasSession = scrcpySessions.some((session) => session.deviceId === device.deviceId);
    if (hasSession) {
      closeScrcpyByDeviceId(device.deviceId);
    } else {
      openScrcpy(device);
    }
  }

  function startGroupCreation() {
    const selectedIds = [...selected];
    setGroupMaster(selectedIds[0]);
    setGroupName('Group ' + (groups.length + 1));
    setGroupCreating(true);
  }

  function confirmGroupCreation() {
    const slaveIds = [...selected].filter((id) => id !== groupMaster);
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
    setSelected(new Set());
  }

  function mirrorGroup(group: DeviceGroup) {
    setActiveMirrorGroup(group);
  }

  /** Returns an onTouchDevice callback for master devices that fans out taps to slaves. */
  function makeTouchFanout(deviceId: string): ((type: 'down' | 'move' | 'up', x: number, y: number) => void) | undefined {
    const group = groups.find((g) => g.masterDeviceId === deviceId);
    if (!group || group.slaveDeviceIds.length === 0) return undefined;
    return (type, x, y) => {
      if (type !== 'down') return; // fan out taps only; move/up would flood
      const value = `${x},${y}`;
      for (const slaveId of group.slaveDeviceIds) {
        void executeDeviceAction(slaveId, {
          kind: 'click',
          target: { kind: 'coordinate', value },
        }).catch(() => {});
      }
    };
  }

  const allSelected = filtered.length > 0 && filtered.every((d) => selected.has(d.deviceId));

  if (activeMirrorGroup) {
    return (
      <section className="panel">
        <GroupMirrorView
          group={activeMirrorGroup}
          devices={devices}
          onExit={() => setActiveMirrorGroup(null)}
        />
      </section>
    );
  }

  return (
    <section className="panel">
      {/* panel header */}
      <div className="panel-header">
        <div className="flex items-center gap-3">
          <h2 className="m-0">Devices</h2>
          {loading && (
            <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="var(--muted)" strokeWidth="2">
              <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
            </svg>
          )}
        </div>
        <div className="actions">
          <label
            className="inline-flex items-center gap-2 rounded-xl border px-2.5 py-1"
            style={{ borderColor: 'var(--border)', background: 'var(--surface-strong)' }}
          >
            <span className="text-[0.65rem] font-semibold uppercase tracking-[0.08em]" style={{ color: 'var(--muted)' }}>
              Max mirror
            </span>
            <input
              type="number"
              min={MIN_MAX_SCRCPY_SESSIONS}
              max={MAX_MAX_SCRCPY_SESSIONS}
              step={1}
              value={maxScrcpySessions}
              onChange={(e) => {
                const parsed = Number.parseInt(e.target.value, 10);
                if (Number.isNaN(parsed)) return;
                updateMaxScrcpySessions(parsed);
              }}
              aria-label="Maximum concurrent scrcpy sessions"
              className="w-16 rounded-lg px-2 py-1 text-center text-xs"
              style={{ minHeight: '28px' }}
            />
          </label>
          <ViewToggle view={view} onChange={setView} />
          <button type="button" onClick={() => refresh()} disabled={loading}>
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
              <path d="M3 3v5h5" />
            </svg>
            Refresh
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {/* summary + search */}
      <div className="flex flex-wrap items-center gap-3 mb-4">
        <SummaryBar devices={devices} />
        <div className="ml-auto relative">
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" stroke="var(--muted)" strokeWidth="2" strokeLinecap="round">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" />
          </svg>
          <input
            type="search"
            placeholder="Filter devices…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ width: '200px', paddingLeft: '2rem' }}
            aria-label="Filter devices"
          />
        </div>
      </div>

      {/* bulk action bar */}
      <BulkBar
        count={selected.size}
        onClear={() => setSelected(new Set())}
        onScrcpyAll={handleScrcpySelected}
        onCreateGroup={startGroupCreation}
      />

      {/* inline group creation form */}
      {groupCreating && (
        <div className="flex flex-wrap items-center gap-3 rounded-xl border px-4 py-3 mt-2"
          style={{ borderColor: 'var(--border)', background: 'var(--surface-strong)' }}>
          <span className="text-xs font-semibold uppercase tracking-wide" style={{ color: 'var(--muted)' }}>New group</span>
          <input
            type="text"
            placeholder="Group name"
            value={groupName}
            onChange={(e) => setGroupName(e.target.value)}
            style={{ width: '160px' }}
            aria-label="Group name"
          />
          <label htmlFor="group-master-select" className="flex items-center gap-2 text-xs" style={{ color: 'var(--muted)' }}>
            Master:
            <select
              id="group-master-select"
              value={groupMaster}
              onChange={(e) => setGroupMaster(e.target.value)}
              style={{ fontSize: '0.75rem', padding: '0.2rem 0.4rem' }}
            >
              {[...selected].map((id) => {
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
            <button type="button" className="topbar-link" style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.8rem' }}
              onClick={confirmGroupCreation}>
              Save group
            </button>
            <button type="button" className="btn-secondary" style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.6rem' }}
              onClick={() => setGroupCreating(false)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* groups section */}
      {groups.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-2">
          {groups.map((group) => {
            const masterDevice = devices.find((d) => d.deviceId === group.masterDeviceId);
            const masterName = masterDevice ? getAndroidIdentity(masterDevice) || group.masterDeviceId : group.masterDeviceId;
            return (
              <div key={group.id} className="flex items-center gap-2 rounded-xl border px-3 py-1.5"
                style={{ borderColor: 'var(--border)', background: 'var(--surface)', fontSize: '0.75rem' }}>
                <span className="font-semibold">{group.name}</span>
                <span className="inline-flex items-center gap-1" style={{ color: 'var(--muted)' }}>
                  {/* master crown icon */}
                  <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M2 20h20M5 20V9l7-5 7 5v11" />
                  </svg>
                  {masterName}
                </span>
                <span style={{ color: 'var(--muted)' }}>
                  {group.slaveDeviceIds.length} slave{group.slaveDeviceIds.length !== 1 ? 's' : ''}
                </span>
                <button type="button" className="btn-secondary" style={{ minHeight: '24px', fontSize: '0.68rem', padding: '0.1rem 0.5rem' }}
                  onClick={() => mirrorGroup(group)}>
                  Mirror
                </button>
                <button type="button" className="btn-danger" style={{ minHeight: '24px', fontSize: '0.68rem', padding: '0.1rem 0.4rem' }}
                  onClick={() => removeGroup(group.id)}
                  aria-label={`Delete group ${group.name}`}>
                  <svg viewBox="0 0 24 24" className="w-3 h-3" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
                    <path d="M18 6 6 18M6 6l12 12" />
                  </svg>
                </button>
              </div>
            );
          })}
        </div>
      )}

      {/* content */}
      {filtered.length === 0 ? (
        <EmptyState loading={loading} />
      ) : view === 'grid' ? (
        <div className="mt-4 grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))' }}>
          {filtered.map((device) => (
            <DeviceCard
              key={device.deviceId}
              device={device}
              selected={selected.has(device.deviceId)}
              onSelect={handleSelect}
              onToggleScrcpy={toggleScrcpy}
              isScrcpyActive={scrcpySessions.some((session) => session.deviceId === device.deviceId)}
            />
          ))}
        </div>
      ) : (
        <div className="table-wrap mt-4">
          <table>
            <thead>
              <tr>
                <th style={{ width: '36px' }}>
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={(e) => handleSelectAll(e.target.checked)}
                    style={{ accentColor: 'var(--accent)', width: '14px', height: '14px', cursor: 'pointer' }}
                    aria-label="Select all"
                  />
                </th>
                <th>Device</th>
                <th>Status</th>
                <th>ADB Serial</th>
                <th>Session</th>
                <th>Heartbeat</th>
                <th>Capabilities</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((device) => (
                <DeviceRow
                  key={device.deviceId}
                  device={device}
                  selected={selected.has(device.deviceId)}
                  onSelect={handleSelect}
                  onToggleScrcpy={toggleScrcpy}
                  isScrcpyActive={scrcpySessions.some((session) => session.deviceId === device.deviceId)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* scrcpy sessions */}
      {scrcpySessions.length > 0 && (
        <section className="mt-4">
          <div className="panel-subhead">
            <h3 className="m-0 text-sm font-semibold uppercase tracking-[0.08em]" style={{ color: 'var(--muted)' }}>
              Active scrcpy sessions ({scrcpySessions.length}/{maxScrcpySessions})
            </h3>
            <button type="button" className="btn-secondary" onClick={() => setScrcpySessions([])}>
              Close all
            </button>
          </div>
          <div className="scrcpy-grid">
            {scrcpySessions.map((session) => (
              <ScrcpyView
                key={session.id}
                sessionId={session.id}
                deviceId={session.deviceId}
                adbSerial={session.adbSerial}
                deviceName={session.deviceName}
                onClose={() => closeScrcpyBySessionId(session.id)}
                onTouchDevice={makeTouchFanout(session.deviceId)}
              />
            ))}
          </div>
        </section>
      )}
    </section>
  );
}
