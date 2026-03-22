import { useEffect, useMemo, useState } from 'react';
import {
  listAccountCreations,
  startAccountCreation,
  type AccountCreation,
} from '../api/accountCreations';
import { listPersonas, type Persona } from '../api/personas';
import { listDevices } from '../../device-control/api/devices';
import type { Device } from '../../../types';
import { PersonasPanel } from './PersonasPanel';
import { AccountsPanel } from './AccountsPanel';
import { LoginPanel } from './LoginPanel';
import { runStatusColor as STATUS_COLOR, inputStyle, accentBtnStyle as btnStyle } from '../../../components/ui/statusStyles';
import { useDataList } from '../../../shared/react/useDataList';
import { StatusRow } from '../../../components/ui/StatusRow';
import { CollapsibleForm } from '../../../components/ui/CollapsibleForm';

// ── inner sub-tabs ────────────────────────────────────────────────────────────

type SubTab = 'create' | 'login' | 'accounts' | 'personas';

function AccountCreationRow({ run }: { run: AccountCreation }) {
  return (
    <StatusRow
      status={run.status}
      statusColors={STATUS_COLOR}
      primary={(
        <span style={{ fontWeight: 600, minWidth: 200, fontFamily: 'monospace', fontSize: '0.72rem' }}>
          {run.id.slice(0, 24)}
        </span>
      )}
      meta={<span style={{ color: 'var(--muted)', minWidth: 60 }}>{run.kind}</span>}
      tail={run.phase && (
        <span
          style={{
            fontSize: '0.7rem',
            padding: '0.1rem 0.4rem',
            border: '1px solid var(--border)',
            borderRadius: '0.25rem',
          }}
        >
          phase: {run.phase}
        </span>
      )}
      error={run.error}
    />
  );
}

// ── start account-creation form ───────────────────────────────────────────────

function StartAccountCreationFormBody({ onStarted, close }: { onStarted: () => void; close: () => void }) {
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [devices, setDevices] = useState<Device[]>([]);
  const [form, setForm] = useState({
    deviceId: '',
    personaId: '',
    kind: 'google+instagram' as 'google+instagram' | 'google',
    phoneNumber: '',
  });

  useEffect(() => {
    listPersonas({ status: 'available' }).then(setPersonas).catch(() => {});
    listDevices().then((ds) => setDevices(ds.filter((d) => d.connected))).catch(() => {});
  }, []);

  function set(field: string, value: string) {
    setForm((f) => ({ ...f, [field]: value }));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.deviceId.trim()) {
      setError('Device ID is required');
      return;
    }
    setStarting(true);
    setError('');
    try {
      await startAccountCreation({
        kind: form.kind,
        deviceId: form.deviceId.trim(),
        personaId: form.personaId || undefined,
        phoneNumber: form.phoneNumber || undefined,
      });
      close();
      onStarted();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Start failed');
    } finally {
      setStarting(false);
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: '0.5rem',
        padding: '0.75rem',
        border: '1px solid var(--border)',
        borderRadius: '0.5rem',
        marginTop: '0.5rem',
        fontSize: '0.8rem',
      }}
    >
      <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
        <select value={form.kind} onChange={(e) => set('kind', e.target.value)} style={inputStyle}>
          <option value="google+instagram">Google + Instagram</option>
          <option value="google">Google only</option>
        </select>
        <select
          value={form.deviceId}
          onChange={(e) => set('deviceId', e.target.value)}
          style={{ ...inputStyle, minWidth: 200 }}
        >
          <option value="">— select device —</option>
          {devices.map((d) => {
            const label = [d.deviceMetadata?.brand, d.deviceMetadata?.model].filter(Boolean).join(' ')
              || d.adbSerial
              || d.deviceId;
            return (
              <option key={d.deviceId} value={d.deviceId}>
                {label}
              </option>
            );
          })}
        </select>
        <select value={form.personaId} onChange={(e) => set('personaId', e.target.value)} style={inputStyle}>
          <option value="">— generate persona via AI —</option>
          {personas.map((p) => (
            <option key={p.id} value={p.id}>
              {p.firstName} {p.lastName} ({p.kind})
            </option>
          ))}
        </select>
        <input
          placeholder="Phone number (for OTP)"
          value={form.phoneNumber}
          onChange={(e) => set('phoneNumber', e.target.value)}
          style={inputStyle}
        />
      </div>
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.75rem' }}>{error}</span>}
      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <button type="submit" disabled={starting} style={btnStyle}>
          {starting ? 'Starting...' : 'Start'}
        </button>
        <button
          type="button"
          onClick={close}
          style={{ ...btnStyle, background: 'none', color: 'var(--muted)' }}
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function StartAccountCreationForm({ onStarted }: { onStarted: () => void }) {
  return (
    <CollapsibleForm triggerLabel="Start Account Creation">
      {({ close }) => <StartAccountCreationFormBody onStarted={onStarted} close={close} />}
    </CollapsibleForm>
  );
}

// ── account-creation list ─────────────────────────────────────────────────────

function AccountCreationList() {
  const stream = useMemo(() => ({
    topics: ['account-manager.account-creations'],
    getKey: (run: AccountCreation) => run.id,
  }), []);
  const { data: runs, loading, error, reload: load } = useDataList(listAccountCreations, {
    stream,
  });

  const hasRunning = runs.some((c) => c.status === 'running');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
          {runs.length} account creation{runs.length !== 1 ? 's' : ''}
          {hasRunning && (
            <span style={{ marginLeft: '0.5rem', color: 'var(--yellow, #facc15)', fontSize: '0.72rem' }}>
              ● running
            </span>
          )}
        </span>
        <StartAccountCreationForm onStarted={load} />
      </div>

      {loading && <span style={{ color: 'var(--muted)', fontSize: '0.8rem' }}>Loading...</span>}
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.8rem' }}>{error}</span>}

      <div style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', overflow: 'hidden' }}>
        {runs.length === 0 && !loading ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--muted)', fontSize: '0.8rem' }}>
            No account creations yet. Click "+ Start Account Creation" to begin.
          </div>
        ) : (
          runs.map((c) => <AccountCreationRow key={c.id} run={c} />)
        )}
      </div>
    </div>
  );
}

// ── main panel ────────────────────────────────────────────────────────────────

export function AccountManagerPanel() {
  const [sub, setSub] = useState<SubTab>('create');

  const subTabs: { key: SubTab; label: string }[] = [
    { key: 'create', label: 'Create' },
    { key: 'login', label: 'Login' },
    { key: 'accounts', label: 'Accounts' },
    { key: 'personas', label: 'Personas' },
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
      {/* sub-tab strip */}
      <div style={{ display: 'flex', gap: '0.25rem', borderBottom: '1px solid var(--border)', paddingBottom: '0.5rem' }}>
        {subTabs.map((t) => (
          <button
            key={t.key}
            type="button"
            onClick={() => setSub(t.key)}
            style={{
              cursor: 'pointer',
              background: 'none',
              border: 'none',
              borderBottom: sub === t.key ? '2px solid var(--accent, #818cf8)' : '2px solid transparent',
              color: sub === t.key ? 'var(--accent, #818cf8)' : 'var(--muted)',
              fontSize: '0.8rem',
              fontWeight: sub === t.key ? 600 : 400,
              padding: '0.25rem 0.75rem 0.4rem',
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {sub === 'create' && <AccountCreationList />}
      {sub === 'login' && <LoginPanel />}
      {sub === 'accounts' && <AccountsPanel />}
      {sub === 'personas' && <PersonasPanel />}
    </div>
  );
}
