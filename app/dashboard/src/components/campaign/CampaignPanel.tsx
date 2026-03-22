import { useEffect, useState } from 'react';
import { listCampaigns, startCampaign, type Campaign } from '../../api/campaigns';
import { listPersonas, type Persona } from '../../api/personas';
import { PersonaPanel } from './PersonaPanel';
import { AccountPanel } from './AccountPanel';
import { LoginPanel } from './LoginPanel';
import { PostPanel } from './PostPanel';
import { campaignStatusColor as STATUS_COLOR, inputStyle, accentBtnStyle as btnStyle } from './styles';
import { useDataList } from '../../hooks/useDataList';
import { StatusRow } from './StatusRow';
import { CollapsibleForm } from './CollapsibleForm';

// ── inner sub-tabs ────────────────────────────────────────────────────────────

type SubTab = 'campaigns' | 'personas' | 'accounts' | 'login' | 'posts';

function CampaignRow({ campaign }: { campaign: Campaign }) {
  return (
    <StatusRow
      status={campaign.status}
      statusColors={STATUS_COLOR}
      primary={(
        <span style={{ fontWeight: 600, minWidth: 200, fontFamily: 'monospace', fontSize: '0.72rem' }}>
          {campaign.id.slice(0, 24)}
        </span>
      )}
      meta={<span style={{ color: 'var(--muted)', minWidth: 60 }}>{campaign.kind}</span>}
      tail={campaign.phase && (
        <span
          style={{
            fontSize: '0.7rem',
            padding: '0.1rem 0.4rem',
            border: '1px solid var(--border)',
            borderRadius: '0.25rem',
          }}
        >
          phase: {campaign.phase}
        </span>
      )}
      error={campaign.error}
    />
  );
}

// ── start campaign form ───────────────────────────────────────────────────────

function StartCampaignFormBody({ onStarted, close }: { onStarted: () => void; close: () => void }) {
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [form, setForm] = useState({
    deviceId: '',
    personaId: '',
    kind: 'google+instagram' as 'google+instagram' | 'google',
    phoneNumber: '',
  });

  useEffect(() => {
    listPersonas({ status: 'available' }).then(setPersonas).catch(() => {});
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
      await startCampaign({
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
        <input
          placeholder="Device ID *"
          value={form.deviceId}
          onChange={(e) => set('deviceId', e.target.value)}
          style={{ ...inputStyle, minWidth: 200 }}
        />
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

function StartCampaignForm({ onStarted }: { onStarted: () => void }) {
  return (
    <CollapsibleForm triggerLabel="Start Campaign">
      {({ close }) => <StartCampaignFormBody onStarted={onStarted} close={close} />}
    </CollapsibleForm>
  );
}

// ── campaign list ─────────────────────────────────────────────────────────────

function CampaignList() {
  const { data: campaigns, loading, error, reload: load } = useDataList(listCampaigns, {
    pollWhile: (c) => c.status === 'running',
  });

  const hasRunning = campaigns.some((c) => c.status === 'running');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
          {campaigns.length} campaign{campaigns.length !== 1 ? 's' : ''}
          {hasRunning && (
            <span style={{ marginLeft: '0.5rem', color: 'var(--yellow, #facc15)', fontSize: '0.72rem' }}>
              ● running
            </span>
          )}
        </span>
        <StartCampaignForm onStarted={load} />
      </div>

      {loading && <span style={{ color: 'var(--muted)', fontSize: '0.8rem' }}>Loading...</span>}
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.8rem' }}>{error}</span>}

      <div style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', overflow: 'hidden' }}>
        {campaigns.length === 0 && !loading ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--muted)', fontSize: '0.8rem' }}>
            No campaigns yet. Click "+ Start Campaign" to begin.
          </div>
        ) : (
          campaigns.map((c) => <CampaignRow key={c.id} campaign={c} />)
        )}
      </div>
    </div>
  );
}

// ── main panel ────────────────────────────────────────────────────────────────

export function CampaignPanel() {
  const [sub, setSub] = useState<SubTab>('campaigns');

  const subTabs: { key: SubTab; label: string }[] = [
    { key: 'campaigns', label: 'Campaigns' },
    { key: 'personas', label: 'Personas' },
    { key: 'accounts', label: 'Accounts' },
    { key: 'login', label: 'Login Google' },
    { key: 'posts', label: 'Posts' },
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

      {sub === 'campaigns' && <CampaignList />}
      {sub === 'personas' && <PersonaPanel />}
      {sub === 'accounts' && <AccountPanel />}
      {sub === 'login' && <LoginPanel />}
      {sub === 'posts' && <PostPanel />}
    </div>
  );
}
