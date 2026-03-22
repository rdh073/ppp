import type React from 'react';

/** Status dot color for campaign-style status (running/done/failed). */
export const campaignStatusColor: Record<string, string> = {
  running: 'var(--yellow, #facc15)',
  done: 'var(--green)',
  failed: 'var(--red, #f87171)',
  pending: 'var(--muted)',
};

/** Status dot color for persona status (available/in_use/used/failed). */
export const personaStatusColor: Record<string, string> = {
  available: 'var(--green)',
  in_use: 'var(--yellow, #facc15)',
  used: 'var(--muted)',
  failed: 'var(--red, #f87171)',
};

/** Shared text input style used across campaign form panels. */
export const inputStyle: React.CSSProperties = {
  background: 'var(--bg, #1a1a2e)',
  border: '1px solid var(--border)',
  borderRadius: '0.25rem',
  color: 'inherit',
  fontSize: '0.78rem',
  padding: '0.25rem 0.5rem',
  minWidth: 100,
};

/** Primary accent button (indigo/purple). */
export const accentBtnStyle: React.CSSProperties = {
  cursor: 'pointer',
  background: 'var(--accent, #818cf8)',
  border: 'none',
  borderRadius: '0.375rem',
  color: '#fff',
  fontSize: '0.75rem',
  padding: '0.3rem 0.75rem',
};

/** Outline/ghost button. */
export const outlineBtn: React.CSSProperties = {
  cursor: 'pointer',
  background: 'none',
  border: '1px solid var(--border)',
  borderRadius: '0.25rem',
  color: 'inherit',
  fontSize: '0.75rem',
  padding: '0.2rem 0.6rem',
};
