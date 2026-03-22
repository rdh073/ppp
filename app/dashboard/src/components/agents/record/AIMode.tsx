import { useState } from 'react';
import { ElapsedTimer } from '../../ui/ElapsedTimer';
import type { RunState } from '../../../hooks/useRecordingSession';

interface AIModeProps {
  runState: RunState;
  onRun: (opts: { goal: string; maxSteps: number; workflowName?: string; timeout: number }) => void;
  onCancel: () => void;
}

export function AIMode({ runState, onRun, onCancel }: AIModeProps) {
  const [goal, setGoal] = useState('');
  const [workflowName, setWorkflowName] = useState('');
  const [maxSteps, setMaxSteps] = useState(20);
  const [timeoutSec, setTimeoutSec] = useState(120);

  const isRunning = runState === 'running';
  const canRun = runState === 'idle' || runState === 'done' || runState === 'error';

  const handleRun = () => {
    if (!goal.trim()) return;
    onRun({
      goal: goal.trim(),
      maxSteps,
      workflowName: workflowName.trim() || undefined,
      timeout: timeoutSec * 1000,
    });
  };

  return (
    <div className="grid gap-3">
      {/* goal input */}
      <label className="grid gap-1.5 text-sm font-medium" style={{ color: 'var(--muted)' }}>
        Goal
        <textarea
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          placeholder="e.g. Open Settings and configure Private DNS to dns.adguard.com"
          disabled={isRunning}
          style={{ minHeight: '72px', resize: 'vertical', fontFamily: 'inherit' }}
        />
      </label>

      {/* workflow + steps + timeout */}
      <div className="grid gap-3" style={{ gridTemplateColumns: '1fr 80px 90px' }}>
        <label className="grid gap-1.5 text-sm font-medium" style={{ color: 'var(--muted)' }}>
          Workflow Name
          <input
            value={workflowName}
            onChange={(e) => setWorkflowName(e.target.value)}
            placeholder="auto-generated"
            disabled={isRunning}
          />
        </label>
        <label className="grid gap-1.5 text-sm font-medium" style={{ color: 'var(--muted)' }}>
          Max Steps
          <input
            type="number"
            min={1}
            max={50}
            value={maxSteps}
            onChange={(e) => setMaxSteps(Math.max(1, Math.min(50, Number(e.target.value))))}
            disabled={isRunning}
          />
        </label>
        <label className="grid gap-1.5 text-sm font-medium" style={{ color: 'var(--muted)' }}>
          Timeout (s)
          <input
            type="number"
            min={10}
            max={600}
            value={timeoutSec}
            onChange={(e) => setTimeoutSec(Math.max(10, Math.min(600, Number(e.target.value))))}
            disabled={isRunning}
          />
        </label>
      </div>

      {/* running state */}
      {isRunning && (
        <div
          className="flex items-center gap-3 rounded-xl border px-3 py-2.5"
          style={{ borderColor: 'rgba(34,197,94,0.3)', background: 'rgba(34,197,94,0.07)' }}
        >
          <svg
            className="w-4 h-4 shrink-0"
            style={{ animation: 'task-spin 1s linear infinite', color: '#4ade80' }}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
          </svg>
          <span style={{ fontSize: '0.78rem', fontWeight: 600, color: '#4ade80' }}>AI agent running…</span>
          <ElapsedTimer running={isRunning} />
        </div>
      )}

      {/* run / cancel */}
      <div className="flex gap-2">
        <button
          type="button"
          disabled={!canRun || !goal.trim()}
          onClick={handleRun}
          style={{ flex: 1, gap: '0.45rem' }}
        >
          <svg viewBox="0 0 24 24" className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 3 19 12 5 21V3z" />
          </svg>
          Run with AI
        </button>
        {isRunning && (
          <button type="button" className="btn-secondary" onClick={onCancel} style={{ gap: '0.35rem' }}>
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
            Cancel
          </button>
        )}
      </div>

      <p style={{ margin: 0, fontSize: '0.72rem', color: 'var(--muted)', lineHeight: 1.55 }}>
        The AI agent will observe the screen, tap, type, and scroll to achieve the goal — then generate a replayable script automatically.
      </p>
    </div>
  );
}
