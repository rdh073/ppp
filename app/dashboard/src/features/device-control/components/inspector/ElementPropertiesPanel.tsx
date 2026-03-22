import { useRef, useState } from 'react';
import type { UiTarget } from '../../types/inspector';

interface Props {
  target: UiTarget | null;
  onClose: () => void;
}

// ── inline copy button (smaller than shared CopyButton) ──────────────────────

function CopyBtn({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>();

  const handleCopy = () => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="btn-secondary"
      style={{ minHeight: 22, padding: '0.15rem 0.45rem', fontSize: '0.65rem', gap: '0.25rem' }}
      title={`Copy ${label ?? 'value'}`}
    >
      {copied ? (
        <svg viewBox="0 0 24 24" className="w-2.5 h-2.5 shrink-0" fill="none" stroke="#4ade80" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
          <path d="M20 6 9 17l-5-5" />
        </svg>
      ) : (
        <svg viewBox="0 0 24 24" className="w-2.5 h-2.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect width="14" height="14" x="8" y="8" rx="2" /><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
        </svg>
      )}
      {label ?? 'Copy'}
    </button>
  );
}

// ── selector row ─────────────────────────────────────────────────────────────

interface SelectorDef {
  label: string;
  selectorKind: string;
  value: string;
}

function SelectorRow({ def, uiRole }: { def: SelectorDef; uiRole: string }) {
  const isInput = uiRole === 'input';
  const tapSnippet = `tap({ kind: 'click', target: { kind: '${def.selectorKind}', value: '${def.value.replace(/'/g, "\\'")}' } });`;
  const inputSnippet = isInput
    ? `input({ kind: '${def.selectorKind}', value: '${def.value.replace(/'/g, "\\'")}' }, 'your_text');`
    : null;

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.3rem 0' }}>
      <span
        style={{
          fontSize: '0.62rem',
          fontWeight: 600,
          color: 'var(--muted)',
          minWidth: 72,
          textTransform: 'uppercase',
          letterSpacing: '0.03em',
        }}
      >
        {def.label}
      </span>
      <code
        style={{
          flex: 1,
          fontSize: '0.68rem',
          fontFamily: 'Fira Code, ui-monospace, monospace',
          color: '#93c5fd',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
        title={def.value}
      >
        {def.value}
      </code>
      <CopyBtn text={def.value} label="value" />
      <CopyBtn text={tapSnippet} label="tap()" />
      {inputSnippet && <CopyBtn text={inputSnippet} label="input()" />}
    </div>
  );
}

// ── flag pill ────────────────────────────────────────────────────────────────

function FlagPill({ name, value }: { name: string; value: boolean }) {
  return (
    <span
      style={{
        fontSize: '0.6rem',
        fontWeight: 600,
        padding: '0.1rem 0.4rem',
        borderRadius: 999,
        background: value ? 'rgba(34,197,94,0.12)' : 'rgba(255,255,255,0.04)',
        color: value ? '#4ade80' : 'var(--muted)',
        border: `1px solid ${value ? 'rgba(34,197,94,0.25)' : 'rgba(255,255,255,0.08)'}`,
      }}
    >
      {name}
    </span>
  );
}

// ── main panel ───────────────────────────────────────────────────────────────

export function ElementPropertiesPanel({ target, onClose }: Props) {
  if (!target) {
    return (
      <div
        style={{
          padding: '1rem 1.25rem',
          fontSize: '0.78rem',
          color: 'var(--muted)',
          borderTop: '1px solid var(--border)',
        }}
      >
        Click an element to inspect its properties.
      </div>
    );
  }

  const selectors: SelectorDef[] = [];
  if (target.text) selectors.push({ label: 'text', selectorKind: 'text', value: target.text });
  if (target.resourceId) selectors.push({ label: 'resource_id', selectorKind: 'resource_id', value: target.resourceId });
  if (target.semanticKey) selectors.push({ label: 'semantic_key', selectorKind: 'semantic_key', value: target.semanticKey });
  if (target.label) selectors.push({ label: 'content_desc', selectorKind: 'content_desc', value: target.label });

  const [l, t, r, b] = target.bounds;

  return (
    <div
      style={{
        borderTop: '1px solid var(--border)',
        background: 'var(--surface)',
        padding: '0.75rem 1rem',
        maxHeight: 320,
        overflowY: 'auto',
      }}
    >
      {/* header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
        <span
          style={{
            fontSize: '0.62rem',
            fontWeight: 700,
            textTransform: 'uppercase',
            letterSpacing: '0.04em',
            padding: '0.15rem 0.5rem',
            borderRadius: 999,
            background: 'rgba(59,130,246,0.12)',
            color: '#93c5fd',
            border: '1px solid rgba(59,130,246,0.25)',
          }}
        >
          {target.uiRole}
        </span>
        {target.role && (
          <span style={{ fontSize: '0.7rem', color: 'var(--muted)' }}>{target.role}</span>
        )}
        <span style={{ flex: 1 }} />
        <button
          type="button"
          onClick={onClose}
          className="btn-secondary"
          style={{ minHeight: 22, padding: '0.15rem 0.4rem', fontSize: '0.65rem' }}
        >
          <svg viewBox="0 0 24 24" className="w-3 h-3" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M18 6 6 18M6 6l12 12" />
          </svg>
        </button>
      </div>

      {/* selectors */}
      {selectors.length > 0 && (
        <div style={{ marginBottom: '0.5rem' }}>
          <div style={{ fontSize: '0.62rem', fontWeight: 700, color: 'var(--muted)', marginBottom: '0.25rem', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Selectors
          </div>
          {selectors.map((s) => (
            <SelectorRow key={s.selectorKind} def={s} uiRole={target.uiRole} />
          ))}
        </div>
      )}

      {/* properties */}
      <div>
        <div style={{ fontSize: '0.62rem', fontWeight: 700, color: 'var(--muted)', marginBottom: '0.35rem', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
          Properties
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', marginBottom: '0.4rem', flexWrap: 'wrap' }}>
          <code
            style={{
              fontSize: '0.66rem',
              fontFamily: 'Fira Code, ui-monospace, monospace',
              color: 'var(--muted)',
            }}
          >
            bounds [{l}, {t}, {r}, {b}] ({r - l}x{b - t})
          </code>
          <CopyBtn text={`[${l}, ${t}, ${r}, ${b}]`} label="bounds" />
        </div>
        <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap' }}>
          <FlagPill name="actionable" value={target.actionable} />
          <FlagPill name="enabled" value={target.enabled} />
          <FlagPill name="scrollable" value={target.scrollable} />
          <FlagPill name="focused" value={target.focused} />
          {target.checked !== null && <FlagPill name="checked" value={target.checked} />}
          {target.selected && <FlagPill name="selected" value />}
          {target.password && <FlagPill name="password" value />}
        </div>
      </div>

      {/* target ID */}
      <div style={{ marginTop: '0.5rem', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
        <span style={{ fontSize: '0.6rem', color: 'var(--muted)' }}>targetId:</span>
        <code style={{ fontSize: '0.62rem', fontFamily: 'Fira Code, ui-monospace, monospace', color: 'var(--muted)', opacity: 0.7 }}>
          {target.targetId}
        </code>
      </div>
    </div>
  );
}
