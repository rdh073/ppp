import type { ReactNode } from 'react';

export interface StatusDotProps {
  status: string;
  statusColors: Record<string, string>;
  size?: number;
}

export function resolveStatusColor(status: string, statusColors: Record<string, string>): string {
  return statusColors[status] ?? 'var(--muted)';
}

export function StatusDot({ status, statusColors, size = 8 }: StatusDotProps) {
  return (
    <span
      style={{
        width: size,
        height: size,
        borderRadius: '50%',
        background: resolveStatusColor(status, statusColors),
        flexShrink: 0,
      }}
    />
  );
}

export interface StatusRowProps {
  status: string;
  statusColors: Record<string, string>;
  primary: ReactNode;
  meta?: ReactNode;
  tail?: ReactNode;
  statusText?: string;
  statusNode?: ReactNode;
  error?: string;
  details?: ReactNode;
}

export function StatusRow({
  status,
  statusColors,
  primary,
  meta,
  tail,
  statusText,
  statusNode,
  error,
  details,
}: StatusRowProps) {
  const color = resolveStatusColor(status, statusColors);

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: details ? '0.35rem' : 0,
        padding: '0.5rem 0.75rem',
        borderBottom: '1px solid var(--border)',
        fontSize: '0.8rem',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        <StatusDot status={status} statusColors={statusColors} />
        {primary}
        {meta}
        {tail}

        <span style={{ marginLeft: 'auto', display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
          {statusNode ?? (
            <span style={{ fontSize: '0.72rem', color }}>
              {statusText ?? status}
            </span>
          )}
        </span>

        {error && (
          <span
            style={{
              color: 'var(--red, #f87171)',
              fontSize: '0.7rem',
              maxWidth: 200,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
            title={error}
          >
            {error}
          </span>
        )}
      </div>

      {details}
    </div>
  );
}

