import { requestJson } from '../../../shared/http/client';
import type { Device } from '../../../types';

export function listDevices(): Promise<Device[]> {
  return requestJson('/devices', {
    method: 'GET',
  });
}

export function getDevice(deviceId: string): Promise<Device> {
  return requestJson(`/devices/${encodeURIComponent(deviceId)}`, {
    method: 'GET',
  });
}

export function executeDeviceAction(
  deviceId: string,
  action: { kind: string; target?: { kind: string; value: string }; [key: string]: unknown },
): Promise<unknown> {
  return requestJson(`/devices/${encodeURIComponent(deviceId)}/execute`, {
    method: 'POST',
    body: { action },
    timeoutMs: 15_000,
  });
}
