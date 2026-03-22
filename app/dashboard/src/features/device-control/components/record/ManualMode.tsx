import { useState } from 'react';
import { ElapsedTimer } from '../../../../components/ui/ElapsedTimer';
import type { RunState } from '../../hooks/useRecordingSession';

interface ManualModeProps {
  runState: RunState;
  seq: number;
  onStart: (workflowName?: string) => void;
  onStop: (workflowName?: string) => void;
}

export function ManualMode({ runState, seq, onStart, onStop }: ManualModeProps) {
  const [workflowName, setWorkflowName] = useState('');
  const isRecording = runState === 'recording';

  return (
    <div className="grid gap-3">
      {/* status indicator */}
      <div
        className="flex items-center gap-3 rounded-xl border px-3 py-2.5"
        style={{
          borderColor: isRecording ? 'rgba(239,68,68,0.35)' : 'var(--border)',
          background: isRecording ? 'rgba(239,68,68,0.07)' : 'var(--surface)',
          transition: 'all 200ms ease',
        }}
      >
        <div className="relative flex items-center justify-center" style={{ width: '10px', height: '10px' }}>
          {isRecording && (
            <span
              className="absolute inline-block rounded-full"
              style={{ width: '10px', height: '10px', background: '#ef4444', opacity: 0.5, animation: 'badge-pulse 1.2s ease-in-out infinite' }}
            />
          )}
          <span
            className="relative inline-block rounded-full"
            style={{ width: '8px', height: '8px', background: isRecording ? '#ef4444' : '#475569', flexShrink: 0 }}
          />
        </div>
        <span style={{ fontSize: '0.78rem', fontWeight: 600, color: isRecording ? '#fca5a5' : 'var(--muted)' }}>
          {isRecording ? 'Recording…' : 'Ready to record'}
        </span>
        {isRecording && (
          <>
            <span style={{ fontSize: '0.72rem', color: 'var(--muted)' }}>·</span>
            <span style={{ fontSize: '0.72rem', color: 'var(--muted)', fontFamily: 'Fira Code, monospace' }}>
              {seq} action{seq !== 1 ? 's' : ''}
            </span>
            <ElapsedTimer running={isRecording} />
          </>
        )}
      </div>

      {/* workflow name input (only when idle) */}
      {!isRecording && (
        <label className="grid gap-1.5 text-sm font-medium" style={{ color: 'var(--muted)' }}>
          Workflow Name
          <input
            value={workflowName}
            onChange={(e) => setWorkflowName(e.target.value)}
            placeholder="e.g. setup-private-dns (optional)"
          />
        </label>
      )}

      {/* action button */}
      <button
        type="button"
        onClick={isRecording ? () => onStop(workflowName || undefined) : () => onStart(workflowName || undefined)}
        style={{
          background: isRecording
            ? 'linear-gradient(180deg, #ef4444, #b91c1c)'
            : 'linear-gradient(180deg, var(--accent), var(--accent-strong))',
          borderColor: isRecording ? 'rgba(239,68,68,0.5)' : 'var(--border-strong)',
          color: isRecording ? '#fff' : 'var(--on-accent)',
          gap: '0.45rem',
        }}
      >
        {isRecording ? (
          <>
            <svg viewBox="0 0 24 24" className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <rect x="6" y="6" width="12" height="12" rx="1" />
            </svg>
            Stop Recording
          </>
        ) : (
          <>
            <span className="inline-block rounded-full" style={{ width: '8px', height: '8px', background: 'var(--on-accent)', flexShrink: 0 }} />
            Start Recording
          </>
        )}
      </button>

      <p style={{ margin: 0, fontSize: '0.72rem', color: 'var(--muted)', lineHeight: 1.55 }}>
        Perform actions on the device — each tap, input, and scroll is captured. Stop when done to generate the RhinoJS script.
      </p>
    </div>
  );
}
