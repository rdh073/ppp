// StatusDot, StatusBadge, CapabilityChip — shared display atoms

export function StatusDot({ status }: { status: 'online' | 'stale' | 'offline' }) {
  const base = 'inline-block w-2 h-2 rounded-full shrink-0';
  if (status === 'online')
    return (
      <span className="relative inline-flex items-center justify-center w-3 h-3">
        <span className={`${base} bg-[var(--state-success)] animate-ping absolute opacity-60`} />
        <span className={`${base} bg-[var(--state-success)] relative`} />
      </span>
    );
  if (status === 'stale') return <span className={`${base} bg-yellow-400`} />;
  return <span className={`${base} bg-[var(--muted)]`} />;
}

export function StatusBadge({ status }: { status: 'online' | 'stale' | 'offline' }) {
  const map = {
    online: { label: 'Online', color: 'var(--state-success)', border: 'var(--state-success-border)', bg: 'rgba(34,197,94,0.07)' },
    stale: { label: 'Stale', color: '#fbbf24', border: 'rgba(251,191,36,0.3)', bg: 'rgba(251,191,36,0.07)' },
    offline: { label: 'Offline', color: 'var(--muted)', border: 'var(--border)', bg: 'transparent' },
  };
  const { label, color, border, bg } = map[status];
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5"
      style={{ borderColor: border, background: bg }}>
      <StatusDot status={status} />
      <span className="text-[0.68rem] font-semibold uppercase tracking-[0.06em]" style={{ color }}>{label}</span>
    </span>
  );
}

export function CapabilityChip({ name }: { name: string }) {
  return (
    <span
      className="inline-block rounded-full border px-2 py-0.5 text-[0.63rem] font-medium"
      style={{
        background: 'var(--capability-chip-bg)',
        color: 'var(--capability-chip-text)',
        borderColor: 'var(--capability-chip-border)',
      }}
    >
      {name}
    </span>
  );
}
