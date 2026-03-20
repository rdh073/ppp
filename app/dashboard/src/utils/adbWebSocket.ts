import { AdbPacketHeader } from '@yume-chan/adb';
import type { AdbDaemonConnection, AdbPacketData, AdbPacketInit } from '@yume-chan/adb';
import { Consumable, PushReadableStream } from '@yume-chan/stream-extra';
import { Uint8ArrayExactReadable } from '@yume-chan/struct';
import { API_URL } from '../config';

function apiUrlToWs(url: string): string {
  return url.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
}

export function adbWsUrl(deviceId: string, serial?: string): string {
  const base = `${apiUrlToWs(API_URL)}/devices/${encodeURIComponent(deviceId)}/adb-ws`;
  return serial ? `${base}?serial=${encodeURIComponent(serial)}` : base;
}

/**
 * ADB daemon connection backed by a WebSocket to the server-agent ADB proxy.
 *
 * Framing: each binary WebSocket message is one complete ADB packet
 * (24-byte little-endian header + payload).
 *
 * Follows the same implementation pattern as @yume-chan/adb-daemon-webusb:
 * - readable: PushReadableStream parsing AdbPacketHeader + payload per WS message
 * - writable: pipeFrom(MaybeConsumable.WritableStream, AdbPacketSerializeStream)
 */
export class AdbDaemonWebSocketConnection implements AdbDaemonConnection {
  readonly readable: AdbDaemonConnection['readable'];
  readonly writable: AdbDaemonConnection['writable'];

  private constructor(ws: WebSocket) {
    ws.binaryType = 'arraybuffer';

    // Readable: WebSocket binary messages → AdbPacketData
    this.readable = new PushReadableStream<AdbPacketData>((controller) => {
      ws.onmessage = async (event: MessageEvent) => {
        const buffer = event.data as ArrayBuffer;
        if (buffer.byteLength < 24) return;

        const headerBytes = new Uint8Array(buffer, 0, 24);
        const header = AdbPacketHeader.deserialize(new Uint8ArrayExactReadable(headerBytes));

        const payload =
          header.payloadLength > 0
            ? new Uint8Array(buffer.slice(24, 24 + header.payloadLength))
            : new Uint8Array(0);

        const packet: AdbPacketData = {
          command: header.command,
          arg0: header.arg0,
          arg1: header.arg1,
          payload,
        };
        await controller.enqueue(packet);
      };

      ws.onerror = () => controller.error(new Error('ADB WebSocket error'));
      ws.onclose = () => controller.close();
    });

    // Writable: Consumable<AdbPacketInit> → serialize header+payload as ONE ws.send
    // AdbPacketSerializeStream emits header and payload as separate chunks (USB design),
    // so we serialize manually to keep them in a single WebSocket message.
    this.writable = new Consumable.WritableStream<AdbPacketInit>({
      write(init: AdbPacketInit) {
        const payload = init.payload instanceof Uint8Array ? init.payload : new Uint8Array(0);
        const buf = new ArrayBuffer(24 + payload.byteLength);
        const view = new DataView(buf);
        view.setUint32(0, init.command, true);
        view.setUint32(4, init.arg0, true);
        view.setUint32(8, init.arg1, true);
        view.setUint32(12, payload.byteLength, true);
        view.setUint32(16, init.checksum, true);
        view.setUint32(20, init.magic, true);
        new Uint8Array(buf, 24).set(payload);
        ws.send(buf);
      },
      close() {
        ws.close();
      },
    });
  }

  static connect(deviceId: string, serial?: string): Promise<AdbDaemonWebSocketConnection> {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(adbWsUrl(deviceId, serial));
      ws.binaryType = 'arraybuffer';
      ws.onopen = () => resolve(new AdbDaemonWebSocketConnection(ws));
      ws.onerror = () => reject(new Error('Failed to connect to ADB WebSocket proxy'));
    });
  }
}
