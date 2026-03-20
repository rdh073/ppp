import { useState } from 'react';
import { useDevices } from '../../hooks/useDevices';
import { getAndroidIdentity } from '../../utils/deviceIdentity';
import { ScrcpyView } from './ScrcpyView';
import type { Device } from '../../types';

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
        <span className={`${base} bg-[#22c55e] animate-ping absolute opacity-60`} />
        <span className={`${base} bg-[#22c55e] relative`} />
      </span>
    );
  if (status === 'stale') return <span className={`${base} bg-[#f59e0b]`} />;
  return <span className={`${base} bg-[#475569]`} />;
}

function StatusBadge({ status }: { status: 'online' | 'stale' | 'offline' }) {
  if (status === 'online')
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[0.65rem] font-semibold uppercase tracking-wide"
        style={{ background: 'rgba(34,197,94,0.12)', color: '#4ade80', border: '1px solid rgba(34,197,94,0.25)' }}>
        <StatusDot status="online" /> Online
      </span>
    );
  if (status === 'stale')
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[0.65rem] font-semibold uppercase tracking-wide"
        style={{ background: 'rgba(245,158,11,0.12)', color: '#fbbf24', border: '1px solid rgba(245,158,11,0.25)' }}>
        <StatusDot status="stale" /> Stale
      </span>
    );
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[0.65rem] font-semibold uppercase tracking-wide"
      style={{ background: 'rgba(71,85,105,0.18)', color: '#94a3b8', border: '1px solid rgba(71,85,105,0.3)' }}>
      <StatusDot status="offline" /> Offline
    </span>
  );
}

function CapabilityChip({ name }: { name: string }) {
  return (
    <span className="inline-flex items-center rounded-md px-1.5 py-0.5 text-[0.62rem] font-medium"
      style={{ background: 'rgba(122,162,247,0.1)', color: 'var(--accent)', border: '1px solid rgba(122,162,247,0.2)' }}>
      {name}
    </span>
  );
}

// ── device card ────────────────────────────────────────────────────────────

interface DeviceCardProps {
  device: Device;
  selected: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onScrcpy: (device: Device) => void;
}

function DeviceCard({ device, selected, onSelect, onScrcpy }: DeviceCardProps) {
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
        borderColor: selected ? 'var(--accent)' : status === 'online' ? 'rgba(34,197,94,0.2)' : 'var(--border)',
        background: selected
          ? 'linear-gradient(135deg, rgba(122,162,247,0.08), var(--surface))'
          : 'var(--surface)',
        boxShadow: selected ? '0 0 0 1px var(--accent), 0 8px 24px rgba(122,162,247,0.1)' : undefined,
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
          onClick={() => onScrcpy(device)}
          aria-label={`Open Scrcpy for ${identity}`}
        >
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <rect x="2" y="3" width="20" height="14" rx="2" />
            <path d="M8 21h8M12 17v4" />
          </svg>
          Mirror
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
  onScrcpyFirst: () => void;
}

function BulkBar({ count, onClear, onScrcpyFirst }: BulkBarProps) {
  if (count === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-xl border px-4 py-2.5 transition-all duration-200"
      style={{ borderColor: 'var(--accent)', background: 'rgba(122,162,247,0.08)' }}>
      <span className="text-sm font-semibold" style={{ color: 'var(--accent)' }}>
        {count} selected
      </span>
      <div className="flex gap-2 ml-auto">
        <button type="button" className="btn-secondary" style={{ minHeight: '30px', fontSize: '0.73rem', padding: '0.25rem 0.6rem' }}
          onClick={onScrcpyFirst}>
          Mirror first
        </button>
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
        background: view === v ? 'linear-gradient(180deg, var(--accent), var(--accent-strong))' : 'var(--surface)',
        borderColor: view === v ? 'var(--accent)' : 'var(--border-strong)',
        color: view === v ? 'var(--on-accent)' : 'var(--text)',
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

function DeviceRow({ device, selected, onSelect, onScrcpy }: DeviceCardProps) {
  const status = deviceStatus(device);
  const identity = getAndroidIdentity(device);
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
          onClick={() => onScrcpy(device)}>
          <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <rect x="2" y="3" width="20" height="14" rx="2" />
            <path d="M8 21h8M12 17v4" />
          </svg>
          Mirror
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

// ── main panel ─────────────────────────────────────────────────────────────

export function DevicePanel() {
  const { devices, loading, error, refresh } = useDevices();
  const [view, setView] = useState<'grid' | 'list'>('grid');
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [scrcpyOpen, setScrcpyOpen] = useState(false);
  const [scrcpyDeviceId, setScrcpyDeviceId] = useState<string | undefined>();
  const [scrcpyAdbSerial, setScrcpyAdbSerial] = useState<string | undefined>();

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

  function openScrcpy(device: Device) {
    setScrcpyDeviceId(device.deviceId);
    setScrcpyAdbSerial(device.adbSerial ?? undefined);
    setScrcpyOpen(true);
  }

  function handleScrcpyFirst() {
    const firstId = [...selected][0];
    const device = devices.find((d) => d.deviceId === firstId);
    if (device) openScrcpy(device);
  }

  const allSelected = filtered.length > 0 && filtered.every((d) => selected.has(d.deviceId));

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
      <BulkBar count={selected.size} onClear={() => setSelected(new Set())} onScrcpyFirst={handleScrcpyFirst} />

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
              onScrcpy={openScrcpy}
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
                  onScrcpy={openScrcpy}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* scrcpy overlay */}
      {scrcpyOpen && (
        <ScrcpyView
          onClose={() => {
            setScrcpyOpen(false);
            setScrcpyDeviceId(undefined);
            setScrcpyAdbSerial(undefined);
          }}
          deviceId={scrcpyDeviceId}
          adbSerial={scrcpyAdbSerial}
        />
      )}
    </section>
  );
}
