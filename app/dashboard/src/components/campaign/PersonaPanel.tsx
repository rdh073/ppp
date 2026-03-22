import { useState } from 'react';
import { listPersonas, createPersona, deletePersona, type Persona } from '../../api/personas';
import { personaStatusColor as STATUS_COLOR, inputStyle, accentBtnStyle as btnStyle } from './styles';
import { useDataList } from '../../hooks/useDataList';
import { StatusRow } from './StatusRow';
import { CollapsibleForm } from './CollapsibleForm';

function PersonaRow({ persona, onDeleted }: { persona: Persona; onDeleted: () => void }) {
  const [deleting, setDeleting] = useState(false);

  async function handleDelete() {
    setDeleting(true);
    try {
      await deletePersona(persona.id);
      onDeleted();
    } catch {
      setDeleting(false);
    }
  }

  return (
    <StatusRow
      status={persona.status}
      statusColors={STATUS_COLOR}
      primary={(
        <span style={{ fontWeight: 600, minWidth: 140 }}>
          {persona.firstName} {persona.lastName}
        </span>
      )}
      meta={(
        <>
          <span style={{ color: 'var(--muted)', minWidth: 60 }}>{persona.kind}</span>
          {persona.email && (
            <span style={{ color: 'var(--muted)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {persona.email}
            </span>
          )}
          {persona.username && !persona.email && (
            <span style={{ color: 'var(--muted)', flex: 1 }}>@{persona.username}</span>
          )}
        </>
      )}
      statusNode={(
        <>
          <span
            style={{
              fontSize: '0.7rem',
              padding: '0.1rem 0.4rem',
              border: '1px solid var(--border)',
              borderRadius: '0.25rem',
              color: STATUS_COLOR[persona.status] ?? 'var(--muted)',
            }}
          >
            {persona.status}
          </span>
          <button
            type="button"
            onClick={handleDelete}
            disabled={deleting}
            style={{
              cursor: 'pointer',
              background: 'none',
              border: '1px solid var(--border)',
              borderRadius: '0.25rem',
              color: 'var(--muted)',
              fontSize: '0.7rem',
              padding: '0.1rem 0.5rem',
            }}
          >
            {deleting ? '...' : 'Delete'}
          </button>
        </>
      )}
    />
  );
}

function CreatePersonaFormBody({ onCreated, close }: { onCreated: () => void; close: () => void }) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [form, setForm] = useState({
    kind: 'google' as 'google' | 'instagram',
    firstName: '',
    lastName: '',
    gender: '',
    birthDate: '',
    email: '',
    username: '',
    password: '',
  });

  function set(field: string, value: string) {
    setForm((f) => ({ ...f, [field]: value }));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.firstName || !form.lastName) {
      setError('First name and last name are required');
      return;
    }
    setSaving(true);
    setError('');
    try {
      await createPersona({
        ...form,
        gender: form.gender || undefined,
        birthDate: form.birthDate || undefined,
        email: form.email || undefined,
        username: form.username || undefined,
        password: form.password || undefined,
      });
      close();
      onCreated();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Create failed');
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', padding: '0.75rem', border: '1px solid var(--border)', borderRadius: '0.5rem', marginTop: '0.5rem', fontSize: '0.8rem' }}>
      <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
        <select value={form.kind} onChange={(e) => set('kind', e.target.value)} style={inputStyle}>
          <option value="google">google</option>
          <option value="instagram">instagram</option>
        </select>
        <input placeholder="First name *" value={form.firstName} onChange={(e) => set('firstName', e.target.value)} style={inputStyle} />
        <input placeholder="Last name *" value={form.lastName} onChange={(e) => set('lastName', e.target.value)} style={inputStyle} />
        <input placeholder="Gender" value={form.gender} onChange={(e) => set('gender', e.target.value)} style={inputStyle} />
        <input placeholder="Birth date (YYYY-MM-DD)" value={form.birthDate} onChange={(e) => set('birthDate', e.target.value)} style={inputStyle} />
        <input placeholder="Email" value={form.email} onChange={(e) => set('email', e.target.value)} style={inputStyle} />
        <input placeholder="Username" value={form.username} onChange={(e) => set('username', e.target.value)} style={inputStyle} />
        <input placeholder="Password" type="password" value={form.password} onChange={(e) => set('password', e.target.value)} style={inputStyle} />
      </div>
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.75rem' }}>{error}</span>}
      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <button type="submit" disabled={saving} style={btnStyle}>
          {saving ? 'Saving...' : 'Save'}
        </button>
        <button type="button" onClick={close} style={{ ...btnStyle, background: 'none', color: 'var(--muted)' }}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function CreatePersonaForm({ onCreated }: { onCreated: () => void }) {
  return (
    <CollapsibleForm triggerLabel="Add Persona">
      {({ close }) => <CreatePersonaFormBody onCreated={onCreated} close={close} />}
    </CollapsibleForm>
  );
}

export function PersonaPanel() {
  const { data: personas, loading, error, reload: load } = useDataList(listPersonas);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
          {personas.length} persona{personas.length !== 1 ? 's' : ''}
        </span>
        <CreatePersonaForm onCreated={load} />
      </div>

      {loading && <span style={{ color: 'var(--muted)', fontSize: '0.8rem' }}>Loading...</span>}
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.8rem' }}>{error}</span>}

      <div style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', overflow: 'hidden' }}>
        {personas.length === 0 && !loading ? (
          <div style={{ padding: '1rem', textAlign: 'center', color: 'var(--muted)', fontSize: '0.8rem' }}>
            No personas saved. Add one manually or let a campaign generate one via AI.
          </div>
        ) : (
          personas.map((p) => <PersonaRow key={p.id} persona={p} onDeleted={load} />)
        )}
      </div>
    </div>
  );
}
