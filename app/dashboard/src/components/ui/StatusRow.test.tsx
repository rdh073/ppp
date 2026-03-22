import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { StatusRow, resolveStatusColor } from './StatusRow';

describe('StatusRow', () => {
  it('resolves configured color and falls back to muted', () => {
    expect(resolveStatusColor('running', { running: '#facc15' })).toBe('#facc15');
    expect(resolveStatusColor('unknown', { running: '#facc15' })).toBe('var(--muted)');
  });

  it('renders status row with status and error message', () => {
    const html = renderToStaticMarkup(
      <StatusRow
        status="running"
        statusColors={{ running: '#facc15' }}
        primary={<span>campaign-1</span>}
        meta={<span>google</span>}
        error="task failed"
      />,
    );

    expect(html).toContain('campaign-1');
    expect(html).toContain('google');
    expect(html).toContain('running');
    expect(html).toContain('task failed');
    expect(html).toContain('background:#facc15');
  });

  it('renders details and custom status node', () => {
    const html = renderToStaticMarkup(
      <StatusRow
        status="done"
        statusColors={{ done: '#22c55e' }}
        primary={<span>campaign-2</span>}
        statusNode={<span>DONE BADGE</span>}
        details={<div>detail-content</div>}
      />,
    );

    expect(html).toContain('campaign-2');
    expect(html).toContain('DONE BADGE');
    expect(html).toContain('detail-content');
  });
});

