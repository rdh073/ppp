import { useState } from 'react';
import { getAndroidIdentity } from '../../../utils/deviceIdentity';
import type { Device } from '../../../types';

export interface ScrcpySession {
  id: string;
  deviceId: string;
  adbSerial?: string;
  deviceName: string;
}

const DEFAULT_MAX = 6;
const MIN_MAX = 1;
const MAX_MAX = 12;
const STORAGE_KEY = 'ppp.dashboard.maxScrcpySessions';

function clamp(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_MAX;
  return Math.max(MIN_MAX, Math.min(MAX_MAX, value));
}

function cap(sessions: ScrcpySession[], max: number): ScrcpySession[] {
  if (sessions.length <= max) return sessions;
  return sessions.slice(sessions.length - max);
}

function readStoredLimit(): number {
  if (typeof window === 'undefined') return DEFAULT_MAX;
  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (!raw) return DEFAULT_MAX;
  const parsed = Number.parseInt(raw, 10);
  return Number.isNaN(parsed) ? DEFAULT_MAX : clamp(parsed);
}

export interface ScrcpySessionsHook {
  sessions: ScrcpySession[];
  maxSessions: number;
  minMaxSessions: number;
  maxMaxSessions: number;
  openSession: (device: Device) => void;
  closeBySessionId: (id: string) => void;
  closeByDeviceId: (deviceId: string) => void;
  openMany: (devices: Device[]) => void;
  closeAll: () => void;
  setMaxSessions: (value: number) => void;
  isActive: (deviceId: string) => boolean;
}

export function useScrcpySessions(): ScrcpySessionsHook {
  const [sessions, setSessions] = useState<ScrcpySession[]>([]);
  const [maxSessions, setMaxSessionsState] = useState<number>(readStoredLimit);

  function setMaxSessions(value: number) {
    const next = clamp(value);
    setMaxSessionsState(next);
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, String(next));
    }
    setSessions((prev) => cap(prev, next));
  }

  function openSession(device: Device) {
    setSessions((prev) => {
      if (prev.some((s) => s.deviceId === device.deviceId)) return prev;
      return cap([
        ...prev,
        {
          id: `${device.deviceId}-${Date.now()}`,
          deviceId: device.deviceId,
          adbSerial: device.adbSerial ?? undefined,
          deviceName: getAndroidIdentity(device),
        },
      ], maxSessions);
    });
  }

  function closeBySessionId(id: string) {
    setSessions((prev) => prev.filter((s) => s.id !== id));
  }

  function closeByDeviceId(deviceId: string) {
    setSessions((prev) => prev.filter((s) => s.deviceId !== deviceId));
  }

  function openMany(devices: Device[]) {
    const now = Date.now();
    setSessions((prev) => {
      const existing = new Set(prev.map((s) => s.deviceId));
      const toAdd = devices.filter((d) => !existing.has(d.deviceId));
      if (toAdd.length === 0) return prev;
      const newSessions = toAdd.map((d, i) => ({
        id: `${d.deviceId}-${now + i}`,
        deviceId: d.deviceId,
        adbSerial: d.adbSerial ?? undefined,
        deviceName: getAndroidIdentity(d),
      }));
      return cap([...prev, ...newSessions], maxSessions);
    });
  }

  function closeAll() {
    setSessions([]);
  }

  function isActive(deviceId: string): boolean {
    return sessions.some((s) => s.deviceId === deviceId);
  }

  return {
    sessions,
    maxSessions,
    minMaxSessions: MIN_MAX,
    maxMaxSessions: MAX_MAX,
    openSession,
    closeBySessionId,
    closeByDeviceId,
    openMany,
    closeAll,
    setMaxSessions,
    isActive,
  };
}
