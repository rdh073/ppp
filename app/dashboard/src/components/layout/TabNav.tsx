export type Tab = 'devices' | 'tasks' | 'workflows' | 'events';

interface TabNavProps {
  active: Tab;
  onChange: (value: Tab) => void;
}

interface TabDefinition {
  key: Tab;
  label: string;
  short: string;
  hint: string;
}

const TABS: TabDefinition[] = [
  { key: 'devices', label: 'Devices', short: 'DV', hint: 'fleet status' },
  { key: 'tasks', label: 'Tasks', short: 'TK', hint: 'manual control' },
  { key: 'workflows', label: 'Workflows', short: 'WF', hint: 'automation plans' },
  { key: 'events', label: 'Events', short: 'EV', hint: 'observability' },
];

export function TabNav({ active, onChange }: TabNavProps) {
  return (
    <div className="tabs">
      {TABS.map((tab) => (
        <button
          type="button"
          key={tab.key}
          className={active === tab.key ? 'tab tab-active' : 'tab'}
          onClick={() => onChange(tab.key)}
          aria-pressed={active === tab.key}
        >
          <span className="tab-token" aria-hidden>
            {tab.short}
          </span>
          <span className="tab-copy">
            <span className="tab-label">{tab.label}</span>
            <span className="tab-hint">{tab.hint}</span>
          </span>
        </button>
      ))}
    </div>
  );
}
