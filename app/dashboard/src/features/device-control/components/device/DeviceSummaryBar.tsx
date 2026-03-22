import type { Device } from '../../../../types';
import { deviceStatus } from './utils';

function Stat({ label, value, color }: { label: string; value: number; color?: string }) {
  return (
    <div className="flex items-center gap-2 rounded-xl border px-3 py-1.5"
      style={{ borderColor: 'var(--border)', background: 'var(--surface)' }}>
      <span className="text-lg font-bold leading-none tabular-nums" style={{ color: color ?? 'var(--text)' }}>{value}</span>
      <span className="text-[0.68rem] uppercase tracking-wide" style={{ color: 'var(--muted)' }}>{label}</span>
    </div>
  );
}

export function DeviceSummaryBar({ devices }: { devices: Device[] }) {
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
