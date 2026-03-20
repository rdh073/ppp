import type { LucideIcon } from 'lucide-react';
import { Activity, Boxes, ListChecks, Smartphone } from 'lucide-react';

export type Tab = 'devices' | 'tasks' | 'workflows' | 'events';

interface TabNavProps {
  active: Tab;
  onChange: (value: Tab) => void;
}

interface TabDefinition {
  key: Tab;
  label: string;
  icon: LucideIcon;
  hint: string;
}

const TABS: TabDefinition[] = [
  { key: 'devices', label: 'Devices', icon: Smartphone, hint: 'fleet status' },
  { key: 'tasks', label: 'Tasks', icon: ListChecks, hint: 'manual control' },
  { key: 'workflows', label: 'Workflows', icon: Boxes, hint: 'automation plans' },
  { key: 'events', label: 'Events', icon: Activity, hint: 'observability' },
];

export function TabNav({ active, onChange }: TabNavProps) {
  return (
    <div className="tabs">
      {TABS.map((tab) => {
        const Icon = tab.icon;
        return (
          <button
            type="button"
            key={tab.key}
            className={active === tab.key ? 'tab tab-active' : 'tab'}
            onClick={() => onChange(tab.key)}
            aria-pressed={active === tab.key}
          >
            <span className="tab-token" aria-hidden>
              <Icon size={16} />
            </span>
            <span className="tab-copy">
              <span className="tab-label">{tab.label}</span>
              <span className="tab-hint">{tab.hint}</span>
            </span>
          </button>
        );
      })}
    </div>
  );
}
