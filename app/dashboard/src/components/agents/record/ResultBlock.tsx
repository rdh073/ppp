import { CopyButton } from '../../ui/CopyButton';
import type { RecordOutput } from '../../../hooks/useRecordingSession';

export function ResultBlock({ output }: { output: RecordOutput }) {
  return (
    <div className="grid gap-3" style={{ marginTop: '0.75rem' }}>
      {/* summary row */}
      <div
        className="flex flex-wrap items-center gap-2 rounded-xl border px-3 py-2"
        style={{ borderColor: 'rgba(34,197,94,0.3)', background: 'rgba(34,197,94,0.07)' }}
      >
        <svg viewBox="0 0 24 24" className="w-4 h-4 shrink-0" fill="none" stroke="#4ade80" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" /><path d="m9 11 3 3L22 4" />
        </svg>
        <span style={{ color: '#4ade80', fontSize: '0.78rem', fontWeight: 600 }}>
          {output.actionCount} action{output.actionCount !== 1 ? 's' : ''} recorded
        </span>
        {output.durationMs !== undefined && (
          <span style={{ color: 'var(--muted)', fontSize: '0.72rem' }}>
            · {(output.durationMs / 1000).toFixed(1)}s
          </span>
        )}
        {output.steps !== undefined && (
          <span style={{ color: 'var(--muted)', fontSize: '0.72rem' }}>
            · {output.steps} LLM steps
          </span>
        )}
        {output.done !== undefined && (
          <span style={{
            marginLeft: 'auto',
            fontSize: '0.68rem',
            fontWeight: 700,
            textTransform: 'uppercase',
            letterSpacing: '0.06em',
            color: output.done ? '#4ade80' : '#fbbf24',
          }}>
            {output.done ? 'Goal achieved' : 'Max steps reached'}
          </span>
        )}
      </div>

      {/* reason (LLM only) */}
      {output.reason && (
        <p style={{ margin: 0, fontSize: '0.78rem', color: 'var(--muted)', lineHeight: 1.6, fontStyle: 'italic' }}>
          &ldquo;{output.reason}&rdquo;
        </p>
      )}

      {/* script label */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
        <span style={{ fontSize: '0.68rem', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: '#4ade80' }}>
          RhinoJS Script
        </span>
        <span style={{ fontSize: '0.66rem', color: 'var(--muted)', opacity: 0.7 }}>
          — saved to Macros library
        </span>
      </div>

      {/* code block */}
      <div className="relative">
        <div className="absolute top-2 right-2 z-10">
          <CopyButton text={output.script} />
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
            maxHeight: '260px',
            overflow: 'auto',
            whiteSpace: 'pre',
            overflowWrap: 'normal',
          }}
        >
          {output.script}
        </pre>
      </div>
    </div>
  );
}
