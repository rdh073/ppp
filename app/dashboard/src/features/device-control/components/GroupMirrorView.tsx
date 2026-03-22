import { ScrcpyView } from './ScrcpyView';
import { executeDeviceAction } from '../api/devices';
import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import type { Device } from '../../../types';
import type { DeviceGroup } from '../store/groups';

interface Props {
  group: DeviceGroup;
  devices: Device[];
  onExit: () => void;
}

export function GroupMirrorView({ group, devices, onExit }: Props) {
  const masterDevice = devices.find((d) => d.deviceId === group.masterDeviceId);
  const slaveDevices = group.slaveDeviceIds
    .map((id) => devices.find((d) => d.deviceId === id))
    .filter((d): d is Device => !!d);

  const sessionPrefix = group.id + '-gm';

  function handleMasterTouch(type: 'down' | 'move' | 'up', x: number, y: number) {
    if (type !== 'down') return;
    const value = `${x},${y}`;
    for (const slave of slaveDevices) {
      void executeDeviceAction(slave.deviceId, {
        kind: 'click',
        target: { kind: 'coordinate', value },
      }).catch(() => {});
    }
  }

  return (
    <div className="group-mirror-view">
      {/* ── header bar ── */}
      <div className="group-mirror-header">
        <button type="button" className="btn-secondary group-mirror-back" onClick={onExit}
          aria-label="Exit group mirror">
          <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor"
            strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M19 12H5M5 12l7-7M5 12l7 7" />
          </svg>
          Exit mirror
        </button>

        <div className="group-mirror-title">
          {/* group icon */}
          <svg viewBox="0 0 24 24" className="w-4 h-4 shrink-0" fill="none" stroke="var(--accent)"
            strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <rect x="5" y="2" width="14" height="20" rx="2" />
            <path d="M12 18h.01" />
          </svg>
          <span className="font-semibold">{group.name}</span>
          <span className="group-mirror-count">
            1 master · {slaveDevices.length} slave{slaveDevices.length !== 1 ? 's' : ''}
          </span>
        </div>

        <div className="group-mirror-legend">
          <span className="group-mirror-legend-item group-mirror-legend-master">
            <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="currentColor"
              strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
            </svg>
            Master
          </span>
          <span className="group-mirror-legend-item group-mirror-legend-slave">
            <svg viewBox="0 0 24 24" className="w-3 h-3 shrink-0" fill="none" stroke="currentColor"
              strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="3" /><circle cx="12" cy="12" r="9" />
            </svg>
            Slaves (receive touch)
          </span>
        </div>
      </div>

      {/* ── split panel ── */}
      <div className="group-mirror-layout">
        {/* master — left, prominent */}
        <div className="group-mirror-master-panel">
          <div className="group-mirror-master-label">
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor"
              strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
            </svg>
            Master · touch here to fan out
          </div>
          {masterDevice ? (
            <ScrcpyView
              sessionId={sessionPrefix + '-master'}
              deviceId={masterDevice.deviceId}
              adbSerial={masterDevice.adbSerial}
              deviceName={getAndroidIdentity(masterDevice) || masterDevice.deviceId}
              onClose={onExit}
              onTouchDevice={handleMasterTouch}
            />
          ) : (
            <div className="group-mirror-offline">
              <svg viewBox="0 0 24 24" className="w-8 h-8" fill="none" stroke="var(--muted)"
                strokeWidth="1.5" strokeLinecap="round">
                <rect x="5" y="2" width="14" height="20" rx="2" /><path d="M12 18h.01" />
              </svg>
              <span>Master device offline</span>
            </div>
          )}
        </div>

        {/* slaves — right, compact grid */}
        <div className="group-mirror-slaves-panel">
          <div className="group-mirror-slave-label">
            <svg viewBox="0 0 24 24" className="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor"
              strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="3" /><circle cx="12" cy="12" r="9" />
            </svg>
            {slaveDevices.length} slave device{slaveDevices.length !== 1 ? 's' : ''} · receiving touch
          </div>
          <div className="group-mirror-slaves-grid">
            {slaveDevices.map((slave) => (
              <div key={slave.deviceId} className="group-mirror-slave-card">
                <ScrcpyView
                  sessionId={sessionPrefix + '-' + slave.deviceId}
                  deviceId={slave.deviceId}
                  adbSerial={slave.adbSerial}
                  deviceName={getAndroidIdentity(slave) || slave.deviceId}
                  onClose={() => {/* slaves persist until exit */}}
                />
              </div>
            ))}
            {slaveDevices.length === 0 && (
              <div className="group-mirror-offline" style={{ gridColumn: '1/-1' }}>
                <svg viewBox="0 0 24 24" className="w-8 h-8" fill="none" stroke="var(--muted)"
                  strokeWidth="1.5" strokeLinecap="round">
                  <rect x="5" y="2" width="14" height="20" rx="2" /><path d="M12 18h.01" />
                </svg>
                <span>No slave devices in this group</span>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
