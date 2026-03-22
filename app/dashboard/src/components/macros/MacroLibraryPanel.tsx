import { useCallback, useEffect, useState } from 'react';
import { listMacros, deleteMacro, promoteMacro, type SavedMacro } from '../../api/macros';
import { CopyButton } from '../ui/CopyButton';

// ── macro detail ──────────────────────────────────────────────────────────────

function MacroDetail({ macro, onDelete }: { macro: SavedMacro; onDelete: () => void }) {
  const [deleting, setDeleting] = useState(false);
  const [promoting, setPromoting] = useState(false);
  const [promoted, setPromoted] = useState<string | null>(null);
  const [promoteError, setPromoteError] = useState('');

  async function handleDelete() {
    setDeleting(true);
    try {
      await deleteMacro(macro.id);
      onDelete();
    } catch {
      setDeleting(false);
    }
  }

  async function handlePromote() {
    setPromoting(true);
    setPromoteError('');
    setPromoted(null);
    try {
      const result = await promoteMacro(macro.id, macro.workflowName);
      setPromoted(result.workflowName);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Promote failed';
      setPromoteError(msg.includes('501') ? 'Workflow directory not configured on server' : msg);
    } finally {
      setPromoting(false);
    }
  }

  return (
    <div className="grid gap-3" style={{ marginTop: '0.5rem' }}>
      {/* code block */}
      <div className="relative">
        <div className="absolute top-2 right-2 z-10">
          <CopyButton text={macro.script} />
        </div>
        <pre
          style={{
            margin: 0,
            padding: '0.75rem',
            paddingTop: '2.5rem',
            background: 'rgba(0,0,0,0.88)',
            border: '1px solid var(--border)',
            borderRadius: '0.75rem',
            fontFamily: 'Fira Code, ui-monospace, monospace',
            fontSize: '0.7rem',
            lineHeight: 1.65,
            color: '#4ade80',
            maxHeight: '280px',
            overflow: 'auto',
            whiteSpace: 'pre',
            overflowWrap: 'normal',
          }}
        >
          {macro.script}
        </pre>
      </div>

      {/* promote feedback */}
      {promoted && (
        <p style={{ margin: 0, fontSize: '0.73rem', color: '#4ade80' }}>
          Workflow &ldquo;{promoted}&rdquo; saved — visible in Workflows tab.
        </p>
      )}
      {promoteError && (
        <p style={{ margin: 0, fontSize: '0.73rem', color: 'var(--error)' }}>{promoteError}</p>
      )}

      {/* actions */}
      <div className="flex justify-between items-center gap-2">
        <button
          type="button"
          className="btn-secondary"
          disabled={promoting || promoted !== null}
          onClick={handlePromote}
          style={{ fontSize: '0.73rem', gap: '0.35rem' }}
        >
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 12h14M12 5l7 7-7 7" />
          </svg>
          {promoting ? 'Promoting…' : promoted ? 'Promoted' : 'Promote to Workflow'}
        </button>

        <button
          type="button"
          className="btn-secondary"
          disabled={deleting}
          onClick={handleDelete}
          style={{ fontSize: '0.73rem', gap: '0.35rem', color: 'var(--error)' }}
        >
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6" />
          </svg>
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
  );
}

// ── macro row ─────────────────────────────────────────────────────────────────

function MacroRow({ macro, onDelete }: { macro: SavedMacro; onDelete: () => void }) {
  const [expanded, setExpanded] = useState(false);

  const date = new Date(macro.createdAt);
  const dateStr = date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  const timeStr = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });

  return (
    <div
      className="rounded-xl border"
      style={{ borderColor: expanded ? 'rgba(34,197,94,0.3)' : 'var(--border)', background: 'var(--surface)', transition: 'border-color 200ms ease' }}
    >
      {/* header row */}
      <button
        type="button"
        className="w-full flex items-center gap-3 px-4 py-3 text-left cursor-pointer"
        style={{ background: 'transparent', border: 'none', borderRadius: 'inherit' }}
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        {/* source badge */}
        <span
          className="shrink-0 text-[0.62rem] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full"
          style={{
            background: macro.source === 'ai' ? 'rgba(34,197,94,0.12)' : 'rgba(99,102,241,0.12)',
            color: macro.source === 'ai' ? '#4ade80' : '#a5b4fc',
            border: `1px solid ${macro.source === 'ai' ? 'rgba(34,197,94,0.25)' : 'rgba(99,102,241,0.25)'}`,
          }}
        >
          {macro.source === 'ai' ? 'AI' : 'Manual'}
        </span>

        {/* name + device */}
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold truncate" style={{ color: 'var(--text)' }}>{macro.workflowName}</div>
          <div className="text-[0.68rem] font-mono truncate" style={{ color: 'var(--muted)' }}>{macro.deviceId}</div>
        </div>

        {/* stats */}
        <div className="shrink-0 flex items-center gap-3 text-[0.68rem]" style={{ color: 'var(--muted)' }}>
          <span>{macro.actionCount} actions</span>
          {macro.steps !== undefined && <span>{macro.steps} steps</span>}
          {macro.durationMs !== undefined && <span>{(macro.durationMs / 1000).toFixed(1)}s</span>}
          {macro.done !== undefined && (
            <span style={{ color: macro.done ? '#4ade80' : '#fbbf24', fontWeight: 600 }}>
              {macro.done ? '✓ done' : 'partial'}
            </span>
          )}
          <span style={{ color: 'var(--muted)', opacity: 0.7 }}>{dateStr} {timeStr}</span>
        </div>

        {/* chevron */}
        <svg
          viewBox="0 0 24 24"
          className="w-4 h-4 shrink-0"
          fill="none"
          stroke="var(--muted)"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          style={{ transform: expanded ? 'rotate(180deg)' : 'rotate(0)', transition: 'transform 200ms ease' }}
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>

      {/* expanded detail */}
      {expanded && (
        <div className="px-4 pb-4 border-t" style={{ borderColor: 'var(--border)' }}>
          {macro.reason && (
            <p style={{ margin: '0.75rem 0 0', fontSize: '0.78rem', color: 'var(--muted)', fontStyle: 'italic', lineHeight: 1.6 }}>
              &ldquo;{macro.reason}&rdquo;
            </p>
          )}
          <MacroDetail macro={macro} onDelete={onDelete} />
        </div>
      )}
    </div>
  );
}

// ── main panel ────────────────────────────────────────────────────────────────

export function MacroLibraryPanel() {
  const [macros, setMacros] = useState<SavedMacro[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setMacros(await listMacros());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load macros');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  function handleDelete(id: string) {
    setMacros((prev) => prev.filter((m) => m.id !== id));
  }

  return (
    <section className="panel">
      <div className="panel-header">
        <div className="flex items-center gap-3">
          <h2 className="m-0">Macro Library</h2>
          {loading && (
            <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="var(--muted)" strokeWidth="2">
              <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
            </svg>
          )}
        </div>
        <div className="actions">
          <button type="button" onClick={load} disabled={loading}>
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
              <path d="M3 3v5h5" />
            </svg>
            Refresh
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {!loading && macros.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 gap-3">
          <div
            className="flex items-center justify-center w-14 h-14 rounded-2xl"
            style={{ background: 'rgba(34,197,94,0.07)', border: '1px solid rgba(34,197,94,0.15)' }}
          >
            <svg viewBox="0 0 24 24" className="w-7 h-7" fill="none" stroke="var(--muted)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" /><circle cx="12" cy="12" r="3" />
            </svg>
          </div>
          <p className="text-sm font-medium" style={{ color: 'var(--muted)' }}>No macros yet</p>
          <p className="text-xs text-center max-w-[260px]" style={{ color: 'var(--muted)', opacity: 0.7 }}>
            Record a macro or run AI automation on a device — results will appear here.
          </p>
        </div>
      )}

      <div className="grid gap-2 mt-2">
        {macros.map((macro) => (
          <MacroRow key={macro.id} macro={macro} onDelete={() => handleDelete(macro.id)} />
        ))}
      </div>
    </section>
  );
}
