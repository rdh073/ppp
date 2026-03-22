interface Props {
  active: boolean;
  onToggle: () => void;
  loading: boolean;
  onRefresh: () => void;
  autoRefresh: boolean;
  onToggleAutoRefresh: () => void;
  snapshotInfo: {
    packageName: string | null;
    targetCount: number;
  } | null;
}

export function InspectorToolbar({
  active,
  onToggle,
  loading,
  onRefresh,
  autoRefresh,
  onToggleAutoRefresh,
  snapshotInfo,
}: Props) {
  return (
    <div className="flex items-center gap-2">
      {/* Inspector toggle */}
      <button
        type="button"
        className="btn-secondary"
        onClick={onToggle}
        title={active ? 'Close Inspector' : 'Open Inspector'}
        style={{
          gap: '0.3rem',
          fontSize: '0.73rem',
          ...(active
            ? { color: '#4ade80', borderColor: 'rgba(34,197,94,0.4)', background: 'rgba(34,197,94,0.08)' }
            : {}),
        }}
      >
        {/* crosshair / scan icon */}
        <svg
          viewBox="0 0 24 24"
          className="w-3.5 h-3.5 shrink-0"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M3 7V5a2 2 0 0 1 2-2h2" />
          <path d="M17 3h2a2 2 0 0 1 2 2v2" />
          <path d="M21 17v2a2 2 0 0 1-2 2h-2" />
          <path d="M7 21H5a2 2 0 0 1-2-2v-2" />
          <circle cx="12" cy="12" r="1" />
          <path d="M12 9v-1" />
          <path d="M12 16v1" />
          <path d="M9 12H8" />
          <path d="M16 12h1" />
        </svg>
        Inspect
      </button>

      {active && (
        <>
          {/* Refresh button */}
          <button
            type="button"
            className="btn-secondary"
            onClick={onRefresh}
            disabled={loading}
            style={{ minHeight: 26, padding: '0.15rem 0.45rem', fontSize: '0.68rem', gap: '0.25rem' }}
            title="Refresh snapshot"
          >
            <svg
              viewBox="0 0 24 24"
              className={`w-3 h-3 shrink-0${loading ? ' animate-spin' : ''}`}
              fill="none"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
              <path d="M3 3v5h5" />
            </svg>
          </button>

          {/* Auto-refresh toggle */}
          <label
            className="flex items-center gap-1 cursor-pointer"
            style={{ fontSize: '0.68rem', color: 'var(--muted)', userSelect: 'none' }}
          >
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={onToggleAutoRefresh}
              style={{ width: 12, height: 12, accentColor: '#4ade80' }}
            />
            Auto
          </label>

          {/* Snapshot info badge */}
          {snapshotInfo && (
            <span
              style={{
                fontSize: '0.62rem',
                color: 'var(--muted)',
                padding: '0.1rem 0.4rem',
                borderRadius: 4,
                background: 'rgba(255,255,255,0.04)',
                border: '1px solid rgba(255,255,255,0.06)',
                whiteSpace: 'nowrap',
              }}
            >
              {snapshotInfo.packageName ?? '—'} · {snapshotInfo.targetCount} elements
            </span>
          )}
        </>
      )}
    </div>
  );
}
