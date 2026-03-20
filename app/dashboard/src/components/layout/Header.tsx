import type { Tab } from './TabNav';
import {
  Activity,
  Cpu,
  ExternalLink,
  LayoutDashboard,
  Moon,
  Network,
  Server,
  Sun,
} from 'lucide-react';

interface HeaderProps {
  activeTab: Tab;
  apiUrl: string;
  metricsUrl: string;
  healthStatus: 'checking' | 'ok' | 'down';
  theme: 'monokai' | 'light';
  onToggleTheme: () => void;
}

const TAB_LABELS: Record<Tab, string> = {
  devices: 'Device Registry',
  tasks: 'Task Control',
  workflows: 'Workflow Catalog',
  events: 'Event Feed',
};

export function Header({ activeTab, apiUrl, metricsUrl, healthStatus, theme, onToggleTheme }: HeaderProps) {
  const healthLabel =
    healthStatus === 'ok' ? 'API online' : healthStatus === 'down' ? 'API unreachable' : 'API checking';
  const statusIcon =
    healthStatus === 'ok' ? (
      <Activity size={14} aria-hidden="true" />
    ) : healthStatus === 'down' ? (
      <Server size={14} aria-hidden="true" />
    ) : (
      <Cpu size={14} aria-hidden="true" />
    );

  return (
    <header className="topbar">
      <div className="topbar-main">
        <p className="topbar-kicker">
          <LayoutDashboard size={13} aria-hidden="true" />
          <span>Automation Control</span>
        </p>
        <h1>PPP Control Dashboard</h1>
        <p>
          Control, monitor, and intervene across android-agent sessions with one dashboard in real
          time.
        </p>
        <div className="topbar-chip-row">
          <span className="topbar-chip">
            <span className="topbar-chip-icon" aria-hidden="true">
              <Server size={14} />
            </span>
            <span>Focus: {TAB_LABELS[activeTab]}</span>
          </span>
          <span className={`topbar-chip topbar-chip-${healthStatus}`}>
            <span className="topbar-chip-icon" aria-hidden="true">
              {statusIcon}
            </span>
            <span>{healthLabel}</span>
          </span>
          <span className="topbar-chip">
            <span className="topbar-chip-icon" aria-hidden="true">
              <Activity size={14} />
            </span>
            <span>Live polling active</span>
          </span>
        </div>
      </div>
      <div className="topbar-meta">
        <div className="topbar-api-wrap">
          <span className="topbar-meta-label">Control endpoint</span>
          <span className="topbar-api" title={apiUrl}>
            <Network size={14} aria-hidden="true" />
            <code className="topbar-api-value">{apiUrl}</code>
          </span>
        </div>
        <span className="topbar-meta-label topbar-meta-label--compact">Workspace action</span>
        <div className="row topbar-actions">
          <button type="button" className="topbar-link" onClick={onToggleTheme}>
            {theme === 'monokai' ? <Moon size={16} aria-hidden="true" /> : <Sun size={16} aria-hidden="true" />}
            <span>{theme === 'monokai' ? 'Dark' : 'Light'}</span>
          </button>
          <a className="topbar-link topbar-link--ghost" href={metricsUrl} target="_blank" rel="noreferrer">
            <span className="topbar-link-icon">
              <ExternalLink size={16} aria-hidden="true" />
            </span>
            Open Metrics
          </a>
        </div>
      </div>
    </header>
  );
}
