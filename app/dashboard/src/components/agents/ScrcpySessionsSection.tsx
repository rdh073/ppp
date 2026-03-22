import { ScrcpyView } from './ScrcpyView';
import type { ScrcpySession } from '../../hooks/useScrcpySessions';

interface ScrcpySessionsSectionProps {
  sessions: ScrcpySession[];
  maxSessions: number;
  onCloseAll: () => void;
  onCloseSession: (sessionId: string) => void;
  makeTouchFanout: (deviceId: string) => ((type: 'down' | 'move' | 'up', x: number, y: number) => void) | undefined;
}

export function ScrcpySessionsSection({
  sessions,
  maxSessions,
  onCloseAll,
  onCloseSession,
  makeTouchFanout,
}: ScrcpySessionsSectionProps) {
  if (sessions.length === 0) return null;
  return (
    <section className="mt-4">
      <div className="panel-subhead">
        <h3
          className="m-0 text-sm font-semibold uppercase tracking-[0.08em]"
          style={{ color: 'var(--muted)' }}
        >
          Active scrcpy sessions ({sessions.length}/{maxSessions})
        </h3>
        <button type="button" className="btn-secondary" onClick={onCloseAll}>
          Close all
        </button>
      </div>
      <div className="scrcpy-grid">
        {sessions.map((session) => (
          <ScrcpyView
            key={session.id}
            sessionId={session.id}
            deviceId={session.deviceId}
            adbSerial={session.adbSerial}
            deviceName={session.deviceName}
            onClose={() => onCloseSession(session.id)}
            onTouchDevice={makeTouchFanout(session.deviceId)}
          />
        ))}
      </div>
    </section>
  );
}
