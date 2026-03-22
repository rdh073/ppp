export function DeviceViewToggle({ view, onChange }: { view: 'grid' | 'list'; onChange: (v: 'grid' | 'list') => void }) {
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
          : 'rgba(34, 197, 94, 0.08)',
        borderColor: view === v ? 'var(--accent)' : 'rgba(34, 197, 94, 0.32)',
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
