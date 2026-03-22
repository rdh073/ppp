import { useEffect, useState } from 'react';
import { Header } from './components/layout/Header';
import { TabNav, type Tab } from './components/layout/TabNav';
import { DevicePanel } from './features/device-control/components/DevicePanel';
import { TaskPanel } from './features/tasks/components/TaskPanel';
import { WorkflowPanel } from './features/workflows/components/WorkflowPanel';
import { EventPanel } from './features/events/components/EventPanel';
import { MacroLibraryPanel } from './features/macros/components/MacroLibraryPanel';
import { AccountManagerPanel } from './features/account-manager/components/AccountManagerPanel';
import { PostCampaignPanel } from './features/campaigns/components/PostCampaignPanel';
import { API_URL, HEALTH_POLL_MS, METRICS_URL } from './config';
import './index.css';

type DashboardTheme = 'monokai' | 'light';
const THEME_STORAGE_KEY = 'ppp-dashboard-theme';

export function App() {
  const [activeTab, setActiveTab] = useState<Tab>('devices');
  const [healthStatus, setHealthStatus] = useState<'checking' | 'ok' | 'down'>('checking');
  const [theme, setTheme] = useState<DashboardTheme>(() => {
    if (typeof window === 'undefined') {
      return 'monokai';
    }
    const saved = window.localStorage.getItem(THEME_STORAGE_KEY);
    return saved === 'light' ? 'light' : 'monokai';
  });

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    window.localStorage.setItem(THEME_STORAGE_KEY, theme);
  }, [theme]);

  useEffect(() => {
    let closed = false;

    async function probeHealth() {
      try {
        const response = await fetch(`${API_URL}/healthz`, {
          method: 'GET',
          headers: { Accept: 'text/plain' },
        });
        if (closed) return;
        setHealthStatus(response.ok ? 'ok' : 'down');
      } catch {
        if (closed) return;
        setHealthStatus('down');
      }
    }

    void probeHealth();
    const timer = window.setInterval(probeHealth, Math.max(2000, HEALTH_POLL_MS));
    return () => {
      closed = true;
      window.clearInterval(timer);
    };
  }, []);

  return (
    <div className="dashboard">
      <div className="layout-shell">
        <Header
          activeTab={activeTab}
          apiUrl={API_URL}
          metricsUrl={METRICS_URL}
          healthStatus={healthStatus}
          theme={theme}
          onToggleTheme={() => setTheme((current) => (current === 'monokai' ? 'light' : 'monokai'))}
        />
        <main className="content">
          <section className="tab-strip">
            <TabNav active={activeTab} onChange={setActiveTab} />
          </section>
          <section className="panel-frame">
            <div
              style={{ display: activeTab === 'devices' ? 'block' : 'none' }}
              aria-hidden={activeTab !== 'devices'}
            >
              <DevicePanel />
            </div>
            {activeTab === 'tasks' && <TaskPanel />}
            {activeTab === 'workflows' && <WorkflowPanel />}
            {activeTab === 'events' && <EventPanel />}
            {activeTab === 'macros' && <MacroLibraryPanel />}
            {activeTab === 'accountCreation' && <AccountManagerPanel />}
            {activeTab === 'campaign' && <PostCampaignPanel />}
          </section>
        </main>
      </div>
    </div>
  );
}
