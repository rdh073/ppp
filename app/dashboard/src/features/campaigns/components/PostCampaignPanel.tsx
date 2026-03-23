import { useEffect, useMemo, useRef, useState } from 'react';
import {
  getPostCampaignCapabilities,
  listPostCampaigns,
  startPostCampaign,
  type PostCampaign,
  type PostCampaignCapabilities,
  type PostJob,
} from '../api/posts';
import { listAccounts, type Account } from '../../account-manager/api/accounts';
import { runStatusColor as STATUS_COLOR, inputStyle } from '../../../components/ui/statusStyles';
import { useDataList } from '../../../shared/react/useDataList';
import { StatusRow } from '../../../components/ui/StatusRow';
import { CollapsibleForm } from '../../../components/ui/CollapsibleForm';

// ── helpers ───────────────────────────────────────────────────────────────────

function readFileAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      resolve(result.split(',')[1] ?? result);
    };
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

// ── AI content output components ──────────────────────────────────────────────

function AIBadge() {
  return (
    <span style={{ fontSize: '0.68rem', color: 'var(--muted)', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
      <svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M12 2l2.4 7.4H22l-6.2 4.5 2.4 7.4L12 17l-6.2 4.3 2.4-7.4L2 9.4h7.6L12 2z" />
      </svg>
      AI Generated
    </span>
  );
}

function AIContentCard({ children }: { children: React.ReactNode }) {
  return (
    <div style={{
      border: '1px solid var(--border)',
      borderRadius: '0.5rem',
      padding: '0.75rem',
      background: 'var(--bg-sub)',
      display: 'flex',
      flexDirection: 'column',
      gap: '0.5rem',
    }}>
      {children}
    </div>
  );
}

// ── PostJobChip ────────────────────────────────────────────────────────────────

function PostJobChip({ job, accounts }: { job: PostJob; accounts: Account[] }) {
  const acct = accounts.find((a) => a.id === job.accountId);
  const label = acct?.username || acct?.email || job.accountId.slice(0, 10);
  return (
    <span
      title={job.error}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '0.25rem',
        fontSize: '0.68rem',
        padding: '0.1rem 0.4rem',
        borderRadius: '0.25rem',
        border: '1px solid var(--border)',
        color: STATUS_COLOR[job.status] ?? 'var(--muted)',
      }}
    >
      <span
        style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: STATUS_COLOR[job.status] ?? 'var(--muted)',
          flexShrink: 0,
        }}
      />
      @{label}:{job.status}
    </span>
  );
}

// ── PostCampaignRow ────────────────────────────────────────────────────────────

function PostCampaignRow({ campaign, accounts }: { campaign: PostCampaign; accounts: Account[] }) {
  return (
    <StatusRow
      status={campaign.status}
      statusColors={STATUS_COLOR}
      primary={(
        <span style={{ fontFamily: 'monospace', fontSize: '0.7rem', color: 'var(--muted)' }}>
          {campaign.id.slice(0, 22)}
        </span>
      )}
      meta={(
        <span style={{ fontSize: '0.7rem', color: 'var(--muted)' }}>
          img:{campaign.imageSource} txt:{campaign.textSource}
        </span>
      )}
      error={campaign.error}
      details={(
        <>
          {campaign.caption && (
            <span
              style={{ fontSize: '0.72rem', color: 'var(--muted)', maxWidth: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            >
              "{campaign.caption}"
            </span>
          )}
          <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap' }}>
            {campaign.jobs.map((j) => (
              <PostJobChip key={j.id} job={j} accounts={accounts} />
            ))}
          </div>
        </>
      )}
    />
  );
}

// ── StartPostForm ──────────────────────────────────────────────────────────────

function StartPostFormBody({ onStarted, close }: { onStarted: () => void; close: () => void }) {
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');

  // Accounts
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [capabilities, setCapabilities] = useState<PostCampaignCapabilities | null>(null);
  const [capabilityError, setCapabilityError] = useState('');

  // Image
  const [imageSource, setImageSource] = useState<'manual' | 'ai'>('manual');
  const [imageFile, setImageFile] = useState<File | null>(null);
  const [manualImagePreview, setManualImagePreview] = useState('');
  const [imagePrompt, setImagePrompt] = useState('');
  const [generatingImage, setGeneratingImage] = useState(false);
  const [generatedImageB64, setGeneratedImageB64] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Text
  const [textSource, setTextSource] = useState<'manual' | 'ai'>('manual');
  const [textContent, setTextContent] = useState('');
  const [textPrompt, setTextPrompt] = useState('');
  const [generatingCaption, setGeneratingCaption] = useState(false);
  const [generatedCaption, setGeneratedCaption] = useState('');

  const imageAIAvailable = capabilities?.imageAI.available ?? true;
  const textAIAvailable = capabilities?.textAI.available ?? true;

  useEffect(() => {
    listAccounts({ kind: 'instagram' }).then((all) => {
      setAccounts(all.filter((a) => a.status === 'active' && a.deviceId));
    }).catch(() => {});
  }, []);

  useEffect(() => {
    let cancelled = false;
    getPostCampaignCapabilities()
      .then((next) => { if (!cancelled) { setCapabilities(next); setCapabilityError(''); } })
      .catch(() => { if (!cancelled) setCapabilityError('AI availability could not be loaded. AI requests may still fail on submit.'); });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    if (!imageAIAvailable && imageSource === 'ai') setImageSource('manual');
  }, [imageAIAvailable, imageSource]);

  useEffect(() => {
    if (!textAIAvailable && textSource === 'ai') setTextSource('manual');
  }, [textAIAvailable, textSource]);

  function toggleAccount(id: string) {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setImageFile(file);
    setManualImagePreview(URL.createObjectURL(file));
  }

  async function handleGenerateImage() {
    if (!imagePrompt.trim()) { setError('Enter an image prompt first'); return; }
    setGeneratingImage(true);
    setError('');
    try {
      const { generateImagePreview } = await import('../api/posts');
      const res = await generateImagePreview(imagePrompt);
      setGeneratedImageB64(res.imageBase64);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Image generation failed');
    } finally {
      setGeneratingImage(false);
    }
  }

  async function handleGenerateCaption() {
    if (!textPrompt.trim()) { setError('Enter a caption prompt first'); return; }
    setGeneratingCaption(true);
    setError('');
    try {
      const { generateCaptionPreview } = await import('../api/posts');
      const res = await generateCaptionPreview(textPrompt);
      setGeneratedCaption(res.caption);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Caption generation failed');
    } finally {
      setGeneratingCaption(false);
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (selectedIds.size === 0) { setError('Select at least one account'); return; }
    if (imageSource === 'ai' && !imageAIAvailable) { setError(capabilities?.imageAI.reason || 'AI image generation is unavailable'); return; }
    if (textSource === 'ai' && !textAIAvailable) { setError(capabilities?.textAI.reason || 'AI caption generation is unavailable'); return; }
    if (imageSource === 'manual' && !imageFile) { setError('Choose an image file'); return; }
    if (imageSource === 'ai' && !imagePrompt.trim()) { setError('Enter an image prompt'); return; }
    if (textSource === 'manual' && !textContent.trim()) { setError('Enter a caption'); return; }
    if (textSource === 'ai' && !textPrompt.trim()) { setError('Enter a caption prompt'); return; }

    setStarting(true);
    setError('');
    try {
      let imageBase64: string | undefined;
      if (imageSource === 'manual' && imageFile) {
        imageBase64 = await readFileAsBase64(imageFile);
      } else if (imageSource === 'ai' && generatedImageB64) {
        // Use pre-generated image — submit as manual so server doesn't re-generate
        imageBase64 = generatedImageB64;
      }

      const effectiveImageSource = (imageSource === 'ai' && generatedImageB64) ? 'manual' : imageSource;
      const effectiveTextSource = (textSource === 'ai' && generatedCaption) ? 'manual' : textSource;

      await startPostCampaign({
        accountIds: [...selectedIds],
        imageSource: effectiveImageSource,
        imageBase64,
        imagePrompt: effectiveImageSource === 'ai' ? imagePrompt : undefined,
        textSource: effectiveTextSource,
        textContent: effectiveTextSource === 'manual'
          ? (textSource === 'ai' ? generatedCaption : textContent)
          : undefined,
        textPrompt: effectiveTextSource === 'ai' ? textPrompt : undefined,
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
        gap: '0.75rem',
        padding: '0.75rem',
        border: '1px solid var(--border)',
        borderRadius: '0.5rem',
        fontSize: '0.8rem',
      }}
    >
      {/* Account selector */}
      <div>
        <div style={{ marginBottom: '0.35rem', color: 'var(--muted)', fontSize: '0.75rem' }}>
          Accounts — active on device only ({accounts.length} available)
        </div>
        {accounts.length === 0 ? (
          <span style={{ fontSize: '0.75rem', color: 'var(--muted)' }}>
            No active Instagram accounts with a device. Login first.
          </span>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', maxHeight: 140, overflowY: 'auto' }}>
            {accounts.map((a) => (
              <label key={a.id} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer', fontSize: '0.78rem' }}>
                <input type="checkbox" checked={selectedIds.has(a.id)} onChange={() => toggleAccount(a.id)} />
                <span style={{ fontFamily: 'monospace' }}>@{a.username || a.email || a.id.slice(0, 12)}</span>
                <span style={{ color: 'var(--muted)', fontSize: '0.68rem' }}>device: {a.deviceId?.slice(0, 10)}</span>
              </label>
            ))}
          </div>
        )}
      </div>

      {capabilities && (
        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', fontSize: '0.72rem', color: 'var(--muted)' }}>
          <span>AI image: {imageAIAvailable ? 'ready' : 'unavailable'}</span>
          <span>AI caption: {textAIAvailable ? 'ready' : 'unavailable'}</span>
        </div>
      )}
      {capabilityError && (
        <span style={{ color: 'var(--muted)', fontSize: '0.72rem' }}>{capabilityError}</span>
      )}

      {/* Image source */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <span style={{ color: 'var(--muted)', fontSize: '0.75rem', minWidth: 60 }}>Image:</span>
          {(['manual', 'ai'] as const).map((m) => (
            <button
              key={m}
              type="button"
              disabled={m === 'ai' && !imageAIAvailable}
              onClick={() => { setImageSource(m); setGeneratedImageB64(''); }}
              style={{
                ...toggleBtn,
                cursor: m === 'ai' && !imageAIAvailable ? 'not-allowed' : 'pointer',
                opacity: m === 'ai' && !imageAIAvailable ? 0.45 : 1,
                background: imageSource === m ? '#E1306C' : 'none',
                color: imageSource === m ? '#fff' : 'var(--muted)',
              }}
            >
              {m === 'manual' ? 'Manual' : 'AI'}
            </button>
          ))}
        </div>

        {imageSource === 'manual' ? (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <input ref={fileInputRef} type="file" accept="image/*" onChange={handleFileChange} style={{ display: 'none' }} />
            <button type="button" onClick={() => fileInputRef.current?.click()} style={outlineBtn}>
              {imageFile ? imageFile.name : 'Choose file…'}
            </button>
            {manualImagePreview && (
              <img src={manualImagePreview} alt="selected image" style={{ width: 48, height: 48, objectFit: 'cover', borderRadius: '0.25rem', border: '1px solid var(--border)' }} />
            )}
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <input
                placeholder="Describe the image to generate…"
                value={imagePrompt}
                onChange={(e) => setImagePrompt(e.target.value)}
                style={{ ...inputStyle, flex: 1 }}
              />
              <button
                type="button"
                disabled={generatingImage || !imagePrompt.trim()}
                onClick={handleGenerateImage}
                style={{ ...outlineBtn, fontSize: '0.7rem', padding: '0 0.5rem', whiteSpace: 'nowrap' }}
              >
                {generatingImage ? 'Generating…' : generatedImageB64 ? 'Regenerate' : 'Generate Image'}
              </button>
            </div>

            {!generatedImageB64 && !generatingImage && (
              <span style={{ fontSize: '0.68rem', color: 'var(--muted)' }}>
                Generate an image to review it before posting.
              </span>
            )}

            {generatedImageB64 && (
              <AIContentCard>
                <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-start' }}>
                  <img
                    src={`data:image/jpeg;base64,${generatedImageB64}`}
                    alt="AI generated image"
                    style={{ width: 240, height: 240, objectFit: 'cover', borderRadius: '0.375rem', border: '1px solid var(--border)', flexShrink: 0 }}
                  />
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', paddingTop: '0.25rem' }}>
                    <AIBadge />
                    <button
                      type="button"
                      disabled={generatingImage || !imagePrompt.trim()}
                      onClick={handleGenerateImage}
                      style={{ ...outlineBtn, fontSize: '0.68rem', padding: '0.2rem 0.5rem', width: 'fit-content' }}
                    >
                      {generatingImage ? 'Generating…' : 'Regenerate'}
                    </button>
                  </div>
                </div>
              </AIContentCard>
            )}
          </div>
        )}

        {!imageAIAvailable && capabilities?.imageAI.reason && (
          <span style={{ color: 'var(--muted)', fontSize: '0.72rem' }}>{capabilities.imageAI.reason}</span>
        )}
      </div>

      {/* Caption source */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <span style={{ color: 'var(--muted)', fontSize: '0.75rem', minWidth: 60 }}>Caption:</span>
          {(['manual', 'ai'] as const).map((m) => (
            <button
              key={m}
              type="button"
              disabled={m === 'ai' && !textAIAvailable}
              onClick={() => { setTextSource(m); setGeneratedCaption(''); }}
              style={{
                ...toggleBtn,
                cursor: m === 'ai' && !textAIAvailable ? 'not-allowed' : 'pointer',
                opacity: m === 'ai' && !textAIAvailable ? 0.45 : 1,
                background: textSource === m ? '#E1306C' : 'none',
                color: textSource === m ? '#fff' : 'var(--muted)',
              }}
            >
              {m === 'manual' ? 'Manual' : 'AI'}
            </button>
          ))}
        </div>

        {textSource === 'manual' ? (
          <textarea
            placeholder="Write your caption…"
            value={textContent}
            onChange={(e) => setTextContent(e.target.value)}
            rows={3}
            style={{ ...inputStyle, resize: 'vertical', width: '100%', boxSizing: 'border-box' }}
          />
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <input
                placeholder="Describe what the caption should say…"
                value={textPrompt}
                onChange={(e) => setTextPrompt(e.target.value)}
                style={{ ...inputStyle, flex: 1 }}
              />
              <button
                type="button"
                disabled={generatingCaption || !textPrompt.trim()}
                onClick={handleGenerateCaption}
                style={{ ...outlineBtn, fontSize: '0.7rem', padding: '0 0.5rem', whiteSpace: 'nowrap' }}
              >
                {generatingCaption ? 'Generating…' : generatedCaption ? 'Regenerate' : 'Generate Caption'}
              </button>
            </div>

            {!generatedCaption && !generatingCaption && (
              <span style={{ fontSize: '0.68rem', color: 'var(--muted)' }}>
                Generate a caption to review and edit it before posting.
              </span>
            )}

            {generatedCaption && (
              <AIContentCard>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <AIBadge />
                  <button
                    type="button"
                    disabled={generatingCaption || !textPrompt.trim()}
                    onClick={handleGenerateCaption}
                    style={{ ...outlineBtn, fontSize: '0.68rem', padding: '0.2rem 0.5rem' }}
                  >
                    {generatingCaption ? 'Generating…' : 'Regenerate'}
                  </button>
                </div>
                <textarea
                  value={generatedCaption}
                  onChange={(e) => setGeneratedCaption(e.target.value)}
                  rows={4}
                  style={{ ...inputStyle, resize: 'vertical', width: '100%', boxSizing: 'border-box' }}
                  aria-label="Generated caption — edit as needed"
                />
              </AIContentCard>
            )}
          </div>
        )}

        {!textAIAvailable && capabilities?.textAI.reason && (
          <span style={{ color: 'var(--muted)', fontSize: '0.72rem' }}>{capabilities.textAI.reason}</span>
        )}
      </div>

      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.75rem' }}>{error}</span>}

      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <button
          type="submit"
          disabled={starting || selectedIds.size === 0}
          style={{ ...submitBtn, opacity: starting || selectedIds.size === 0 ? 0.5 : 1 }}
        >
          {starting ? 'Starting…' : `Post to ${selectedIds.size} account${selectedIds.size !== 1 ? 's' : ''}`}
        </button>
        <button type="button" onClick={close} style={{ ...submitBtn, background: 'none', color: 'var(--muted)' }}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function StartPostForm({ onStarted }: { onStarted: () => void }) {
  return (
    <CollapsibleForm triggerLabel="New Post" accentColor="#E1306C">
      {({ close }) => <StartPostFormBody onStarted={onStarted} close={close} />}
    </CollapsibleForm>
  );
}

const toggleBtn: React.CSSProperties = {
  cursor: 'pointer',
  border: '1px solid var(--border)',
  borderRadius: '0.25rem',
  fontSize: '0.72rem',
  padding: '0.2rem 0.6rem',
};

const outlineBtn: React.CSSProperties = {
  cursor: 'pointer',
  background: 'none',
  border: '1px solid var(--border)',
  borderRadius: '0.25rem',
  color: 'inherit',
  fontSize: '0.75rem',
  padding: '0.25rem 0.6rem',
};

const submitBtn: React.CSSProperties = {
  cursor: 'pointer',
  background: '#E1306C',
  border: 'none',
  borderRadius: '0.375rem',
  color: '#fff',
  fontSize: '0.75rem',
  padding: '0.3rem 0.75rem',
};

// ── PostCampaignPanel (main) ──────────────────────────────────────────────────

export function PostCampaignPanel() {
  const stream = useMemo(() => ({
    topics: ['campaigns.posts'],
    getKey: (campaign: PostCampaign) => campaign.id,
  }), []);
  const { data: campaigns, loading, error, reload: load } = useDataList(listPostCampaigns, { stream });
  const [accounts, setAccounts] = useState<Account[]>([]);

  useEffect(() => {
    listAccounts({ kind: 'instagram' }).then(setAccounts).catch(() => {});
  }, []);

  const hasRunning = campaigns.some((c) => c.status === 'running');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: '0.8rem', color: 'var(--muted)' }}>
          {campaigns.length} post{campaigns.length !== 1 ? 's' : ''}
          {hasRunning && (
            <span style={{ marginLeft: '0.5rem', color: 'var(--yellow, #facc15)', fontSize: '0.72rem' }}>
              ● running
            </span>
          )}
        </span>
      </div>

      <StartPostForm onStarted={load} />

      {loading && <span style={{ color: 'var(--muted)', fontSize: '0.8rem' }}>Loading…</span>}
      {error && <span style={{ color: 'var(--red, #f87171)', fontSize: '0.8rem' }}>{error}</span>}

      <div style={{ border: '1px solid var(--border)', borderRadius: '0.5rem', overflow: 'hidden' }}>
        {campaigns.length === 0 && !loading ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--muted)', fontSize: '0.8rem' }}>
            No posts yet. Click "+ New Post" to begin.
          </div>
        ) : (
          [...campaigns].reverse().map((c) => (
            <PostCampaignRow key={c.id} campaign={c} accounts={accounts} />
          ))
        )}
      </div>
    </div>
  );
}
