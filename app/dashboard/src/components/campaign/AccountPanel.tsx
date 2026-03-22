import { useCallback, useEffect, useState } from 'react';
import { listAccounts, type Account } from '../../api/accounts';

function AccountRow({ account }: { account: Account }) {
  const kindColor = account.kind === 'google' ? '#4285F4' : '#E1306C';

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.75rem',
        padding: '0.5rem 0.75rem',
        borderBottom: '1px solid var(--border)',
        fontSize: '0.8rem',
      }}
    >
      <span
        style={{
          padding: '0.1rem 0.4rem',
          borderRadius: '0.25rem',
          fontSize: '0.7rem',
          background: kindColor + '22',
          color: kindColor,
          border: `1px solid ${kindColor}44`,
          minWidth: 70,
          textAlign: 'center',
        }}
      >
        {account.kind}
      </span>
      <span style={{ fontWeight: 600, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {account.email || account.username || account.id}
      </span>
      <span style={{ color: 'var(--muted)', fontSize: '0.72rem', minWidth: 120 }}>
        device: {account.deviceId.slice(0, 12)}
      </span>
      {account.linkedAccountId && (
        <span style={{ color: 'var(--muted)', fontSize: '0.72rem' }} title={`Linked: ${account.linkedAccountId}`}>
          linked
        </span>
      )}
      <span
        style={{
          fontSize: '0.7rem',
          color: account.status === 'active' ? 'var(--green)' : 'var(--red, #f87171)',
        }}
      >
        {account.status}
      </span>
      <span style={{ color: 'var(--muted)', fontSize: '0.7rem' }}>
        {new Date(account.createdAt).toLocaleDateString()}
      </span>
    </div>
  );
}

export function AccountPanel() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [kindFilter, setKindFilter] = useState('');

  const load = useCallback(async () => {
    try {
      setAccounts(await listAccounts(kindFilter ? { kind: kindFilter } : undefined));
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Load failed');
    } finally {
      setLoading(false);
    }
  }, [kindFilter]);

  useEffect(() => { void load(); }, [load]);

  const google = accounts.filter((a) => a.kind === 'google');
  const instagram = accounts.filter((a) => a.kind === 'instagram');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
        <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
          {accounts.length} account{accounts.length !== 1 ? 's' : ''}
        </span>
        <div style={{ display: 'flex', gap: '0.25rem', marginLeft: 'auto' }}>
          {['', 'google', 'instagram'].map((k) => (
            <button
              key={k || 'all'}
              type="button"
              onClick={() => setKindFilter(k)}
              style={{
                cursor: 'pointer',
                border: '1px solid var(--border)',
                borderRadius: '0.25rem',
                background: kindFilter === k ? 'var(--accent, #818cf8)' : 'none',
                color: kindFilter === k ? '#fff' : 'var(--muted)',
                fontSize: '0.72rem',
                padding: '0.2rem 0.6rem',
              }}
            >
              {k || 'All'}
            </button>
          ))}
        </div>
        <button
          type="button"
          onClick={() => { setLoading(true); void load(); }}
          style={{ cursor: 'pointer', background: 'none', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--muted)', fontSize: '0.72rem', padding: '0.2rem 0.6rem' }}
        >
          Refresh
        </button>
      </div>

      {loading && <span style={{ color: 'var(--muted)', fontSize: '0.8rem' }}>Loading...</span>}
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.8rem' }}>{error}</span>}

      {!loading && accounts.length === 0 ? (
        <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--muted)', fontSize: '0.8rem', border: '1px solid var(--border)', borderRadius: '0.5rem' }}>
          No accounts created yet. Start a campaign to create accounts.
        </div>
      ) : (
        <>
          {(kindFilter === '' || kindFilter === 'google') && google.length > 0 && (
            <section>
              <div style={{ fontSize: '0.75rem', color: '#4285F4', fontWeight: 600, padding: '0.25rem 0.75rem', background: '#4285F411', borderRadius: '0.375rem 0.375rem 0 0', border: '1px solid var(--border)' }}>
                Google ({google.length})
              </div>
              <div style={{ border: '1px solid var(--border)', borderTop: 'none', borderRadius: '0 0 0.5rem 0.5rem', overflow: 'hidden' }}>
                {google.map((a) => <AccountRow key={a.id} account={a} />)}
              </div>
            </section>
          )}
          {(kindFilter === '' || kindFilter === 'instagram') && instagram.length > 0 && (
            <section>
              <div style={{ fontSize: '0.75rem', color: '#E1306C', fontWeight: 600, padding: '0.25rem 0.75rem', background: '#E1306C11', borderRadius: '0.375rem 0.375rem 0 0', border: '1px solid var(--border)' }}>
                Instagram ({instagram.length})
              </div>
              <div style={{ border: '1px solid var(--border)', borderTop: 'none', borderRadius: '0 0 0.5rem 0.5rem', overflow: 'hidden' }}>
                {instagram.map((a) => <AccountRow key={a.id} account={a} />)}
              </div>
            </section>
          )}
        </>
      )}
    </div>
  );
}
