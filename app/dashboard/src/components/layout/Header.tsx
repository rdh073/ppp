import type { Tab } from './TabNav';

interface HeaderProps {
  activeTab: Tab;
  apiUrl: string;
  metricsUrl: string;
  healthStatus: 'checking' | 'ok' | 'down';
}

const TAB_LABELS: Record<Tab, string> = {
  devices: 'Device Registry',
  tasks: 'Task Control',
  workflows: 'Workflow Catalog',
  events: 'Event Feed',
};

export function Header({ activeTab, apiUrl, metricsUrl, healthStatus }: HeaderProps) {
  const healthLabel =
    healthStatus === 'ok' ? 'API online' : healthStatus === 'down' ? 'API unreachable' : 'API checking';

  return (
    <header className="topbar">
      <div className="topbar-main">
        <h1>PPP Operator Console</h1>
        <p>Control, monitor, and intervene across android-agent sessions in real time.</p>
      </div>
      <div className="topbar-meta">
        <span className="topbar-chip">Focus: {TAB_LABELS[activeTab]}</span>
        <span className={`topbar-chip topbar-chip-${healthStatus}`}>{healthLabel}</span>
        <span className="topbar-api">API: {apiUrl}</span>
        <a className="topbar-link" href={metricsUrl} target="_blank" rel="noreferrer">
          Open Metrics
        </a>
      </div>
    </header>
  );
}
