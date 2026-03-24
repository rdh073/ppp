import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import { ScrcpyView } from './ScrcpyView';
import { RecordMacroPanel } from './RecordMacroPanel';
import { recordEntry } from '../api/recording';
import { observeDevice } from '../api/inspect';
import type { UiSnapshot } from '../types/inspector';
import type { Device } from '../../../types';
import type { ScrcpySession, ScrcpySessionsHook } from '../hooks/useScrcpySessions';
import type { RunState } from '../hooks/useRecordingSession';

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

  const [recordingActive, setRecordingActive] = useState(false);
  const touchStartRef = useRef<{ x: number; y: number } | null>(null);
  const videoSizeRef = useRef({ width: 0, height: 0 });

  const handleRunStateChange = useCallback((state: RunState) => {
    setRecordingActive(state === 'recording');
  }, []);

  // Scale video-space (x, y) to device-space coordinates using the snapshot bounds
  // to infer the device's native resolution.
  const toDeviceCoords = useCallback(
    (x: number, y: number, snapshot: UiSnapshot): { x: number; y: number } => {
      const { width: vw, height: vh } = videoSizeRef.current;
      if (!vw || !vh) return { x, y };
      // Infer device dimensions from the max bounds across all snapshot targets.
      let dw = 0, dh = 0;
      for (const t of snapshot.targets) {
        if (t.bounds[2] > dw) dw = t.bounds[2];
        if (t.bounds[3] > dh) dh = t.bounds[3];
      }
      if (!dw || !dh) return { x, y };
      return { x: Math.round(x * dw / vw), y: Math.round(y * dh / vh) };
    },
    [],
  );

  // Resolve the best accessibility selector for a tap at device-space (x, y).
  // Picks the smallest-area actionable target that contains the point (most specific/innermost),
  // then applies selector priority: semantic_key > resource_id > text > coordinate fallback.
  const resolveSelector = useCallback(
    (x: number, y: number, snapshot: UiSnapshot): { kind: string; value: string } => {
      let best: UiTarget | null = null;
      let bestArea = Infinity;
      for (const t of snapshot.targets) {
        if (!t.actionable) continue;
        const [l, top, r, bot] = t.bounds;
        if (x < l || x > r || y < top || y > bot) continue;
        const area = (r - l) * (bot - top);
        if (area < bestArea) { bestArea = area; best = t; }
      }
      if (best) {
        if (best.semanticKey) return { kind: 'semantic_key', value: best.semanticKey };
        if (best.resourceId) return { kind: 'resource_id', value: best.resourceId };
        if (best.text) return { kind: 'text', value: best.text };
      }
      return { kind: 'coordinate', value: `${x},${y}` };
    },
    [],
  );

  const handleTouchDevice = useCallback(
    (type: 'down' | 'move' | 'up', x: number, y: number) => {
      // Always fanout to slave devices in the group
      if (session) {
        makeTouchFanout(session.deviceId)?.(type, x, y);
      }

      // When recording, append gesture to the active recording session.
      // Scrcpy already handled the actual device interaction; we only need to
      // observe the screen and store the entry (no re-execution on device).
      if (recordingActive) {
        if (type === 'down') {
          touchStartRef.current = { x, y };
        } else if (type === 'up') {
          const start = touchStartRef.current;
          touchStartRef.current = null;
          if (!start) return;

          const dy = y - start.y;
          const dist = Math.hypot(x - start.x, dy);

          if (dist < 30) {
            // Tap: observe post-tap snapshot, scale to device coords, resolve selector, store entry
            void observeDevice(device.deviceId)
              .then((snapshot) => {
                const { x: dx, y: dy } = toDeviceCoords(x, y, snapshot);
                const target = resolveSelector(dx, dy, snapshot);
                const executeResult = { snapshotBefore: snapshot, snapshotAfter: snapshot };
                return recordEntry(device.deviceId, {
                  actionParams: { action: { kind: 'click', target } },
                  executeResult,
                });
              })
              .catch(() => {
                // Fallback: can't scale without snapshot, skip recording this tap
              });
          } else if (Math.abs(dy) > 80 && Math.abs(dy) > Math.abs(x - start.x)) {
            // Swipe gesture: record with device-space start/end coordinates
            void observeDevice(device.deviceId)
              .then((snapshot) => {
                const { x: sx, y: sy } = toDeviceCoords(start.x, start.y, snapshot);
                const { x: ex, y: ey } = toDeviceCoords(x, y, snapshot);
                return recordEntry(device.deviceId, {
                  actionParams: { action: { kind: 'swipe', startX: sx, startY: sy, endX: ex, endY: ey } },
                  executeResult: {},
                });
              })
              .catch(() => {});
          }
        }
      }
    },
    [session, makeTouchFanout, recordingActive, device.deviceId, resolveSelector, toDeviceCoords],
  );

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
              onTouchDevice={handleTouchDevice}
              onVideoSizeChange={(w, h) => { videoSizeRef.current = { width: w, height: h }; }}
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
            onRunStateChange={handleRunStateChange}
          />
        </div>
      </div>
    </div>,
    document.body,
  );
}
