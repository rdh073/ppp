export interface DeviceBulkBarProps {
  count: number;
  onClear: () => void;
  onScrcpyAll: () => void;
  onCreateGroup?: () => void;
}

export function DeviceBulkBar({ count, onClear, onScrcpyAll, onCreateGroup }: DeviceBulkBarProps) {
  if (count === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-xl border px-4 py-2.5 transition-all duration-200"
      style={{ borderColor: 'var(--accent)', background: 'rgba(34,197,94,0.07)' }}>
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
