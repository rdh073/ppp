import { useCallback, useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import type { SavedMacro } from '../api/macros';
import type { Device } from '../../../types';
import { listDevices, executeScript } from '../../device-control/api/devices';
import { useDeviceStore } from '../../device-control/store/devices';
import { useScrcpySessions } from '../../device-control/hooks/useScrcpySessions';
import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import { ScrcpyView } from '../../device-control/components/ScrcpyView';
import { CopyButton } from '../../../components/ui/CopyButton';

// ── types ────────────────────────────────────────────────────────────────────

type ExecStatus = 'idle' | 'running' | 'success' | 'error';

interface ExecState {
  status: ExecStatus;
  durationMs?: number;
  error?: string;
}

type DeviceFilterStatus = 'all' | 'online' | 'stale' | 'offline';

const HEARTBEAT_STALE_MS = 90_000;

function deviceStatus(device: Device): 'online' | 'stale' | 'offline' {
  if (!device.sessionId) return 'offline';
  const ts = new Date(device.lastHeartbeatAt).getTime();
  if (!Number.isFinite(ts)) return 'offline';
  return Date.now() - ts < HEARTBEAT_STALE_MS ? 'online' : 'stale';
}

// ── device picker row ────────────────────────────────────────────────────────

function DevicePickerRow({
  device,
  checked,
  execState,
  onToggle,
}: {
  device: Device;
  checked: boolean;
  execState?: ExecState;
  onToggle: (deviceId: string, checked: boolean) => void;
}) {
  const status = deviceStatus(device);
  const statusColor = status === 'online' ? '#4ade80' : status === 'stale' ? '#fbbf24' : 'var(--muted)';

  return (
    <label
      className="flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer"
      style={{ background: checked ? 'rgba(34,197,94,0.08)' : 'transparent', transition: 'background 150ms ease' }}
    >
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onToggle(device.deviceId, e.target.checked)}
        style={{ accentColor: 'var(--accent)', width: '14px', height: '14px', cursor: 'pointer' }}
      />
      <span
        className="w-2 h-2 rounded-full shrink-0"
        style={{ background: statusColor }}
      />
      <span className="flex-1 min-w-0 text-xs font-medium truncate" style={{ color: 'var(--text)' }}>
        {getAndroidIdentity(device)}
      </span>
      {execState && execState.status !== 'idle' && (
        <span className="shrink-0 text-[0.65rem]" style={{ color: execState.status === 'running' ? 'var(--muted)' : execState.status === 'success' ? '#4ade80' : 'var(--error)' }}>
          {execState.status === 'running' && (
            <svg className="w-3 h-3 animate-spin inline-block" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
            </svg>
          )}
          {execState.status === 'success' && `${((execState.durationMs ?? 0) / 1000).toFixed(1)}s`}
          {execState.status === 'error' && 'failed'}
        </span>
      )}
    </label>
  );
}

// ── main modal ───────────────────────────────────────────────────────────────

interface Props {
  macro: SavedMacro;
  onClose: () => void;
}

export function ReplayMacroModal({ macro, onClose }: Props) {
  const storeDevices = useDeviceStore((s) => s.devices);
  const [localDevices, setLocalDevices] = useState<Device[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [deviceSearch, setDeviceSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<DeviceFilterStatus>('all');
  const [execStates, setExecStates] = useState<Map<string, ExecState>>(new Map());
  const scrcpy = useScrcpySessions();

  // Use store devices if available, else fetch once
  const devices = storeDevices.length > 0 ? storeDevices : localDevices;
  useEffect(() => {
    if (storeDevices.length === 0) {
      void listDevices().then(setLocalDevices).catch(() => {});
    }
  }, [storeDevices.length]);

  // Body scroll lock
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = prev; };
  }, []);

  // Escape key
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  // Filtered devices
  const filtered = useMemo(() => {
    const q = deviceSearch.trim().toLowerCase();
    return devices.filter((d) => {
      if (statusFilter !== 'all' && deviceStatus(d) !== statusFilter) return false;
      if (!q) return true;
      const identity = getAndroidIdentity(d).toLowerCase();
      return identity.includes(q) || d.deviceId.toLowerCase().includes(q);
    });
  }, [devices, deviceSearch, statusFilter]);

  // Toggle device selection + auto-open/close scrcpy
  const toggleDevice = useCallback((deviceId: string, checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (checked) next.add(deviceId);
      else next.delete(deviceId);
      return next;
    });
    const device = devices.find((d) => d.deviceId === deviceId);
    if (!device) return;
    if (checked) {
      scrcpy.openSession(device);
    } else {
      scrcpy.closeByDeviceId(deviceId);
    }
  }, [devices, scrcpy]);

  // Run script on all selected devices
  const anyRunning = [...execStates.values()].some((s) => s.status === 'running');

  const runOnSelected = useCallback(async () => {
    const ids = [...selectedIds];
    if (ids.length === 0) return;

    // Set all to running
    setExecStates((prev) => {
      const next = new Map(prev);
      for (const id of ids) next.set(id, { status: 'running' });
      return next;
    });

    // Execute in parallel
    await Promise.allSettled(
      ids.map(async (deviceId) => {
        try {
          const result = await executeScript(deviceId, macro.script);
          setExecStates((prev) => {
            const next = new Map(prev);
            next.set(deviceId, { status: 'success', durationMs: result.durationMs });
            return next;
          });
        } catch (e) {
          const msg = e instanceof Error ? e.message : 'Script execution failed';
          setExecStates((prev) => {
            const next = new Map(prev);
            next.set(deviceId, { status: 'error', error: msg });
            return next;
          });
        }
      }),
    );
  }, [selectedIds, macro.script]);

  // Sessions for selected devices only
  const selectedSessions = scrcpy.sessions.filter((s) => selectedIds.has(s.deviceId));

  const allDone = selectedIds.size > 0 && [...selectedIds].every((id) => {
    const s = execStates.get(id);
    return s && (s.status === 'success' || s.status === 'error');
  });

  return createPortal(
    <div className="replay-macro-modal-backdrop" role="presentation" onClick={() => onClose()}>
      <div
        className="replay-macro-modal"
        role="dialog"
        aria-modal="true"
        aria-label="Replay Macro"
        onClick={(e) => e.stopPropagation()}
      >
        {/* left: device picker + scrcpy mirrors */}
        <div className="replay-macro-modal-left">
          <h3 className="m-0 text-sm font-semibold" style={{ color: 'var(--text)' }}>Target Devices</h3>

          {/* search + filter */}
          <div className="flex items-center gap-2 mt-3">
            <div className="relative flex-1">
              <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" stroke="var(--muted)" strokeWidth="2" strokeLinecap="round">
                <circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" />
              </svg>
              <input
                type="search"
                placeholder="Filter devices..."
                value={deviceSearch}
                onChange={(e) => setDeviceSearch(e.target.value)}
                className="w-full text-xs rounded-lg"
                style={{ paddingLeft: '2rem', minHeight: '30px' }}
                aria-label="Filter devices"
              />
            </div>
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value as DeviceFilterStatus)}
              className="text-xs rounded-lg"
              style={{ minHeight: '30px', padding: '0 0.5rem' }}
              aria-label="Filter by status"
            >
              <option value="all">All</option>
              <option value="online">Online</option>
              <option value="stale">Stale</option>
              <option value="offline">Offline</option>
            </select>
          </div>

          {/* device list */}
          <div className="mt-2 grid gap-0.5" style={{ maxHeight: '180px', overflowY: 'auto' }}>
            {filtered.length === 0 ? (
              <p className="text-xs py-4 text-center" style={{ color: 'var(--muted)' }}>No devices found</p>
            ) : (
              filtered.map((d) => (
                <DevicePickerRow
                  key={d.deviceId}
                  device={d}
                  checked={selectedIds.has(d.deviceId)}
                  execState={execStates.get(d.deviceId)}
                  onToggle={toggleDevice}
                />
              ))
            )}
          </div>

          {/* scrcpy mirrors */}
          {selectedSessions.length > 0 && (
            <div className="mt-3 grid gap-2" style={{ gridTemplateColumns: selectedSessions.length === 1 ? '1fr' : 'repeat(auto-fill, minmax(260px, 1fr))' }}>
              {selectedSessions.map((session) => (
                <div key={session.id} className="rounded-xl overflow-hidden border" style={{ borderColor: 'var(--border)', minHeight: '200px' }}>
                  <ScrcpyView
                    sessionId={session.id}
                    deviceId={session.deviceId}
                    adbSerial={session.adbSerial}
                    deviceName={session.deviceName}
                    onClose={() => {
                      scrcpy.closeBySessionId(session.id);
                      setSelectedIds((prev) => {
                        const next = new Set(prev);
                        next.delete(session.deviceId);
                        return next;
                      });
                    }}
                  />
                </div>
              ))}
            </div>
          )}
        </div>

        {/* right: script + execution controls */}
        <div className="replay-macro-modal-right">
          {/* header */}
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <h3 className="m-0 text-sm font-semibold" style={{ color: 'var(--text)' }}>{macro.workflowName}</h3>
              <div className="flex items-center gap-2 mt-1">
                <span
                  className="text-[0.6rem] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full"
                  style={{
                    background: macro.source === 'ai' ? 'rgba(34,197,94,0.12)' : 'rgba(99,102,241,0.12)',
                    color: macro.source === 'ai' ? '#4ade80' : '#a5b4fc',
                    border: `1px solid ${macro.source === 'ai' ? 'rgba(34,197,94,0.25)' : 'rgba(99,102,241,0.25)'}`,
                  }}
                >
                  {macro.source === 'ai' ? 'AI' : 'Manual'}
                </span>
                <span className="text-[0.68rem]" style={{ color: 'var(--muted)' }}>{macro.actionCount} actions</span>
              </div>
            </div>
            <button
              type="button"
              onClick={onClose}
              className="btn-secondary shrink-0"
              style={{ padding: '0.25rem', minHeight: '28px', minWidth: '28px' }}
              aria-label="Close"
            >
              <svg viewBox="0 0 24 24" className="w-3.5 h-3.5" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M18 6 6 18M6 6l12 12" />
              </svg>
            </button>
          </div>

          {/* script preview */}
          <div className="relative mt-3">
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
                fontSize: '0.65rem',
                lineHeight: 1.65,
                color: '#4ade80',
                maxHeight: '240px',
                overflow: 'auto',
                whiteSpace: 'pre',
                overflowWrap: 'normal',
              }}
            >
              {macro.script}
            </pre>
          </div>

          {/* execution results */}
          {execStates.size > 0 && (
            <div className="mt-3 grid gap-1">
              <p className="text-[0.68rem] font-semibold m-0" style={{ color: 'var(--muted)' }}>Execution Results</p>
              {[...selectedIds].map((id) => {
                const state = execStates.get(id);
                if (!state) return null;
                const device = devices.find((d) => d.deviceId === id);
                const label = device ? getAndroidIdentity(device) : id;
                return (
                  <div key={id} className="flex items-center gap-2 px-3 py-1.5 rounded-lg" style={{ background: 'var(--surface)' }}>
                    {state.status === 'running' && (
                      <svg className="w-3 h-3 animate-spin shrink-0" viewBox="0 0 24 24" fill="none" stroke="var(--muted)" strokeWidth="2">
                        <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
                      </svg>
                    )}
                    {state.status === 'success' && (
                      <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="#4ade80" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M20 6 9 17l-5-5" />
                      </svg>
                    )}
                    {state.status === 'error' && (
                      <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="var(--error)" strokeWidth="2.5" strokeLinecap="round">
                        <path d="M18 6 6 18M6 6l12 12" />
                      </svg>
                    )}
                    <span className="flex-1 min-w-0 text-xs truncate" style={{ color: 'var(--text)' }}>{label}</span>
                    {state.status === 'success' && state.durationMs != null && (
                      <span className="text-[0.65rem] shrink-0" style={{ color: '#4ade80' }}>{(state.durationMs / 1000).toFixed(1)}s</span>
                    )}
                    {state.status === 'error' && (
                      <span className="text-[0.65rem] shrink-0 truncate max-w-[120px]" style={{ color: 'var(--error)' }} title={state.error}>{state.error}</span>
                    )}
                  </div>
                );
              })}
            </div>
          )}

          {/* run button */}
          <div className="mt-4">
            <button
              type="button"
              className="w-full flex items-center justify-center gap-2 rounded-xl py-2.5 text-sm font-semibold"
              style={{
                background: selectedIds.size === 0 || anyRunning ? 'rgba(34,197,94,0.15)' : '#22c55e',
                color: selectedIds.size === 0 || anyRunning ? 'rgba(34,197,94,0.4)' : '#000',
                border: 'none',
                cursor: selectedIds.size === 0 || anyRunning ? 'not-allowed' : 'pointer',
                transition: 'background 150ms ease',
              }}
              disabled={selectedIds.size === 0 || anyRunning}
              onClick={() => void runOnSelected()}
            >
              <svg viewBox="0 0 24 24" className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <polygon points="5 3 19 12 5 21 5 3" />
              </svg>
              {anyRunning ? 'Running...' : allDone ? 'Run Again' : `Run on ${selectedIds.size} Device${selectedIds.size !== 1 ? 's' : ''}`}
            </button>
            {selectedIds.size === 0 && (
              <p className="text-[0.68rem] text-center mt-2 m-0" style={{ color: 'var(--muted)', opacity: 0.7 }}>
                Select devices from the left panel to replay this macro.
              </p>
            )}
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
