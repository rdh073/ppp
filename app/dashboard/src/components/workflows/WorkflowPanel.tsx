import { useDeferredValue, useMemo, useState } from 'react';
import {
  BarChart3,
  Check,
  ChevronDown,
  Copy,
  GitBranch,
  ListTree,
  RefreshCw,
  Search,
  Workflow,
  Wrench,
  Zap,
  GitMerge,
} from 'lucide-react';
import { useWorkflows } from '../../hooks/useWorkflows';
import type { WorkflowDefinition, WorkflowStepDef } from '../../types';

type StepKind = 'action' | 'toolcall' | 'routing';

interface WorkflowSummary {
  stepCount: number;
  branchLinks: number;
  retryableSteps: number;
  actionSteps: number;
  toolSteps: number;
}

function normalizeEdges(raw?: string | string[]): string[] {
  if (!raw) {
    return [];
  }

  if (Array.isArray(raw)) {
    return raw.filter(Boolean);
  }

  return [raw].filter(Boolean);
}

function edgeCount(raw?: string | string[]): number {
  return normalizeEdges(raw).length;
}

function formatEdges(raw?: string | string[]): string {
  const values = normalizeEdges(raw);
  return values.length > 0 ? values.join(', ') : '—';
}

function stepKind(step: WorkflowStepDef): StepKind {
  if (step.action) {
    return 'action';
  }
  if ('tool_call' in step && (step as { tool_call?: unknown }).tool_call) {
    return 'toolcall';
  }
  return 'routing';
}

function summarizeWorkflow(workflow?: WorkflowDefinition | null): WorkflowSummary {
  if (!workflow?.steps) {
    return {
      stepCount: 0,
      branchLinks: 0,
      retryableSteps: 0,
      actionSteps: 0,
      toolSteps: 0,
    };
  }

  let branchLinks = 0;
  let retryableSteps = 0;
  let actionSteps = 0;
  let toolSteps = 0;

  for (const step of Object.values(workflow.steps)) {
    const success = edgeCount(step.on_success);
    const failure = edgeCount(step.on_failure);

    if (success + failure > 1) {
      branchLinks++;
    }
    if ((step.max_retry ?? 0) > 0) {
      retryableSteps++;
    }

    const kind = stepKind(step);
    if (kind === 'action') {
      actionSteps++;
    }
    if (kind === 'toolcall') {
      toolSteps++;
    }
  }

  return {
    stepCount: Object.keys(workflow.steps).length,
    branchLinks,
    retryableSteps,
    actionSteps,
    toolSteps,
  };
}

function buildExecutionPreview(workflow?: WorkflowDefinition | null): string[] {
  if (!workflow?.steps) {
    return [];
  }

  const allSteps = Object.keys(workflow.steps);
  if (allSteps.length === 0) {
    return [];
  }

  const start = workflow.entry && workflow.steps[workflow.entry] ? workflow.entry : allSteps[0];
  const visited = new Set<string>();
  const preview: string[] = [];
  const queue: string[] = [start];

  while (queue.length > 0 && preview.length < 12) {
    const cur = queue.shift();
    if (!cur || visited.has(cur)) {
      continue;
    }

    visited.add(cur);
    preview.push(cur);

    const step = workflow.steps[cur];
    for (const next of [...normalizeEdges(step.on_success), ...normalizeEdges(step.on_failure)]) {
      if (!visited.has(next) && workflow.steps[next]) {
        queue.push(next);
      }
    }
  }

  for (const name of allSteps) {
    if (preview.length >= 12) {
      break;
    }
    if (!visited.has(name)) {
      preview.push(name);
    }
  }

  return preview;
}

const KIND_META: Record<StepKind, { label: string; cls: string }> = {
  action: { label: 'Action', cls: 'wf-step-kind--action' },
  toolcall: { label: 'Tool', cls: 'wf-step-kind--toolcall' },
  routing: { label: 'Routing', cls: 'wf-step-kind--routing' },
};

const KIND_ICON: Record<StepKind, typeof Zap> = {
  action: Zap,
  toolcall: Wrench,
  routing: GitMerge,
};

function StepKindChip({ kind }: { kind: StepKind }) {
  const { label, cls } = KIND_META[kind];
  const Icon = KIND_ICON[kind];

  return (
    <span className={`wf-step-kind ${cls}`}>
      <Icon size={10} aria-hidden="true" />
      {label}
    </span>
  );
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => {
        setCopied(false);
      }, 1800);
    });
  };

  return (
    <button
      type="button"
      className="btn-secondary wf-copy-btn"
      onClick={handleCopy}
      aria-label="Copy workflow JSON"
    >
      {copied ? <Check size={13} aria-hidden="true" /> : <Copy size={13} aria-hidden="true" />}
      {copied ? 'Copied' : 'Copy'}
    </button>
  );
}

export function WorkflowPanel() {
  const { workflows, selected, loading, error, loadWorkflowByName, refresh } = useWorkflows();
  const [searchQuery, setSearchQuery] = useState('');
  const [rawOpen, setRawOpen] = useState(false);
  const deferredQuery = useDeferredValue(searchQuery);

  const filteredWorkflows = useMemo(() => {
    const q = deferredQuery.trim().toLowerCase();
    return q ? workflows.filter((workflow) => workflow.name.toLowerCase().includes(q)) : workflows;
  }, [deferredQuery, workflows]);

  const quickNames = useMemo(() => {
    if (!deferredQuery.trim()) {
      return workflows.slice(0, 7).map((workflow) => workflow.name);
    }

    return filteredWorkflows.slice(0, 7).map((workflow) => workflow.name);
  }, [deferredQuery, filteredWorkflows, workflows]);

  const selectedSummary = useMemo(() => summarizeWorkflow(selected), [selected]);
  const executionPreview = useMemo(() => buildExecutionPreview(selected), [selected]);

  const selectedSteps = useMemo(() => {
    if (!selected?.steps) {
      return [];
    }
    return Object.entries(selected.steps);
  }, [selected]);

  const rawJson = useMemo(() => (selected ? JSON.stringify(selected, null, 2) : ''), [selected]);

  const hasSelected = Boolean(selected);

  const clearSearch = () => {
    setSearchQuery('');
  };

  return (
    <section className="panel">
      <div className="panel-header">
        <div className="workflow-title">
          <Workflow className="workflow-title-icon" aria-hidden="true" />
          <h2>Workflow Studio</h2>
          <span className="workflow-count-badge">{workflows.length} loaded</span>
          {selected?.name ? <span className="wf-selected-badge">{selected.name}</span> : null}
        </div>
        <div className="workflow-header-actions">
          <button
            type="button"
            className="btn-secondary"
            onClick={() => {
              void refresh();
            }}
            disabled={loading}
          >
            <RefreshCw className={loading ? 'workflow-spin' : ''} aria-hidden="true" />
            {loading ? 'Syncing…' : 'Refresh'}
          </button>
        </div>
      </div>

      <div className="workflow-summary workflow-metric-grid">
        <article className="workflow-metric">
          <ListTree size={16} aria-hidden="true" />
          <span className="workflow-metric-label">Workflows</span>
          <strong>{filteredWorkflows.length}</strong>
          <span className="workflow-submetric">of {workflows.length}</span>
        </article>
        <article className="workflow-metric">
          <GitBranch size={16} aria-hidden="true" />
          <span className="workflow-metric-label">Steps</span>
          <strong>{selectedSummary.stepCount}</strong>
          <span className="workflow-submetric">{hasSelected ? `in ${selected?.name}` : 'selected'}</span>
        </article>
        <article className="workflow-metric">
          <BarChart3 size={16} aria-hidden="true" />
          <span className="workflow-metric-label">Branches / Retries</span>
          <strong>
            {selectedSummary.branchLinks} / {selectedSummary.retryableSteps}
          </strong>
          <span className="workflow-submetric">for selected workflow</span>
        </article>
      </div>

      {error && <p role="alert" className="task-notice task-notice--error">{error}</p>}

      <div className="workflow-search-row wf-search-strip">
        <label className="workflow-search-label" htmlFor="workflow-search">
          <div className="workflow-search-input-wrap">
            <Search size={14} className="workflow-search-icon" aria-hidden="true" />
            <input
              id="workflow-search"
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              placeholder="Filter by workflow name"
            />
          </div>
        </label>
        <div className="wf-search-actions">
          <button
            type="button"
            className="wf-search-clear"
            onClick={clearSearch}
            disabled={!searchQuery}
          >
            Clear
          </button>
          <button
            type="button"
            className="wf-load-btn"
            onClick={() => {
              const trimmed = searchQuery.trim();
              if (trimmed) {
                void loadWorkflowByName(trimmed);
              }
            }}
            disabled={!searchQuery.trim() || loading}
          >
            Load
          </button>
        </div>
      </div>

      {quickNames.length > 0 && (
        <div className="wf-chip-row" aria-label="workflow quick names">
          {quickNames.map((name) => (
            <button
              type="button"
              key={name}
              className={`wf-quick-chip ${name === selected?.name ? 'wf-quick-chip--active' : ''}`}
              onClick={() => {
                setSearchQuery(name);
                void loadWorkflowByName(name);
              }}
            >
              {name}
            </button>
          ))}
        </div>
      )}

      <div className="workflow-grid workflow-grid-next">
        <div className="workflow-catalog">
          <p className="panel-subhead">
            <span className="wf-list-title">Catalog</span>
            <span className="field-hint">
              {filteredWorkflows.length === 0
                ? 'No workflows'
                : `${filteredWorkflows.length} result${filteredWorkflows.length === 1 ? '' : 's'}`}
            </span>
          </p>

          {filteredWorkflows.length === 0 ? (
            <p className="field-hint">No workflows match this filter.</p>
          ) : (
            <ul className="workflow-card-list">
              {filteredWorkflows.map((workflow) => {
                const isActive = selected?.name === workflow.name;
                const summary = summarizeWorkflow(workflow);

                return (
                  <li key={workflow.name}>
                    <button
                      type="button"
                      className={`workflow-card ${isActive ? 'workflow-card-active' : ''}`}
                      onClick={() => {
                        void loadWorkflowByName(workflow.name);
                        setSearchQuery(workflow.name);
                      }}
                    >
                      <span className="workflow-card-head">
                        <span className="workflow-card-name">{workflow.name}</span>
                        <span className="workflow-card-version">v{workflow.version ?? 1}</span>
                      </span>
                      <span className="workflow-card-meta">
                        <span className="workflow-chip">{summary.stepCount} steps</span>
                        {summary.branchLinks > 0 && (
                          <span className="workflow-chip">{summary.branchLinks} links</span>
                        )}
                        {summary.actionSteps > 0 && (
                          <span className="workflow-chip wf-chip--action">{summary.actionSteps} action</span>
                        )}
                        {summary.toolSteps > 0 && (
                          <span className="workflow-chip wf-chip--tool">{summary.toolSteps} tool</span>
                        )}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <div className="workflow-detail">
          {selected ? (
            <>
              {selected.entry && (
                <p className="workflow-entry-row">
                  <span>Entry</span>
                  <code className="workflow-entry-code">{selected.entry}</code>
                </p>
              )}

              <div className="workflow-metric-grid wf-metric-tight">
                <article className="workflow-metric">
                  <ListTree size={16} aria-hidden="true" />
                  <span className="workflow-metric-label">Steps</span>
                  <strong>{selectedSummary.stepCount}</strong>
                  <span className="workflow-submetric">nodes defined</span>
                </article>
                <article className="workflow-metric">
                  <GitBranch size={16} aria-hidden="true" />
                  <span className="workflow-metric-label">Branches</span>
                  <strong>{selectedSummary.branchLinks}</strong>
                  <span className="workflow-submetric">conditional paths</span>
                </article>
                <article className="workflow-metric">
                  <BarChart3 size={16} aria-hidden="true" />
                  <span className="workflow-metric-label">Retry Capable</span>
                  <strong>{selectedSummary.retryableSteps}</strong>
                  <span className="workflow-submetric">steps with retry</span>
                </article>
              </div>

              <div className="workflow-timeline">
                <div className="panel-subhead">
                  <span>Execution Preview</span>
                  <span className="field-hint">{executionPreview.length} nodes</span>
                </div>
                {executionPreview.length === 0 ? (
                  <p className="field-hint">No steps to render.</p>
                ) : (
                  <ol className="workflow-timeline-list">
                    {executionPreview.map((stepName, index) => {
                      const step = selected.steps?.[stepName];
                      const kind = step ? stepKind(step) : 'routing';

                      return (
                        <li key={stepName} className="workflow-timeline-item">
                          <span className={`workflow-timeline-dot wf-dot--${kind}`} aria-hidden="true" />
                          <code>{stepName}</code>
                          {selected.entry === stepName && (
                            <span className="wf-entry-pill">entry</span>
                          )}
                          <StepKindChip kind={kind} />
                          {index === executionPreview.length - 1 && <span className="field-hint">end</span>}
                        </li>
                      );
                    })}
                  </ol>
                )}
              </div>

              <div className="table-wrap workflow-steps-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Step</th>
                      <th>Type</th>
                      <th>Action / Tool</th>
                      <th>On Success</th>
                      <th>On Failure</th>
                      <th>Retry</th>
                    </tr>
                  </thead>
                  <tbody>
                    {selectedSteps.map(([stepName, step]) => {
                      const kind = stepKind(step);
                      const actionLabel =
                        step.action?.action ??
                        step.action?.kind ??
                        ((step as Record<string, unknown>).tool_call ? 'tool_call' : undefined) ??
                        step.type ??
                        '—';

                      return (
                        <tr key={stepName}>
                          <td className="task-mono-cell">{stepName}</td>
                          <td>
                            <StepKindChip kind={kind} />
                          </td>
                          <td className="task-mono-cell">{actionLabel}</td>
                          <td className="task-mono-cell wf-muted-cell">
                            {formatEdges(step.on_success)}
                          </td>
                          <td className="task-mono-cell wf-muted-cell">
                            {formatEdges(step.on_failure)}
                          </td>
                          <td className="wf-center-cell">{step.max_retry ?? 0}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </>
          ) : (
            <div className="wf-empty-detail">
              <Workflow size={32} aria-hidden="true" className="wf-empty-icon" />
              <p>Select a workflow to inspect its structure, transitions, and step definitions.</p>
            </div>
          )}
        </div>
      </div>

      <div className="workflow-raw">
        <button
          type="button"
          className="wf-raw-toggle"
          onClick={() => setRawOpen((open) => !open)}
          aria-expanded={rawOpen}
          disabled={!selected}
        >
          <ChevronDown
            size={14}
            aria-hidden="true"
            className={`wf-raw-icon${rawOpen ? ' wf-raw-icon--open' : ''}`}
          />
          Raw Definition
          {selected && <span className="workflow-count-badge wf-raw-badge">{selected.name}</span>}
          {selected && <span className="wf-copy-slot"><CopyButton text={rawJson} /></span>}
        </button>
        {rawOpen && selected && <pre className="wf-raw-pre">{rawJson}</pre>}
        {!selected && !rawOpen && (
          <p className="field-hint wf-raw-empty">Select a workflow to view JSON.</p>
        )}
      </div>
    </section>
  );
}
