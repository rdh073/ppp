import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import { ScrcpyView } from './ScrcpyView';
import { RecordMacroPanel } from './RecordMacroPanel';
import type { Device } from '../../../types';
import type { ScrcpySession, ScrcpySessionsHook } from '../hooks/useScrcpySessions';

interface Props {
  device: Device;
  session: ScrcpySession | null;
  scrcpy: ScrcpySessionsHook;
  makeTouchFanout: (deviceId: string) => ((type: 'down' | 'move' | 'up', x: number, y: number) => void) | undefined;
  onClose: () => void;
}

export function RecordMacroModal({ device, session, scrcpy, makeTouchFanout, onClose }: Props) {
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = prev; };
  }, []);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return createPortal(
    <div className="record-macro-modal-backdrop" role="presentation" onClick={() => onClose()}>
      <div
        className="record-macro-modal"
        role="dialog"
        aria-modal="true"
        aria-label="Record Macro"
        onClick={(e) => e.stopPropagation()}
      >
        {/* left: scrcpy mirror */}
        <div className="record-macro-modal-scrcpy">
          {session ? (
            <ScrcpyView
              sessionId={session.id}
              deviceId={session.deviceId}
              adbSerial={session.adbSerial}
              deviceName={session.deviceName}
              onClose={() => scrcpy.closeBySessionId(session.id)}
              onTouchDevice={makeTouchFanout(session.deviceId)}
            />
          ) : (
            <div className="record-macro-modal-connecting">
              <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
              </svg>
              Connecting scrcpy…
            </div>
          )}
        </div>

        {/* right: record controls */}
        <div className="record-macro-modal-controls">
          <RecordMacroPanel
            deviceId={device.deviceId}
            deviceLabel={getAndroidIdentity(device)}
            onClose={onClose}
          />
        </div>
      </div>
    </div>,
    document.body,
  );
}
