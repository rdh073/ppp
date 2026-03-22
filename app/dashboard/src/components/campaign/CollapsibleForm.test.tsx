import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { CollapsibleForm, collapsibleFormReducer } from './CollapsibleForm';

describe('CollapsibleForm', () => {
  it('reduces open/close transitions correctly', () => {
    expect(collapsibleFormReducer(false, 'open')).toBe(true);
    expect(collapsibleFormReducer(true, 'close')).toBe(false);
    expect(collapsibleFormReducer(false, 'close')).toBe(false);
  });

  it('renders trigger when closed', () => {
    const html = renderToStaticMarkup(
      <CollapsibleForm triggerLabel="Start Campaign">
        {() => <div>form-content</div>}
      </CollapsibleForm>,
    );

    expect(html).toContain('+ Start Campaign');
    expect(html).not.toContain('form-content');
  });

  it('renders form content when initially open', () => {
    const html = renderToStaticMarkup(
      <CollapsibleForm triggerLabel="Start Campaign" initialOpen>
        {() => <div>form-content</div>}
      </CollapsibleForm>,
    );

    expect(html).toContain('form-content');
    expect(html).not.toContain('+ Start Campaign');
  });
});

