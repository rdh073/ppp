import { useDeferredValue, useEffect, useMemo, useState } from 'react';
import {
  Activity,
  ArrowDown,
  BellRing,
  Filter,
  RefreshCw,
  Search,
  X,
} from 'lucide-react';
import { getAcceptedEventPayload, type AcceptedPayloadPreview } from '../../api/events';
import { useEvents } from '../../hooks/useEvents';
import { EVENT_POLL_MS } from '../../config';

const MAX_PAYLOAD_BYTES = 16_384;
const MAX_CACHED_PAYLOADS = 10;
const MAX_EVENTS_FOR_FILTERS = 200;

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
  const [payloadById, setPayloadById] = useState<Record<string, AcceptedPayloadPreview>>({});
  const [payloadLoadingId, setPayloadLoadingId] = useState('');
  const [payloadError, setPayloadError] = useState<string | null>(null);

  const deferredKindFilter = useDeferredValue(kindFilter);
  const deferredSourceFilter = useDeferredValue(sourceFilter);
  const deferredDeviceFilter = useDeferredValue(deviceFilter);
  const deferredLimit = useDeferredValue(limit);
  const deferredOrder = useDeferredValue(order);

  const queryParams = useMemo(
    () => ({
      limit: deferredLimit,
      kind: deferredKindFilter.trim() || undefined,
      source: deferredSourceFilter.trim() || undefined,
      order: deferredOrder,
      deviceId: deferredDeviceFilter.trim() || undefined,
      includePayload: false,
    }),
    [deferredDeviceFilter, deferredKindFilter, deferredLimit, deferredOrder, deferredSourceFilter],
  );

  const { entries, loading, error, refresh, loadMore, hasMore } = useEvents(EVENT_POLL_MS, queryParams);

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
        const kind = asText(eventRecord.Kind);
        const source = asText(raw.source);
        const deviceId = asText(eventRecord.DeviceID);
        const occurredAtRaw = asText(eventRecord.OccurredAt);
        const acceptedAtRaw = asText(raw.acceptedAt);

        return {
          id,
          key: `${id}-${acceptedAtRaw}`,
          kind,
          deviceId,
          seqNo: asText(eventRecord.SeqNo),
          source,
          occurredAt: formatTimestamp(occurredAtRaw),
          acceptedAt: formatTimestamp(acceptedAtRaw),
        };
      })
      .filter((row): row is NonNullable<typeof row> => row !== null);
  }, [entries]);

  const selectedPayload = useMemo(() => {
    if (!selected) {
      return '';
    }
    const payload = payloadById[selected];
    if (!payload) {
      return '';
    }
    if (!payload.truncated) {
      return payload.payloadText || '(empty payload)';
    }
    const omitted = Math.max(0, payload.sizeBytes - payload.payloadText.length);
    return `${payload.payloadText}\n\n... payload truncated (${omitted} bytes omitted)`;
  }, [payloadById, selected]);

  const summary = useMemo(() => {
    const uniqueKinds = new Set<string>();
    const uniqueSources = new Set<string>();
    const uniqueDevices = new Set<string>();

    for (const row of rows) {
      if (row.kind !== '-') {
        uniqueKinds.add(row.kind);
      }
      if (row.source !== '-') {
        uniqueSources.add(row.source);
      }
      if (row.deviceId !== '-') {
        uniqueDevices.add(row.deviceId);
      }
    }

    return {
      visibleRows: rows.length,
      uniqueKinds: uniqueKinds.size,
      uniqueSources: uniqueSources.size,
      uniqueDevices: uniqueDevices.size,
    };
  }, [rows]);

  const topKinds = useMemo(() => {
    const map = new Map<string, number>();
    for (const row of rows.slice(0, MAX_EVENTS_FOR_FILTERS)) {
      if (row.kind === '-') {
        continue;
      }
      map.set(row.kind, (map.get(row.kind) ?? 0) + 1);
    }

    return Array.from(map.entries())
      .sort((a, b) => b[1] - a[1])
      .slice(0, 6)
      .map(([name]) => name);
  }, [rows]);

  const selectedRow = useMemo(() => {
    if (!selected) {
      return null;
    }
    return rows.find((row) => row.id === selected) ?? null;
  }, [rows, selected]);

  const clearFilters = () => {
    setKindFilter('');
    setSourceFilter('');
    setDeviceFilter('');
    setOrder('desc');
    setLimit(25);
  };

  useEffect(() => {
    if (!selected || payloadById[selected] !== undefined) {
      return;
    }

    let cancelled = false;
    setPayloadError(null);
    setPayloadLoadingId(selected);

    void getAcceptedEventPayload(selected, MAX_PAYLOAD_BYTES)
      .then((record) => {
        if (cancelled) {
          return;
        }
        setPayloadById((prev) => {
          const next: Record<string, AcceptedPayloadPreview> = { ...prev, [selected]: record };
          const keys = Object.keys(next);
          if (keys.length > MAX_CACHED_PAYLOADS) {
            delete next[keys[0]];
          }
          return next;
        });
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
        <div className="event-title">
          <BellRing className="event-title-icon" aria-hidden="true" />
          <h2>Event Stream</h2>
          <span className="event-count-badge">{summary.visibleRows} shown</span>
        </div>
        <div className="event-actions">
          <button type="button" className="btn-secondary" onClick={() => refresh()}>
            <RefreshCw className={loading ? 'event-spin' : ''} aria-hidden="true" />
            {loading ? 'Syncing…' : 'Refresh'}
          </button>
        </div>
      </div>

      <div className="event-summary">
        <article className="event-metric">
          <Activity aria-hidden="true" />
          <span className="event-metric-label">Visible</span>
          <strong>{summary.visibleRows}</strong>
        </article>
        <article className="event-metric">
          <Search aria-hidden="true" />
          <span className="event-metric-label">Kinds</span>
          <strong>{summary.uniqueKinds}</strong>
        </article>
        <article className="event-metric">
          <Filter aria-hidden="true" />
          <span className="event-metric-label">Sources / Devices</span>
          <strong>
            {summary.uniqueSources}/{summary.uniqueDevices}
          </strong>
        </article>
      </div>

      <div className="event-filters">
        <label className="event-filter-label" htmlFor="event-kind-filter">
          Kind
          <input
            id="event-kind-filter"
            value={kindFilter}
            onChange={(event) => setKindFilter(event.target.value)}
            placeholder="filter by kind"
          />
        </label>
        <label className="event-filter-label" htmlFor="event-source-filter">
          Source
          <input
            id="event-source-filter"
            value={sourceFilter}
            onChange={(event) => setSourceFilter(event.target.value)}
            placeholder="filter by source"
          />
        </label>
        <label className="event-filter-label" htmlFor="event-device-filter">
          Device
          <input
            id="event-device-filter"
            value={deviceFilter}
            onChange={(event) => setDeviceFilter(event.target.value)}
            placeholder="device id"
          />
        </label>
        <label className="event-filter-label" htmlFor="event-order-filter">
          Order
          <select
            id="event-order-filter"
            value={order}
            onChange={(event) => setOrder(event.target.value as 'asc' | 'desc')}
          >
            <option value="desc">desc</option>
            <option value="asc">asc</option>
          </select>
        </label>
        <label className="event-filter-label" htmlFor="event-limit-filter">
          Limit
          <input
            id="event-limit-filter"
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
        <div className="event-filter-actions">
          <button type="button" className="event-filter-clear" onClick={clearFilters}>
            <X aria-hidden="true" />
            Clear
          </button>
        </div>
      </div>

      {topKinds.length > 0 && (
        <div className="event-chip-row" aria-label="top kinds">
          {topKinds.map((kind) => (
            <button
              type="button"
              key={kind}
              className={`event-chip ${kindFilter === kind ? 'event-chip-active' : ''}`}
              onClick={() => setKindFilter((prev) => (prev === kind ? '' : kind))}
            >
              {kind}
            </button>
          ))}
        </div>
      )}

      <div className="event-layout">
        <div className="event-stream">
          <div className="panel-subhead">
            <span>Timeline</span>
            {loading && <span className="field-hint">syncing…</span>}
          </div>

          {error && <p role="alert" className="error">{error}</p>}

          <div className="table-wrap event-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Kind</th>
                  <th>Device</th>
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
                    <td colSpan={8} className="event-empty">
                      No events in the current filter.
                    </td>
                  </tr>
                ) : (
                  rows.map((row) => (
                    <tr
                      key={row.key}
                      className={selected === row.id ? 'event-row--active' : ''}
                    >
                      <td className="event-mono">{row.id}</td>
                      <td>
                        <span className="event-kind-pill">{row.kind}</span>
                      </td>
                      <td className="event-mono">{row.deviceId}</td>
                      <td>{row.seqNo}</td>
                      <td>{row.occurredAt}</td>
                      <td>{row.acceptedAt}</td>
                      <td>{row.source}</td>
                      <td>
                        <button
                          type="button"
                          className="task-inline-link"
                          onClick={() => setSelected(row.id)}
                        >
                          View
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
              <button
                type="button"
                className="btn-secondary"
                onClick={() => void loadMore()}
                disabled={loading}
              >
                <ArrowDown aria-hidden="true" />
                Load older events
              </button>
            </div>
          )}
        </div>

        <aside className="event-detail">
          <div className="panel-subhead">
            <span>Payload</span>
            {selected && (
              <button
                type="button"
                className="event-inline-action"
                onClick={() => setSelected('')}
              >
                Hide
              </button>
            )}
          </div>

          {!selectedRow ? (
            <p className="field-hint">Select a row to inspect event payload.</p>
          ) : (
            <div className="event-detail-body">
              <div className="event-detail-meta">
                <span>
                  <span className="event-detail-key">ID</span>
                  {selectedRow.id}
                </span>
                <span>
                  <span className="event-detail-key">Kind</span>
                  {selectedRow.kind}
                </span>
                <span>
                  <span className="event-detail-key">Device</span>
                  {selectedRow.deviceId}
                </span>
                <span>
                  <span className="event-detail-key">Source</span>
                  {selectedRow.source}
                </span>
                <span>
                  <span className="event-detail-key">Occurred</span>
                  {selectedRow.occurredAt}
                </span>
                <span>
                  <span className="event-detail-key">Accepted</span>
                  {selectedRow.acceptedAt}
                </span>
              </div>

              {payloadLoadingId === selected && <p className="status">Loading payload...</p>}
              {payloadError && <p role="alert" className="error">{payloadError}</p>}
              <pre className="payload event-payload">{selectedPayload || '(empty payload)'}</pre>
            </div>
          )}
        </aside>
      </div>

      {loading && rows.length === 0 ? (
        <p className="status" aria-live="polite">
          Syncing events…
        </p>
      ) : null}
    </section>
  );
}
