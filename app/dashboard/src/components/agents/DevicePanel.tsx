import { useState } from 'react';
import { useDevices } from '../../hooks/useDevices';
import { useDeviceSelection } from '../../hooks/useDeviceSelection';
import { useScrcpySessions } from '../../hooks/useScrcpySessions';
import { useGroupManagement } from '../../hooks/useGroupManagement';
import { getAndroidIdentity } from '../../utils/deviceIdentity';
import { GroupMirrorView } from './GroupMirrorView';
import { RecordMacroPanel } from './RecordMacroPanel';
import { ScrcpySessionsSection } from './ScrcpySessionsSection';
import { DeviceCard } from './device/DeviceCard';
import { DeviceRow } from './device/DeviceRow';
import { DeviceSummaryBar } from './device/DeviceSummaryBar';
import { DeviceBulkBar } from './device/DeviceBulkBar';
import { DeviceViewToggle } from './device/DeviceViewToggle';
import { DeviceGroupForm } from './device/DeviceGroupForm';
import { DeviceGroupList } from './device/DeviceGroupList';
import { filterDevices } from './device/utils';
import type { Device } from '../../types';

// ── empty state ─────────────────────────────────────────────────────────────

function EmptyState({ loading }: { loading: boolean }) {
  if (loading) return null;
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3">
      <div
        className="flex items-center justify-center w-14 h-14 rounded-2xl"
        style={{ background: 'rgba(34,197,94,0.07)', border: '1px solid rgba(34,197,94,0.15)' }}
      >
        <svg viewBox="0 0 24 24" className="w-7 h-7" fill="none" stroke="var(--muted)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
          <rect x="5" y="2" width="14" height="20" rx="2" />
          <circle cx="12" cy="17" r="1" />
        </svg>
      </div>
      <p className="text-sm font-medium" style={{ color: 'var(--muted)' }}>No active device sessions</p>
      <p className="text-xs text-center max-w-[260px]" style={{ color: 'var(--muted)', opacity: 0.7 }}>
        Start the android-agent on a device to see it appear here.
      </p>
    </div>
  );
}

// ── main panel ──────────────────────────────────────────────────────────────

export function DevicePanel() {
  const { devices, loading, error, refresh } = useDevices();
  const [view, setView] = useState<'grid' | 'list'>('grid');
  const [search, setSearch] = useState('');
  const [recordingDeviceId, setRecordingDeviceId] = useState<string | null>(null);

  const filtered = filterDevices(devices, search);
  const { selected, allSelected, handleSelect, handleSelectAll, clearSelection } = useDeviceSelection(filtered);
  const scrcpy = useScrcpySessions();
  const groupMgmt = useGroupManagement(clearSelection);

  function toggleScrcpy(device: Device) {
    scrcpy.isActive(device.deviceId)
      ? scrcpy.closeByDeviceId(device.deviceId)
      : scrcpy.openSession(device);
  }

  function toggleRecord(device: Device) {
    setRecordingDeviceId((prev) => prev === device.deviceId ? null : device.deviceId);
  }

  if (groupMgmt.activeMirrorGroup) {
    return (
      <section className="panel">
        <GroupMirrorView
          group={groupMgmt.activeMirrorGroup}
          devices={devices}
          onExit={() => groupMgmt.setActiveMirrorGroup(null)}
        />
      </section>
    );
  }

  const sharedCardProps = {
    onToggleScrcpy: toggleScrcpy,
    onRecord: toggleRecord,
  };

  const selectedIds = [...selected];

  return (
    <section className="panel">
      {/* panel header */}
      <div className="panel-header">
        <div className="flex items-center gap-3">
          <h2 className="m-0">Devices</h2>
          {loading && (
            <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="var(--muted)" strokeWidth="2">
              <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
            </svg>
          )}
        </div>
        <div className="actions">
          <label
            className="inline-flex items-center gap-2 rounded-xl border px-2.5 py-1"
            style={{ borderColor: 'var(--border)', background: 'var(--surface-strong)' }}
          >
            <span className="text-[0.65rem] font-semibold uppercase tracking-[0.08em]" style={{ color: 'var(--muted)' }}>
              Max mirror
            </span>
            <input
              type="number"
              min={scrcpy.minMaxSessions}
              max={scrcpy.maxMaxSessions}
              step={1}
              value={scrcpy.maxSessions}
              onChange={(e) => {
                const parsed = Number.parseInt(e.target.value, 10);
                if (!Number.isNaN(parsed)) scrcpy.setMaxSessions(parsed);
              }}
              aria-label="Maximum concurrent scrcpy sessions"
              className="w-16 rounded-lg px-2 py-1 text-center text-xs"
              style={{ minHeight: '28px' }}
            />
          </label>
          <DeviceViewToggle view={view} onChange={setView} />
          <button type="button" onClick={() => refresh()} disabled={loading}>
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
              <path d="M3 3v5h5" />
            </svg>
            Refresh
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {/* summary + search */}
      <div className="flex flex-wrap items-center gap-3 mb-4">
        <DeviceSummaryBar devices={devices} />
        <div className="ml-auto relative">
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" stroke="var(--muted)" strokeWidth="2" strokeLinecap="round">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" />
          </svg>
          <input
            type="search"
            placeholder="Filter devices…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ width: '200px', paddingLeft: '2rem' }}
            aria-label="Filter devices"
          />
        </div>
      </div>

      {/* bulk action bar */}
      <DeviceBulkBar
        count={selected.size}
        onClear={clearSelection}
        onScrcpyAll={() => scrcpy.openMany(devices.filter((d) => selected.has(d.deviceId)))}
        onCreateGroup={() => groupMgmt.startGroupCreation(selectedIds)}
      />

      {/* group creation form */}
      {groupMgmt.groupCreating && (
        <DeviceGroupForm
          groupName={groupMgmt.groupName}
          groupMaster={groupMgmt.groupMaster}
          selectedIds={selectedIds}
          devices={devices}
          onGroupNameChange={groupMgmt.setGroupName}
          onGroupMasterChange={groupMgmt.setGroupMaster}
          onConfirm={() => groupMgmt.confirmGroupCreation(selectedIds)}
          onCancel={groupMgmt.cancelGroupCreation}
        />
      )}

      {/* device groups */}
      <DeviceGroupList
        groups={groupMgmt.groups}
        onMirrorGroup={groupMgmt.setActiveMirrorGroup}
        onRemoveGroup={groupMgmt.removeGroup}
      />

      {/* device list */}
      {filtered.length === 0 ? (
        <EmptyState loading={loading} />
      ) : view === 'grid' ? (
        <div className="mt-4 grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))' }}>
          {filtered.map((device) => (
            <DeviceCard
              key={device.deviceId}
              device={device}
              selected={selected.has(device.deviceId)}
              onSelect={handleSelect}
              isScrcpyActive={scrcpy.isActive(device.deviceId)}
              isRecording={recordingDeviceId === device.deviceId}
              {...sharedCardProps}
            />
          ))}
        </div>
      ) : (
        <div className="table-wrap mt-4">
          <table>
            <thead>
              <tr>
                <th style={{ width: '36px' }}>
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={(e) => handleSelectAll(e.target.checked, filtered)}
                    style={{ accentColor: 'var(--accent)', width: '14px', height: '14px', cursor: 'pointer' }}
                    aria-label="Select all"
                  />
                </th>
                <th>Device</th>
                <th>Status</th>
                <th>ADB Serial</th>
                <th>Session</th>
                <th>Heartbeat</th>
                <th>Capabilities</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((device) => (
                <DeviceRow
                  key={device.deviceId}
                  device={device}
                  selected={selected.has(device.deviceId)}
                  onSelect={handleSelect}
                  isScrcpyActive={scrcpy.isActive(device.deviceId)}
                  isRecording={recordingDeviceId === device.deviceId}
                  {...sharedCardProps}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* record macro panel */}
      {recordingDeviceId && (() => {
        const dev = devices.find((d) => d.deviceId === recordingDeviceId);
        return dev ? (
          <div className="mt-4">
            <RecordMacroPanel
              deviceId={recordingDeviceId}
              deviceLabel={getAndroidIdentity(dev)}
              onClose={() => setRecordingDeviceId(null)}
            />
          </div>
        ) : null;
      })()}

      {/* scrcpy sessions */}
      <ScrcpySessionsSection
        sessions={scrcpy.sessions}
        maxSessions={scrcpy.maxSessions}
        onCloseAll={scrcpy.closeAll}
        onCloseSession={scrcpy.closeBySessionId}
        makeTouchFanout={groupMgmt.makeTouchFanout}
      />
    </section>
  );
}
