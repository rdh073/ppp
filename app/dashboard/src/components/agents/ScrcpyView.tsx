import { useEffect, useRef, useState } from 'react';
import { Adb, AdbDaemonTransport } from '@yume-chan/adb';
import AdbWebCredentialStore from '@yume-chan/adb-credential-web';
import { AdbDaemonWebUsbDeviceManager } from '@yume-chan/adb-daemon-webusb';
import { AdbScrcpyClient, AdbScrcpyOptions2_7 } from '@yume-chan/adb-scrcpy';
import { AndroidMotionEventAction, DefaultServerPath } from '@yume-chan/scrcpy';
import {
  BitmapVideoFrameRenderer,
  WebCodecsVideoDecoder,
  WebGLVideoFrameRenderer,
} from '@yume-chan/scrcpy-decoder-webcodecs';
import { AdbDaemonWebSocketConnection } from '../../utils/adbWebSocket';
import { getDevice } from '../../api/devices';

// scrcpy server v2.7 — served from /public. v3.x crashes on Waydroid (JVM abort at startup).
const SCRCPY_SERVER_V2_7 = '/scrcpy-server-v2.7';

type Phase = 'idle' | 'connecting' | 'pushing' | 'starting' | 'live' | 'error';
type Mode = 'usb' | 'network';

interface Props {
  onClose: () => void;
  sessionId: string;
  /** Device ID to use for the network (WebSocket) ADB proxy mode. */
  deviceId?: string;
  /** Known ADB serial (e.g. "192.168.1.10:5555"). Used as-is when provided. */
  adbSerial?: string;
  deviceName?: string;
}

export function ScrcpyView({ onClose, sessionId, deviceId, adbSerial: initialAdbSerial, deviceName }: Props) {
  const [phase, setPhase] = useState<Phase>('idle');
  const [statusMsg, setStatusMsg] = useState('');
  const [mode, setMode] = useState<Mode>(deviceId ? 'network' : 'usb');
  const [adbSerialInput, setAdbSerialInput] = useState(initialAdbSerial ?? '');
  const [serialFetching, setSerialFetching] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const clientRef = useRef<AdbScrcpyClient<AdbScrcpyOptions2_7<true>> | null>(null);
  const adbRef = useRef<Adb | null>(null);
  const videoSizeRef = useRef({ width: 0, height: 0 });
  const pointerActiveRef = useRef(false);

  useEffect(() => {
    return () => {
      void clientRef.current?.close();
      void adbRef.current?.close();
    };
  }, []);

  // Auto-fetch ADB serial from server when in network mode and serial not yet known.
  useEffect(() => {
    if (mode !== 'network' || !deviceId || adbSerialInput) return;
    setSerialFetching(true);
    getDevice(deviceId)
      .then((d) => { if (d.adbSerial) setAdbSerialInput(d.adbSerial); })
      .catch(() => {})
      .finally(() => setSerialFetching(false));
  }, [mode, deviceId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function getAdb(): Promise<Adb> {
    const credentialStore = new AdbWebCredentialStore('PPP Dashboard');

    if (mode === 'network') {
      if (!deviceId) throw new Error('No device ID provided for network mode');
      const serial = adbSerialInput.trim() || undefined;
      setStatusMsg(`Connecting to device ${serial ?? deviceId} via server ADB proxy...`);
      const connection = await AdbDaemonWebSocketConnection.connect(deviceId, serial);
      const transport = await AdbDaemonTransport.authenticate({
        serial: deviceId,
        connection,
        credentialStore,
      });
      return new Adb(transport);
    }

    // USB mode
    const manager = AdbDaemonWebUsbDeviceManager.BROWSER;
    if (!manager) throw new Error('WebUSB is not supported in this browser');
    setStatusMsg('Select your Android device...');
    const device = await manager.requestDevice();
    if (!device) throw new Error('No device selected');
    setStatusMsg(`Authenticating with ${device.name}...`);
    const connection = await device.connect();
    const transport = await AdbDaemonTransport.authenticate({
      serial: device.serial,
      connection,
      credentialStore,
    });
    return new Adb(transport);
  }

  async function connect() {
    setPhase('connecting');
    setStatusMsg('');
    try {
      const adb = await getAdb();
      adbRef.current = adb;

      setPhase('pushing');
      setStatusMsg('Pushing scrcpy server to device...');
      const serverResponse = await fetch(SCRCPY_SERVER_V2_7);
      if (!serverResponse.ok || !serverResponse.body) {
        throw new Error(`Failed to fetch scrcpy server binary (${serverResponse.status})`);
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      await AdbScrcpyClient.pushServer(adb, serverResponse.body as any);

      setPhase('starting');
      setStatusMsg('Starting scrcpy...');

      const options = new AdbScrcpyOptions2_7({
        video: true,
        audio: false,
        control: true,
        maxSize: 1080,
        videoBitRate: 4_000_000,
        maxFps: 30,
        // Use forward tunnel: server listens, browser connects via adb.createSocket().
        // Reverse tunnel (default) requires device to initiate connections back to the
        // host — this doesn't work through the WebSocket ADB proxy.
        tunnelForward: true,
      });
      const client = await AdbScrcpyClient.start(adb, DefaultServerPath, options);
      clientRef.current = client as AdbScrcpyClient<AdbScrcpyOptions2_7<true>>;

      const videoStream = await Promise.race([
        client.videoStream,
        new Promise<never>((_, reject) =>
          setTimeout(() => reject(new Error('Timed out waiting for video stream (15 s).')), 15_000),
        ),
      ]);
      if (!videoStream) throw new Error('No video stream from scrcpy');

      videoSizeRef.current = {
        width: videoStream.width || videoStream.metadata.width || 1080,
        height: videoStream.height || videoStream.metadata.height || 1920,
      };

      videoStream.sizeChanged((size) => {
        videoSizeRef.current = size;
      });

      const renderer = WebGLVideoFrameRenderer.isSupported
        ? new WebGLVideoFrameRenderer()
        : new BitmapVideoFrameRenderer();

      const canvas = renderer.canvas as HTMLCanvasElement;
      canvas.style.cssText = 'max-width:100%;max-height:70vh;display:block;touch-action:none;';
      containerRef.current?.appendChild(canvas);

      const decoder = new WebCodecsVideoDecoder({
        codec: videoStream.metadata.codec,
        renderer,
      });

      void videoStream.stream.pipeTo(decoder.writable).catch(() => {});

      setPhase('live');
      setStatusMsg('');
    } catch (err) {
      setPhase('error');
      if (err && typeof err === 'object' && 'output' in err && err.output) {
        try {
          const lines: string[] = [];
          for await (const line of err.output as AsyncIterable<string>) {
            lines.push(line);
          }
          setStatusMsg(
            (err instanceof Error ? err.message : String(err)) +
              (lines.length ? '\n\nServer output:\n' + lines.join('\n') : ''),
          );
        } catch {
          setStatusMsg(err instanceof Error ? err.message : String(err));
        }
      } else {
        setStatusMsg(err instanceof Error ? err.message : String(err));
      }
    }
  }

  async function disconnect() {
    await clientRef.current?.close();
    clientRef.current = null;
    await adbRef.current?.close();
    adbRef.current = null;
    onClose();
  }

  function getVideoCoords(e: React.PointerEvent<HTMLDivElement>) {
    const canvas = containerRef.current?.querySelector('canvas');
    if (!canvas) return null;
    const rect = canvas.getBoundingClientRect();
    const { width, height } = videoSizeRef.current;
    if (!width || !height) return null;
    return {
      x: Math.round(((e.clientX - rect.left) / rect.width) * width),
      y: Math.round(((e.clientY - rect.top) / rect.height) * height),
    };
  }

  function handlePointerDown(e: React.PointerEvent<HTMLDivElement>) {
    e.currentTarget.setPointerCapture(e.pointerId);
    const coords = getVideoCoords(e);
    if (!coords) return;
    pointerActiveRef.current = true;
    void clientRef.current?.controller
      ?.injectTouch({
        action: AndroidMotionEventAction.Down,
        pointerId: BigInt(e.pointerId),
        pointerX: coords.x,
        pointerY: coords.y,
        videoWidth: videoSizeRef.current.width,
        videoHeight: videoSizeRef.current.height,
        pressure: 1,
        actionButton: 0,
        buttons: 0,
      })
      .catch(() => {});
  }

  function handlePointerMove(e: React.PointerEvent<HTMLDivElement>) {
    if (!pointerActiveRef.current) return;
    const coords = getVideoCoords(e);
    if (!coords) return;
    void clientRef.current?.controller
      ?.injectTouch({
        action: AndroidMotionEventAction.Move,
        pointerId: BigInt(e.pointerId),
        pointerX: coords.x,
        pointerY: coords.y,
        videoWidth: videoSizeRef.current.width,
        videoHeight: videoSizeRef.current.height,
        pressure: 1,
        actionButton: 0,
        buttons: 0,
      })
      .catch(() => {});
  }

  function handlePointerUp(e: React.PointerEvent<HTMLDivElement>) {
    pointerActiveRef.current = false;
    const coords = getVideoCoords(e);
    if (!coords) return;
    void clientRef.current?.controller
      ?.injectTouch({
        action: AndroidMotionEventAction.Up,
        pointerId: BigInt(e.pointerId),
        pointerX: coords.x,
        pointerY: coords.y,
        videoWidth: videoSizeRef.current.width,
        videoHeight: videoSizeRef.current.height,
        pressure: 0,
        actionButton: 0,
        buttons: 0,
      })
      .catch(() => {});
  }

  const modeLabel = mode === 'usb' ? 'Scrcpy (USB)' : 'Scrcpy (Network)';

  return (
    <div className="scrcpy-shell">
      <div className="panel-subhead">
        <div className="scrcpy-subhead-main flex items-center gap-2">
          <span>{modeLabel}</span>
          {deviceName && <span className="scrcpy-session-badge">{deviceName}</span>}
        </div>
        <button type="button" className="btn-secondary" onClick={() => void disconnect()}>
          Disconnect
        </button>
      </div>

      {statusMsg && <p className={phase === 'error' ? 'error' : 'status'}>{statusMsg}</p>}

      {phase === 'idle' && (
        <div className="scrcpy-connect">
          <div className="mode-select">
            <label>
              <input
                type="radio"
                name={`scrcpy-mode-${sessionId}`}
                value="usb"
                checked={mode === 'usb'}
                onChange={() => setMode('usb')}
              />
              {' USB (WebUSB)'}
            </label>
            <label>
              <input
                type="radio"
                name={`scrcpy-mode-${sessionId}`}
                value="network"
                checked={mode === 'network'}
                onChange={() => setMode('network')}
                disabled={!deviceId}
              />
              {' Network (via server)'}
              {!deviceId && ' — select a device first'}
            </label>
          </div>
          {mode === 'usb' && (
            <p className="field-hint">
              Connect your Android device via USB with USB debugging enabled.
            </p>
          )}
          {mode === 'network' && (
            <>
              <p className="field-hint">
                Uses the server-agent as an ADB WebSocket proxy. Requires the device to be
                connected via ADB (USB or WiFi ADB).
              </p>
              <label className="field-label">
                ADB serial
                {serialFetching && (
                  <span className="field-hint" style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                    <svg className="animate-spin" style={{ width: '0.75rem', height: '0.75rem' }} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0" strokeLinecap="round" />
                    </svg>
                    Fetching from server…
                  </span>
                )}
                <input
                  type="text"
                  placeholder="e.g. 192.168.1.10:5555 or emulator-5554"
                  value={adbSerialInput}
                  onChange={(e) => setAdbSerialInput(e.target.value)}
                  className="field-input"
                />
                <span className="field-hint">
                  Auto-detected from server. Leave empty to use binding store fallback.
                </span>
              </label>
            </>
          )}
          <button type="button" onClick={() => void connect()}>
            Connect
          </button>
        </div>
      )}

      <div
        ref={containerRef}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerUp}
      />
    </div>
  );
}
