import { useEffect, useState } from 'react';
import { Header } from './components/layout/Header';
import { TabNav, type Tab } from './components/layout/TabNav';
import { DevicePanel } from './components/agents/DevicePanel';
import { TaskPanel } from './components/tasks/TaskPanel';
import { WorkflowPanel } from './components/workflows/WorkflowPanel';
import { EventPanel } from './components/events/EventPanel';
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
          </section>
        </main>
      </div>
    </div>
  );
}
