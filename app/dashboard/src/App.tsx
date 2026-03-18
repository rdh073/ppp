import { useState } from 'react';
import { Header } from './components/layout/Header';
import { TabNav, type Tab } from './components/layout/TabNav';
import { DevicePanel } from './components/agents/DevicePanel';
import { TaskPanel } from './components/tasks/TaskPanel';
import { WorkflowPanel } from './components/workflows/WorkflowPanel';
import { EventPanel } from './components/events/EventPanel';
import './index.css';

const API_URL = (import.meta.env.VITE_API_URL || 'http://localhost:3000').replace(/\/$/, '');
const METRICS_URL = API_URL ? `${API_URL}/metrics` : 'http://localhost:3000/metrics';

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

  return (
    <div className="dashboard">
      <div className="layout-shell">
        <Header activeTab={activeTab} metricsUrl={METRICS_URL} />
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
