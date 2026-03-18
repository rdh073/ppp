import { requestJson } from './client';
import type { Device } from '../types';

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
