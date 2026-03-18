import { useDevices } from '../../hooks/useDevices';

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function DevicePanel() {
  const { devices, loading, error, refresh } = useDevices(5000);

  return (
    <section className="panel">
      <div className="panel-header">
        <h2>Devices</h2>
        <button type="button" onClick={() => refresh()} disabled={loading}>
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="status">Connected devices: {devices.length}</div>

      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Device ID</th>
              <th>Session</th>
              <th>Agent instance</th>
              <th>Connected at</th>
              <th>Last heartbeat</th>
              <th>Capabilities</th>
            </tr>
          </thead>
          <tbody>
            {devices.length === 0 && !loading ? (
              <tr>
                <td colSpan={6}>No active device session.</td>
              </tr>
            ) : (
              devices.map((device) => (
                <tr key={device.deviceId}>
                  <td>{device.deviceId}</td>
                  <td>{device.sessionId || '-'}</td>
                  <td>{device.agentInstanceId || '-'}</td>
                  <td>{formatTime(device.connectedAt)}</td>
                  <td>{formatTime(device.lastHeartbeatAt)}</td>
                  <td>{device.capabilities?.map((c) => c.name).join(', ') || '-'}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
