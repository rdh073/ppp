import { useEffect, useState } from 'react';
import { useRecordingSession } from '../hooks/useRecordingSession';
import type { RunState } from '../hooks/useRecordingSession';
import { ManualMode } from './record/ManualMode';
import { AIMode } from './record/AIMode';
import { ResultBlock } from './record/ResultBlock';

type Mode = 'manual' | 'ai';

// ── mode tab atom ────────────────────────────────────────────────────────────

function ModeTab({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        background: active ? 'linear-gradient(180deg, var(--accent), var(--accent-strong))' : 'var(--surface)',
        color: active ? 'var(--on-accent)' : 'var(--muted)',
        borderColor: active ? 'var(--accent)' : 'var(--border)',
        flex: 1,
        minHeight: '34px',
        fontSize: '0.78rem',
        fontWeight: 600,
        gap: '0.4rem',
        borderRadius: '0.5rem',
        transition: 'all 180ms ease',
      }}
    >
      {children}
    </button>
  );
}

// ── main panel ──────────────────────────────────────────────────────────────

interface RecordMacroPanelProps {
  deviceId: string;
  deviceLabel: string;
  onClose: () => void;
  onRunStateChange?: (state: RunState) => void;
}

export function RecordMacroPanel({ deviceId, deviceLabel, onClose, onRunStateChange }: RecordMacroPanelProps) {
  const [mode, setMode] = useState<Mode>('manual');
  const { runState, seq, output, error, startManual, stopManual, runAI, cancel, reset } = useRecordingSession(deviceId);

  useEffect(() => {
    onRunStateChange?.(runState);
  }, [runState, onRunStateChange]);

  const handleModeChange = (m: Mode) => {
    if (runState === 'recording' || runState === 'running') return;
    setMode(m);
  };

  return (
    <div
      className="rounded-2xl border"
      style={{
        borderColor: 'var(--border-strong)',
        background: 'var(--surface-strong)',
        boxShadow: '0 24px 64px rgba(0,0,0,0.15)',
        overflow: 'hidden',
      }}
    >
      {/* header */}
      <div className="flex items-center gap-3 px-4 py-3 border-b" style={{ borderColor: 'var(--border)' }}>
        <div
          className="flex items-center justify-center w-8 h-8 rounded-lg shrink-0"
          style={{ background: 'rgba(34,197,94,0.12)', border: '1px solid rgba(34,197,94,0.25)' }}
        >
          <svg viewBox="0 0 24 24" className="w-4 h-4" fill="none" stroke="#4ade80" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="10" /><circle cx="12" cy="12" r="3" fill="#4ade80" stroke="none" />
          </svg>
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold leading-tight" style={{ color: 'var(--text)' }}>Record Macro</div>
          <div className="text-[0.68rem] font-mono truncate" style={{ color: 'var(--muted)' }}>{deviceId}</div>
        </div>
        <span
          className="text-[0.68rem] font-medium px-2 py-0.5 rounded-full border truncate max-w-[120px]"
          style={{ borderColor: 'var(--border)', color: 'var(--muted)', background: 'rgba(255,255,255,0.02)' }}
        >
          {deviceLabel}
        </span>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="btn-secondary shrink-0"
          style={{ padding: '0.25rem', minHeight: '28px', minWidth: '28px' }}
        >
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M18 6 6 18M6 6l12 12" />
          </svg>
        </button>
      </div>

      <div className="p-4 grid gap-4">
        {/* mode tabs */}
        <div className="flex gap-1.5 rounded-xl border p-1" style={{ borderColor: 'var(--border)', background: 'var(--surface)' }}>
          <ModeTab active={mode === 'manual'} onClick={() => handleModeChange('manual')}>
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" /><circle cx="12" cy="12" r="3" />
            </svg>
            Manual
          </ModeTab>
          <ModeTab active={mode === 'ai'} onClick={() => handleModeChange('ai')}>
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 2a10 10 0 1 0 10 10" /><path d="M12 6v6l4 2" />
            </svg>
            AI-Driven
          </ModeTab>
        </div>

        {/* error */}
        {error && (
          <div
            className="flex items-start gap-2 rounded-xl border px-3 py-2.5"
            style={{ borderColor: 'rgba(239,68,68,0.3)', background: 'rgba(239,68,68,0.08)' }}
          >
            <svg viewBox="0 0 24 24" className="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="#f87171" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" /><path d="M12 8v4m0 4h.01" />
            </svg>
            <span style={{ fontSize: '0.78rem', color: '#fca5a5', lineHeight: 1.5 }}>{error}</span>
          </div>
        )}

        {/* active mode */}
        {mode === 'manual' ? (
          <ManualMode runState={runState} seq={seq} onStart={startManual} onStop={stopManual} />
        ) : (
          <AIMode runState={runState} onRun={runAI} onCancel={cancel} />
        )}

        {/* result */}
        {output && <ResultBlock output={output} />}

        {/* record again */}
        {(runState === 'done' || runState === 'error') && output && (
          <button type="button" className="btn-secondary" onClick={reset} style={{ gap: '0.4rem', fontSize: '0.78rem' }}>
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" />
            </svg>
            Record again
          </button>
        )}
      </div>
    </div>
  );
}
