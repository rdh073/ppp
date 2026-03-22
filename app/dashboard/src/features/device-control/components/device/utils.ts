import { getAndroidIdentity } from '../../../../utils/deviceIdentity';
import type { Device } from '../../../../types';

export function relativeTime(value: string): string {
  const diff = Date.now() - new Date(value).getTime();
  if (Number.isNaN(diff)) return value;
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export function isOnline(device: Device): boolean {
  return !!device.sessionId;
}

export function heartbeatAge(device: Device): number {
  return Date.now() - new Date(device.lastHeartbeatAt).getTime();
}

export function deviceStatus(device: Device): 'online' | 'stale' | 'offline' {
  if (!isOnline(device)) return 'offline';
  return heartbeatAge(device) < 90_000 ? 'online' : 'stale';
}

export function filterDevices(devices: Device[], query: string): Device[] {
  const q = query.toLowerCase().trim();
  if (!q) return devices;
  return devices.filter((d) => {
    const identity = getAndroidIdentity(d).toLowerCase();
    return (
      identity.includes(q) ||
      d.deviceId.toLowerCase().includes(q) ||
      (d.adbSerial ?? '').toLowerCase().includes(q) ||
      (d.deviceMetadata?.androidVersion ?? '').toLowerCase().includes(q) ||
      d.capabilities.some((c) => c.name.toLowerCase().includes(q))
    );
  });
}
