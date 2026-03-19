import { useEffect, useState } from 'react';
import { Header } from './components/layout/Header';
import { TabNav, type Tab } from './components/layout/TabNav';
import { DevicePanel } from './components/agents/DevicePanel';
import { TaskPanel } from './components/tasks/TaskPanel';
import { WorkflowPanel } from './components/workflows/WorkflowPanel';
import { EventPanel } from './components/events/EventPanel';
import { API_URL, METRICS_URL, POLL_MS } from './config';
import './index.css';

function renderPanel(active: Tab) {
  switch (active) {
    case 'devices':
      return <DevicePanel />;
    case 'tasks':
      return <TaskPanel />;
    case 'workflows':
      return <WorkflowPanel />;
    case 'events':
      return <EventPanel />;
    default:
      return <DevicePanel />;
  }
}

export function App() {
  const [activeTab, setActiveTab] = useState<Tab>('devices');
  const [healthStatus, setHealthStatus] = useState<'checking' | 'ok' | 'down'>('checking');

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
    const timer = window.setInterval(probeHealth, Math.max(2000, POLL_MS));
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
        />
        <main className="content">
          <section className="tab-strip">
            <TabNav active={activeTab} onChange={setActiveTab} />
          </section>
          <section className="panel-frame">{renderPanel(activeTab)}</section>
        </main>
      </div>
    </div>
  );
}
