import { FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import { useTasks } from '../../hooks/useTasks';
import { useDevices } from '../../hooks/useDevices';
import { useWorkflows } from '../../hooks/useWorkflows';
import { getAndroidIdentity } from '../../utils/deviceIdentity';
import type { Task, TaskStatus } from '../../types';

// ─── helpers ─────────────────────────────────────────────────────────────────

interface ArtifactField { key: string; value: string }

function extractWorkflowInputKeys(
  workflowName: string,
  workflows: Array<{ name: string; steps?: Record<string, unknown> }>,
): string[] {
  const wf = workflows.find((w) => w.name === workflowName);
  if (!wf?.steps) return [];
  const keys = new Set<string>();
  const pat = /\{\{\s*input\.([a-zA-Z0-9_]+)\s*\}\}/g;
  const walk = (v: unknown) => {
    if (typeof v === 'string') {
      let m: RegExpExecArray | null = pat.exec(v);
      while (m) { keys.add(m[1]); m = pat.exec(v); }
      pat.lastIndex = 0;
    } else if (Array.isArray(v)) {
      v.forEach(walk);
    } else if (v && typeof v === 'object') {
      Object.values(v as Record<string, unknown>).forEach(walk);
    }
  };
  walk(wf.steps);
  return Array.from(keys);
}

// ─── status badge ─────────────────────────────────────────────────────────────

const STATUS_CLS: Record<TaskStatus, string> = {
  pending:   'task-badge--pending',
  running:   'task-badge--running',
  paused:    'task-badge--paused',
  completed: 'task-badge--completed',
  failed:    'task-badge--failed',
  cancelled: 'task-badge--cancelled',
};

function StatusBadge({ status }: { status: TaskStatus }) {
  return (
    <span className={`task-badge ${STATUS_CLS[status] ?? ''}`}>
      {status === 'running' && <span className="task-badge-dot" />}
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
}

// ─── icon component (ensures consistent 16×16) ───────────────────────────────

function Icon({ path, size = 16, className = '' }: { path: string; size?: number; className?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
      style={{ flexShrink: 0 }}
    >
      {/* eslint-disable-next-line react/no-danger */}
      <path d={path} />
    </svg>
  );
}

// ─── task row ─────────────────────────────────────────────────────────────────

function TaskRow({
  task, loadingById, onRefresh, onCancel,
}: { task: Task; loadingById: Record<string, boolean>; onRefresh: (id: string) => void; onCancel: (id: string) => void }) {
  const isTerminal = task.status === 'cancelled' || task.status === 'completed' || task.status === 'failed';
  const busy = !!loadingById[task.id];
  return (
    <tr className={task.status === 'failed' ? 'task-row--failed' : ''}>
      <td><span className="task-id-mono" title={task.id}>{task.id.slice(0, 8)}…</span></td>
      <td><StatusBadge status={task.status} /></td>
      <td className="task-goal-cell" title={task.goal}>{task.goal}</td>
      <td><span className="task-id-mono">{task.assignedDevice ? task.assignedDevice.slice(0, 12) : '—'}</span></td>
      <td>{task.workflowName ?? '—'}</td>
      <td className="task-mono-cell">{task.currentStep ?? '—'}</td>
      <td className="task-error-cell" title={task.lastCommandError ?? ''}>
        {task.lastCommandError
          ? <span className="task-error-text">{task.lastCommandError.slice(0, 48)}{task.lastCommandError.length > 48 ? '…' : ''}</span>
          : '—'}
      </td>
      <td className="task-time-cell">{new Date(task.createdAt).toLocaleString()}</td>
      <td className="task-time-cell">{new Date(task.updatedAt).toLocaleString()}</td>
      <td>
        <div className="task-row-actions">
          <button type="button" className="btn-secondary task-icon-btn" disabled={busy} onClick={() => onRefresh(task.id)} aria-label="Refresh">
            {busy
              ? <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="task-spin" aria-hidden="true"><path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" /></svg>
              : <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M4 4v5h5M20 20v-5h-5" strokeLinecap="round" strokeLinejoin="round" /><path d="M4 9a9 9 0 0 1 15.66-4.13M20 15a9 9 0 0 1-15.66 4.13" strokeLinecap="round" /></svg>}
          </button>
          {!isTerminal && (
            <button type="button" className="btn-danger task-icon-btn" disabled={busy} onClick={() => onCancel(task.id)} aria-label="Cancel">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" strokeLinecap="round" /></svg>
            </button>
          )}
        </div>
      </td>
    </tr>
  );
}

// ─── main ─────────────────────────────────────────────────────────────────────

export function TaskPanel() {
  const [page, setPage]                   = useState(0);
  const [pageSize, setPageSize]           = useState(20);
  const [statusFilter, setStatusFilter]   = useState('');
  const [deviceFilter, setDeviceFilter]   = useState('');
  const [workflowFilter, setWorkflowFilter] = useState('');
  const [createOpen, setCreateOpen]       = useState(false);
  const [taskLookupId, setTaskLookupId]   = useState('');

  const { tasks, hasMore, loading, loadingById, error, create, refreshById, refreshList, cancel } = useTasks({
    limit: pageSize, offset: page * pageSize,
    status: statusFilter || undefined,
    deviceId: deviceFilter || undefined,
    workflowName: workflowFilter || undefined,
  });

  const { devices }   = useDevices();
  const { workflows } = useWorkflows();

  const [goal, setGoal]               = useState('');
  const [deviceId, setDeviceId]       = useState('');
  const [workflowName, setWorkflowName] = useState('');
  const [artifactFields, setArtifactFields] = useState<ArtifactField[]>([]);
  const [artifactDrafts, setArtifactDrafts] = useState<Record<string, ArtifactField[]>>({});
  const prevWfRef = useRef('');

  const suggestedKeys = useMemo(() => extractWorkflowInputKeys(workflowName, workflows), [workflowName, workflows]);

  useEffect(() => {
    const cur = workflowName.trim(), prev = prevWfRef.current;
    if (cur === prev) return;
    if (prev) setArtifactDrafts((d) => ({ ...d, [prev]: artifactFields.map((f) => ({ ...f })) }));
    prevWfRef.current = cur;
    if (!cur) { setArtifactFields([]); return; }
    const draft = artifactDrafts[cur];
    if (draft) { setArtifactFields(draft.map((f) => ({ ...f }))); return; }
    setArtifactFields(suggestedKeys.map((key) => ({ key, value: '' })));
  }, [artifactDrafts, artifactFields, workflowName, suggestedKeys]);

  const artifactMap = useMemo(() => {
    const m: Record<string, string> = {};
    artifactFields.forEach(({ key, value }) => { const k = key.trim(); if (k) m[k] = value; });
    return m;
  }, [artifactFields]);

  const hasDupe = useMemo(() => {
    const seen = new Set<string>();
    for (const { key } of artifactFields) {
      const k = key.trim(); if (!k) continue;
      if (seen.has(k)) return true; seen.add(k);
    }
    return false;
  }, [artifactFields]);

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!goal.trim() || !workflowName.trim() || hasDupe) return;
    try {
      await create({ goal: goal.trim(), deviceId: deviceId.trim() || undefined, workflowName: workflowName.trim(), inputArtifacts: artifactMap });
      setGoal('');
      setPage(0);
      await refreshList();
      const reset = suggestedKeys.map((key) => ({ key, value: '' }));
      setArtifactFields(reset);
      setArtifactDrafts((d) => ({ ...d, [workflowName.trim()]: reset }));
    } catch { /* handled in store */ }
  };

  useEffect(() => { setPage(0); }, [statusFilter, deviceFilter, workflowFilter]);

  const liveCounts = useMemo(() => {
    let running = 0, pending = 0, failed = 0;
    tasks.forEach((t) => { if (t.status === 'running') running++; else if (t.status === 'pending') pending++; else if (t.status === 'failed') failed++; });
    return { running, pending, failed };
  }, [tasks]);

  return (
    <section className="panel">

      {/* header */}
      <div className="panel-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ color: 'var(--accent)', flexShrink: 0 }}>
            <path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2" />
            <rect x="9" y="3" width="6" height="4" rx="1" />
            <path d="M9 12h6M9 16h4" />
          </svg>
          <h2>Tasks</h2>
          {tasks.length > 0 && (
            <span className="task-count-badge">{tasks.length}{hasMore ? '+' : ''}</span>
          )}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
          {liveCounts.running > 0 && <span className="task-badge task-badge--running"><span className="task-badge-dot" />{liveCounts.running} running</span>}
          {liveCounts.pending > 0 && <span className="task-badge task-badge--pending">{liveCounts.pending} pending</span>}
          {liveCounts.failed  > 0 && <span className="task-badge task-badge--failed">{liveCounts.failed} failed</span>}
          <button type="button" onClick={() => setCreateOpen((o) => !o)} style={{ gap: '0.35rem' }}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" aria-hidden="true"
              style={{ transition: 'transform 200ms', transform: createOpen ? 'rotate(45deg)' : 'none' }}>
              <path d="M12 5v14M5 12h14" />
            </svg>
            New Task
          </button>
        </div>
      </div>

      {/* create form */}
      {createOpen && (
        <div className="task-create-panel">
          {workflows.length === 0 && (
            <p className="task-notice task-notice--warn">
              No workflow definitions loaded. Check <code>workflow-dir</code> config.
            </p>
          )}
          <form className="task-form" onSubmit={onSubmit}>
            <label>
              Goal
              <input value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="Describe the automation goal" required />
            </label>
            <label>
              Device
              <select value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
                <option value="">Auto-assign</option>
                {devices.map((d) => (
                  <option key={d.deviceId} value={d.deviceId}>{getAndroidIdentity(d)} ({d.deviceId})</option>
                ))}
              </select>
            </label>
            <label>
              Workflow
              <select value={workflowName} onChange={(e) => setWorkflowName(e.target.value)} required>
                <option value="" disabled>Select workflow</option>
                {workflows.map((w) => <option key={w.name} value={w.name}>{w.name}</option>)}
              </select>
            </label>

            <div className="task-field-wide">
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
                <span style={{ fontSize: '0.8125rem', fontWeight: 600, color: 'var(--muted)' }}>Input Artifacts</span>
                <span className="field-hint" style={{ flex: 1 }}>Key/value pairs required by the workflow.</span>
                <button type="button" className="btn-secondary" onClick={() => setArtifactFields((f) => [...f, { key: '', value: '' }])}>
                  + Add field
                </button>
              </div>
              {artifactFields.length === 0 ? (
                <p className="field-hint">No fields — select a workflow or add custom fields.</p>
              ) : (
                <div className="artifact-list">
                  {artifactFields.map((field, i) => (
                    <div className="artifact-row" key={`${field.key}-${i}`}>
                      <input
                        value={field.key}
                        onChange={(e) => { const n = [...artifactFields]; n[i] = { ...n[i], key: e.target.value }; setArtifactFields(n); }}
                        placeholder="key (e.g. private_dns_hostname)"
                      />
                      <input
                        value={field.value}
                        onChange={(e) => { const n = [...artifactFields]; n[i] = { ...n[i], value: e.target.value }; setArtifactFields(n); }}
                        placeholder="value"
                      />
                      <button type="button" className="btn-danger" onClick={() => setArtifactFields((f) => f.filter((_, idx) => idx !== i))}>
                        Remove
                      </button>
                    </div>
                  ))}
                </div>
              )}
              {hasDupe && <p className="task-notice task-notice--error" style={{ marginTop: '0.5rem' }}>Duplicate artifact keys.</p>}
            </div>

            <div className="actions" style={{ gridColumn: '1 / -1' }}>
              <button type="submit" disabled={loading || !workflowName.trim() || workflows.length === 0 || hasDupe}>
                {loading ? 'Creating…' : 'Create task'}
              </button>
              <button type="button" className="btn-secondary" onClick={() => setCreateOpen(false)}>Discard</button>
            </div>
          </form>
        </div>
      )}

      {/* filters */}
      <div className="task-filters-bar">
        <div className="event-filters" style={{ flex: 1 }}>
          <label>
            Status
            <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
              <option value="">All</option>
              <option value="pending">Pending</option>
              <option value="running">Running</option>
              <option value="paused">Paused</option>
              <option value="completed">Completed</option>
              <option value="failed">Failed</option>
              <option value="cancelled">Cancelled</option>
            </select>
          </label>
          <label>
            Device
            <select value={deviceFilter} onChange={(e) => setDeviceFilter(e.target.value)}>
              <option value="">All devices</option>
              {devices.map((d) => (
                <option key={d.deviceId} value={d.deviceId}>{getAndroidIdentity(d)} ({d.deviceId})</option>
              ))}
            </select>
          </label>
          <label>
            Workflow
            <select value={workflowFilter} onChange={(e) => setWorkflowFilter(e.target.value)}>
              <option value="">All workflows</option>
              {workflows.map((w) => <option key={w.name} value={w.name}>{w.name}</option>)}
            </select>
          </label>
        </div>
        <div className="row">
          <input
            value={taskLookupId}
            onChange={(e) => setTaskLookupId(e.target.value)}
            placeholder="Lookup task ID…"
            style={{ minWidth: '160px', maxWidth: '200px' }}
          />
          <button
            type="button"
            className="btn-secondary"
            disabled={!taskLookupId.trim() || !!loadingById[taskLookupId]}
            onClick={() => { if (taskLookupId.trim()) refreshById(taskLookupId.trim()); }}
          >
            {loadingById[taskLookupId] ? 'Loading…' : 'Load'}
          </button>
        </div>
      </div>

      {/* pagination */}
      <div className="row" style={{ marginBottom: '0.5rem' }}>
        <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.8125rem', color: 'var(--muted)', fontWeight: 500 }}>
          Per page
          <select
            value={pageSize}
            onChange={(e) => { const n = Number(e.target.value); if (Number.isFinite(n)) { setPageSize(n); setPage(0); } }}
            style={{ width: 'auto' }}
          >
            <option value={10}>10</option>
            <option value={20}>20</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
          </select>
        </label>
        <button type="button" className="btn-secondary" onClick={() => setPage((p) => Math.max(0, p - 1))} disabled={loading || page === 0}>
          ‹ Prev
        </button>
        <span style={{ fontSize: '0.8125rem', color: 'var(--muted)', padding: '0 0.25rem' }}>Page {page + 1}</span>
        <button type="button" className="btn-secondary" onClick={() => setPage((p) => p + 1)} disabled={loading || !hasMore}>
          Next ›
        </button>
      </div>

      {error && <p className="task-notice task-notice--error">{error}</p>}

      {/* table */}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Status</th>
              <th>Goal</th>
              <th>Device</th>
              <th>Workflow</th>
              <th>Step</th>
              <th>Last Error</th>
              <th>Created</th>
              <th>Updated</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {loading && tasks.length === 0 ? (
              <tr><td colSpan={10} style={{ textAlign: 'center', padding: '2rem', color: 'var(--muted)', fontSize: '0.875rem' }}>Loading…</td></tr>
            ) : tasks.length === 0 ? (
              <tr>
                <td colSpan={10} style={{ textAlign: 'center', padding: '2rem', color: 'var(--muted)', fontSize: '0.875rem' }}>
                  No tasks found.{' '}
                  {!createOpen && (
                    <button type="button" className="task-inline-link" onClick={() => setCreateOpen(true)}>Create one</button>
                  )}
                </td>
              </tr>
            ) : (
              tasks.map((task) => (
                <TaskRow
                  key={task.id}
                  task={task}
                  loadingById={loadingById}
                  onRefresh={refreshById}
                  onCancel={(id) => void cancel(id)}
                />
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
