import { FormEvent, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useTasks } from '../../hooks/useTasks';
import { useDevices } from '../../hooks/useDevices';
import { useWorkflows } from '../../hooks/useWorkflows';
import { getAndroidIdentity } from '../../utils/deviceIdentity';
import type { Task, TaskStatus } from '../../types';

// ─── helpers ─────────────────────────────────────────────────────────────────

interface ArtifactField { key: string; value: string }
type CreateNoticeTone = 'success' | 'warn' | 'error';
type DeviceFilterStatus = 'all' | 'online' | 'stale' | 'offline';
type DeviceConnectivityStatus = Exclude<DeviceFilterStatus, 'all'>;
interface CreateNotice {
  tone: CreateNoticeTone;
  message: string;
}

const CREATE_NOTICE_CLASS: Record<CreateNoticeTone, string> = {
  success: 'task-notice--success',
  warn: 'task-notice--warn',
  error: 'task-notice--error',
};

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

const STATUS_SUMMARY: Array<{ status: TaskStatus; label: string; pulse?: boolean }> = [
  { status: 'running', label: 'running', pulse: true },
  { status: 'pending', label: 'pending' },
  { status: 'paused', label: 'paused' },
  { status: 'completed', label: 'completed' },
  { status: 'failed', label: 'failed' },
  { status: 'cancelled', label: 'cancelled' },
];

const HEARTBEAT_STALE_MS = 90_000;

const DEVICE_FILTER_OPTIONS: Array<{ value: DeviceFilterStatus; label: string }> = [
  { value: 'all', label: 'All status' },
  { value: 'online', label: 'Online' },
  { value: 'stale', label: 'Stale' },
  { value: 'offline', label: 'Offline' },
];

function deviceConnectivityStatus(sessionId: string, lastHeartbeatAt: string): DeviceConnectivityStatus {
  if (!sessionId) return 'offline';
  const heartbeatTs = new Date(lastHeartbeatAt).getTime();
  if (!Number.isFinite(heartbeatTs)) return 'offline';
  return Date.now() - heartbeatTs < HEARTBEAT_STALE_MS ? 'online' : 'stale';
}

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
  const [selectedDeviceIds, setSelectedDeviceIds] = useState<string[]>([]);
  const [devicePickerOpen, setDevicePickerOpen] = useState(false);
  const [deviceSearch, setDeviceSearch] = useState('');
  const [deviceStatusFilterCreate, setDeviceStatusFilterCreate] = useState<DeviceFilterStatus>('all');
  const [workflowName, setWorkflowName] = useState('');
  const [artifactFields, setArtifactFields] = useState<ArtifactField[]>([]);
  const [artifactDrafts, setArtifactDrafts] = useState<Record<string, ArtifactField[]>>({});
  const [createNotice, setCreateNotice] = useState<CreateNotice | null>(null);
  const prevWfRef = useRef('');
  const deferredDeviceSearch = useDeferredValue(deviceSearch);
  const selectedDeviceSet = useMemo(() => new Set(selectedDeviceIds), [selectedDeviceIds]);
  const selectedDevices = useMemo(
    () => selectedDeviceIds
      .map((deviceId) => devices.find((device) => device.deviceId === deviceId))
      .filter((device): device is (typeof devices)[number] => Boolean(device)),
    [devices, selectedDeviceIds],
  );
  const deviceStatusById = useMemo(() => {
    const statusMap = new Map<string, DeviceConnectivityStatus>();
    devices.forEach((device) => {
      statusMap.set(device.deviceId, deviceConnectivityStatus(device.sessionId, device.lastHeartbeatAt));
    });
    return statusMap;
  }, [devices]);
  const filteredDevices = useMemo(() => {
    const q = deferredDeviceSearch.trim().toLowerCase();
    return devices.filter((device) => {
      const status = deviceStatusById.get(device.deviceId) ?? 'offline';
      if (deviceStatusFilterCreate !== 'all' && status !== deviceStatusFilterCreate) return false;
      if (!q) return true;
      const identity = getAndroidIdentity(device).toLowerCase();
      return identity.includes(q) || device.deviceId.toLowerCase().includes(q);
    });
  }, [deferredDeviceSearch, deviceStatusById, deviceStatusFilterCreate, devices]);

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

  useEffect(() => {
    const knownDeviceIds = new Set(devices.map((device) => device.deviceId));
    setSelectedDeviceIds((current) => current.filter((deviceId) => knownDeviceIds.has(deviceId)));
  }, [devices]);

  useEffect(() => {
    if (!createOpen) setDevicePickerOpen(false);
  }, [createOpen]);

  useEffect(() => {
    if (!devicePickerOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setDevicePickerOpen(false);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [devicePickerOpen]);

  const toggleSelectedDevice = (deviceId: string, checked: boolean) => {
    setSelectedDeviceIds((current) => {
      if (checked) {
        if (current.includes(deviceId)) return current;
        return [...current, deviceId];
      }
      return current.filter((item) => item !== deviceId);
    });
  };

  const selectAllDevices = () => {
    setSelectedDeviceIds(devices.map((device) => device.deviceId));
  };

  const selectAllFilteredDevices = () => {
    if (filteredDevices.length === 0) return;
    setSelectedDeviceIds((current) => {
      const next = new Set(current);
      filteredDevices.forEach((device) => next.add(device.deviceId));
      return Array.from(next);
    });
  };

  const clearSelectedDevices = () => {
    setSelectedDeviceIds([]);
  };

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!goal.trim() || !workflowName.trim() || hasDupe) return;
    setCreateNotice(null);

    const targetDeviceIds = selectedDeviceIds.length > 0 ? selectedDeviceIds : [undefined];
    let successCount = 0;
    const failures: Array<{ deviceId?: string; message: string }> = [];

    try {
      for (const deviceId of targetDeviceIds) {
        try {
          await create({
            goal: goal.trim(),
            deviceId: deviceId?.trim() || undefined,
            workflowName: workflowName.trim(),
            inputArtifacts: artifactMap,
          });
          successCount += 1;
        } catch (raw) {
          const message = raw instanceof Error ? raw.message : 'Failed to create task';
          failures.push({ deviceId, message });
        }
      }

      if (successCount === 0) {
        const firstFailure = failures[0];
        setCreateNotice({
          tone: 'error',
          message: firstFailure
            ? `Failed to create task${firstFailure.deviceId ? ` (${firstFailure.deviceId})` : ''}: ${firstFailure.message}`
            : 'Failed to create task.',
        });
        return;
      }

      setGoal('');
      setPage(0);
      await refreshList();
      const reset = suggestedKeys.map((key) => ({ key, value: '' }));
      setArtifactFields(reset);
      setArtifactDrafts((d) => ({ ...d, [workflowName.trim()]: reset }));

      if (failures.length > 0) {
        setCreateNotice({
          tone: 'warn',
          message: `Created ${successCount} task(s), but ${failures.length} device request(s) failed.`,
        });
      } else if (targetDeviceIds.length > 1) {
        setCreateNotice({
          tone: 'success',
          message: `Created ${successCount} tasks for ${successCount} devices using workflow ${workflowName.trim()}.`,
        });
      } else if (selectedDeviceIds.length === 0) {
        setCreateNotice({
          tone: 'success',
          message: `Task created with auto-assign using workflow ${workflowName.trim()}.`,
        });
      } else {
        setCreateNotice({
          tone: 'success',
          message: `Task created for device ${selectedDeviceIds[0]} using workflow ${workflowName.trim()}.`,
        });
      }
    } catch { /* handled in store */ }
  };

  useEffect(() => { setPage(0); }, [statusFilter, deviceFilter, workflowFilter]);

  const liveCounts = useMemo(() => {
    const counts: Record<TaskStatus, number> = {
      pending: 0,
      running: 0,
      paused: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    };
    tasks.forEach((task) => {
      counts[task.status] += 1;
    });
    return counts;
  }, [tasks]);

  const devicePickerModal = devicePickerOpen && typeof document !== 'undefined'
    ? createPortal(
        <div className="task-device-modal-backdrop" role="presentation" onClick={() => setDevicePickerOpen(false)}>
          <div
            className="task-device-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="task-device-picker-title"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="task-device-modal-header">
              <div>
                <h3 id="task-device-picker-title">Select Target Devices</h3>
                <p className="field-hint">Choose one or more devices. Same workflow and artifacts will be used for all selected devices.</p>
              </div>
              <button type="button" className="btn-secondary" onClick={() => setDevicePickerOpen(false)}>Close</button>
            </div>

            <div className="task-device-modal-controls">
              <label className="task-device-modal-search" htmlFor="task-device-search">
                Search
                <input
                  id="task-device-search"
                  value={deviceSearch}
                  onChange={(event) => setDeviceSearch(event.target.value)}
                  placeholder="Filter by device name or ID"
                />
              </label>
              <label className="task-device-modal-status" htmlFor="task-device-status-filter">
                Status
                <select
                  id="task-device-status-filter"
                  value={deviceStatusFilterCreate}
                  onChange={(event) => setDeviceStatusFilterCreate(event.target.value as DeviceFilterStatus)}
                >
                  {DEVICE_FILTER_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </label>
              <button type="button" className="btn-secondary" onClick={selectAllFilteredDevices} disabled={filteredDevices.length === 0}>
                Select all filtered
              </button>
            </div>

            <div className="task-device-modal-grid">
              <section className="task-device-modal-list" aria-label="Available devices">
                <h4>Available ({filteredDevices.length})</h4>
                {filteredDevices.length === 0 ? (
                  <p className="field-hint">No devices match this filter.</p>
                ) : (
                  <div className="task-device-modal-list-scroll">
                    {filteredDevices.map((device) => {
                      const checked = selectedDeviceSet.has(device.deviceId);
                      const status = deviceStatusById.get(device.deviceId) ?? 'offline';
                      return (
                        <label key={device.deviceId} className={`task-device-item ${checked ? 'task-device-item--active' : ''}`}>
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(event) => toggleSelectedDevice(device.deviceId, event.target.checked)}
                          />
                          <span className="task-device-item-meta">
                            <span className="task-device-item-name">{getAndroidIdentity(device)}</span>
                            <span className="task-device-item-id">{device.deviceId}</span>
                          </span>
                          <span className={`task-device-status task-device-status--${status}`}>{status}</span>
                        </label>
                      );
                    })}
                  </div>
                )}
              </section>

              <aside className="task-device-modal-selected" aria-label="Selected devices">
                <h4>Selected ({selectedDevices.length})</h4>
                {selectedDevices.length === 0 ? (
                  <p className="field-hint">No selected devices. Auto-assign mode will create one task.</p>
                ) : (
                  <ul className="task-device-selected-list">
                    {selectedDevices.map((device) => (
                      <li key={device.deviceId} className="task-device-selected-item">
                        <div>
                          <strong>{getAndroidIdentity(device)}</strong>
                          <span>{device.deviceId}</span>
                        </div>
                        <button
                          type="button"
                          className="btn-secondary"
                          onClick={() => toggleSelectedDevice(device.deviceId, false)}
                        >
                          Remove
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </aside>
            </div>

            <div className="task-device-modal-footer">
              <span className="task-device-modal-count">
                {selectedDeviceIds.length === 0 ? 'Auto-assign mode (1 task)' : `${selectedDeviceIds.length} devices selected`}
              </span>
              <div className="task-device-modal-footer-actions">
                <button type="button" className="btn-secondary" onClick={clearSelectedDevices} disabled={selectedDeviceIds.length === 0}>
                  Clear
                </button>
                <button type="button" onClick={() => setDevicePickerOpen(false)}>Apply Selection</button>
              </div>
            </div>
          </div>
        </div>,
        document.body,
      )
    : null;

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
          {STATUS_SUMMARY.map(({ status, label, pulse }) => (
            <span key={status} className={`task-badge ${STATUS_CLS[status] ?? ''}`}>
              {pulse && liveCounts[status] > 0 && <span className="task-badge-dot" />}
              {liveCounts[status]} {label}
            </span>
          ))}
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
          {createNotice && (
            <p className={`task-notice ${CREATE_NOTICE_CLASS[createNotice.tone]}`}>
              {createNotice.message}
            </p>
          )}
          <form className="task-form" onSubmit={onSubmit}>
            <label className="task-field-goal">
              Goal
              <input value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="Describe the automation goal" required />
            </label>
            <label>
              Workflow
              <select value={workflowName} onChange={(e) => setWorkflowName(e.target.value)} required>
                <option value="" disabled>Select workflow</option>
                {workflows.map((w) => <option key={w.name} value={w.name}>{w.name}</option>)}
              </select>
            </label>

            <div className="task-field-wide task-device-picker">
              <div className="task-device-picker-headline">
                <span className="task-device-picker-title">Target Devices</span>
                <span className="field-hint">
                  Up to 20+ devices supported. Use picker to apply one workflow across selected devices.
                </span>
              </div>
              <div className="task-device-picker-tools">
                <button type="button" className="btn-secondary" onClick={() => setDevicePickerOpen(true)}>
                  Choose devices
                </button>
                <button type="button" className="btn-secondary" onClick={selectAllDevices} disabled={devices.length === 0}>
                  Quick select all
                </button>
                <span className="task-device-picker-count">
                  {selectedDeviceIds.length === 0 ? 'Auto-assign (1 task)' : `${selectedDeviceIds.length} selected`}
                </span>
              </div>
              <div className="task-selected-chip-row">
                {selectedDevices.length === 0 ? (
                  <p className="field-hint">No explicit device selected. Task will use auto-assign routing.</p>
                ) : (
                  <>
                    {selectedDevices.slice(0, 4).map((device) => (
                      <span key={device.deviceId} className="task-selected-chip">
                        <span className="task-selected-chip-label">{getAndroidIdentity(device)}</span>
                        <button
                          type="button"
                          className="task-selected-chip-remove"
                          onClick={() => toggleSelectedDevice(device.deviceId, false)}
                          aria-label={`Remove ${device.deviceId}`}
                        >
                          ×
                        </button>
                      </span>
                    ))}
                    {selectedDevices.length > 4 && (
                      <span className="task-selected-chip task-selected-chip--more">
                        +{selectedDevices.length - 4} more
                      </span>
                    )}
                  </>
                )}
              </div>
            </div>

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
                {loading ? 'Creating…' : `Create ${Math.max(1, selectedDeviceIds.length)} task${Math.max(1, selectedDeviceIds.length) > 1 ? 's' : ''}`}
              </button>
              <button type="button" className="btn-secondary" onClick={() => setCreateOpen(false)}>Discard</button>
            </div>
          </form>

          {devicePickerModal}
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
