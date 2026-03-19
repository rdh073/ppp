import { useEffect, useMemo, useState } from 'react';
import { getAcceptedEvent } from '../../api/events';
import { useEvents } from '../../hooks/useEvents';
import { POLL_MS } from '../../config';

const MAX_PAYLOAD_CHARS = 60_000;

function safeStringify(value: unknown): string {
  const seen = new WeakSet<object>();
  try {
    return JSON.stringify(
      value,
      (_key, currentValue) => {
        if (typeof currentValue === 'bigint') {
          return currentValue.toString();
        }
        if (currentValue instanceof Error) {
          return {
            name: currentValue.name,
            message: currentValue.message,
            stack: currentValue.stack,
          };
        }
        if (currentValue && typeof currentValue === 'object') {
          if (seen.has(currentValue)) {
            return '[Circular]';
          }
          seen.add(currentValue);
        }
        return currentValue;
      },
      2,
    );
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return `{"error":"failed_to_render_payload","message":${JSON.stringify(message)}}`;
  }
}

function asText(value: unknown, fallback = '-'): string {
  if (typeof value === 'string') {
    return value;
  }
  if (typeof value === 'number' || typeof value === 'boolean' || typeof value === 'bigint') {
    return String(value);
  }
  return fallback;
}

function formatTimestamp(value: unknown): string {
  if (typeof value !== 'string' || value.length === 0) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function EventPanel() {
  const [kindFilter, setKindFilter] = useState('');
  const [sourceFilter, setSourceFilter] = useState('');
  const [deviceFilter, setDeviceFilter] = useState('');
  const [order, setOrder] = useState<'asc' | 'desc'>('desc');
  const [limit, setLimit] = useState(25);
  const [selected, setSelected] = useState('');
  const [payloadById, setPayloadById] = useState<Record<string, unknown>>({});
  const [payloadLoadingId, setPayloadLoadingId] = useState('');
  const [payloadError, setPayloadError] = useState<string | null>(null);
  const queryParams = useMemo(() => ({
    limit,
    kind: kindFilter || undefined,
    source: sourceFilter || undefined,
    order,
    deviceId: deviceFilter || undefined,
    includePayload: false,
  }), [deviceFilter, kindFilter, limit, order, sourceFilter]);

  const { entries, loading, error, refresh, loadMore, hasMore } = useEvents(POLL_MS, queryParams);

  const rows = useMemo(() => {
    return entries
      .map((item) => {
        const raw = item as unknown as Record<string, unknown>;
        const event = raw.event;
        if (!event || typeof event !== 'object') {
          return null;
        }
        const eventRecord = event as Record<string, unknown>;
        const id = asText(eventRecord.ID, '');
        if (!id) {
          return null;
        }

        return {
          id,
          key: `${id}-${asText(raw.acceptedAt, '')}`,
          kind: asText(eventRecord.Kind),
          deviceId: asText(eventRecord.DeviceID),
          seqNo: asText(eventRecord.SeqNo),
          occurredAt: formatTimestamp(eventRecord.OccurredAt),
          acceptedAt: formatTimestamp(raw.acceptedAt),
          source: asText(raw.source),
          payload: eventRecord.Payload ?? null,
        };
      })
      .filter((row): row is NonNullable<typeof row> => row !== null);
  }, [entries]);

  const selectedPayload = useMemo(() => {
    const row = rows.find((entry) => entry.id === selected);
    const cachedPayload = selected ? payloadById[selected] : undefined;
    const sourcePayload = cachedPayload !== undefined ? cachedPayload : row?.payload ?? null;
    const serialized = safeStringify(sourcePayload);
    if (serialized.length <= MAX_PAYLOAD_CHARS) {
      return serialized;
    }
    return `${serialized.slice(0, MAX_PAYLOAD_CHARS)}\n\n... payload truncated (${serialized.length - MAX_PAYLOAD_CHARS} chars omitted)`;
  }, [payloadById, rows, selected]);

  useEffect(() => {
    if (!selected || payloadById[selected] !== undefined) {
      return;
    }

    let cancelled = false;
    setPayloadError(null);
    setPayloadLoadingId(selected);

    void getAcceptedEvent(selected)
      .then((record) => {
        if (cancelled) {
          return;
        }
        setPayloadById((prev) => ({
          ...prev,
          [selected]: record.event?.Payload ?? null,
        }));
      })
      .catch((raw) => {
        if (cancelled) {
          return;
        }
        const message = raw instanceof Error ? raw.message : 'Failed to load payload';
        setPayloadError(message);
      })
      .finally(() => {
        if (!cancelled) {
          setPayloadLoadingId('');
        }
      });

    return () => {
      cancelled = true;
    };
  }, [payloadById, selected]);

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
              if (next === '') {
                setLimit(25);
                return;
              }
              const parsed = Number(next);
              if (!Number.isFinite(parsed)) {
                setLimit(25);
                return;
              }
              setLimit(Math.min(500, Math.max(1, Math.floor(parsed))));
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
            {rows.length === 0 && !loading ? (
              <tr>
                <td colSpan={8}>No events in the current filter.</td>
              </tr>
            ) : (
              rows.map((row) => (
                <tr key={row.key}>
                  <td>{row.id}</td>
                  <td>{row.kind}</td>
                  <td>{row.deviceId}</td>
                  <td>{row.seqNo}</td>
                  <td>{row.occurredAt}</td>
                  <td>{row.acceptedAt}</td>
                  <td>{row.source}</td>
                  <td>
                    <button type="button" onClick={() => setSelected(row.id)}>
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
        <div>
          <div className="row">
            <button type="button" onClick={() => setSelected('')}>Hide payload</button>
          </div>
          {payloadLoadingId === selected && <p>Loading payload...</p>}
          {payloadError && <p className="error">{payloadError}</p>}
          <pre className="payload">{selectedPayload}</pre>
        </div>
      )}
    </section>
  );
}
