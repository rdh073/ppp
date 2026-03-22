import { useEffect, useState } from 'react';
import {
  createLoginPlatformApi,
  type LoginCampaign,
} from '../../api/login';
import { listAccounts, type Account } from '../../api/accounts';
import { campaignStatusColor as STATUS_COLOR, inputStyle } from './styles';
import { useDataList } from '../../hooks/useDataList';
import { StatusRow } from './StatusRow';
import { CollapsibleForm } from './CollapsibleForm';

function LoginRow({ campaign }: { campaign: LoginCampaign }) {
  return (
    <StatusRow
      status={campaign.status}
      statusColors={STATUS_COLOR}
      primary={(
        <span style={{ fontFamily: 'monospace', fontSize: '0.72rem', color: 'var(--muted)', minWidth: 160 }}>
          {campaign.accountId.slice(0, 20)}
        </span>
      )}
      meta={(
        <span style={{ color: 'var(--muted)', minWidth: 120, fontSize: '0.72rem' }}>
          device: {campaign.deviceId.slice(0, 12)}
        </span>
      )}
      error={campaign.error}
    />
  );
}

// ── shared form ──────────────────────────────────────────────────────────────

type InputMode = 'account' | 'manual';

interface StartFormProps {
  accountKind: 'google' | 'instagram';
  accentColor: string;
  onStarted: () => void;
  onStart: (req: { accountId?: string; email?: string; password?: string; deviceId: string }) => Promise<unknown>;
}

function StartLoginFormBody({ accountKind, accentColor, onStarted, onStart, close }: StartFormProps & { close: () => void }) {
  const [mode, setMode] = useState<InputMode>('account');
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [form, setForm] = useState({ accountId: '', email: '', password: '', deviceId: '' });

  useEffect(() => {
    if (mode === 'account') {
      listAccounts({ kind: accountKind }).then(setAccounts).catch(() => {});
    }
  }, [mode, accountKind]);

  function set(field: string, value: string) {
    setForm((f) => ({ ...f, [field]: value }));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.deviceId.trim()) { setError('Device ID is required'); return; }
    if (mode === 'account' && !form.accountId) { setError('Select an account'); return; }
    if (mode === 'manual' && (!form.email || !form.password)) { setError('Email and password are required'); return; }
    setStarting(true);
    setError('');
    try {
      await onStart(
        mode === 'account'
          ? { accountId: form.accountId, deviceId: form.deviceId.trim() }
          : { email: form.email, password: form.password, deviceId: form.deviceId.trim() },
      );
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
      {/* mode toggle */}
      <div style={{ display: 'flex', gap: '0.25rem' }}>
        {(['account', 'manual'] as InputMode[]).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => setMode(m)}
            style={{
              cursor: 'pointer',
              border: '1px solid var(--border)',
              borderRadius: '0.25rem',
              background: mode === m ? accentColor : 'none',
              color: mode === m ? '#fff' : 'var(--muted)',
              fontSize: '0.72rem',
              padding: '0.2rem 0.6rem',
            }}
          >
            {m === 'account' ? 'From DB' : 'Manual input'}
          </button>
        ))}
      </div>

      <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
        <input
          placeholder="Device ID *"
          value={form.deviceId}
          onChange={(e) => set('deviceId', e.target.value)}
          style={{ ...inputStyle, minWidth: 200 }}
        />
        {mode === 'account' ? (
          <select value={form.accountId} onChange={(e) => set('accountId', e.target.value)} style={inputStyle}>
            <option value="">— select account —</option>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.email || a.username || a.id} ({a.status})
              </option>
            ))}
          </select>
        ) : (
          <>
            <input
              placeholder="Email *"
              value={form.email}
              onChange={(e) => set('email', e.target.value)}
              style={inputStyle}
            />
            <input
              placeholder="Password *"
              type="password"
              value={form.password}
              onChange={(e) => set('password', e.target.value)}
              style={inputStyle}
            />
          </>
        )}
      </div>

      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.75rem' }}>{error}</span>}

      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <button type="submit" disabled={starting} style={{ ...btnStyle, background: accentColor }}>
          {starting ? 'Starting...' : 'Login'}
        </button>
        <button type="button" onClick={close} style={{ ...btnStyle, background: 'none', color: 'var(--muted)' }}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function StartLoginForm({ accountKind, accentColor, onStarted, onStart }: StartFormProps) {
  const label = accountKind === 'google' ? 'Login Google' : 'Login Instagram';

  return (
    <CollapsibleForm triggerLabel={label} accentColor={accentColor}>
      {({ close }) => (
        <StartLoginFormBody
          accountKind={accountKind}
          accentColor={accentColor}
          onStarted={onStarted}
          onStart={onStart}
          close={close}
        />
      )}
    </CollapsibleForm>
  );
}

// Google-blue submit button — intentionally different from global accent
const btnStyle: React.CSSProperties = {
  cursor: 'pointer',
  background: '#4285F4',
  border: 'none',
  borderRadius: '0.375rem',
  color: '#fff',
  fontSize: '0.75rem',
  padding: '0.3rem 0.75rem',
};

// ── unified platform panel ────────────────────────────────────────────────────

const PLATFORM_ACCENT: Record<'google' | 'instagram', string> = {
  google: '#4285F4',
  instagram: '#E1306C',
};

interface LoginPlatformPanelProps {
  platform: 'google' | 'instagram';
  label: string;
}

function LoginPlatformPanel({ platform, label }: LoginPlatformPanelProps) {
  const api = createLoginPlatformApi(platform);
  const { data: campaigns, loading, error, reload } = useDataList(api.list, {
    pollWhile: (c) => c.status === 'running',
  });

  return (
    <LoginList
      campaigns={campaigns}
      loading={loading}
      error={error}
      emptyText={`No ${label} login sessions yet.`}
      form={
        <StartLoginForm
          accountKind={platform}
          accentColor={PLATFORM_ACCENT[platform]}
          onStarted={reload}
          onStart={api.start}
        />
      }
      hasRunning={campaigns.some((c) => c.status === 'running')}
    />
  );
}

export function GoogleLoginPanel() {
  return <LoginPlatformPanel platform="google" label="Google" />;
}

export function InstagramLoginPanel() {
  return <LoginPlatformPanel platform="instagram" label="Instagram" />;
}

// ── shared list layout ────────────────────────────────────────────────────────

interface LoginListProps {
  campaigns: LoginCampaign[];
  loading: boolean;
  error: string;
  emptyText: string;
  form: React.ReactNode;
  hasRunning: boolean;
}

function LoginList({ campaigns, loading, error, emptyText, form, hasRunning }: LoginListProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
          {campaigns.length} login{campaigns.length !== 1 ? 's' : ''}
          {hasRunning && (
            <span style={{ marginLeft: '0.5rem', color: 'var(--yellow, #facc15)', fontSize: '0.72rem' }}>
              ● running
            </span>
          )}
        </span>
        {form}
      </div>

      {loading && <span style={{ color: 'var(--muted)', fontSize: '0.8rem' }}>Loading...</span>}
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.8rem' }}>{error}</span>}

      <div style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', overflow: 'hidden' }}>
        {campaigns.length === 0 && !loading ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--muted)', fontSize: '0.8rem' }}>
            {emptyText}
          </div>
        ) : (
          campaigns.map((c) => <LoginRow key={c.id} campaign={c} />)
        )}
      </div>
    </div>
  );
}

// ── main panel ────────────────────────────────────────────────────────────────

type Platform = 'google' | 'instagram';

export function LoginPanel() {
  const [platform, setPlatform] = useState<Platform>('google');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      {/* platform toggle */}
      <div style={{ display: 'flex', gap: '0.25rem' }}>
        {([
          { key: 'google' as Platform, label: 'Google', color: '#4285F4' },
          { key: 'instagram' as Platform, label: 'Instagram', color: '#E1306C' },
        ]).map((p) => (
          <button
            key={p.key}
            type="button"
            onClick={() => setPlatform(p.key)}
            style={{
              cursor: 'pointer',
              border: '1px solid var(--border)',
              borderRadius: '0.25rem',
              background: platform === p.key ? p.color : 'none',
              color: platform === p.key ? '#fff' : 'var(--muted)',
              fontSize: '0.75rem',
              fontWeight: platform === p.key ? 600 : 400,
              padding: '0.25rem 0.75rem',
            }}
          >
            {p.label}
          </button>
        ))}
      </div>

      {platform === 'google' ? <GoogleLoginPanel /> : <InstagramLoginPanel />}
    </div>
  );
}
