import { useState } from 'react';
import { useEvents } from '../../hooks/useEvents';

export function EventPanel() {
  const [kindFilter, setKindFilter] = useState('');
  const [sourceFilter, setSourceFilter] = useState('');
  const [deviceFilter, setDeviceFilter] = useState('');
  const [order, setOrder] = useState<'asc' | 'desc'>('desc');
  const [limit, setLimit] = useState(25);
  const { entries, loading, error, refresh, loadMore, hasMore } = useEvents(5000, {
    limit,
    kind: kindFilter || undefined,
    source: sourceFilter || undefined,
    order,
    deviceId: deviceFilter || undefined,
  });
  const [selected, setSelected] = useState<string>('');

  return (
    <section className="panel">
      <div className="panel-header">
        <h2>Events</h2>
        <div className="row">
          <button type="button" onClick={() => refresh()}>
            {loading ? 'Refreshing...' : 'Refresh'}
          </button>
        </div>
      </div>

      <div className="event-filters">
        <label>
          Kind
          <input value={kindFilter} onChange={(event) => setKindFilter(event.target.value)} />
        </label>
        <label>
          Source
          <input value={sourceFilter} onChange={(event) => setSourceFilter(event.target.value)} />
        </label>
        <label>
          Device ID
          <input value={deviceFilter} onChange={(event) => setDeviceFilter(event.target.value)} />
        </label>
        <label>
          Order
          <select value={order} onChange={(event) => setOrder(event.target.value as 'asc' | 'desc')}>
            <option value="desc">desc</option>
            <option value="asc">asc</option>
          </select>
        </label>
        <label>
          Limit
          <input
            value={limit}
            type="number"
            min={1}
            max={500}
            onChange={(event) => {
              const next = event.target.value.trim();
              setLimit(next === '' ? 25 : Number(next));
            }}
          />
        </label>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Kind</th>
              <th>Device ID</th>
              <th>Seq</th>
              <th>Occurred</th>
              <th>Accepted</th>
              <th>Source</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody>
            {entries.length === 0 && !loading ? (
              <tr>
                <td colSpan={8}>No events in the current filter.</td>
              </tr>
            ) : (
              entries.map((row) => (
                <tr key={`${row.event.ID}-${row.acceptedAt}`}>
                  <td>{row.event.ID}</td>
                  <td>{row.event.Kind}</td>
                  <td>{row.event.DeviceID || '-'}</td>
                  <td>{row.event.SeqNo}</td>
                  <td>{new Date(row.event.OccurredAt).toLocaleString()}</td>
                  <td>{new Date(row.acceptedAt).toLocaleString()}</td>
                  <td>{row.source}</td>
                  <td>
                    <button
                      type="button"
                      onClick={() => setSelected(`${row.event.ID}`)}
                    >
                      Show payload
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {hasMore && (
        <div className="row">
          <button type="button" onClick={() => void loadMore()} disabled={loading}>
            Load older events
          </button>
        </div>
      )}

      {selected && (
        <pre className="payload">
          {JSON.stringify(
            entries.find((item) => item.event.ID === selected)?.event?.Payload ?? null,
            null,
            2,
          )}
        </pre>
      )}
    </section>
  );
}
