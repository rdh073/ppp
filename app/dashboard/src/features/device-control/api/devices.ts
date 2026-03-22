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

export function executeScript(
  deviceId: string,
  source: string,
  params?: Record<string, string>,
  timeout?: number,
): Promise<{ output: Record<string, unknown>; logs: unknown[]; durationMs: number }> {
  const t = timeout ?? 30_000;
  return requestJson(`/devices/${encodeURIComponent(deviceId)}/script`, {
    method: 'POST',
    body: { source, params: params ?? {}, timeout: t },
    timeoutMs: t + 15_000,
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
