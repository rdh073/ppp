import { useReducer } from 'react';
import type { ReactNode } from 'react';
import { accentBtnStyle } from './statusStyles';

export type CollapsibleFormAction = 'open' | 'close';

export function collapsibleFormReducer(open: boolean, action: CollapsibleFormAction): boolean {
  return action === 'open';
}

export interface CollapsibleFormProps {
  triggerLabel: string;
  accentColor?: string;
  initialOpen?: boolean;
  children: (controls: { close: () => void }) => ReactNode;
}

export function CollapsibleForm({
  triggerLabel,
  accentColor,
  initialOpen = false,
  children,
}: CollapsibleFormProps) {
  const [open, dispatch] = useReducer(collapsibleFormReducer, initialOpen);

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => dispatch('open')}
        style={{
          ...accentBtnStyle,
          background: accentColor ?? accentBtnStyle.background,
          padding: '0.35rem 0.9rem',
        }}
      >
        + {triggerLabel}
      </button>
    );
  }

  return <>{children({ close: () => dispatch('close') })}</>;
}
